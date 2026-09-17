package analyzer

import (
	"strings"
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
	partial.StatsCoverage.Returned = 1
	partial.Issues = []string{"Nodes Stats 部分回應: total=2 successful=1 failed=1 returned=1"}
	got := NodeAPICoverage(&partial)
	if got.Status != diagnostic.StatusUnknown {
		t.Errorf("partial status=%s, want unknown", got.Status)
	}
	if len(got.Findings) != 2 {
		t.Fatalf("coverage findings=%v，應只保留 Stats／Info 各一行", got.Findings)
	}
	for _, finding := range got.Findings {
		if strings.Contains(finding, "部分回應") {
			t.Errorf("coverage finding 重複描述 partial response: %q", finding)
		}
	}
}

func TestExpectedESNodeCoverage(t *testing.T) {
	snapshot := &nodecontext.Snapshot{
		StatsCoverage: completeCoverage(2),
		Nodes:         []nodecontext.Node{{Name: "node-a"}, {Name: "node-b"}},
	}
	if got := ExpectedESNodeCoverage(nil, snapshot); got.Status != diagnostic.StatusSkipped {
		t.Fatalf("without baseline=%+v", got)
	}
	if got := ExpectedESNodeCoverage([]string{"node-a", "node-b"}, snapshot); got.Status != diagnostic.StatusPass {
		t.Fatalf("complete=%+v", got)
	}
	got := ExpectedESNodeCoverage([]string{"node-a", "node-b", "node-c"}, snapshot)
	if got.Status != diagnostic.StatusCritical || !strings.Contains(strings.Join(got.Findings, " "), "node-c") {
		t.Fatalf("missing=%+v", got)
	}
	partial := *snapshot
	partial.StatsCoverage = nodecontext.Coverage{Available: true, Total: 3, Successful: 2, Failed: 1, Returned: 2}
	if got := ExpectedESNodeCoverage([]string{"node-a", "node-b", "node-c"}, &partial); got.Status != diagnostic.StatusUnknown {
		t.Fatalf("partial=%+v", got)
	}
}

func TestExpectedESNodeCoverageUsesIPForDuplicateNames(t *testing.T) {
	snapshot := &nodecontext.Snapshot{
		StatsCoverage: completeCoverage(3),
		Nodes: []nodecontext.Node{
			{Name: "Elasticsearch01", IP: "10.99.1.250:9300"},
			{Name: "Elasticsearch02", IP: "10.99.1.251:9300"},
			{Name: "Elasticsearch02", IP: "10.99.1.252:9300"},
		},
	}
	expected := []string{
		"Elasticsearch01|10.99.1.250",
		"Elasticsearch02|10.99.1.251",
		"Elasticsearch02|10.99.1.252",
	}
	got := ExpectedESNodeCoverage(expected, snapshot)
	if got.Status != diagnostic.StatusPass {
		t.Fatalf("duplicate names should match by IP: %+v", got)
	}
}

func TestNodeContextResultsConvergesPartialCoverage(t *testing.T) {
	unlimited := true
	locked := true
	nodes := make([]nodecontext.Node, 3)
	for i := range nodes {
		nodes[i] = nodecontext.Node{
			Name: "n" + string(rune('1'+i)),
			OS: nodecontext.OS{
				Swap:   nodecontext.Memory{TotalBytes: i64p(0), UsedBytes: i64p(0)},
				Cgroup: nodecontext.Cgroup{Memory: nodecontext.CgroupMemory{LimitUnlimited: &unlimited}},
			},
			Process: nodecontext.Process{
				OpenFileDescriptors: i64p(10),
				MaxFileDescriptors:  i64p(1000),
				MemoryLocked:        &locked,
			},
			JVM: nodecontext.JVM{UptimeMillis: i64p(7_200_000)},
		}
	}
	snapshot := &nodecontext.Snapshot{
		StatsCoverage: nodecontext.Coverage{Available: true, Total: 4, Successful: 3, Failed: 1, Returned: 3},
		InfoCoverage:  completeCoverage(3),
		Nodes:         nodes,
		Issues:        []string{"Nodes Stats 部分回應: total=4 successful=3 failed=1 returned=3"},
	}

	byID := make(map[string]diagnostic.Result)
	for _, result := range NodeContextResults(snapshot, testThresholds()) {
		byID[result.ID] = result
	}
	if got := byID["node_api_coverage"]; got.Status != diagnostic.StatusUnknown || len(got.Findings) != 2 {
		t.Fatalf("node_api_coverage=%+v", got)
	}
	for _, id := range []string{
		"node_swap_usage",
		"node_file_descriptor_pressure",
		"node_cgroup_memory_pressure",
		"recent_node_restart",
		"node_memory_lock",
	} {
		got := byID[id]
		if got.Status != diagnostic.StatusUnknown {
			t.Errorf("%s status=%s, want unknown", id, got.Status)
		}
		if !strings.Contains(got.Summary, "node_api_coverage") {
			t.Errorf("%s summary 未指向完整性根因: %q", id, got.Summary)
		}
		if len(got.Findings) != 0 {
			t.Errorf("%s 不應重複 coverage findings: %v", id, got.Findings)
		}
	}

	nodes[0].OS.Swap.UsedBytes = i64p(1)
	results := NodeContextResults(snapshot, testThresholds())
	for _, result := range results {
		if result.ID != "node_swap_usage" {
			continue
		}
		if result.Status != diagnostic.StatusWarning {
			t.Fatalf("已觀測到 swap 使用時仍應保留 warning: %+v", result)
		}
		if !result.RequiresExtra || !strings.Contains(result.ExtraReason, "可能低估") {
			t.Fatalf("partial coverage 應標示異常數量可能低估: %+v", result)
		}
		return
	}
	t.Fatal("缺少 node_swap_usage")
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
		res := NodeFileDescriptorPressure(nodes, th)
		if got := res.Status; got != tc.want {
			t.Errorf("open=%d status=%s, want %s", tc.open, got, tc.want)
		}
		if res.NumericJudgment == nil || len(res.NumericJudgment.Rows) != 1 || res.NumericJudgment.Rows[0].Status != tc.want {
			t.Fatalf("open=%d NumericJudgment=%+v, want one %s row", tc.open, res.NumericJudgment, tc.want)
		}
	}
}

func TestNodeCgroupMemoryPressure(t *testing.T) {
	th := testThresholds()
	linux := nodecontext.OS{Name: "Linux", Cgroup: nodecontext.Cgroup{Memory: nodecontext.CgroupMemory{UsageBytes: u64p(900), LimitBytes: u64p(1000), LimitUnlimited: boolp(false)}}}
	got := NodeCgroupMemoryPressure([]nodecontext.Node{{Name: "n1", OS: linux}}, th)
	if got.Status != diagnostic.StatusWarning || !got.RequiresExtra {
		t.Errorf("90%% cgroup got=%+v", got)
	}
	if got.NumericJudgment == nil || len(got.NumericJudgment.Rows) != 1 || got.NumericJudgment.Rows[0].Status != diagnostic.StatusWarning {
		t.Fatalf("90%% cgroup NumericJudgment=%+v, want one warning row", got.NumericJudgment)
	}
	unlimited := nodecontext.OS{Name: "Linux", Cgroup: nodecontext.Cgroup{Memory: nodecontext.CgroupMemory{LimitUnlimited: boolp(true)}}}
	if got := NodeCgroupMemoryPressure([]nodecontext.Node{{Name: "n1", OS: unlimited}}, th).Status; got != diagnostic.StatusPass {
		t.Errorf("unlimited status=%s, want pass", got)
	}
	if got := NodeCgroupMemoryPressure([]nodecontext.Node{{Name: "n1", OS: nodecontext.OS{Name: "Linux"}}}, th).Status; got != diagnostic.StatusUnknown {
		t.Errorf("missing Linux cgroup status=%s, want unknown", got)
	}
}
