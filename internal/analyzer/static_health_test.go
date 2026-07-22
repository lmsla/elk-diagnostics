package analyzer

import (
	"fmt"
	"testing"
	"time"

	"elk-diagnostics/internal/collector"
	"elk-diagnostics/internal/diagnostic"
	"elk-diagnostics/internal/nodecontext"
)

var staticNow = time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)

func TestPendingClusterTasksThresholds(t *testing.T) {
	th := testThresholds()
	cases := []struct {
		seconds int
		want    diagnostic.Status
	}{
		{29, diagnostic.StatusPass},
		{30, diagnostic.StatusWarning},
		{300, diagnostic.StatusCritical},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%ds", tc.seconds), func(t *testing.T) {
			got := PendingClusterTasks([]collector.PendingClusterTask{{Source: "put-mapping", QueueTimeMillis: int64(tc.seconds * 1000)}}, th)
			if got.Status != tc.want {
				t.Fatalf("status=%s, want %s: %+v", got.Status, tc.want, got)
			}
		})
	}
}

func TestLongRunningTasks(t *testing.T) {
	th := testThresholds()
	self := collector.RunningTask{ID: "n1:1", Action: "cluster:monitor/tasks/lists", RunningNanos: int64(10 * time.Minute)}
	if got := LongRunningTasks([]collector.RunningTask{self}, th).Status; got != diagnostic.StatusPass {
		t.Fatalf("task-list self query status=%s, want pass", got)
	}
	long := collector.RunningTask{ID: "n1:2", Action: "indices:data/write/reindex", RunningNanos: int64(5 * time.Minute), Cancellable: true}
	if got := LongRunningTasks([]collector.RunningTask{long}, th); got.Status != diagnostic.StatusWarning || !got.RequiresExtra {
		t.Fatalf("long task = %+v", got)
	}
}

func TestShardSizing(t *testing.T) {
	th := testThresholds()
	if got := ShardSizing([]collector.ShardSize{{Index: "logs", Primary: true, StoreBytes: 49 << 30}}, th).Status; got != diagnostic.StatusPass {
		t.Fatalf("49 GiB status=%s, want pass", got)
	}
	if got := ShardSizing([]collector.ShardSize{{Index: "logs", Primary: true, StoreBytes: 50 << 30}}, th).Status; got != diagnostic.StatusWarning {
		t.Fatalf("50 GiB status=%s, want warning", got)
	}
	small := make([]collector.ShardSize, th.StaticHealth.ShardSmallCountWarn)
	for i := range small {
		small[i] = collector.ShardSize{Index: fmt.Sprintf("logs-%03d", i), Primary: true, StoreBytes: 1 << 20}
	}
	if got := ShardSizing(small, th).Status; got != diagnostic.StatusWarning {
		t.Fatalf("%d small shards status=%s, want warning", len(small), got)
	}
}

func TestSnapshotFreshness(t *testing.T) {
	th := testThresholds()
	if got := SnapshotFreshness(nil, th, staticNow).Status; got != diagnostic.StatusSkipped {
		t.Fatalf("no SLM policy status=%s, want skipped", got)
	}
	policy := collector.SLMPolicy{Name: "daily", LastSuccessMillis: staticNow.Add(-47 * time.Hour).UnixMilli()}
	if got := SnapshotFreshness([]collector.SLMPolicy{policy}, th, staticNow).Status; got != diagnostic.StatusPass {
		t.Fatalf("47h old status=%s, want pass", got)
	}
	policy.LastSuccessMillis = staticNow.Add(-48 * time.Hour).UnixMilli()
	if got := SnapshotFreshness([]collector.SLMPolicy{policy}, th, staticNow).Status; got != diagnostic.StatusWarning {
		t.Fatalf("48h old status=%s, want warning", got)
	}
	policy.LastSuccessMillis = staticNow.Add(-168 * time.Hour).UnixMilli()
	if got := SnapshotFreshness([]collector.SLMPolicy{policy}, th, staticNow).Status; got != diagnostic.StatusCritical {
		t.Fatalf("168h old status=%s, want critical", got)
	}
	policy.LastSuccessMillis = staticNow.Add(-time.Hour).UnixMilli()
	policy.LastFailureMillis = staticNow.Add(-30 * time.Minute).UnixMilli()
	if got := SnapshotFreshness([]collector.SLMPolicy{policy}, th, staticNow).Status; got != diagnostic.StatusCritical {
		t.Fatalf("newer failure status=%s, want critical", got)
	}
}

func TestNodeRuntimeConsistency(t *testing.T) {
	base := collector.NodeRuntime{ID: "a", Name: "n1", Roles: []string{"data_hot"}, ESVersion: "8.14.3", BuildHash: "aaa", JVMVersion: "21", VMVersion: "21", HeapInitBytes: 1 << 30, HeapMaxBytes: 1 << 30}
	peer := base
	peer.ID, peer.Name = "b", "n2"
	complete := &collector.NodeRuntimeSnapshot{Coverage: completeCoverage(2), Nodes: []collector.NodeRuntime{base, peer}}
	if got := NodeRuntimeConsistency(complete).Status; got != diagnostic.StatusPass {
		t.Fatalf("consistent status=%s, want pass", got)
	}
	drift := *complete
	drift.Nodes = append([]collector.NodeRuntime(nil), complete.Nodes...)
	drift.Nodes[1].ESVersion = "8.15.0"
	if got := NodeRuntimeConsistency(&drift).Status; got != diagnostic.StatusWarning {
		t.Fatalf("version drift status=%s, want warning", got)
	}
	partial := *complete
	partial.Coverage = nodecontext.Coverage{Available: true, Total: 2, Successful: 1, Failed: 1, Returned: 1}
	if got := NodeRuntimeConsistency(&partial).Status; got != diagnostic.StatusUnknown {
		t.Fatalf("partial coverage status=%s, want unknown", got)
	}
}

func TestTLSCertificateExpiry(t *testing.T) {
	th := testThresholds()
	future := collector.TLSCertificate{Subject: "CN=node", Expiry: staticNow.Add(31 * 24 * time.Hour).Format(time.RFC3339), HasPrivateKey: true}
	if got := TLSCertificateExpiry([]collector.TLSCertificate{future}, th, staticNow); got.Status != diagnostic.StatusPass || !got.RequiresExtra {
		t.Fatalf("future certificate = %+v", got)
	}
	expiredIdentity := future
	expiredIdentity.Expiry = staticNow.Add(-time.Hour).Format(time.RFC3339)
	if got := TLSCertificateExpiry([]collector.TLSCertificate{expiredIdentity}, th, staticNow).Status; got != diagnostic.StatusCritical {
		t.Fatalf("expired identity status=%s, want critical", got)
	}
	expiredTrust := expiredIdentity
	expiredTrust.HasPrivateKey = false
	if got := TLSCertificateExpiry([]collector.TLSCertificate{expiredTrust}, th, staticNow).Status; got != diagnostic.StatusWarning {
		t.Fatalf("expired trust status=%s, want warning", got)
	}
	bad := future
	bad.Expiry = "not-a-time"
	if got := TLSCertificateExpiry([]collector.TLSCertificate{bad}, th, staticNow).Status; got != diagnostic.StatusUnknown {
		t.Fatalf("invalid expiry status=%s, want unknown", got)
	}
}

func TestLicenseHealth(t *testing.T) {
	th := testThresholds()
	if got := LicenseHealth(collector.LicenseInfo{Status: "active", Type: "basic"}, th, staticNow).Status; got != diagnostic.StatusPass {
		t.Fatalf("basic no-expiry status=%s, want pass", got)
	}
	near := collector.LicenseInfo{Status: "active", Type: "trial", ExpiryMillis: staticNow.Add(29 * 24 * time.Hour).UnixMilli()}
	if got := LicenseHealth(near, th, staticNow).Status; got != diagnostic.StatusWarning {
		t.Fatalf("near expiry status=%s, want warning", got)
	}
	near.ExpiryMillis = staticNow.Add(-time.Hour).UnixMilli()
	if got := LicenseHealth(near, th, staticNow).Status; got != diagnostic.StatusCritical {
		t.Fatalf("expired status=%s, want critical", got)
	}
}

func TestReplicaCoverage(t *testing.T) {
	if got := ReplicaCoverage([]collector.IndexReplica{{Index: "logs", Replicas: 1}}).Status; got != diagnostic.StatusPass {
		t.Fatalf("replica=1 status=%s, want pass", got)
	}
	if got := ReplicaCoverage([]collector.IndexReplica{{Index: "logs", Replicas: 0}}); got.Status != diagnostic.StatusWarning || !got.RequiresExtra {
		t.Fatalf("replica=0 = %+v", got)
	}
	if got := ReplicaCoverage([]collector.IndexReplica{{Index: "logs", Replicas: 1, AutoExpand: "0-all"}}).Status; got != diagnostic.StatusWarning {
		t.Fatalf("auto-expand all status=%s, want warning", got)
	}
}

func TestAllocationAwareness(t *testing.T) {
	if got := AllocationAwareness(nil, nil).Status; got != diagnostic.StatusSkipped {
		t.Fatalf("not configured status=%s, want skipped", got)
	}
	nodes := []collector.TopologyNode{
		{Name: "n1", Roles: []string{"data_hot"}, Attributes: map[string]string{"zone": "a"}},
		{Name: "n2", Roles: []string{"data_hot"}, Attributes: map[string]string{"zone": "b"}},
	}
	snapshot := &collector.NodeTopologySnapshot{Coverage: completeCoverage(2), Nodes: nodes}
	if got := AllocationAwareness([]string{"zone"}, snapshot); got.Status != diagnostic.StatusPass || !got.RequiresExtra {
		t.Fatalf("two zones = %+v", got)
	}
	delete(nodes[1].Attributes, "zone")
	if got := AllocationAwareness([]string{"zone"}, snapshot).Status; got != diagnostic.StatusWarning {
		t.Fatalf("missing zone status=%s, want warning", got)
	}
	partial := &collector.NodeTopologySnapshot{Coverage: nodecontext.Coverage{Available: true, Total: 2, Successful: 1, Failed: 1, Returned: 1}, Nodes: nodes[:1]}
	if got := AllocationAwareness([]string{"zone"}, partial).Status; got != diagnostic.StatusUnknown {
		t.Fatalf("partial topology status=%s, want unknown", got)
	}
}
