package diagnostic

import "testing"

func TestInfoDoesNotEscalateOverallOrExitCode(t *testing.T) {
	r := NewReport(Meta{}, []Result{{Status: StatusInfo, Conclusion: ConclusionNormal, Summary: "需觀察"}})
	if r.Summary.Info != 1 {
		t.Fatalf("info count=%d, want 1", r.Summary.Info)
	}
	if r.OverallStatus != StatusPass {
		t.Fatalf("overall=%s, want pass when only info exists", r.OverallStatus)
	}
	if r.ExitCode() != 0 {
		t.Fatalf("exit code=%d, want 0 when only info exists", r.ExitCode())
	}
}
