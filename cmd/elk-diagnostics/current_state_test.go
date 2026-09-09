package main

import (
	"testing"

	"elk-diagnostics/internal/collector"
	"elk-diagnostics/internal/diagnostic"
	"elk-diagnostics/internal/nodecontext"
)

func intPtr(v int) *int           { return &v }
func floatPtr(v float64) *float64 { return &v }
func int64Ptr(v int64) *int64     { return &v }

func TestBuildCurrentState(t *testing.T) {
	esATotal, esAAvailable := int64(1000), int64(580)
	esBTotal, esBAvailable := int64(2000), int64(980)
	state := buildCurrentState(currentStateInput{
		HealthReport: &collector.HealthReport{Status: "red"},
		ClusterHealth: collector.ClusterHealth{
			Status: "yellow", NumberOfNodes: intPtr(3), NumberOfDataNodes: intPtr(3),
			ActivePrimaryShards: intPtr(10), ActiveShards: intPtr(20), UnassignedShards: intPtr(2),
			UnassignedPrimaryShards: intPtr(1), RelocatingShards: intPtr(0), InitializingShards: intPtr(0),
			ActiveShardsPercent: floatPtr(90.9),
		},
		ExpectedNodes: []string{"es-a", "es-b", "es-c"},
		NodeSnapshot: &nodecontext.Snapshot{
			StatsCoverage: nodecontext.Coverage{Available: true, Total: 2, Successful: 2, Returned: 2},
			Nodes: []nodecontext.Node{
				{Name: "es-a", Roles: []string{"master", "data_hot"}, Filesystem: nodecontext.Filesystem{TotalBytes: &esATotal, AvailableBytes: &esAAvailable}},
				{Name: "es-b", Roles: []string{"data_hot"}, Filesystem: nodecontext.Filesystem{TotalBytes: &esBTotal, AvailableBytes: &esBAvailable}},
			},
			MissingNodes: []string{"es-c"},
		},
		MasterEligible: 1, MasterEligibleKnown: true,
		ILMStatus: "RUNNING", ILMKnown: true,
		License: collector.LicenseInfo{Status: "active", Type: "trial"}, LicenseKnown: true,
		ShardLimits: collector.ClusterShardLimits{MaxShardsPerNode: intPtr(1000), MaxShardsPerNodeFrozen: intPtr(3000)}, ShardLimitsKnown: true,
		CPUs: []collector.NodeCPU{{Name: "es-a", DiskPercent: 42, DiskKnown: true}, {Name: "es-b", DiskPercent: 51, DiskKnown: true}},
		Results: []diagnostic.Result{
			{Measurements: []diagnostic.Measurement{
				{Metric: "kibana.instance.count", Value: 2}, {Metric: "kibana.instance.available.count", Value: 1},
				{Metric: "kibana.instance.unknown.count", Value: 1},
			}},
			{Measurements: []diagnostic.Measurement{
				{Metric: "logstash.instance.count", Value: 2}, {Metric: "logstash.instance.available.count", Value: 2},
				{Metric: "logstash.health_report.skipped.count", Value: 1},
			}},
		},
		KibanaEvidence:   []collector.KibanaEvidence{{StatusBody: []byte(`{"version":{"number":"9.3.0"}}`)}},
		LogstashEvidence: []collector.LogstashEvidence{{RootBody: []byte(`{"version":"9.3.0"}`)}},
		KibanaRequested:  true, LogstashRequested: true,
	})

	if state.Health.Status != "yellow" || !state.Health.Known {
		t.Fatalf("health = %+v", state.Health)
	}
	if state.Health.Source != "_cluster/health" {
		t.Fatalf("health source = %q, want _cluster/health", state.Health.Source)
	}
	if state.Nodes.Expected == nil || *state.Nodes.Expected != 3 || state.Nodes.Responding == nil || *state.Nodes.Responding != 2 || state.Nodes.Missing == nil || *state.Nodes.Missing != 1 || !state.Nodes.MissingKnown {
		t.Fatalf("nodes = %+v", state.Nodes)
	}
	if state.Shards.Total == nil || *state.Shards.Total != 22 || state.Shards.MaxPerNode == nil || *state.Shards.MaxPerNode != 1000 || state.Shards.CapacityNodeCount == nil || *state.Shards.CapacityNodeCount != 2 || state.Shards.Capacity == nil || *state.Shards.Capacity != 2000 || state.Shards.MaxTotal == nil || *state.Shards.MaxTotal != 2000 || state.Shards.Remaining == nil || *state.Shards.Remaining != 1978 {
		t.Fatalf("shards = %+v", state.Shards)
	}
	if !state.Disk.Known || state.Disk.NodeCount != 2 || len(state.Disk.Nodes) != 3 || state.Disk.MaxUsedPercent == nil || *state.Disk.MaxUsedPercent != 51 || state.Disk.MaxNode != "es-b" || state.Disk.UsedBytes != nil || state.Disk.AvailableBytes != nil || state.Disk.TotalBytes != nil {
		t.Fatalf("disk = %+v", state.Disk)
	}
	if !state.Disk.Nodes[2].Missing || state.Disk.Nodes[2].Name != "es-c" {
		t.Fatalf("disk missing node = %+v", state.Disk.Nodes)
	}
	if state.Master.EligibleCount == nil || *state.Master.EligibleCount != 1 || len(state.Master.EligibleNames) != 1 || state.Master.EligibleNames[0] != "es-a" {
		t.Fatalf("master = %+v", state.Master)
	}
	if state.Kibana == nil || state.Kibana.Total == nil || *state.Kibana.Total != 2 || state.Kibana.Available == nil || *state.Kibana.Available != 1 {
		t.Fatalf("kibana = %+v", state.Kibana)
	}
	if len(state.Kibana.Versions) != 1 || state.Kibana.Versions[0] != "9.3.0" {
		t.Fatalf("kibana versions = %+v", state.Kibana.Versions)
	}
	if state.Logstash == nil || state.Logstash.Total == nil || *state.Logstash.Total != 2 || state.Logstash.NotApplicable == nil || *state.Logstash.NotApplicable != 1 {
		t.Fatalf("logstash = %+v", state.Logstash)
	}
	if len(state.Logstash.Versions) != 1 || state.Logstash.Versions[0] != "9.3.0" {
		t.Fatalf("logstash versions = %+v", state.Logstash.Versions)
	}
}

func TestCurrentHealthFallsBackToHealthReport(t *testing.T) {
	got := currentHealth(collector.ClusterHealth{}, &collector.HealthReport{Status: "red"})
	if !got.Known || got.Status != "red" || got.Source != "_health_report" {
		t.Fatalf("health fallback = %+v", got)
	}

	unknown := currentHealth(collector.ClusterHealth{}, nil)
	if unknown.Known || unknown.Status != "unknown" || unknown.Source != "—" {
		t.Fatalf("unknown health = %+v", unknown)
	}
}

func TestBuildCurrentStateDoesNotInferMissingNodesFromIncompleteCoverage(t *testing.T) {
	state := buildCurrentState(currentStateInput{
		ExpectedNodes: []string{"es-a", "es-b"},
		NodeSnapshot: &nodecontext.Snapshot{
			StatsCoverage: nodecontext.Coverage{Available: true, Total: 2, Successful: 1, Failed: 1, Returned: 1},
			Nodes:         []nodecontext.Node{{Name: "es-a"}},
		},
	})
	if state.Nodes.MissingKnown || state.Nodes.Missing != nil {
		t.Fatalf("不完整 coverage 不應推論缺失節點: %+v", state.Nodes)
	}
}

func TestCurrentShardsCapacityExcludesFrozenNodes(t *testing.T) {
	snapshot := &nodecontext.Snapshot{
		StatsCoverage: nodecontext.Coverage{Available: true, Total: 3, Successful: 3, Returned: 3},
		Nodes: []nodecontext.Node{
			{Name: "hot", Roles: []string{"data_hot"}},
			{Name: "frozen", Roles: []string{"data_frozen"}},
			{Name: "ingest", Roles: []string{"ingest"}},
		},
	}
	state := currentShards(collector.ClusterHealth{
		ActiveShards: intPtr(10), UnassignedShards: intPtr(0),
		RelocatingShards: intPtr(0), InitializingShards: intPtr(0),
	}, collector.ClusterShardLimits{MaxShardsPerNode: intPtr(1000)}, true, snapshot)
	if state.CapacityNodeCount == nil || *state.CapacityNodeCount != 1 || state.Capacity == nil || *state.Capacity != 1000 {
		t.Fatalf("frozen 節點不應納入容量計算: %+v", state)
	}
}

func TestCurrentDiskAggregatesCompleteNodes(t *testing.T) {
	totalA, availableA := int64Ptr(100), int64Ptr(40)
	totalB, availableB := int64Ptr(200), int64Ptr(100)
	snapshot := &nodecontext.Snapshot{
		Nodes: []nodecontext.Node{
			{Name: "es-a", Filesystem: nodecontext.Filesystem{TotalBytes: totalA, AvailableBytes: availableA}},
			{Name: "es-b", Filesystem: nodecontext.Filesystem{TotalBytes: totalB, AvailableBytes: availableB}},
		},
	}
	state := currentDiskWithSnapshot([]string{"es-a", "es-b"}, snapshot, []collector.NodeCPU{
		{Name: "es-a", DiskPercent: 60, DiskKnown: true},
		{Name: "es-b", DiskPercent: 50, DiskKnown: true},
	})
	if state.UsedBytes == nil || *state.UsedBytes != 160 || state.AvailableBytes == nil || *state.AvailableBytes != 140 || state.TotalBytes == nil || *state.TotalBytes != 300 || state.UsedPercent == nil || *state.UsedPercent != 53 {
		t.Fatalf("完整節點應提供磁碟合計: %+v", state)
	}
}
