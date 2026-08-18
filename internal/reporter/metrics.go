package reporter

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"

	"elk-diagnostics/internal/diagnostic"
	"elk-diagnostics/internal/nodecontext"
)

const metricsSchemaVersion = "1"

type metricCluster struct {
	UUID      string `json:"uuid,omitempty"`
	Name      string `json:"name,omitempty"`
	ESVersion string `json:"es_version,omitempty"`
}

// metricObservation 每行可獨立索引；刻意不包含 host、帳密或採集包路徑。
type metricObservation struct {
	Timestamp       string        `json:"@timestamp"`
	TimestampSource string        `json:"timestamp_source"`
	SchemaVersion   string        `json:"schema_version"`
	Cluster         metricCluster `json:"cluster"`
	Mode            string        `json:"mode"`
	Metric          string        `json:"metric"`
	Kind            string        `json:"kind"`
	Value           *float64      `json:"value,omitempty"`
	State           string        `json:"state,omitempty"`
	Unit            string        `json:"unit,omitempty"`
	EntityType      string        `json:"entity_type,omitempty"`
	EntityID        string        `json:"entity_id,omitempty"`
	EntityName      string        `json:"entity_name,omitempty"`
	Component       string        `json:"component,omitempty"`
	PeerGroup       string        `json:"peer_group,omitempty"`
	DiagnosticID    string        `json:"diagnostic_id,omitempty"`
	Category        string        `json:"category,omitempty"`
	DiagnosticState string        `json:"diagnostic_status,omitempty"`
	Source          string        `json:"source,omitempty"`
}

// MetricsNDJSON 將單次診斷轉成版本化觀測值。它只讀結構化欄位，不解析 findings 文字。
func MetricsNDJSON(r diagnostic.Report) ([]byte, error) {
	timestamp, timestampSource := r.Meta.CollectedAt, "collected_at"
	if timestamp == "" {
		timestamp, timestampSource = r.Meta.GeneratedAt, "generated_at"
	}
	if _, err := time.Parse(time.RFC3339, timestamp); err != nil {
		return nil, fmt.Errorf("缺少有效的 RFC3339 採集時間: %q", timestamp)
	}

	base := metricObservation{
		Timestamp:       timestamp,
		TimestampSource: timestampSource,
		SchemaVersion:   metricsSchemaVersion,
		Mode:            r.Meta.Mode,
		Cluster: metricCluster{
			UUID: r.Meta.Cluster.UUID, Name: r.Meta.Cluster.Name, ESVersion: r.Meta.Cluster.ESVersion,
		},
	}
	var observations []metricObservation
	addState := func(metric, state, entityType, entityID, entityName, component, source string) {
		o := base
		o.Metric, o.Kind, o.State = metric, "state", state
		o.EntityType, o.EntityID, o.EntityName = entityType, entityID, entityName
		o.Component, o.Source = component, source
		observations = append(observations, o)
	}
	addValue := func(metric, kind, unit, entityType, entityID, entityName, component, source string, value float64) {
		o := base
		o.Metric, o.Kind, o.Unit = metric, kind, unit
		o.EntityType, o.EntityID, o.EntityName = entityType, entityID, entityName
		o.Component, o.Source = component, source
		o.Value = &value
		observations = append(observations, o)
	}

	addState("report.overall_status", string(r.OverallStatus), "cluster", r.Meta.Cluster.UUID, r.Meta.Cluster.Name, "", "report")
	for _, item := range []struct {
		status string
		count  int
	}{
		{"pass", r.Summary.Pass}, {"info", r.Summary.Info}, {"warning", r.Summary.Warning}, {"critical", r.Summary.Critical},
		{"skipped", r.Summary.Skipped}, {"unknown", r.Summary.Unknown},
	} {
		addValue("report.result.count", "gauge", "count", "cluster", r.Meta.Cluster.UUID, r.Meta.Cluster.Name, item.status, "report", float64(item.count))
	}
	for _, result := range r.Results {
		addState("diagnostic.status", string(result.Status), "diagnostic", result.ID, result.Title, "", result.Source)
		observations[len(observations)-1].DiagnosticID = result.ID
		observations[len(observations)-1].Category = result.Category
		for _, measurement := range result.Measurements {
			addValue(measurement.Metric, measurement.Kind, measurement.Unit, measurement.EntityType,
				measurement.EntityID, measurement.EntityName, measurement.Component, result.Source, measurement.Value)
			o := &observations[len(observations)-1]
			o.PeerGroup = measurement.PeerGroup
			o.DiagnosticID, o.Category, o.DiagnosticState = result.ID, result.Category, string(result.Status)
		}
	}
	if r.NodeContext != nil {
		appendNodeContextMetrics(r.NodeContext, addValue)
	}

	var out bytes.Buffer
	for _, observation := range observations {
		line, err := json.Marshal(observation)
		if err != nil {
			return nil, err
		}
		out.Write(line)
		out.WriteByte('\n')
	}
	return out.Bytes(), nil
}

type addMetric func(metric, kind, unit, entityType, entityID, entityName, component, source string, value float64)

func appendNodeContextMetrics(snapshot *nodecontext.Snapshot, add addMetric) {
	coverage := func(prefix string, c nodecontext.Coverage) {
		add(prefix+".total", "gauge", "count", "cluster", "", "", "", "node_context", float64(c.Total))
		add(prefix+".successful", "gauge", "count", "cluster", "", "", "", "node_context", float64(c.Successful))
		add(prefix+".failed", "gauge", "count", "cluster", "", "", "", "node_context", float64(c.Failed))
		add(prefix+".returned", "gauge", "count", "cluster", "", "", "", "node_context", float64(c.Returned))
	}
	coverage("nodes.stats", snapshot.StatsCoverage)
	coverage("nodes.info", snapshot.InfoCoverage)

	for _, node := range snapshot.Nodes {
		entity := func(metric, kind, unit, component string, value float64) {
			add(metric, kind, unit, "node", node.ID, node.Name, component, "node_context", value)
		}
		addInt(entity, "node.os.cpu.percent", "gauge", "percent", "", node.OS.CPUPercent)
		addFloat(entity, "node.os.load.1m", "gauge", "ratio", "", node.OS.Load1m)
		addFloat(entity, "node.os.load.5m", "gauge", "ratio", "", node.OS.Load5m)
		addFloat(entity, "node.os.load.15m", "gauge", "ratio", "", node.OS.Load15m)
		appendMemory(entity, "node.os.memory", node.OS.Memory)
		appendMemory(entity, "node.os.swap", node.OS.Swap)
		addInt64(entity, "node.cgroup.cpu.usage", "counter", "nanoseconds", "", node.OS.Cgroup.CPU.UsageNanos)
		addInt64(entity, "node.cgroup.cpu.times_throttled", "counter", "count", "", node.OS.Cgroup.CPU.TimesThrottled)
		addInt64(entity, "node.cgroup.cpu.time_throttled", "counter", "nanoseconds", "", node.OS.Cgroup.CPU.TimeThrottledNanos)
		addUint64(entity, "node.cgroup.memory.usage", "gauge", "bytes", "", node.OS.Cgroup.Memory.UsageBytes)
		addUint64(entity, "node.cgroup.memory.limit", "gauge", "bytes", "", node.OS.Cgroup.Memory.LimitBytes)
		addInt(entity, "node.process.cpu.percent", "gauge", "percent", "", node.Process.CPUPercent)
		addInt64(entity, "node.process.cpu.total", "counter", "milliseconds", "", node.Process.CPUTotalMillis)
		addInt64(entity, "node.process.open_file_descriptors", "gauge", "count", "", node.Process.OpenFileDescriptors)
		addInt64(entity, "node.process.max_file_descriptors", "gauge", "count", "", node.Process.MaxFileDescriptors)
		addInt64(entity, "node.filesystem.total", "gauge", "bytes", "", node.Filesystem.TotalBytes)
		addInt64(entity, "node.filesystem.free", "gauge", "bytes", "", node.Filesystem.FreeBytes)
		addInt64(entity, "node.filesystem.available", "gauge", "bytes", "", node.Filesystem.AvailableBytes)
		addInt64(entity, "node.jvm.uptime", "gauge", "milliseconds", "", node.JVM.UptimeMillis)
		addInt64(entity, "node.jvm.heap.used", "gauge", "bytes", "", node.JVM.HeapUsedBytes)
		addInt64(entity, "node.jvm.heap.max", "gauge", "bytes", "", node.JVM.HeapMaxBytes)
		addInt(entity, "node.jvm.heap.used_pct", "gauge", "percent", "", node.JVM.HeapUsedPct)
		addInt64(entity, "node.jvm.old.used", "gauge", "bytes", "", node.JVM.OldUsedBytes)
		addInt64(entity, "node.jvm.old.max", "gauge", "bytes", "", node.JVM.OldMaxBytes)
		for _, gc := range node.JVM.GCCollectors {
			addInt64(entity, "node.jvm.gc.collection_count", "counter", "count", gc.Name, gc.CollectionCount)
			addInt64(entity, "node.jvm.gc.collection_time", "counter", "milliseconds", gc.Name, gc.CollectionTimeMillis)
		}
		for _, path := range node.Filesystem.DataPaths {
			addInt64(entity, "node.filesystem.path.total", "gauge", "bytes", path.Path, path.TotalBytes)
			addInt64(entity, "node.filesystem.path.free", "gauge", "bytes", path.Path, path.FreeBytes)
			addInt64(entity, "node.filesystem.path.available", "gauge", "bytes", path.Path, path.AvailableBytes)
		}
		for _, device := range node.Filesystem.Devices {
			addInt64(entity, "node.io.operations", "counter", "count", device.Name, device.Operations)
			addInt64(entity, "node.io.read_operations", "counter", "count", device.Name, device.ReadOperations)
			addInt64(entity, "node.io.write_operations", "counter", "count", device.Name, device.WriteOperations)
			addInt64(entity, "node.io.read", "counter", "kilobytes", device.Name, device.ReadKilobytes)
			addInt64(entity, "node.io.write", "counter", "kilobytes", device.Name, device.WriteKilobytes)
			addInt64(entity, "node.io.time", "counter", "milliseconds", device.Name, device.IOTimeMillis)
		}
	}
}

type entityMetric func(metric, kind, unit, component string, value float64)

func appendMemory(add entityMetric, prefix string, memory nodecontext.Memory) {
	addInt64(add, prefix+".total", "gauge", "bytes", "", memory.TotalBytes)
	addInt64(add, prefix+".used", "gauge", "bytes", "", memory.UsedBytes)
	addInt64(add, prefix+".free", "gauge", "bytes", "", memory.FreeBytes)
	addInt(add, prefix+".used_pct", "gauge", "percent", "", memory.UsedPct)
	addInt(add, prefix+".free_pct", "gauge", "percent", "", memory.FreePct)
}

func addInt(add entityMetric, metric, kind, unit, component string, value *int) {
	if value != nil {
		add(metric, kind, unit, component, float64(*value))
	}
}

func addInt64(add entityMetric, metric, kind, unit, component string, value *int64) {
	if value != nil {
		add(metric, kind, unit, component, float64(*value))
	}
}

func addUint64(add entityMetric, metric, kind, unit, component string, value *uint64) {
	if value != nil {
		add(metric, kind, unit, component, float64(*value))
	}
}

func addFloat(add entityMetric, metric, kind, unit, component string, value *float64) {
	if value != nil {
		add(metric, kind, unit, component, *value)
	}
}
