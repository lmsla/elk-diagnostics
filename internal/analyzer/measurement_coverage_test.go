package analyzer

import (
	"testing"
	"time"

	"elk-diagnostics/internal/collector"
	"elk-diagnostics/internal/diagnostic"
)

func TestNumericAnalyzersExposeMeasurements(t *testing.T) {
	th := testThresholds()
	now := staticNow
	combined, replica, limit := int64(10), int64(0), int64(1000)
	runtime := &collector.NodeRuntimeSnapshot{
		Coverage: completeCoverage(1),
		Nodes:    []collector.NodeRuntime{{ID: "n1", Name: "n1", Roles: []string{"data_hot"}, ESVersion: "8.14.3", HeapInitBytes: 1 << 30, HeapMaxBytes: 1 << 30}},
	}
	topology := &collector.NodeTopologySnapshot{
		Coverage: completeCoverage(2),
		Nodes: []collector.TopologyNode{
			{ID: "n1", Name: "n1", Roles: []string{"data_hot"}, Attributes: map[string]string{"zone": "a"}},
			{ID: "n2", Name: "n2", Roles: []string{"data_hot"}, Attributes: map[string]string{"zone": "b"}},
		},
	}
	healthResults := FromHealthReport(loadHR(t, "es8-health/health_report_verbose.json"))

	cases := []struct {
		name string
		got  diagnostic.Result
	}{
		{"health report", healthResults[0]},
		{"master topology", MasterStabilityContext(3, 3)},
		{"index allocation", IndexAllocationBlocked(map[string]string{"logs": "all"}, nil)},
		{"allocation guidance", AllocationGuidance(&collector.AllocationExplanation{Index: "logs", Deciders: []collector.AllocationDecider{{Decider: "same_shard"}}}, true)},
		{"hot spotting", HotSpotting([]collector.NodeCPU{{Name: "n1", CPU: 10}, {Name: "n2", CPU: 20}}, th)},
		{"unbalanced", Unbalanced([]collector.AllocationRow{{Node: "n1", Shards: 2}, {Node: "n2", Shards: 1}})},
		{"data tier", DataTierAvailability(map[string]int{"data_hot": 2})},
		{"mapping", MappingExplosion([]collector.IndexFieldCount{{Index: "logs", FieldCount: 10}}, th)},
		{"ingest", IngestPipelineErrors([]collector.IngestPipeline{{Pipeline: "p", Count: 10, Failed: 1}}, th)},
		{"index health", DataCorruption([]collector.IndexHealth{{Index: "logs", Health: "green"}})},
		{"ilm", ILM("RUNNING", nil)},
		{"ilm migration", IlmTierMigration(nil)},
		{"transform", Transforms(nil)},
		{"remote cluster", RemoteClusters(nil)},
		{"deprecation", UpgradeDeprecations(nil)},
		{"rejected requests", RejectedRequests([]collector.ThreadPoolRow{{Node: "n1", Name: "write"}})},
		{"jvm pressure", JVMPressure([]collector.NodeJVM{{Name: "n1", UsedBytes: 1, MaxBytes: 2, PressurePct: 50}}, th)},
		{"breaker", CircuitBreaker([]collector.NodeBreaker{{Node: "n1", Breaker: "request"}})},
		{"cpu", HighCPU([]collector.NodeCPU{{Name: "n1", CPU: 10, AllocatedProcessors: 2}}, th)},
		{"task backlog", TaskBacklog([]collector.ThreadPoolRow{{Node: "n1", Name: "write"}}, th)},
		{"slow log", SlowLog(nil)},
		{"indexing pressure", IndexingPressure(&collector.IndexingPressureSnapshot{Coverage: completeCoverage(1), Nodes: []collector.IndexingPressureNode{{ID: "n1", Name: "n1", CombinedCoordinatingPrimary: &combined, ReplicaBytes: &replica, LimitBytes: &limit}}}, th)},
		{"index blocks", IndexReadWriteBlocks(nil)},
		{"ccr", CCRHealth(collector.CCRStats{}, th)},
		{"ml", MLHealth(nil, nil)},
		{"planned shutdown", PlannedShutdownHealth(nil)},
		{"voting exclusions", VotingExclusionsHealth(nil)},
		{"pending tasks", PendingClusterTasks(nil, th)},
		{"long tasks", LongRunningTasks(nil, th)},
		{"shard sizing", ShardSizing(nil, th)},
		{"snapshot freshness", SnapshotFreshness([]collector.SLMPolicy{{Name: "daily", LastSuccessMillis: now.Add(-time.Hour).UnixMilli()}}, th, now)},
		{"runtime", NodeRuntimeConsistency(runtime)},
		{"tls", TLSCertificateExpiry([]collector.TLSCertificate{{Alias: "http", Expiry: now.Add(90 * 24 * time.Hour).Format(time.RFC3339)}}, th, now)},
		{"license", LicenseHealth(collector.LicenseInfo{Status: "active", Type: "trial", ExpiryMillis: now.Add(90 * 24 * time.Hour).UnixMilli()}, th, now)},
		{"replica", ReplicaCoverage([]collector.IndexReplica{{Index: "logs", Replicas: 1}})},
		{"awareness", AllocationAwareness([]string{"zone"}, topology)},
		{"restore", RestoreStatus(nil)},
		{"write bottleneck", WriteBottleneck([]collector.NodeCPU{{Name: "n1", AllocatedProcessors: 2}}, []collector.WritePoolRow{{Node: "n1", Size: 2}}, th)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if len(tc.got.Measurements) == 0 {
				t.Fatalf("%s 未輸出 Measurements: %+v", tc.got.ID, tc.got)
			}
		})
	}
}
