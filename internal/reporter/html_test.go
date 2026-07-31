package reporter

import (
	"strings"
	"testing"

	"elk-diagnostics/internal/nodecontext"
)

func TestHTML_NodeContext(t *testing.T) {
	r := sampleReport()
	used, total, fdOpen, fdMax := int64(512), int64(1024), int64(8), int64(100)
	cpu, osMemoryUsed, heapUsed := 25, 93, 54
	r.NodeContext = &nodecontext.Snapshot{
		StatsCoverage: nodecontext.Coverage{Available: true, Total: 1, Successful: 1, Returned: 1},
		InfoCoverage:  nodecontext.Coverage{Available: true, Total: 1, Successful: 1, Returned: 1},
		Nodes: []nodecontext.Node{{
			Name:  "node-a",
			Roles: []string{"data_content", "data_hot", "ingest", "master", "transform"},
			OS: nodecontext.OS{
				CPUPercent: &cpu,
				Memory:     nodecontext.Memory{UsedBytes: &used, TotalBytes: &total, UsedPct: &osMemoryUsed},
			},
			Process:    nodecontext.Process{OpenFileDescriptors: &fdOpen, MaxFileDescriptors: &fdMax},
			Filesystem: nodecontext.Filesystem{DataPaths: []nodecontext.DataPath{{Path: "/srv/es-data"}}},
			JVM:        nodecontext.JVM{HeapUsedPct: &heapUsed},
		}},
	}
	out, err := HTML(r)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, want := range []string{
		"節點概況",
		`<span class="section-en">Node Context</span>`,
		`<table class="node-overview">`,
		`<div class="node-mobile-list">`,
		`<details class="node-technical-details">`,
		"node-a",
		"data_hot",
		`role-more">+2</span>`,
		"/srv/es-data",
		"1/1 成功",
		"<th>OS RAM*</th>",
		"93%",
		"JVM Heap",
		"54%",
		"ⓘ 記憶體指標判讀",
		"不等於 JVM Heap",
		"單次高值不單獨視為記憶體壓力",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("HTML 缺少 %q", want)
		}
	}
	for _, legacy := range []string{"｜ RAM 93%", "｜ Heap 54%", "｜ CPU 25%"} {
		if strings.Contains(s, legacy) {
			t.Errorf("HTML 不應再使用模糊標籤 %q", legacy)
		}
	}
}

func TestHTML_ReportHeaderSeparatesSummaryAndTechnicalMetadata(t *testing.T) {
	r := sampleReport()
	r.Meta.GeneratedAt = "2026-07-30T17:24:32Z"
	r.Meta.CollectedAt = "2026-07-30T17:24:29Z"
	r.Meta.CollectScriptVersion = "0.0.4-mvp"
	r.Meta.Cluster.Host = "(bundle) /var/evidence/M07-fault/bundle"
	r.NodeContext = &nodecontext.Snapshot{Nodes: make([]nodecontext.Node, 4)}

	out, err := HTML(r)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, want := range []string{
		"<title>Elasticsearch 叢集健康診斷報告</title>",
		"<h1>Elasticsearch 叢集健康診斷報告</h1>",
		`class="report-status critical">CRITICAL · 嚴重`,
		`<div class="cluster-name">docker-cluster</div>`,
		"<span>4 個節點</span>",
		"Bundle 離線分析",
		"資料採集時間",
		`datetime="2026-07-30T17:24:29Z"`,
		"報告產生時間",
		`datetime="2026-07-30T17:24:32Z"`,
		"elk-diagnostics 0.0.5",
		"採集器版本",
		"0.0.4-mvp",
		`<details class="technical-info">`,
		"M07-fault/bundle",
		"/var/evidence/M07-fault/bundle",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("HTML 表頭缺少 %q", want)
		}
	}
	if strings.Contains(s, "模式：check") {
		t.Error("HTML 不應對使用者顯示內部模式名稱 check")
	}
}

func TestHTML_DiagnosticCardsHaveExplicitStatusLabels(t *testing.T) {
	out, err := HTML(sampleReport())
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, label := range []string{"PASS", "WARNING", "CRITICAL", "SKIPPED", "UNKNOWN"} {
		want := `<span class="status-label">` + label + `</span>`
		if !strings.Contains(s, want) {
			t.Errorf("HTML 診斷卡缺少明確狀態標籤 %q", label)
		}
	}
}
