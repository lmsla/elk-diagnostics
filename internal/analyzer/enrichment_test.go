package analyzer

import (
	"testing"

	"elk-diagnostics/internal/collector"
	"elk-diagnostics/internal/diagnostic"
	"elk-diagnostics/internal/nodecontext"
)

func TestILMPolicyInventory(t *testing.T) {
	if got := ILMPolicyInventory(nil).Status; got != diagnostic.StatusSkipped {
		t.Fatalf("empty status=%s, want skipped", got)
	}
	policy := collector.ILMPolicyDefinition{Name: "logs", Version: 2, UsedIndices: 1, Phases: []collector.ILMPolicyPhase{{Name: "hot", Actions: []string{"rollover"}}}}
	if got := ILMPolicyInventory([]collector.ILMPolicyDefinition{policy}); got.Status != diagnostic.StatusPass || len(got.Findings) != 1 || len(got.Measurements) != 2 {
		t.Fatalf("policy inventory = %+v", got)
	}
}

func TestSnapshotRepositoryReferences(t *testing.T) {
	policy := collector.SLMPolicy{Name: "daily", Repository: "backup"}
	repository := collector.SnapshotRepository{Name: "backup", Type: "fs"}
	if got := SnapshotRepositoryReferences(nil, nil).Status; got != diagnostic.StatusSkipped {
		t.Fatalf("empty status=%s, want skipped", got)
	}
	if got := SnapshotRepositoryReferences([]collector.SLMPolicy{policy}, []collector.SnapshotRepository{repository}); got.Status != diagnostic.StatusPass || !got.RequiresExtra {
		t.Fatalf("matching repository = %+v", got)
	}
	if got := SnapshotRepositoryReferences([]collector.SLMPolicy{policy}, nil).Status; got != diagnostic.StatusCritical {
		t.Fatalf("missing repository status=%s, want critical", got)
	}
	policy.Repository = ""
	if got := SnapshotRepositoryReferences([]collector.SLMPolicy{policy}, []collector.SnapshotRepository{repository}).Status; got != diagnostic.StatusUnknown {
		t.Fatalf("missing reference field status=%s, want unknown", got)
	}
}

func TestDataStreamHealth(t *testing.T) {
	if got := DataStreamHealth(nil).Status; got != diagnostic.StatusSkipped {
		t.Fatalf("empty status=%s, want skipped", got)
	}
	stream := collector.DataStream{Name: "logs", Status: "GREEN", BackingIndices: 1}
	if got := DataStreamHealth([]collector.DataStream{stream}).Status; got != diagnostic.StatusPass {
		t.Fatalf("green status=%s, want pass", got)
	}
	stream.Status = "YELLOW"
	if got := DataStreamHealth([]collector.DataStream{stream}).Status; got != diagnostic.StatusWarning {
		t.Fatalf("yellow status=%s, want warning", got)
	}
	stream.Status = "RED"
	if got := DataStreamHealth([]collector.DataStream{stream}).Status; got != diagnostic.StatusCritical {
		t.Fatalf("red status=%s, want critical", got)
	}
	stream.Status = "future-status"
	if got := DataStreamHealth([]collector.DataStream{stream}).Status; got != diagnostic.StatusUnknown {
		t.Fatalf("unknown status=%s, want unknown", got)
	}
}

func TestFielddataMemory(t *testing.T) {
	complete := &collector.FielddataSnapshot{
		Coverage: nodecontext.Coverage{Available: true, Total: 1, Successful: 1, Returned: 1},
		Nodes:    []collector.FielddataNode{{Name: "n1", MemoryBytes: 1024}},
	}
	if got := FielddataMemory(complete).Status; got != diagnostic.StatusPass {
		t.Fatalf("no eviction status=%s, want pass", got)
	}
	withEviction := *complete
	withEviction.Nodes = []collector.FielddataNode{{Name: "n1", MemoryBytes: 1024, Evictions: 2}}
	if got := FielddataMemory(&withEviction); got.Status != diagnostic.StatusInfo || got.Conclusion != diagnostic.ConclusionNormal || !got.RequiresExtra || len(got.JudgmentGuide) != 4 || len(got.Measurements) < 7 {
		t.Fatalf("eviction = %+v", got)
	}
	partial := *complete
	partial.Coverage = nodecontext.Coverage{Available: true, Total: 2, Successful: 1, Failed: 1, Returned: 1}
	if got := FielddataMemory(&partial).Status; got != diagnostic.StatusUnknown {
		t.Fatalf("partial status=%s, want unknown", got)
	}
}
