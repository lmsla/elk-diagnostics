package analyzer

import (
	"testing"

	"elk-diagnostics/internal/collector"
	"elk-diagnostics/internal/diagnostic"
	"elk-diagnostics/internal/nodecontext"
)

func TestIndexingPressure(t *testing.T) {
	th := testThresholds()
	pressureNode := func(combined, replica int64) collector.IndexingPressureNode {
		limit := int64(1000)
		return collector.IndexingPressureNode{Name: "n1", CombinedCoordinatingPrimary: &combined, ReplicaBytes: &replica, LimitBytes: &limit}
	}
	base := &collector.IndexingPressureSnapshot{Coverage: completeCoverage(1)}
	base.Nodes = []collector.IndexingPressureNode{pressureNode(799, 0)}
	if got := IndexingPressure(base, th).Status; got != diagnostic.StatusPass {
		t.Fatalf("79.9%% status=%s, want pass", got)
	}
	base.Nodes = []collector.IndexingPressureNode{pressureNode(800, 0)}
	if got := IndexingPressure(base, th); got.Status != diagnostic.StatusWarning || !got.RequiresExtra {
		t.Fatalf("80%% got=%+v", got)
	}
	base.Nodes = []collector.IndexingPressureNode{pressureNode(0, 1425)} // 95% of replica 1.5x cap.
	if got := IndexingPressure(base, th).Status; got != diagnostic.StatusCritical {
		t.Fatalf("replica 95%% status=%s, want critical", got)
	}
	base.Coverage.Failed = 1
	base.Coverage.Successful = 0
	if got := IndexingPressure(base, th).Status; got != diagnostic.StatusUnknown {
		t.Fatalf("partial status=%s, want unknown", got)
	}
}

func TestIndexReadWriteBlocks(t *testing.T) {
	if got := IndexReadWriteBlocks(nil).Status; got != diagnostic.StatusPass {
		t.Fatalf("no blocks status=%s, want pass", got)
	}
	got := IndexReadWriteBlocks([]collector.IndexBlock{{Index: "logs", ReadOnlyAllowDelete: true}})
	if got.Status != diagnostic.StatusCritical || got.Conclusion != diagnostic.ConclusionConfirmed || !got.RequiresExtra {
		t.Fatalf("blocked index got=%+v", got)
	}
}

func TestCCRHealth(t *testing.T) {
	th := testThresholds()
	if got := CCRHealth(collector.CCRStats{}, th).Status; got != diagnostic.StatusSkipped {
		t.Fatalf("unused CCR status=%s, want skipped", got)
	}
	lag := collector.CCRStats{Followers: []collector.CCRFollower{{Index: "copy", GlobalCheckpointLag: int64(th.StaticHealth.CCRLagWarnOps)}}}
	if got := CCRHealth(lag, th); got.Status != diagnostic.StatusWarning || !got.RequiresExtra {
		t.Fatalf("lag got=%+v", got)
	}
	fatal := collector.CCRStats{Followers: []collector.CCRFollower{{Index: "copy", FatalErrors: []string{"fatal"}}}}
	if got := CCRHealth(fatal, th).Status; got != diagnostic.StatusCritical {
		t.Fatalf("fatal status=%s, want critical", got)
	}
}

func TestMLHealth(t *testing.T) {
	if got := MLHealth(nil, nil).Status; got != diagnostic.StatusSkipped {
		t.Fatalf("unused ML status=%s, want skipped", got)
	}
	normal := MLHealth([]collector.MLJob{{ID: "job", State: "closed"}}, []collector.MLDatafeed{{ID: "feed", State: "stopped"}})
	if normal.Status != diagnostic.StatusPass {
		t.Fatalf("closed/stopped status=%s, want pass", normal.Status)
	}
	failed := MLHealth([]collector.MLJob{{ID: "job", State: "failed"}}, nil)
	if failed.Status != diagnostic.StatusCritical {
		t.Fatalf("failed status=%s, want critical", failed.Status)
	}
}

func TestShutdownAndVotingExclusions(t *testing.T) {
	if got := PlannedShutdownHealth(nil).Status; got != diagnostic.StatusPass {
		t.Fatalf("no shutdown status=%s, want pass", got)
	}
	if got := PlannedShutdownUnavailable(403).Status; got != diagnostic.StatusSkipped {
		t.Fatalf("403 status=%s, want skipped", got)
	}
	stalled := PlannedShutdownHealth([]collector.PlannedShutdown{{NodeID: "n1", Status: "STALLED"}})
	if stalled.Status != diagnostic.StatusCritical {
		t.Fatalf("stalled status=%s, want critical", stalled.Status)
	}
	if got := VotingExclusionsHealth(nil).Status; got != diagnostic.StatusPass {
		t.Fatalf("no exclusions status=%s, want pass", got)
	}
	if got := VotingExclusionsHealth([]collector.VotingExclusion{{NodeName: "master-1"}}); got.Status != diagnostic.StatusWarning || !got.RequiresExtra {
		t.Fatalf("exclusion got=%+v", got)
	}
}

func TestRecentRestartAndMemoryLock(t *testing.T) {
	th := testThresholds()
	uptime := int64(30 * 60 * 1000)
	locked := false
	swapTotal := int64(1024)
	swapUsed := int64(0)
	snapshot := &nodecontext.Snapshot{
		StatsCoverage: completeCoverage(1), InfoCoverage: completeCoverage(1),
		Nodes: []nodecontext.Node{{Name: "n1", JVM: nodecontext.JVM{UptimeMillis: &uptime}, Process: nodecontext.Process{MemoryLocked: &locked}, OS: nodecontext.OS{Swap: nodecontext.Memory{TotalBytes: &swapTotal, UsedBytes: &swapUsed}}}},
	}
	if got := RecentNodeRestart(snapshot, th); got.Status != diagnostic.StatusWarning || !got.RequiresExtra {
		t.Fatalf("recent restart got=%+v", got)
	}
	if got := NodeMemoryLock(snapshot); got.Status != diagnostic.StatusWarning || !got.RequiresExtra {
		t.Fatalf("unlocked with swap got=%+v", got)
	}
	swapTotal = 0
	if got := NodeMemoryLock(snapshot).Status; got != diagnostic.StatusPass {
		t.Fatalf("unlocked with swap disabled status=%s, want pass", got)
	}
	snapshot.InfoCoverage.Failed = 1
	snapshot.InfoCoverage.Successful = 0
	if got := NodeMemoryLock(snapshot).Status; got != diagnostic.StatusUnknown {
		t.Fatalf("partial status=%s, want unknown", got)
	}
}
