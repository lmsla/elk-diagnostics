package reporter

import (
	"strings"
	"testing"

	"elk-diagnostics/internal/diagnostic"
	"elk-diagnostics/internal/nodecontext"
)

func sampleReport() diagnostic.Report {
	results := []diagnostic.Result{
		{ID: "cluster_health", Title: "叢集健康 / 未分配 shard", Status: diagnostic.StatusWarning,
			Summary: "This cluster has 7 unavailable replica shards.", Findings: []string{"詳情第一條"}},
		{ID: "data_allocation_blocked", Title: "叢集層級 shard 分配封鎖", Status: diagnostic.StatusUnknown,
			Summary: "資料抓取失敗，無法判定", Findings: []string{"bundle 缺少 cluster_settings.json"}},
		{ID: "red_cluster", Title: "叢集為 RED", Status: diagnostic.StatusCritical,
			Summary: "存在未分配 primary shard", Findings: []string{"status=red"}},
		{ID: "disk", Title: "磁碟容量", Status: diagnostic.StatusPass, Summary: "正常"},
		{ID: "watcher", Title: "Watcher", Status: diagnostic.StatusSkipped, Summary: "未使用"},
	}
	meta := diagnostic.Meta{
		ToolVersion: "0.0.5",
		GeneratedAt: "2026-07-16T02:10:00Z",
		Cluster:     diagnostic.ClusterMeta{Name: "docker-cluster", Host: "https://es:9200", ESVersion: "8.14.3"},
		Mode:        "check",
	}
	return diagnostic.NewReport(meta, results)
}

func TestText_NoANSIWhenColorDisabled(t *testing.T) {
	out := string(Text(sampleReport(), false))
	if strings.ContainsRune(out, '\x1b') {
		t.Errorf("color=false 時不應含 ANSI escape，got:\n%s", out)
	}
}

func TestText_ANSIWhenColorEnabled(t *testing.T) {
	out := string(Text(sampleReport(), true))
	if !strings.Contains(out, "\x1b[") {
		t.Errorf("color=true 時應含 ANSI escape，got:\n%s", out)
	}
}

func TestText_NonPassItemsListedWithFindingIndent(t *testing.T) {
	out := string(Text(sampleReport(), false))
	for _, want := range []string{
		"叢集為 RED", "存在未分配 primary shard",
		"叢集健康 / 未分配 shard", "This cluster has 7 unavailable replica shards.",
		"叢集層級 shard 分配封鎖",
		"     └ status=red",
		"     └ 詳情第一條",
		"     └ bundle 缺少 cluster_settings.json",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("輸出缺少 %q，got:\n%s", want, out)
		}
	}
}

func TestText_CriticalBeforeWarningBeforeUnknown(t *testing.T) {
	out := string(Text(sampleReport(), false))
	iCrit := strings.Index(out, "叢集為 RED")
	iWarn := strings.Index(out, "叢集健康 / 未分配 shard")
	iUnk := strings.Index(out, "叢集層級 shard 分配封鎖")
	if !(iCrit < iWarn && iWarn < iUnk) {
		t.Errorf("排序應為 critical → warning → unknown，got 位置 crit=%d warn=%d unk=%d", iCrit, iWarn, iUnk)
	}
}

func TestText_PassAndSkippedCompressedToSummaryLine(t *testing.T) {
	out := string(Text(sampleReport(), false))
	if !strings.Contains(out, "通過（1）：磁碟容量") {
		t.Errorf("pass 應壓縮成彙總行，got:\n%s", out)
	}
	if !strings.Contains(out, "略過（1）：Watcher") {
		t.Errorf("skipped 應壓縮成彙總行，got:\n%s", out)
	}
	// pass 項目的 summary 文字不該逐項展開在彙總行外。
	if strings.Count(out, "正常") > 1 {
		t.Errorf("pass 項目不應逐項展開 summary，got:\n%s", out)
	}
}

func TestText_HeaderAndDisclaimer(t *testing.T) {
	out := string(Text(sampleReport(), false))
	if !strings.Contains(out, "elk-diagnostics 0.0.5") || !strings.Contains(out, "docker-cluster") || !strings.Contains(out, "ES 8.14.3") {
		t.Errorf("表頭應含工具版本／叢集名／ES 版本，got:\n%s", out)
	}
	if !strings.Contains(out, diagnostic.NewReport(diagnostic.Meta{}, nil).Disclaimer) {
		t.Errorf("末行應含免責聲明，got:\n%s", out)
	}
}

func TestText_NodeContextCoverage(t *testing.T) {
	r := sampleReport()
	r.NodeContext = &nodecontext.Snapshot{
		StatsCoverage: nodecontext.Coverage{Available: true, Total: 2, Successful: 2, Returned: 2},
		InfoCoverage:  nodecontext.Coverage{Available: true, Total: 2, Successful: 2, Returned: 2},
		Nodes:         make([]nodecontext.Node, 2),
	}
	out := string(Text(r, false))
	if !strings.Contains(out, "節點資料：Stats 2/2、Info 2/2；Node Context 2 個節點") {
		t.Errorf("text 應顯示 node coverage，got:\n%s", out)
	}
}

func TestText_ExitCodeUnaffectedByFormat(t *testing.T) {
	r := sampleReport()
	if r.ExitCode() != 2 { // critical 存在
		t.Errorf("ExitCode() = %d, want 2（text 格式不改變收斂/結束碼規則）", r.ExitCode())
	}
}
