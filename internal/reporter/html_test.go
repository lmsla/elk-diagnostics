package reporter

import (
	"strings"
	"testing"

	"elk-diagnostics/internal/diagnostic"
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
			IP:    "10.0.0.1",
			Roles: []string{"data_content", "data_hot", "ingest", "master", "transform"},
			OS: nodecontext.OS{
				CPUPercent: &cpu,
				Memory:     nodecontext.Memory{UsedBytes: &used, TotalBytes: &total, UsedPct: &osMemoryUsed},
			},
			Process:    nodecontext.Process{OpenFileDescriptors: &fdOpen, MaxFileDescriptors: &fdMax},
			Filesystem: nodecontext.Filesystem{DataPaths: []nodecontext.DataPath{{Path: "/srv/es-data"}}},
			JVM:        nodecontext.JVM{HeapUsedPct: &heapUsed},
		}},
		MissingNodes: []string{"node-offline"},
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
		"10.0.0.1",
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
		"缺失節點：node-offline",
		`class="node-missing-row"`,
		"Nodes API 未回應",
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
	r.Meta.CollectedServices = []string{"elasticsearch", "host"}
	r.Meta.Cluster.Host = "(bundle) /var/evidence/M07-fault/bundle"
	r.NodeContext = &nodecontext.Snapshot{Nodes: make([]nodecontext.Node, 4)}

	out, err := HTML(r)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, want := range []string{
		"<title>ELK 服務健康診斷報告</title>",
		`<span class="topbar-title">ELK 服務健康診斷報告</span>`,
		`<section class="banner critical" data-status="critical">`,
		`<h1 class="banner-title">整體狀態：嚴重</h1>`,
		`<dt>叢集名稱</dt><dd>docker-cluster</dd>`,
		`<dt>節點數</dt><dd>4 個節點</dd>`,
		"Bundle 離線分析",
		"資料採集時間",
		`datetime="2026-07-30T17:24:29Z"`,
		"報告產生時間",
		`datetime="2026-07-30T17:24:32Z"`,
		"elk-diagnostics 0.0.5",
		"採集器版本",
		"0.0.4-mvp",
		"採集模組",
		"elasticsearch、host",
		`<details class="tech technical-info">`,
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
	if strings.Contains(s, `class="report-meta"`) {
		t.Error("模板版面不應保留重複的報告摘要卡")
	}
}

func TestDocumentLabelUsesReadablePathSegment(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want string
	}{
		{name: "file", url: "https://www.elastic.co/guide/en/elasticsearch/reference/current/size-your-shards.html", want: "size-your-shards.html"},
		{name: "api", url: "https://www.elastic.co/docs/api/doc/elasticsearch/operation/operation-nodes-stats", want: "operation-nodes-stats"},
		{name: "host", url: "https://www.elastic.co/", want: "www.elastic.co"},
		{name: "invalid", url: "not a URL", want: "not a URL"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := documentLabel(tt.url); got != tt.want {
				t.Fatalf("documentLabel(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}

func TestHTML_DiagnosticCardsHaveExplicitStatusLabels(t *testing.T) {
	out, err := HTML(sampleReport())
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, label := range []string{"PASS", "INFO", "WARNING", "CRITICAL", "SKIPPED", "UNKNOWN"} {
		want := `<span class="status-label">` + label + `</span>`
		if !strings.Contains(s, want) {
			t.Errorf("HTML 診斷卡缺少明確狀態標籤 %q", label)
		}
	}
}

func TestHTML_SeparatesElasticsearchAndKibanaSections(t *testing.T) {
	r := sampleReport()
	for i := range r.Results {
		r.Results[i].Category = "cluster"
	}
	r.Results = append(r.Results,
		diagnostic.Result{ID: "kibana_status", Title: "Kibana 核心健康", Category: "service", Status: diagnostic.StatusPass, Summary: "1 個 Kibana instance 均可用"},
		diagnostic.Result{ID: "kibana_stats", Title: "Kibana 執行觀測", Category: "service", Status: diagnostic.StatusInfo, Summary: "僅供趨勢"},
	)
	out, err := HTML(r)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, want := range []string{
		`href="#section-es-cluster"`,
		`href="#service-kibana"`,
		`id="section-es-cluster"`,
		`id="service-kibana"`,
		`<h2 class="section-title">Kibana</h2><span class="section-en">Service</span>`,
		"Kibana 核心健康",
		"Kibana 執行觀測",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("HTML 服務區塊缺少 %q", want)
		}
	}
	if strings.Contains(s, `service-elasticsearch`) || strings.Contains(s, `>Elasticsearch</a>`) {
		t.Error("導航與內容不應增加 Elasticsearch 服務包裝層")
	}
	if strings.Index(s, `id="section-es-cluster"`) > strings.Index(s, `id="service-kibana"`) {
		t.Error("Elasticsearch 區塊應先於 Kibana 區塊")
	}
}

func TestHTML_UsesUIUXShellAndDynamicCategoryNavigation(t *testing.T) {
	r := sampleReport()
	for i := range r.Results {
		r.Results[i].Category = "cluster"
	}
	r.Results = append(r.Results, diagnostic.Result{
		ID: "kibana_status", Title: "Kibana 核心健康", Category: "service",
		Status: diagnostic.StatusSkipped, Summary: "未採集 Kibana",
	})
	out, err := HTML(r)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, want := range []string{
		"<title>ELK 服務健康診斷報告</title>",
		`class="topbar"`,
		`class="kpi-row"`,
		`class="section-nav"`,
		`href="#section-es-cluster"`,
		`id="section-es-cluster"`,
		`id="service-kibana"`,
		`data-status="skipped"`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("UIUX HTML 缺少 %q", want)
		}
	}
	if strings.Contains(s, `href="#service-kibana"`) {
		t.Error("Kibana 全為 skipped 時不應顯示在動態導航")
	}
	if strings.Contains(s, `section-kibana-service`) {
		t.Error("Kibana 不應再拆成 category 導航")
	}
}

func TestHTML_NavigationOmitsOnlyAllSkippedCategory(t *testing.T) {
	r := sampleReport()
	for i := range r.Results {
		r.Results[i].Category = "cluster"
	}
	r.Results = append(r.Results,
		diagnostic.Result{ID: "snapshot_skipped", Title: "Snapshot", Category: "snapshot", Status: diagnostic.StatusSkipped},
		diagnostic.Result{ID: "security_info", Title: "Security", Category: "security", Status: diagnostic.StatusInfo},
	)
	out, err := HTML(r)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if strings.Contains(s, `href="#section-es-snapshot"`) {
		t.Error("全為 skipped 的 ES 分類不應顯示在導航")
	}
	if !strings.Contains(s, `id="section-es-snapshot"`) || !strings.Contains(s, ">Snapshot</span>") {
		t.Error("全為 skipped 的 ES 分類與診斷卡仍須保留在報告內容")
	}
	if !strings.Contains(s, `href="#section-es-security"`) {
		t.Error("含非 skipped 結果的 ES 分類應顯示在導航")
	}
}

func TestHTML_HidesEmptyKibanaSection(t *testing.T) {
	out, err := HTML(sampleReport())
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if strings.Contains(s, `id="service-kibana"`) || strings.Contains(s, `href="#service-kibana"`) {
		t.Error("未採集 Kibana 時不應顯示空的 Kibana 區塊")
	}
}

func TestHTML_InfoCardShowsJudgmentGuide(t *testing.T) {
	out, err := HTML(sampleReport())
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, want := range []string{
		`class="card info check"`,
		`<span class="status-label">INFO</span>`,
		"需觀察",
		"判定方式",
		"單次 evictions &gt; 0",
		"不能單獨判定故障",
		`class="doclink"`,
		`title="https://www.elastic.co/guide/en/elasticsearch/reference/current/modules-fielddata.html"`,
		">modules-fielddata.html</span>",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("INFO 診斷卡缺少 %q", want)
		}
	}
}

func TestHTML_DiagnosticCardShowsCurrentMeasurements(t *testing.T) {
	r := sampleReport()
	r.Results[0].Measurements = []diagnostic.Measurement{
		{Metric: "elasticsearch.node.fielddata.memory", Kind: "gauge", Value: 1024, Unit: "bytes", EntityType: "node", EntityID: "node-1", EntityName: "es-1"},
		{Metric: "elasticsearch.node.fielddata.evictions", Kind: "counter", Value: 3, Unit: "count", EntityType: "node", EntityID: "node-1", EntityName: "es-1"},
		{Metric: "elasticsearch.data_stream.status.count", Kind: "gauge", Value: 2, Unit: "count", Component: "green"},
		{Metric: "elasticsearch.node.resource.deviation_from_median", Kind: "gauge", Value: 1, Unit: "percentage_point", EntityType: "node", EntityID: "node-1", EntityName: "es-1", Component: "cpu"},
		{Metric: "elasticsearch.node.resource.deviation_from_median", Kind: "gauge", Value: 2, Unit: "percentage_point", EntityType: "node", EntityID: "node-1", EntityName: "es-1", Component: "heap.percent"},
		{Metric: "elasticsearch.node.resource.deviation_from_median", Kind: "gauge", Value: 3, Unit: "percentage_point", EntityType: "node", EntityID: "node-1", EntityName: "es-1", Component: "disk.used_percent"},
	}

	out, err := HTML(r)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, want := range []string{
		"本次觀測值",
		"節點 Fielddata 記憶體",
		"節點 Fielddata eviction",
		"Data stream 狀態數量",
		"CPU 相對叢集中位數差距",
		"JVM Heap 相對叢集中位數差距",
		"磁碟使用率相對叢集中位數差距",
		"es-1",
		"green",
		"1.0 KiB",
		"當下值",
		"累積值",
		"累積值需以前後兩次採集的差值判讀",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("HTML 觀測值區塊缺少 %q", want)
		}
	}
}

func TestHTML_HotspotUsesComparableGroupTable(t *testing.T) {
	r := sampleReport()
	r.Results = []diagnostic.Result{{
		ID: "hot_spotting", Title: "Hot spotting", Category: "performance", Status: diagnostic.StatusInfo,
		Summary: "偵測到節點資源利用分布不均，需觀察是否持續",
		Measurements: []diagnostic.Measurement{
			{Metric: "elasticsearch.cluster.resource.median", Kind: "gauge", Value: 62.5, Unit: "percent", Component: "heap.percent", PeerGroup: "node.role=data"},
			{Metric: "elasticsearch.node.resource.current", Kind: "gauge", Value: 80, Unit: "percent", EntityType: "node", EntityName: "es-mn1", Component: "heap.percent", PeerGroup: "node.role=data"},
			{Metric: "elasticsearch.node.resource.deviation_from_median", Kind: "gauge", Value: 17.5, Unit: "percentage_point", EntityType: "node", EntityName: "es-mn1", Component: "heap.percent", PeerGroup: "node.role=data"},
		},
	}}
	out, err := HTML(r)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, want := range []string{
		"本次比較基準", "節點觀測值", "JVM Heap 使用率", "node.role=data", "62.5%", "80%", "17.5 個百分點", "原始快照＋衍生比較", "單次快照不能單獨判定 hot spotting",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("Hot spotting HTML 缺少 %q", want)
		}
	}
	if strings.Contains(s, "CPU 相對叢集中位數差距") {
		t.Error("Hot spotting 不應退回舊的泛用差距表")
	}
}
