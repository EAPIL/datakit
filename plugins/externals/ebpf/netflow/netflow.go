//go:build (linux && amd64 && ebpf) || (linux && arm64 && ebpf)
// +build linux,amd64,ebpf linux,arm64,ebpf

package netflow

import (
	"time"
	"unsafe"

	"github.com/DataDog/ebpf"
	"github.com/DataDog/ebpf/manager"
	client "github.com/influxdata/influxdb1-client/v2"
	"github.com/shirou/gopsutil/host"
	"gitlab.jiagouyun.com/cloudcare-tools/datakit/io/point"
	dkout "gitlab.jiagouyun.com/cloudcare-tools/datakit/plugins/externals/ebpf/output"
	"gitlab.jiagouyun.com/cloudcare-tools/datakit/plugins/externals/ebpf/sysmonitor"
	"golang.org/x/net/context"
)

type ConnResult struct {
	result map[ConnectionInfo]ConnFullStats
	tags   map[string]string
	ts     time.Time
}

const connExpirationInterval = 6 * 3600 // 6 * 3600s

const (
	srcNameM     = "netflow"
	transportTCP = "tcp"
	transportUDP = "udp"
)

type NetFlowTracer struct {
	connStatsRecord *ConnStatsRecord
	resultCh        chan *ConnResult
	closedEventCh   chan *ConncetionClosedInfo
}

func NewNetFlowTracer() *NetFlowTracer {
	return &NetFlowTracer{
		connStatsRecord: newConnStatsRecord(),
		resultCh:        make(chan *ConnResult, 4),
		closedEventCh:   make(chan *ConncetionClosedInfo, 64),
	}
}

func (tracer *NetFlowTracer) Run(ctx context.Context, bpfManger *manager.Manager,
	datakitPostURL string, gTags map[string]string, interval time.Duration,
) error {
	connStatsMap, found, err := bpfManger.GetMap("bpfmap_conn_stats")
	if err != nil || !found {
		return err
	}

	tcpStatsMap, found, err := bpfManger.GetMap("bpfmap_conn_tcp_stats")
	if err != nil || !found {
		return err
	}

	// go tracer.feedHandler(ctx, datakitPostURL)
	go tracer.connCollectHanllder(ctx, connStatsMap, tcpStatsMap,
		interval, gTags, datakitPostURL)
	return nil
}

func (tracer *NetFlowTracer) ClosedEventHandler(cpu int, data []byte,
	perfmap *manager.PerfMap, manager *manager.Manager,
) {
	eventC := (*ConncetionClosedInfoC)(unsafe.Pointer(&data[0])) //nolint:gosec
	event := &ConncetionClosedInfo{
		Info: ConnectionInfo{
			Saddr: (*(*[4]uint32)(unsafe.Pointer(&eventC.conn_info.saddr))), //nolint:gosec
			Daddr: (*(*[4]uint32)(unsafe.Pointer(&eventC.conn_info.daddr))), //nolint:gosec
			Sport: uint32(eventC.conn_info.sport),
			Dport: uint32(eventC.conn_info.dport),
			Pid:   uint32(eventC.conn_info.pid),
			Netns: uint32(eventC.conn_info.netns),
			Meta:  uint32(eventC.conn_info.meta),
		},
		Stats: ConnectionStats{
			SentBytes: uint64(eventC.conn_stats.sent_bytes),
			RecvBytes: uint64(eventC.conn_stats.recv_bytes),
			Flags:     uint32(eventC.conn_stats.flags),
			Direction: uint8(eventC.conn_stats.direction),
			Timestamp: uint64(eventC.conn_stats.timestamp),
		},
		TCPStats: ConnectionTCPStats{
			StateTransitions: uint16(eventC.conn_tcp_stats.state_transitions),
			Retransmits:      int32(eventC.conn_tcp_stats.retransmits),
			Rtt:              uint32(eventC.conn_tcp_stats.rtt),
			RttVar:           uint32(eventC.conn_tcp_stats.rtt_var),
		},
	}
	SrcIPPortRecorder.InsertAndUpdate(event.Info.Saddr)
	if IPPortFilterIn(&event.Info) {
		tracer.closedEventCh <- event
	}
}

func (tracer *NetFlowTracer) bpfMapCleanup(cl []ConnectionInfo, connStatsMap *ebpf.Map) {
	for _, v := range cl {
		c := ConnectionInfoC{
			saddr: (*(*[4]_Ctype_uint)(unsafe.Pointer(&v.Saddr))), //nolint:gosec
			daddr: (*(*[4]_Ctype_uint)(unsafe.Pointer(&v.Daddr))), //nolint:gosec
			sport: _Ctype_ushort(v.Sport),
			dport: _Ctype_ushort(v.Dport),
			pid:   _Ctype_uint(v.Pid),
			netns: _Ctype_uint(v.Netns),
			meta:  _Ctype_uint(v.Meta),
		}
		err := connStatsMap.Delete(unsafe.Pointer(&c)) //nolint:gosec
		if err != nil {
			l.Warn(err)
		}
	}
}

// Lock resource connStatsRecord while scanning connStatMap.
func (tracer *NetFlowTracer) connCollectHanllder(ctx context.Context, connStatsMap *ebpf.Map, tcpStatsMap *ebpf.Map,
	interval time.Duration, gTags map[string]string, datakitPostURL string,
) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	agg := FlowAgg{}

	for {
		select {
		case event := <-tracer.closedEventCh:
			tracer.connStatsRecord.updateClosedUseEvent(event)
		case <-ticker.C:
			var connInfoC ConnectionInfoC

			var connStatsC ConnectionStatsC

			var tcpStatsC ConnectionTCPStatsC

			iter := connStatsMap.IterateFrom(connInfoC)

			connsNeedCleanup := []ConnectionInfo{}
			uptime, err := host.Uptime()
			if err != nil {
				l.Error(err)
			}

			// Collect unclosed connection information and merge it with recorded closed connections
			// and unclosed connections in the previous collection cycle.
			for iter.Next(unsafe.Pointer(&connInfoC), unsafe.Pointer(&connStatsC)) { //nolint:gosec
				connInfo := ConnectionInfo{
					Saddr: (*(*[4]uint32)(unsafe.Pointer(&connInfoC.saddr))), //nolint:gosec
					Daddr: (*(*[4]uint32)(unsafe.Pointer(&connInfoC.daddr))), //nolint:gosec
					Sport: uint32(connInfoC.sport),
					Dport: uint32(connInfoC.dport),
					Pid:   uint32(connInfoC.pid),
					Netns: uint32(connInfoC.netns),
					Meta:  uint32(connInfoC.meta),
				}

				SrcIPPortRecorder.InsertAndUpdate(connInfo.Saddr)

				if !IPPortFilterIn(&connInfo) {
					continue
				}

				connStats := ConnectionStats{
					SentBytes:   uint64(connStatsC.sent_bytes),
					RecvBytes:   uint64(connStatsC.recv_bytes),
					SentPackets: uint64(connStatsC.sent_packets),
					RecvPackets: uint64(connStatsC.recv_packets),
					Flags:       uint32(connStatsC.flags),
					Direction:   uint8(connStatsC.direction),
					Timestamp:   uint64(connStatsC.timestamp),
				}
				connFullStats := ConnFullStats{
					Stats:            connStats,
					TotalClosed:      0,
					TotalEstablished: 0,
				}
				if ConnProtocolIsTCP(connInfo.Meta) {
					pid := connInfoC.pid
					connInfoC.pid = _Ctype_uint(0)
					if err := tcpStatsMap.Lookup(
						unsafe.Pointer(&connInfoC),               //nolint:gosec
						unsafe.Pointer(&tcpStatsC)); err == nil { //nolint:gosec
						connFullStats.TCPStats = ConnectionTCPStats{
							StateTransitions: uint16(tcpStatsC.state_transitions),
							Retransmits:      int32(tcpStatsC.retransmits),
							Rtt:              uint32(tcpStatsC.rtt),
							RttVar:           uint32(tcpStatsC.rtt_var),
						}
					}
					connInfoC.pid = pid
				}
				connFullStats = tracer.connStatsRecord.mergeWithClosedLastActive(connInfo, connFullStats)
				if int(uptime)-int(connFullStats.Stats.Timestamp/1000000000) > connExpirationInterval {
					if connFullStats.TotalClosed == 0 && connFullStats.TotalEstablished == 0 &&
						connFullStats.Stats.RecvBytes == 0 && connFullStats.Stats.SentBytes == 0 {
						connsNeedCleanup = append(connsNeedCleanup, connInfo)
						continue
					}
				}
				err := agg.Append(connInfo, connFullStats)
				if err != nil {
					l.Debug(err)
				}
			}
			if len(connsNeedCleanup) > 0 {
				for _, conn := range connsNeedCleanup {
					tracer.connStatsRecord.deleteLastActive(conn)
				}
				tracer.bpfMapCleanup(connsNeedCleanup, connStatsMap)
			}
			// Collect connections that are closed for the current cycle.
			for k, v := range tracer.connStatsRecord.closedConns {
				err := agg.Append(k, v)
				if err != nil {
					l.Debug(err)
				}
			}
			tracer.connStatsRecord.clearClosedConnsCache()

			pidMap, _ := sysmonitor.AllProcess()
			pts := agg.ToPoint(gTags, k8sNetInfo, pidMap)
			agg.Clean()
			tracer.feedHandler(datakitPostURL, pts)
		case <-ctx.Done():
			return
		}
	}
}

// Receive all connections collected in one cycle and send them to DataKit.
func (tracer *NetFlowTracer) feedHandler(datakitPostURL string, pts []*client.Point) {
	if err := dkout.FeedMeasurement(datakitPostURL, point.WrapPoint(pts)); err != nil {
		l.Debug(err)
	}
}
