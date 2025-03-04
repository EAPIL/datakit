// Unless explicitly stated otherwise all files in this repository are licensed
// under the MIT License.
// This product includes software developed at Guance Cloud (https://www.guance.com/).
// Copyright 2021-present Guance, Inc.

package redis

import (
	"bufio"
	"strings"
	"time"

	"gitlab.jiagouyun.com/cloudcare-tools/datakit/io/point"
	"gitlab.jiagouyun.com/cloudcare-tools/datakit/plugins/inputs"
)

type clientMeasurement struct {
	name     string
	tags     map[string]string
	fields   map[string]interface{}
	ts       time.Time
	resData  map[string]interface{}
	election bool
}

func (m *clientMeasurement) LineProto() (*point.Point, error) {
	return point.NewPoint(m.name, m.tags, m.fields, point.MOptElectionV2(m.election))
}

func (m *clientMeasurement) Info() *inputs.MeasurementInfo {
	return &inputs.MeasurementInfo{
		Name: "redis_client",
		Type: "metric",
		Fields: map[string]interface{}{
			"id": &inputs.FieldInfo{
				DataType: inputs.String,
				Type:     inputs.Gauge,
				Unit:     inputs.UnknownUnit,
				Desc:     "AN unique 64-bit client ID",
			},
			"addr": &inputs.FieldInfo{
				DataType: inputs.String,
				Type:     inputs.Gauge,
				Unit:     inputs.UnknownUnit,
				Desc:     "Address/port of the client",
			},
			"fd": &inputs.FieldInfo{
				DataType: inputs.Int,
				Type:     inputs.Gauge,
				Unit:     inputs.NCount,
				Desc:     "File descriptor corresponding to the socket",
			},
			"age": &inputs.FieldInfo{
				DataType: inputs.Int,
				Type:     inputs.Gauge,
				Unit:     inputs.NCount,
				Desc:     "Total duration of the connection in seconds",
			},
			"idle": &inputs.FieldInfo{
				DataType: inputs.Int,
				Type:     inputs.Gauge,
				Unit:     inputs.NCount,
				Desc:     "Idle time of the connection in seconds",
			},
			"sub": &inputs.FieldInfo{
				DataType: inputs.Int,
				Type:     inputs.Gauge,
				Unit:     inputs.NCount,
				Desc:     "Number of channel subscriptions",
			},
			"psub": &inputs.FieldInfo{
				DataType: inputs.Int,
				Type:     inputs.Gauge,
				Unit:     inputs.NCount,
				Desc:     "Number of pattern matching subscriptions",
			},
		},
		Tags: map[string]interface{}{
			"server": &inputs.TagInfo{
				Desc: "Server addr",
			},
			"name": &inputs.TagInfo{
				Desc: "The name set by the client with CLIENT SETNAME, default unknown",
			},
		},
	}
}

// 解析返回结果.
func (i *Input) parseClientData(list string) ([]inputs.Measurement, error) {
	var collectCache []inputs.Measurement
	rdr := strings.NewReader(list)

	scanner := bufio.NewScanner(rdr)
	for scanner.Scan() {
		line := scanner.Text()

		if len(line) == 0 || line[0] == '#' {
			continue
		}

		parts := strings.SplitN(line, " ", 18)
		if len(parts) < 18 {
			continue
		}

		m := &clientMeasurement{
			name:     "redis_client",
			tags:     make(map[string]string),
			fields:   make(map[string]interface{}),
			resData:  make(map[string]interface{}),
			election: i.Election,
		}
		setHostTagIfNotLoopback(m.tags, i.Host)
		for key, value := range i.Tags {
			m.tags[key] = value
		}

		for _, part := range parts {
			item := strings.Split(part, "=")

			key := item[0]
			val := strings.TrimSpace(item[1])

			if key == "name" {
				if val == "" {
					val = "unknown"
				}
				m.tags["name"] = val
			} else {
				m.resData[key] = val
			}
		}
		m.ts = time.Now()

		if err := m.submit(); err != nil {
			return nil, err
		}

		if len(m.fields) > 0 {
			collectCache = append(collectCache, m)
		}
	}

	return collectCache, nil
}

// 提交数据.
func (m *clientMeasurement) submit() error {
	metricInfo := m.Info()
	for key, item := range metricInfo.Fields {
		if value, ok := m.resData[key]; ok {
			val, err := Conv(value, item.(*inputs.FieldInfo).DataType)
			if err != nil {
				l.Errorf("infoMeasurement metric %v value %v parse error %v", key, value, err)
				return err
			} else {
				m.fields[key] = val
			}
		}
	}

	return nil
}
