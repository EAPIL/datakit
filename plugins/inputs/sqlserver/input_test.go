// Unless explicitly stated otherwise all files in this repository are licensed
// under the MIT License.
// This product includes software developed at Guance Cloud (https://www.guance.com/).
// Copyright 2021-present Guance, Inc.

package sqlserver

import (
	"fmt"
	"net"
	"net/netip"
	"os"
	"sync"
	T "testing"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/GuanceCloud/cliutils/point"
	dt "github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"
	"github.com/stretchr/testify/assert"
	tu "gitlab.jiagouyun.com/cloudcare-tools/datakit/internal/testutils"
	"gitlab.jiagouyun.com/cloudcare-tools/datakit/io"
	dkpt "gitlab.jiagouyun.com/cloudcare-tools/datakit/io/point"
	pl "gitlab.jiagouyun.com/cloudcare-tools/datakit/pipeline"
	"gitlab.jiagouyun.com/cloudcare-tools/datakit/plugins/inputs"
)

type caseSpec struct {
	t *T.T

	name        string
	repo        string
	repoTag     string
	envs        []string
	servicePort string

	ipt    *Input
	feeder *io.MockedFeeder

	pool     *dt.Pool
	resource *dt.Resource

	cr *tu.CaseResult
}

func (cs *caseSpec) checkPoint(pts []*point.Point) error {
	for _, pt := range pts {
		measurement := string(pt.Name())

		switch measurement {
		case "sqlserver_performance":
			msgs := inputs.CheckPoint(pt, inputs.WithDoc(&Performance{}), inputs.WithExtraTags(cs.ipt.Tags))

			for _, msg := range msgs {
				cs.t.Logf("check measurement %s failed: %+#v", measurement, msg)
			}

			// TODO: error here
			// if len(msgs) > 0 {
			//	return fmt.Errorf("check measurement %s failed: %+#v", measurement, msgs)
			//}

		default: // TODO: check other measurement
		}

		// check if tag appended
		if len(cs.ipt.Tags) != 0 {
			cs.t.Logf("checking tags %+#v...", cs.ipt.Tags)

			tags := pt.Tags()
			for k, expect := range cs.ipt.Tags {
				if v := tags.Get([]byte(k)); v != nil {
					got := string(v.GetD())
					if got != expect {
						return fmt.Errorf("expect tag value %s, got %s", expect, got)
					}
				} else {
					return fmt.Errorf("tag %s not found, got %v", k, tags)
				}
			}
		}
	}

	// TODO: some other checking on @pts, such as `if some required measurements exist'...

	return nil
}

func (cs *caseSpec) run() error {
	// start remote sqlserver
	r := tu.GetRemote()
	dockerTCP := r.TCPURL()

	cs.t.Logf("get remote: %+#v, TCP: %s", r, dockerTCP)

	start := time.Now()

	p, err := dt.NewPool(dockerTCP)
	if err != nil {
		return err
	}

	hostname, err := os.Hostname()
	if err != nil {
		cs.t.Logf("get hostname failed: %s, ignored", err)
		hostname = "unknown-hostname"
	}

	containerName := fmt.Sprintf("%s.%s", hostname, cs.name)

	// remove the container if exist.
	if err := p.RemoveContainerByName(containerName); err != nil {
		return err
	}

	resource, err := p.RunWithOptions(&dt.RunOptions{
		// specify container image & tag
		Repository: cs.repo,
		Tag:        cs.repoTag,

		// port binding
		PortBindings: map[docker.Port][]docker.PortBinding{
			"1433/tcp": {{HostIP: "0.0.0.0", HostPort: cs.servicePort}},
		},

		Name: containerName,

		// container run-time envs
		Env: cs.envs,
	}, func(c *docker.HostConfig) {
		c.RestartPolicy = docker.RestartPolicy{Name: "no"}
	})
	if err != nil {
		return err
	}

	cs.pool = p
	cs.resource = resource

	cs.t.Logf("check service(%s:%s)...", r.Host, cs.servicePort)
	if !r.PortOK(cs.servicePort, time.Minute) {
		return fmt.Errorf("service checking failed")
	}

	cs.cr.AddField("container_ready_cost", int64(time.Since(start)))

	var wg sync.WaitGroup

	// start input
	cs.t.Logf("start input...")
	wg.Add(1)
	go func() {
		defer wg.Done()
		cs.ipt.Run()
	}()

	// wait data
	start = time.Now()
	cs.t.Logf("wait points...")
	pts, err := cs.feeder.AnyPoints()
	if err != nil {
		return err
	}

	cs.cr.AddField("point_latency", int64(time.Since(start)))
	cs.cr.AddField("point_count", len(pts))

	cs.t.Logf("get %d points", len(pts))
	if err := cs.checkPoint(pts); err != nil {
		return err
	}

	cs.t.Logf("stop input...")
	cs.ipt.Terminate()

	cs.t.Logf("exit...")
	wg.Wait()

	return nil
}

func buildCases(t *T.T) ([]*caseSpec, error) {
	t.Helper()

	remote := tu.GetRemote()

	bases := []struct {
		name string
		conf string
	}{
		{
			name: "remote-sqlserver",

			conf: fmt.Sprintf(`
host = "%s"
user = "sa"
password = "Abc123abC$"`,
				net.JoinHostPort(remote.Host, fmt.Sprintf("%d", tu.RandPort("tcp")))),
		},

		{
			name: "remote-sqlserver-with-extra-tags",

			// Why config like this? See:
			//    https://gitlab.jiagouyun.com/cloudcare-tools/datakit/-/issues/1391#note_36026
			conf: fmt.Sprintf(`
host = "%s"
user = "sa"
password = "Abc123abC$" # SQLServer require password to be larger than 8bytes, must include number, alphabet and symbol.
[tags]
  tag1 = "some_value"
  tag2 = "some_other_value"`, net.JoinHostPort(remote.Host, fmt.Sprintf("%d", tu.RandPort("tcp")))),
		},
	}

	images := [][2]string{
		{"mcr.microsoft.com/mssql/server", "2017-latest"},
		{"mcr.microsoft.com/mssql/server", "2019-latest"},
		{"mcr.microsoft.com/mssql/server", "2022-latest"},
	}

	// TODO: add per-image configs
	perImageCfgs := []interface{}{}
	_ = perImageCfgs

	var cases []*caseSpec

	// compose cases
	for _, img := range images {
		for _, base := range bases {
			feeder := io.NewMockedFeeder()

			ipt := defaultInput()
			ipt.feeder = feeder

			_, err := toml.Decode(base.conf, ipt)
			assert.NoError(t, err)

			envs := []string{
				"ACCEPT_EULA=Y",
				fmt.Sprintf("SA_PASSWORD=%s", ipt.Password),
			}

			ipport, err := netip.ParseAddrPort(ipt.Host)
			assert.NoError(t, err, "parse %s failed: %s", ipt.Host, err)

			cases = append(cases, &caseSpec{
				t:      t,
				ipt:    ipt,
				name:   base.name,
				feeder: feeder,
				envs:   envs,

				repo:    img[0],
				repoTag: img[1],

				servicePort: fmt.Sprintf("%d", ipport.Port()),

				cr: &tu.CaseResult{
					Name:        t.Name(),
					Case:        base.name,
					ExtraFields: map[string]any{},
					ExtraTags: map[string]string{
						"image":         img[0],
						"image_tag":     img[1],
						"remote_server": ipt.Host,
					},
				},
			})
		}
	}
	return cases, nil
}

func TestSQLServerInput(t *T.T) {
	start := time.Now()
	cases, err := buildCases(t)
	if err != nil {
		cr := &tu.CaseResult{
			Name:          t.Name(),
			Status:        tu.TestPassed,
			FailedMessage: err.Error(),
			Cost:          time.Since(start),
		}

		_ = tu.Flush(cr)
		return
	}

	t.Logf("testing %d cases...", len(cases))

	for _, tc := range cases {
		t.Run(tc.name, func(t *T.T) {
			caseStart := time.Now()

			t.Logf("testing %s...", tc.name)

			if err := tc.run(); err != nil {
				tc.cr.Status = tu.TestFailed
				tc.cr.FailedMessage = err.Error()

				assert.NoError(t, err)
			} else {
				tc.cr.Status = tu.TestPassed
			}

			tc.cr.Cost = time.Since(caseStart)

			assert.NoError(t, tu.Flush(tc.cr))

			t.Cleanup(func() {
				// clean remote docker resources
				if tc.resource == nil {
					return
				}

				assert.NoError(t, tc.pool.Purge(tc.resource))
			})
		})
	}
}

func Test_setHostTagIfNotLoopback(t *T.T) {
	type args struct {
		tags      map[string]string
		ipAndPort string
	}
	tests := []struct {
		name     string
		args     args
		expected map[string]string
	}{
		{
			name: "loopback",
			args: args{
				tags:      map[string]string{},
				ipAndPort: "localhost:1234",
			},
			expected: map[string]string{},
		},
		{
			name: "loopback",
			args: args{
				tags:      map[string]string{},
				ipAndPort: "127.0.0.1:1234",
			},
			expected: map[string]string{},
		},
		{
			name: "normal",
			args: args{
				tags:      map[string]string{},
				ipAndPort: "192.168.1.1:1234",
			},
			expected: map[string]string{
				"host": "192.168.1.1",
			},
		},
		{
			name: "error not ip:port",
			args: args{
				tags:      map[string]string{},
				ipAndPort: "http://192.168.1.1:1234",
			},
			expected: map[string]string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *T.T) {
			setHostTagIfNotLoopback(tt.args.tags, tt.args.ipAndPort)
			assert.Equal(t, tt.expected, tt.args.tags)
		})
	}
}

func TestPipeline(t *T.T) {
	source := `sqlserver`
	t.Run("pl-sqlserver-logging", func(t *T.T) {
		// sqlserver log examples
		logs := []string{
			`2020-01-01 00:00:01.00 spid28s     Server is listening on [ ::1 <ipv6> 1431] accept sockets 1.`,
			`2020-01-01 00:00:02.00 Server      Common language runtime (CLR) functionality initialized.`,
		}

		expected := []*dkpt.Point{
			dkpt.MustNewPoint(source, nil, map[string]any{
				`message`: logs[0],
				`msg`:     `Server is listening on [ ::1 <ipv6> 1431] accept sockets 1.`,
				`origin`:  `spid28s`,
				`status`:  `unknown`,
			}, &dkpt.PointOption{Category: point.Logging.URL(), Time: time.Date(2020, 1, 1, 0, 0, 1, 0, time.UTC)}),

			dkpt.MustNewPoint(source, nil, map[string]any{
				`message`: logs[1],
				`msg`:     `Common language runtime (CLR) functionality initialized.`,
				`origin`:  `Server`,
				`status`:  `unknown`,
			}, &dkpt.PointOption{Category: point.Logging.URL(), Time: time.Date(2020, 1, 1, 0, 0, 2, 0, time.UTC)}),
		}

		p, err := pl.NewPipeline(point.Logging.URL(), "", pipeline)
		assert.NoError(t, err, "NewPipeline: %s", err)

		for idx, ln := range logs {
			pt, err := dkpt.NewPoint(source,
				nil,
				map[string]any{"message": ln},
				&dkpt.PointOption{Category: point.Logging.URL()})
			assert.NoError(t, err)

			after, dropped, err := p.Run(pt, nil, &dkpt.PointOption{Category: point.Logging.URL()}, nil)

			assert.NoError(t, err)
			assert.False(t, dropped)

			assert.Equal(t, expected[idx].String(), after.String())
		}
	})
}
