// Unless explicitly stated otherwise all files in this repository are licensed
// under the MIT License.
// This product includes software developed at Guance Cloud (https://www.guance.com/).
// Copyright 2021-present Guance, Inc.

package jenkins

import (
	"fmt"
	"time"

	"gitlab.jiagouyun.com/cloudcare-tools/datakit/io/point"
	"gitlab.jiagouyun.com/cloudcare-tools/datakit/plugins/inputs"
)

var fieldMap = map[string]string{
	"jenkins.executor.count.value":  "executor_count",
	"jenkins.executor.free.value":   "executor_free_count",
	"jenkins.executor.in-use.value": "executor_in_use_count",
	"jenkins.job.count.value":       "job_count",
	"jenkins.node.offline.value":    "node_offline_count",
	"jenkins.node.online.value":     "node_online_count",
	"jenkins.plugins.active":        "plugins_active",
	"jenkins.plugins.failed":        "plugins_failed",
	"jenkins.project.count.value":   "project_count",
	"jenkins.queue.blocked.value":   "queue_blocked",
	"jenkins.queue.buildable.value": "queue_buildable",
	"jenkins.queue.pending.value":   "queue_pending",
	"jenkins.queue.size.value":      "queue_size",
	"jenkins.queue.stuck.value":     "queue_stuck",

	"system.cpu.load":           "system_cpu_load",
	"vm.blocked.count":          "vm_blocked_count",
	"vm.count":                  "vm_count",
	"vm.cpu.load":               "vm_cpu_load",
	"vm.memory.total.used":      "vm_memory_total_used",
	"vm.memory.total.committed": "vm_memory_total_committed",
}

type Metric struct {
	Version string                            `json:"version"`
	Gauges  map[string]map[string]interface{} `json:"gauges"`
}

type jenkinsPipelineMeasurement struct{}

func (j *jenkinsPipelineMeasurement) LineProto() (*point.Point, error) {
	return nil, nil
}

//nolint:lll
func (j *jenkinsPipelineMeasurement) Info() *inputs.MeasurementInfo {
	return &inputs.MeasurementInfo{
		Name: "jenkins_pipeline",
		Desc: "Jenkins Pipeline Event 相关指标",
		Fields: map[string]interface{}{
			"pipeline_id":    &inputs.FieldInfo{DataType: inputs.String, Unit: inputs.UnknownUnit, Desc: "pipeline id"},
			"duration":       &inputs.FieldInfo{DataType: inputs.Int, Unit: inputs.DurationUS, Desc: "pipeline 持续时长（微秒）"},
			"commit_message": &inputs.FieldInfo{DataType: inputs.String, Unit: inputs.UnknownUnit, Desc: "触发该 pipeline 的代码的最近一次提交附带的 message"},
			"created_at":     &inputs.FieldInfo{DataType: inputs.Int, Unit: inputs.TimestampMS, Desc: "pipeline 创建的毫秒时间戳"},
			"finished_at":    &inputs.FieldInfo{DataType: inputs.Int, Unit: inputs.TimestampMS, Desc: "pipeline 结束的毫秒时间戳"},
			"message":        &inputs.FieldInfo{DataType: inputs.String, Unit: inputs.UnknownUnit, Desc: "该 pipeline 的 id，与 pipeline_id 相同"},
		},
		Tags: map[string]interface{}{
			"object_kind":    inputs.NewTagInfo("Event 类型，此处为 Pipeline"),
			"ci_status":      inputs.NewTagInfo("CI 状态"),
			"pipeline_name":  inputs.NewTagInfo("pipeline 名称"),
			"pipeline_url":   inputs.NewTagInfo("pipeline 的 URL"),
			"commit_sha":     inputs.NewTagInfo("触发 pipeline 的最近一次 commit 的哈希值"),
			"author_email":   inputs.NewTagInfo("作者邮箱"),
			"repository_url": inputs.NewTagInfo("仓库 URL"),
			"operation_name": inputs.NewTagInfo("操作名称"),
			"resource":       inputs.NewTagInfo("项目名"),
			"ref":            inputs.NewTagInfo("涉及的分支"),
		},
	}
}

type jenkinsJobMeasurement struct{}

func (j *jenkinsJobMeasurement) LineProto() (*point.Point, error) {
	return nil, nil
}

//nolint:lll
func (j *jenkinsJobMeasurement) Info() *inputs.MeasurementInfo {
	return &inputs.MeasurementInfo{
		Name: "jenkins_job",
		Desc: "Jenkins Job Event 相关指标",
		Fields: map[string]interface{}{
			"build_id":             &inputs.FieldInfo{DataType: inputs.String, Unit: inputs.UnknownUnit, Desc: "build id"},
			"build_started_at":     &inputs.FieldInfo{DataType: inputs.Int, Unit: inputs.TimestampMS, Desc: "build 开始的毫秒时间戳"},
			"build_finished_at":    &inputs.FieldInfo{DataType: inputs.Int, Unit: inputs.TimestampMS, Desc: "build 结束的毫秒时间戳"},
			"build_duration":       &inputs.FieldInfo{DataType: inputs.Int, Unit: inputs.DurationUS, Desc: "build 持续时长（微秒）"},
			"pipeline_id":          &inputs.FieldInfo{DataType: inputs.String, Unit: inputs.UnknownUnit, Desc: "build 对应的 pipeline id"},
			"runner_id":            &inputs.FieldInfo{DataType: inputs.String, Unit: inputs.UnknownUnit, Desc: "build 对应的 runner id"},
			"build_commit_message": &inputs.FieldInfo{DataType: inputs.String, Unit: inputs.UnknownUnit, Desc: "触发该 build 的最近一次 commit 的 message"},
			"message":              &inputs.FieldInfo{DataType: inputs.String, Unit: inputs.UnknownUnit, Desc: "build 对应的 job name"},
		},
		Tags: map[string]interface{}{
			"object_kind":          inputs.NewTagInfo("Event 类型，此处为 Job"),
			"sha":                  inputs.NewTagInfo("build 对应的 commit 的哈希值"),
			"build_name":           inputs.NewTagInfo("build 的名称"),
			"build_stage":          inputs.NewTagInfo("build 的阶段"),
			"build_status":         inputs.NewTagInfo("build 的状态"),
			"project_name":         inputs.NewTagInfo("项目名"),
			"build_failure_reason": inputs.NewTagInfo("build 失败的原因"),
			"user_email":           inputs.NewTagInfo("作者邮箱"),
			"build_commit_sha":     inputs.NewTagInfo("build 对应的 commit 的哈希值"),
			"build_repo_name":      inputs.NewTagInfo("build 对应的仓库名"),
		},
	}
}

func (n *Input) getPluginMetric() {
	var metric Metric
	err := n.requestJSON(fmt.Sprintf("/metrics/%s/metrics?pretty=true", n.Key), &metric)
	if err != nil {
		l.Error(err.Error())
		n.lastErr = err
		return
	}
	ts := time.Now()
	tags := map[string]string{
		"metric_plugin_version": metric.Version,
		"url":                   n.URL,
	}
	for k, v := range n.Tags {
		tags[k] = v
	}
	fields := map[string]interface{}{}
	for k, v := range metric.Gauges {
		if fieldKey, ok := fieldMap[k]; ok {
			fields[fieldKey] = v["value"]
		}
	}
	if version, ok := metric.Gauges["jenkins.versions.core"]; ok {
		if v, ok := (version["value"]).(string); ok {
			tags["version"] = v
		} else {
			l.Warnf("expect string")
		}
	}
	if len(fields) == 0 {
		err = fmt.Errorf("jenkins empty field")
		l.Error(err.Error())
		n.lastErr = err
		return
	}
	n.collectCache = append(n.collectCache, &Measurement{fields: fields, tags: tags, ts: ts, name: inputName})
	l.Debug(n.collectCache[0])
}

type Measurement struct {
	name   string
	tags   map[string]string
	fields map[string]interface{}
	ts     time.Time
}

func (m *Measurement) LineProto() (*point.Point, error) {
	return point.NewPoint(m.name, m.tags, m.fields, point.MOpt())
}

//nolint:lll
func (m *Measurement) Info() *inputs.MeasurementInfo {
	return &inputs.MeasurementInfo{
		Name: inputName,
		Tags: map[string]interface{}{
			"url":                   inputs.NewTagInfo("jenkins url"),
			"metric_plugin_version": inputs.NewTagInfo("jenkins plugin version"),
			"version":               inputs.NewTagInfo("jenkins  version"),
		},
		Fields: map[string]interface{}{
			"executor_count":        newCountFieldInfo("The number of executors available to Jenkins"),
			"executor_free_count":   newCountFieldInfo("The number of executors available to Jenkins that are not currently in use."),
			"executor_in_use_count": newCountFieldInfo("The number of executors available to Jenkins that are currently in use."),
			"job_count":             newCountFieldInfo("The number of jobs in Jenkins"),
			"node_offline_count":    newCountFieldInfo("The number of build nodes available to Jenkins but currently off-line."),
			"node_online_count":     newCountFieldInfo("The number of build nodes available to Jenkins and currently on-line."),
			"plugins_active":        newCountFieldInfo("The number of plugins in the Jenkins instance that started successfully."),
			"plugins_failed":        newCountFieldInfo("The number of plugins in the Jenkins instance that failed to start."),
			"project_count":         newCountFieldInfo("The number of project to Jenkins"),
			"queue_blocked":         newCountFieldInfo("The number of jobs that are in the Jenkins build queue and currently in the blocked state."),
			"queue_buildable":       newCountFieldInfo("The number of jobs that are in the Jenkins build queue and currently in the blocked state."),
			"queue_pending":         newCountFieldInfo("Number of times a Job has been Pending in a Queue"),
			"queue_size":            newCountFieldInfo("The number of jobs that are in the Jenkins build queue."),
			"queue_stuck":           newCountFieldInfo("he number of jobs that are in the Jenkins build queue and currently in the blocked state"),

			"system_cpu_load":           newRateFieldInfo("The system load on the Jenkins controller as reported by the JVM’s Operating System JMX bean"),
			"vm_blocked_count":          newCountFieldInfo("The number of threads in the Jenkins JVM that are currently blocked waiting for a monitor lock."),
			"vm_count":                  newCountFieldInfo("The total number of threads in the Jenkins JVM. This is the sum of: vm.blocked.count, vm.new.count, vm.runnable.count, vm.terminated.count, vm.timed_waiting.count and vm.waiting.count"),
			"vm_cpu_load":               newRateFieldInfo("The rate of CPU time usage by the JVM per unit time on the Jenkins controller. This is equivalent to the number of CPU cores being used by the Jenkins JVM."),
			"vm_memory_total_used":      newByteFieldInfo("The total amount of memory that the Jenkins JVM is currently using.(Units of measurement: bytes)"),
			"vm_memory_total_committed": newByteFieldInfo("The total amount of memory that is guaranteed by the operating system as available for use by the Jenkins JVM. (Units of measurement: bytes)"),
		},
	}
}
