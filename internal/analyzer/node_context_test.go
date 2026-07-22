package analyzer

import (
	"testing"

	"elk-diagnostics/internal/diagnostic"
	"elk-diagnostics/internal/nodecontext"
)

func intp(v int) *int       { return &v }
func i64p(v int64) *int64   { return &v }
func u64p(v uint64) *uint64 { return &v }
func boolp(v bool) *bool    { return &v }

func completeCoverage(n int) nodecontext.Coverage {
	return nodecontext.Coverage{Available: true, Total: n, Successful: n, Returned: n}
}

func TestNodeAPICoverage(t *testing.T) {
	complete := &nodecontext.Snapshot{StatsCoverage: completeCoverage(2), InfoCoverage: completeCoverage(2), Nodes: make([]nodecontext.Node, 2)}
	if got := NodeAPICoverage(complete).Status; got != diagnostic.StatusPass {
		t.Errorf("complete status=%s, want pass", got)
	}
	partial := *complete
	partial.StatsCoverage.Failed = 1
	partial.StatsCoverage.Successful = 1
	if got := NodeAPICoverage(&partial).Status; got != diagnostic.StatusUnknown {
		t.Errorf("partial status=%s, want unknown", got)
	}
}

func TestNodeSwapUsage(t *testing.T) {
	nodes := []nodecontext.Node{{Name: "n1", OS: nodecontext.OS{Swap: nodecontext.Memory{TotalBytes: i64p(100), UsedBytes: i64p(1)}}}}
	if got := NodeSwapUsage(nodes); got.Status != diagnostic.StatusWarning || got.Conclusion != diagnostic.ConclusionConfirmed {
		t.Errorf("swap used got=%+v", got)
	}
	if got := NodeSwapUsage([]nodecontext.Node{{Name: "n1"}}).Status; got != diagnostic.StatusUnknown {
		t.Errorf("missing swap status=%s, want unknown", got)
	}
}

func TestNodeFileDescriptorPressure(t *testing.T) {
	th := testThresholds()
	cases := []struct {
		open int64
		want diagnostic.Status
	}{{799, diagnostic.StatusPass}, {800, diagnostic.StatusWarning}, {900, diagnostic.StatusCritical}}
	for _, tc := range cases {
		nodes := []nodecontext.Node{{Name: "n1", Process: nodecontext.Process{OpenFileDescriptors: i64p(tc.open), MaxFileDescriptors: i64p(1000)}}}
		if got := NodeFileDescriptorPressure(nodes, th).Status; got != tc.want {
			t.Errorf("open=%d status=%s, want %s", tc.open, got, tc.want)
		}
	}
}

func TestNodeCgroupMemoryPressure(t *testing.T) {
	th := testThresholds()
	linux := nodecontext.OS{Name: "Linux", Cgroup: nodecontext.Cgroup{Memory: nodecontext.CgroupMemory{UsageBytes: u64p(900), LimitBytes: u64p(1000), LimitUnlimited: boolp(false)}}}
	if got := NodeCgroupMemoryPressure([]nodecontext.Node{{Name: "n1", OS: linux}}, th); got.Status != diagnostic.StatusWarning || !got.RequiresExtra {
		t.Errorf("90%% cgroup got=%+v", got)
	}
	unlimited := nodecontext.OS{Name: "Linux", Cgroup: nodecontext.Cgroup{Memory: nodecontext.CgroupMemory{LimitUnlimited: boolp(true)}}}
	if got := NodeCgroupMemoryPressure([]nodecontext.Node{{Name: "n1", OS: unlimited}}, th).Status; got != diagnostic.StatusPass {
		t.Errorf("unlimited status=%s, want pass", got)
	}
	if got := NodeCgroupMemoryPressure([]nodecontext.Node{{Name: "n1", OS: nodecontext.OS{Name: "Linux"}}}, th).Status; got != diagnostic.StatusUnknown {
		t.Errorf("missing Linux cgroup status=%s, want unknown", got)
	}
}
