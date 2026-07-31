package reporter

import (
	"bytes"
	"fmt"
	"html/template"
	"path/filepath"
	"strings"
	"time"

	"elk-diagnostics/internal/diagnostic"
	"elk-diagnostics/internal/nodecontext"
)

// HTML 產出離線可渲染報告（spec-report §5）：單一檔、CSS 全內嵌、零外部 CDN、
// 用原生 <details> 折疊（免 JS）、可列印。
func HTML(r diagnostic.Report) ([]byte, error) {
	byCat := map[string][]diagnostic.Result{}
	for _, res := range r.Results {
		byCat[res.Category] = append(byCat[res.Category], res)
	}
	var groups []htmlGroup
	for _, c := range catOrder {
		if rs, ok := byCat[c]; ok {
			groups = append(groups, htmlGroup{c, catNames[c], rs})
			delete(byCat, c)
		}
	}
	for c, rs := range byCat { // 未知/新增分類，照原 key 附在後面
		groups = append(groups, htmlGroup{c, c, rs})
	}

	data := struct {
		R      diagnostic.Report
		Groups []htmlGroup
	}{r, groups}

	t, err := template.New("report").Funcs(htmlFuncs).Parse(htmlTmpl)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

type htmlGroup struct {
	Key     string
	Name    string
	Results []diagnostic.Result
}

var catOrder = []string{"cluster", "capacity", "data", "management", "performance", "snapshot", "node"}
var catNames = map[string]string{
	"cluster": "叢集", "capacity": "容量", "data": "資料",
	"management": "管理", "performance": "效能", "snapshot": "快照", "node": "節點環境診斷",
}

var htmlFuncs = template.FuncMap{
	"cls": func(s diagnostic.Status) string { return string(s) },
	"badge": func(s diagnostic.Status) string {
		switch s {
		case diagnostic.StatusPass:
			return "✅"
		case diagnostic.StatusWarning:
			return "⚠️"
		case diagnostic.StatusCritical:
			return "❌"
		case diagnostic.StatusSkipped:
			return "⏭️"
		default:
			return "❓"
		}
	},
	"statusText": func(s diagnostic.Status) string {
		switch s {
		case diagnostic.StatusPass:
			return "正常"
		case diagnostic.StatusWarning:
			return "注意"
		case diagnostic.StatusCritical:
			return "嚴重"
		case diagnostic.StatusSkipped:
			return "略過"
		default:
			return "無法判定"
		}
	},
	"statusLabel": func(s diagnostic.Status) string {
		switch s {
		case diagnostic.StatusPass:
			return "PASS"
		case diagnostic.StatusWarning:
			return "WARNING"
		case diagnostic.StatusCritical:
			return "CRITICAL"
		case diagnostic.StatusSkipped:
			return "SKIPPED"
		default:
			return "UNKNOWN"
		}
	},
	"isOpen": func(s diagnostic.Status) bool { return s != diagnostic.StatusPass && s != diagnostic.StatusSkipped },
	// isBundleHost 判斷本次分析是否來自 --from-bundle：Host 欄位在 bundle 模式固定帶
	// "(bundle) " 前綴（見 check.go）。只有 bundle 模式才需要提示「未含採集時間」，
	// 連線模式本來就沒有採集/分析時間差的問題。
	"isBundleHost": func(h string) bool { return strings.HasPrefix(h, "(bundle) ") },
	"clusterName": func(v string) string {
		if strings.TrimSpace(v) == "" {
			return "未提供叢集名稱"
		}
		return v
	},
	"analysisMode": func(mode, host string) string {
		switch {
		case strings.HasPrefix(host, "(bundle) "):
			return "Bundle 離線分析"
		case strings.HasPrefix(host, "(from-file) "):
			return "單一檔案分析"
		case mode == "check":
			return "Live 直接診斷"
		case mode == "diagnose":
			return "症狀診斷"
		default:
			return mode
		}
	},
	"localTime": func(v string) string {
		parsed, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return v
		}
		local := parsed.In(time.Local)
		_, offset := local.Zone()
		sign := "+"
		if offset < 0 {
			sign = "-"
			offset = -offset
		}
		return fmt.Sprintf("%s（UTC%s%02d:%02d）",
			local.Format("2006-01-02 15:04:05"),
			sign, offset/3600, offset%3600/60)
	},
	"sourcePath": func(host string) string {
		for _, prefix := range []string{"(bundle) ", "(from-file) "} {
			host = strings.TrimPrefix(host, prefix)
		}
		return host
	},
	"sourceShort": func(host string) string {
		if !strings.HasPrefix(host, "(bundle) ") && !strings.HasPrefix(host, "(from-file) ") {
			return host
		}
		path := filepath.ToSlash(strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(host, "(bundle) "), "(from-file) ")))
		parts := strings.Split(strings.TrimRight(path, "/"), "/")
		if len(parts) >= 2 {
			return strings.Join(parts[len(parts)-2:], "/")
		}
		return path
	},
	"coverage": func(c nodecontext.Coverage) string {
		if !c.Available {
			return fmt.Sprintf("不可驗證（returned=%d）", c.Returned)
		}
		return fmt.Sprintf("%d/%d 成功，%d 失敗，%d 回傳", c.Successful, c.Total, c.Failed, c.Returned)
	},
	"coverageClass": func(c nodecontext.Coverage) string {
		if c.Complete() {
			return "complete"
		}
		return "incomplete"
	},
	"roles": func(v []string) string {
		if len(v) == 0 {
			return "—"
		}
		return strings.Join(v, ", ")
	},
	"rolePreview": func(v []string) []string {
		if len(v) <= 3 {
			return v
		}
		priority := []string{
			"master", "data_hot", "data_warm", "data_cold",
			"data_frozen", "data_content", "data", "ingest",
		}
		seen := make(map[string]bool, len(v))
		preview := make([]string, 0, 3)
		for _, wanted := range priority {
			for _, role := range v {
				if role == wanted && !seen[role] {
					preview = append(preview, role)
					seen[role] = true
					break
				}
			}
			if len(preview) == 3 {
				return preview
			}
		}
		for _, role := range v {
			if !seen[role] {
				preview = append(preview, role)
				seen[role] = true
			}
			if len(preview) == 3 {
				break
			}
		}
		return preview
	},
	"extraRoleCount": func(v []string) int {
		if len(v) <= 3 {
			return 0
		}
		return len(v) - 3
	},
	"nodeName": func(n nodecontext.Node) string {
		if n.Name != "" {
			return n.Name
		}
		return n.ID
	},
	"intp": func(v *int) string {
		if v == nil {
			return "—"
		}
		return fmt.Sprintf("%d", *v)
	},
	"i64p": func(v *int64) string {
		if v == nil {
			return "—"
		}
		return fmt.Sprintf("%d", *v)
	},
	"pct": func(v *int) string {
		if v == nil {
			return "—"
		}
		return fmt.Sprintf("%d%%", *v)
	},
	"load": func(v *float64) string {
		if v == nil {
			return "—"
		}
		return fmt.Sprintf("%.2f", *v)
	},
	"boolp": func(v *bool) string {
		if v == nil {
			return "—"
		}
		if *v {
			return "是"
		}
		return "否"
	},
	"bytesI": func(v *int64) string {
		if v == nil {
			return "—"
		}
		return humanBytes(uint64(*v))
	},
	"bytesU": func(v *uint64) string {
		if v == nil {
			return "—"
		}
		return humanBytes(*v)
	},
	"fdRatio": func(open, max *int64) string {
		if open == nil || max == nil || *max <= 0 {
			return "—"
		}
		return fmt.Sprintf("%d/%d（%d%%）", *open, *max, 100**open / *max)
	},
	"metricClass": func(kind string, value *int) string {
		if value == nil {
			return "metric-unknown"
		}
		switch kind {
		case "cpu", "heap":
			switch {
			case *value >= 95:
				return "metric-critical"
			case *value >= 85:
				return "metric-warning"
			}
		}
		return ""
	},
	"swapClass": func(used *int64) string {
		if used == nil {
			return "metric-unknown"
		}
		if *used > 0 {
			return "metric-warning"
		}
		return ""
	},
	"fdClass": func(open, max *int64) string {
		if open == nil || max == nil || *max <= 0 {
			return "metric-unknown"
		}
		pct := 100 * *open / *max
		switch {
		case pct >= 90:
			return "metric-critical"
		case pct >= 80:
			return "metric-warning"
		default:
			return ""
		}
	},
	"memoryRatio": func(usage, limit *uint64) string {
		if usage == nil || limit == nil || *limit == 0 {
			return "—"
		}
		return fmt.Sprintf("%s / %s（%.0f%%）", humanBytes(*usage), humanBytes(*limit), 100*float64(*usage)/float64(*limit))
	},
}

func humanBytes(v uint64) string {
	const unit = 1024
	if v < unit {
		return fmt.Sprintf("%d B", v)
	}
	div, exp := uint64(unit), 0
	for n := v / unit; n >= unit && exp < 5; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(v)/float64(div), "KMGTPE"[exp])
}

const htmlTmpl = `<!DOCTYPE html>
<html lang="zh-Hant">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Elasticsearch 叢集健康診斷報告</title>
<style>
  :root{--pass:#2e7d32;--warning:#ed6c02;--critical:#c62828;--skipped:#757575;--unknown:#455a64;}
  *{box-sizing:border-box}
  body{font-family:-apple-system,"Segoe UI","Microsoft JhengHei",sans-serif;margin:0;color:#1a1a1a;background:#f5f5f5;line-height:1.6}
  .wrap{max-width:1180px;margin:0 auto;padding:24px}
  header{background:#fff;border:1px solid #e0e0e0;border-radius:8px;padding:20px}
  .header-title{display:flex;align-items:flex-start;justify-content:space-between;gap:16px}
  header h1{margin:0;font-size:24px;line-height:1.35}
  .report-status{flex:none;border-radius:999px;padding:4px 12px;font-size:12px;font-weight:700;letter-spacing:.03em}
  .report-status.pass{background:#e8f5e9;color:var(--pass)}.report-status.warning{background:#fff3e0;color:#a04b00}
  .report-status.critical{background:#ffebee;color:var(--critical)}.report-status.skipped{background:#f0f0f0;color:var(--skipped)}
  .report-status.unknown{background:#eceff1;color:var(--unknown)}
  .cluster-name{font-size:18px;font-weight:650;margin-top:12px}
  .cluster-facts{display:flex;flex-wrap:wrap;gap:0;margin-top:2px;color:#555;font-size:13px}
  .cluster-facts span+span::before{content:"·";margin:0 8px;color:#aaa}
  .meta-grid{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:10px 18px;margin-top:16px;padding-top:14px;border-top:1px solid #eee}
  .meta-item{min-width:0}
  .meta-label{display:block;color:#777;font-size:11px}
  .meta-value{display:block;font-size:13px;font-weight:600;overflow-wrap:anywhere}
  .technical-info{margin-top:12px;border-top:1px solid #eee;padding-top:8px}
  .technical-info>summary{padding:2px 0;color:#555;font-size:12px;font-weight:600}
  .technical-grid{display:grid;grid-template-columns:110px minmax(0,1fr);gap:4px 12px;margin:8px 0 0;font-size:12px}
  .technical-grid dt{color:#777}.technical-grid dd{margin:0;overflow-wrap:anywhere}
  .banner{margin:16px 0;padding:16px 20px;border-radius:8px;color:#fff;font-size:18px;font-weight:700;display:flex;justify-content:space-between;align-items:center}
  .banner.pass{background:var(--pass)}.banner.warning{background:var(--warning)}.banner.critical{background:var(--critical)}.banner.unknown{background:var(--unknown)}
  .counts{font-size:13px;font-weight:400}
  .counts b{font-weight:700}
  .version-notice{margin:16px 0;padding:10px 16px;background:#fff8e1;border:1px solid #ffca28;border-radius:6px;color:#795548;font-size:13px}
  .hints{margin:16px 0;padding:10px 16px;background:#fff8e1;border:1px solid #ffe082;border-radius:6px}
  .hints h4{margin:0 0 6px;font-size:13px;color:#795548}
  .hints ul{margin:0;padding-left:20px}
  .hints li{font-size:14px;margin:2px 0}
	h2{font-size:17px;margin:24px 0 8px;padding-bottom:4px;border-bottom:2px solid #ddd}
  .section-en{font-size:12px;font-weight:400;color:#777}
	.node-coverage{background:#fff;border:1px solid #e0e0e0;border-radius:8px;padding:12px 14px;font-size:13px;margin:8px 0}
  .coverage-line{display:flex;flex-wrap:wrap;gap:8px 14px;align-items:center}
  .coverage-title{font-weight:700}
  .coverage-badge{border-radius:999px;padding:2px 9px;font-size:12px;background:#f5f5f5}
  .coverage-badge.complete{background:#e8f5e9;color:var(--pass)}
  .coverage-badge.incomplete{background:#eceff1;color:var(--unknown)}
	.node-memory-help{margin:8px 0 0;border-top:1px solid #eee;padding-top:6px}
  .node-memory-help>summary{padding:2px 0;color:#555;font-size:12px;font-weight:600}
  .node-memory-note{margin:5px 0 0;color:#555;font-size:12px}
	.node-issues{color:var(--unknown);margin:6px 0 0;padding-left:20px}
  .node-overview-wrap{overflow-x:auto;background:#fff;border:1px solid #d8dee4;border-radius:8px;margin-top:10px}
  .node-overview{width:100%;border-collapse:collapse;font-size:13px;min-width:820px}
  .node-overview th,.node-overview td{padding:11px 12px;text-align:left;border-bottom:1px solid #e8edf2;vertical-align:middle}
  .node-overview th{background:#f6f8fa;color:#555;font-size:11px;text-transform:uppercase;letter-spacing:.03em;white-space:nowrap}
  .node-overview tbody tr:last-child td{border-bottom:0}
  .node-overview tbody tr:hover{background:#fafbfd}
  .node-overview .node-name-cell{font-weight:700;white-space:nowrap}
  .roles-cell{min-width:230px}
  .role-chip{display:inline-block;margin:2px 3px 2px 0;padding:1px 7px;border-radius:999px;background:#edf2f7;color:#34495e;font-size:11px;white-space:nowrap}
  .role-more{background:#e8eaf6;color:#3949ab}
  .metric-value{font-variant-numeric:tabular-nums;white-space:nowrap}
  .metric-warning{color:#a04b00;font-weight:700;background:#fff3e0;border-radius:4px;padding:1px 5px}
  .metric-critical{color:var(--critical);font-weight:700;background:#ffebee;border-radius:4px;padding:1px 5px}
  .metric-unknown{color:var(--unknown)}
  .node-mobile-list{display:none}
  .node-mobile-card{background:#fff;border:1px solid #d8dee4;border-radius:8px;margin:8px 0;padding:12px 14px}
  .node-mobile-head{display:flex;justify-content:space-between;gap:8px;align-items:flex-start}
  .node-mobile-name{font-weight:700}
  .node-mobile-roles{margin-top:4px}
  .node-metrics{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:8px;margin-top:10px}
  .node-metric{background:#f7f9fb;border-radius:6px;padding:7px 9px}
  .node-metric-label{display:block;color:#777;font-size:10px}
  .node-metric-value{display:block;font-size:14px;font-weight:650}
  .node-technical-details{margin-top:10px;background:#fff;border:1px solid #e0e0e0;border-radius:8px}
  .node-technical-details>summary{font-size:12px;color:#555}
  .node-technical-body{padding:0 12px 8px}
	.node-card{background:#fff;border:1px solid #d8dee4;border-radius:6px;margin:8px 0;overflow:hidden}
	.node-card summary{background:#f8fafc}
	.node-body{padding:10px 14px 14px;border-top:1px solid #e8e8e8}
	.node-body h4{font-size:13px;margin:12px 0 4px}
	.node-grid{display:grid;grid-template-columns:180px 1fr;gap:3px 12px;font-size:13px}
	.node-grid dt{font-weight:600;color:#444}.node-grid dd{margin:0;word-break:break-word}
	.node-table{width:100%;border-collapse:collapse;font-size:12px}.node-table th,.node-table td{border:1px solid #ddd;padding:4px 6px;text-align:left}.node-table th{background:#f5f5f5}
  .card{background:#fff;border:1px solid #e0e0e0;border-left-width:5px;border-radius:6px;margin:8px 0;overflow:hidden}
  .card.pass{border-left-color:var(--pass)}.card.warning{border-left-color:var(--warning)}.card.critical{border-left-color:var(--critical)}.card.skipped{border-left-color:var(--skipped)}.card.unknown{border-left-color:var(--unknown)}
  .status-label{display:inline-block;margin-right:4px;padding:1px 6px;border-radius:4px;font-size:10px;font-weight:700;letter-spacing:.04em;vertical-align:1px}
  .card.pass .status-label{background:#e8f5e9;color:var(--pass)}
  .card.warning .status-label{background:#fff3e0;color:#a04b00}
  .card.critical .status-label{background:#ffebee;color:var(--critical)}
  .card.skipped .status-label{background:#f0f0f0;color:var(--skipped)}
  .card.unknown .status-label{background:#eceff1;color:var(--unknown)}
  summary{padding:10px 14px;cursor:pointer;font-weight:600;list-style:none}
  summary::-webkit-details-marker{display:none}
  summary .sm{font-weight:400;color:#444}
  summary .src{float:right;font-size:11px;color:#888;font-weight:400}
  .body{padding:4px 16px 14px;border-top:1px solid #f0f0f0}
  .body h4{margin:12px 0 4px;font-size:13px;color:#333}
  .body ul{margin:0;padding-left:20px}
  .body li{margin:2px 0;font-size:14px}
  code{background:#f0f0f0;padding:1px 6px;border-radius:4px;font-size:13px}
  .extra,.vw{font-size:13px;color:var(--warning);margin:8px 0 0}
  a{color:#1565c0;word-break:break-all}
  footer{margin:24px 0;padding:14px;background:#eceff1;border-radius:6px;font-size:12px;color:#555}
  @media (max-width:760px){
    .wrap{padding:12px}
    header{padding:16px}
    .header-title{display:block}
    .report-status{display:inline-block;margin-top:8px}
    .meta-grid{grid-template-columns:repeat(2,minmax(0,1fr))}
    .banner{align-items:flex-start;gap:8px;flex-direction:column}
    .node-overview-wrap{display:none}
    .node-mobile-list{display:block}
  }
  @media (max-width:460px){
    header h1{font-size:20px}
    .meta-grid{grid-template-columns:1fr}
    .node-metrics{grid-template-columns:1fr 1fr}
  }
  @media print{
    body{background:#fff}.card,.node-overview-wrap{break-inside:avoid}summary{cursor:default}
    .wrap{max-width:none;padding:0}.node-mobile-list{display:none}.node-overview-wrap{display:block}
  }
</style>
</head>
<body>
<div class="wrap">
<header>
  <div class="header-title">
    <h1>Elasticsearch 叢集健康診斷報告</h1>
    <span class="report-status {{cls .R.OverallStatus}}">{{statusLabel .R.OverallStatus}} · {{statusText .R.OverallStatus}}</span>
  </div>
  <div class="cluster-name">{{clusterName .R.Meta.Cluster.Name}}</div>
  <div class="cluster-facts">
    <span>ES {{.R.Meta.Cluster.ESVersion}}</span>
    {{with .R.NodeContext}}<span>{{len .Nodes}} 個節點</span>{{end}}
    <span>{{analysisMode .R.Meta.Mode .R.Meta.Cluster.Host}}</span>
  </div>
  <div class="meta-grid">
    {{if .R.Meta.CollectedAt}}
    <div class="meta-item"><span class="meta-label">資料採集時間</span><time class="meta-value" datetime="{{.R.Meta.CollectedAt}}">{{localTime .R.Meta.CollectedAt}}</time></div>
    {{else if isBundleHost .R.Meta.Cluster.Host}}
    <div class="meta-item"><span class="meta-label">資料採集時間</span><span class="meta-value vw">bundle 未含採集時間（舊版採集腳本）</span></div>
    {{end}}
    <div class="meta-item"><span class="meta-label">報告產生時間</span><time class="meta-value" datetime="{{.R.Meta.GeneratedAt}}">{{localTime .R.Meta.GeneratedAt}}</time></div>
    <div class="meta-item"><span class="meta-label">工具版本</span><span class="meta-value">elk-diagnostics {{.R.Meta.ToolVersion}}</span></div>
    {{if .R.Meta.CollectScriptVersion}}<div class="meta-item"><span class="meta-label">採集器版本</span><span class="meta-value">{{.R.Meta.CollectScriptVersion}}</span></div>{{end}}
  </div>
  <details class="technical-info">
    <summary>技術資訊</summary>
    <dl class="technical-grid">
      <dt>診斷模式</dt><dd>{{analysisMode .R.Meta.Mode .R.Meta.Cluster.Host}}</dd>
      <dt>資料來源</dt><dd><span title="{{sourcePath .R.Meta.Cluster.Host}}">{{sourceShort .R.Meta.Cluster.Host}}</span></dd>
      <dt>完整來源</dt><dd>{{sourcePath .R.Meta.Cluster.Host}}</dd>
    </dl>
  </details>
</header>

<div class="banner {{cls .R.OverallStatus}}">
  <span>整體狀態：{{statusText .R.OverallStatus}}</span>
  <span class="counts">✅ <b>{{.R.Summary.Pass}}</b>　⚠️ <b>{{.R.Summary.Warning}}</b>　❌ <b>{{.R.Summary.Critical}}</b>　⏭️ <b>{{.R.Summary.Skipped}}</b>　❓ <b>{{.R.Summary.Unknown}}</b></span>
</div>

{{if .R.VersionNotice}}
<div class="version-notice">⚠ {{.R.VersionNotice}}</div>
{{end}}

{{if .R.SuggestedSymptoms}}
<div class="hints">
  <h4>建議進一步排查</h4>
  <ul>
    {{range .R.SuggestedSymptoms}}<li>{{.Reason}} → <code>diagnose --symptom {{.Symptom}}</code></li>{{end}}
  </ul>
</div>
{{end}}

{{with .R.NodeContext}}
<h2>節點概況 <span class="section-en">Node Context</span></h2>
<div class="node-coverage">
  <div class="coverage-line">
    <span class="coverage-title">API 覆蓋</span>
    <span class="coverage-badge {{coverageClass .StatsCoverage}}"><strong>Nodes Stats</strong> {{coverage .StatsCoverage}}</span>
    <span class="coverage-badge {{coverageClass .InfoCoverage}}"><strong>Nodes Info</strong> {{coverage .InfoCoverage}}</span>
  </div>
  <details class="node-memory-help">
    <summary>ⓘ 記憶體指標判讀</summary>
    <p class="node-memory-note">OS RAM 是主機／容器層的單次使用率快照，可能包含可回收的 filesystem cache；不等於 JVM Heap，單次高值不單獨視為記憶體壓力。表格色彩只協助快速閱讀，正式判定仍以診斷結果卡為準。</p>
  </details>
  {{if .Issues}}<ul class="node-issues">{{range .Issues}}<li>{{.}}</li>{{end}}</ul>{{end}}
</div>
<div class="node-overview-wrap">
  <table class="node-overview">
    <thead><tr><th>節點</th><th>角色</th><th>CPU</th><th>OS RAM*</th><th>JVM Heap</th><th>Swap</th><th>FD</th></tr></thead>
    <tbody>
    {{range .Nodes}}
      <tr>
        <td class="node-name-cell">{{nodeName .}}</td>
        <td class="roles-cell" title="{{roles .Roles}}">{{range rolePreview .Roles}}<span class="role-chip">{{.}}</span>{{end}}{{if extraRoleCount .Roles}}<span class="role-chip role-more">+{{extraRoleCount .Roles}}</span>{{end}}</td>
        <td><span class="metric-value {{metricClass "cpu" .OS.CPUPercent}}">{{pct .OS.CPUPercent}}</span></td>
        <td><span class="metric-value">{{pct .OS.Memory.UsedPct}}</span></td>
        <td><span class="metric-value {{metricClass "heap" .JVM.HeapUsedPct}}">{{pct .JVM.HeapUsedPct}}</span></td>
        <td><span class="metric-value {{swapClass .OS.Swap.UsedBytes}}">{{bytesI .OS.Swap.UsedBytes}}</span></td>
        <td><span class="metric-value {{fdClass .Process.OpenFileDescriptors .Process.MaxFileDescriptors}}">{{fdRatio .Process.OpenFileDescriptors .Process.MaxFileDescriptors}}</span></td>
      </tr>
    {{end}}
    </tbody>
  </table>
</div>
<div class="node-mobile-list">
{{range .Nodes}}
  <div class="node-mobile-card">
    <div class="node-mobile-head"><span class="node-mobile-name">{{nodeName .}}</span></div>
    <div class="node-mobile-roles" title="{{roles .Roles}}">{{range rolePreview .Roles}}<span class="role-chip">{{.}}</span>{{end}}{{if extraRoleCount .Roles}}<span class="role-chip role-more">+{{extraRoleCount .Roles}}</span>{{end}}</div>
    <div class="node-metrics">
      <div class="node-metric"><span class="node-metric-label">CPU</span><span class="node-metric-value {{metricClass "cpu" .OS.CPUPercent}}">{{pct .OS.CPUPercent}}</span></div>
      <div class="node-metric"><span class="node-metric-label">OS RAM*</span><span class="node-metric-value">{{pct .OS.Memory.UsedPct}}</span></div>
      <div class="node-metric"><span class="node-metric-label">JVM Heap</span><span class="node-metric-value {{metricClass "heap" .JVM.HeapUsedPct}}">{{pct .JVM.HeapUsedPct}}</span></div>
      <div class="node-metric"><span class="node-metric-label">Swap</span><span class="node-metric-value {{swapClass .OS.Swap.UsedBytes}}">{{bytesI .OS.Swap.UsedBytes}}</span></div>
      <div class="node-metric"><span class="node-metric-label">FD</span><span class="node-metric-value {{fdClass .Process.OpenFileDescriptors .Process.MaxFileDescriptors}}">{{fdRatio .Process.OpenFileDescriptors .Process.MaxFileDescriptors}}</span></div>
    </div>
  </div>
{{end}}
</div>
<details class="node-technical-details">
  <summary>展開節點技術明細</summary>
  <div class="node-technical-body">
{{range .Nodes}}
<details class="node-card">
  <summary>{{nodeName .}} <span class="sm">— {{roles .Roles}}</span></summary>
  <div class="node-body">
    <dl class="node-grid">
      <dt>Node ID / 資料來源</dt><dd>{{.ID}} ｜ Stats={{.StatsAvailable}} Info={{.InfoAvailable}}</dd>
      <dt>OS</dt><dd>{{if .OS.PrettyName}}{{.OS.PrettyName}}{{else}}{{.OS.Name}}{{end}} {{.OS.Version}} ｜ {{.OS.Architecture}} ｜ processors available={{intp .OS.AvailableProcessors}} allocated={{intp .OS.AllocatedProcessors}}</dd>
      <dt>OS CPU / load</dt><dd>{{pct .OS.CPUPercent}} ｜ load 1m={{load .OS.Load1m}} 5m={{load .OS.Load5m}} 15m={{load .OS.Load15m}}</dd>
      <dt>OS RAM（含 cache）</dt><dd>used={{bytesI .OS.Memory.UsedBytes}} / total={{bytesI .OS.Memory.TotalBytes}}（{{pct .OS.Memory.UsedPct}}）｜ swap={{bytesI .OS.Swap.UsedBytes}} / {{bytesI .OS.Swap.TotalBytes}}</dd>
      <dt>Process</dt><dd>PID={{i64p .Process.PID}} ｜ memory locked={{boolp .Process.MemoryLocked}} ｜ CPU={{pct .Process.CPUPercent}} ｜ virtual={{bytesI .Process.TotalVirtualMemoryBytes}} ｜ FD={{fdRatio .Process.OpenFileDescriptors .Process.MaxFileDescriptors}}</dd>
      <dt>Cgroup memory</dt><dd>{{memoryRatio .OS.Cgroup.Memory.UsageBytes .OS.Cgroup.Memory.LimitBytes}} ｜ unlimited={{boolp .OS.Cgroup.Memory.LimitUnlimited}}</dd>
      <dt>Cgroup CPU 累積值</dt><dd>usage_ns={{i64p .OS.Cgroup.CPU.UsageNanos}} ｜ throttled={{i64p .OS.Cgroup.CPU.TimesThrottled}} 次 / {{i64p .OS.Cgroup.CPU.TimeThrottledNanos}} ns</dd>
      <dt>Filesystem</dt><dd>available={{bytesI .Filesystem.AvailableBytes}} / total={{bytesI .Filesystem.TotalBytes}}</dd>
      <dt>JVM Heap</dt><dd>used={{bytesI .JVM.HeapUsedBytes}} / max={{bytesI .JVM.HeapMaxBytes}}（{{pct .JVM.HeapUsedPct}}）｜ old={{bytesI .JVM.OldUsedBytes}} / {{bytesI .JVM.OldMaxBytes}} ｜ uptime_ms={{i64p .JVM.UptimeMillis}}</dd>
    </dl>
    {{if .Filesystem.DataPaths}}<h4>Data paths</h4><table class="node-table"><thead><tr><th>Path</th><th>Mount</th><th>Type</th><th>Available / Total</th></tr></thead><tbody>{{range .Filesystem.DataPaths}}<tr><td>{{.Path}}</td><td>{{.Mount}}</td><td>{{.Type}}</td><td>{{bytesI .AvailableBytes}} / {{bytesI .TotalBytes}}</td></tr>{{end}}</tbody></table>{{end}}
    {{if .Filesystem.Devices}}<h4>Filesystem I/O 累積值</h4><table class="node-table"><thead><tr><th>Device</th><th>Operations</th><th>Read ops / KiB</th><th>Write ops / KiB</th><th>I/O time ms</th></tr></thead><tbody>{{range .Filesystem.Devices}}<tr><td>{{.Name}}</td><td>{{i64p .Operations}}</td><td>{{i64p .ReadOperations}} / {{i64p .ReadKilobytes}}</td><td>{{i64p .WriteOperations}} / {{i64p .WriteKilobytes}}</td><td>{{i64p .IOTimeMillis}}</td></tr>{{end}}</tbody></table>{{end}}
    {{if .JVM.GCCollectors}}<h4>JVM GC 累積值</h4><table class="node-table"><thead><tr><th>Collector</th><th>Count</th><th>Time ms</th></tr></thead><tbody>{{range .JVM.GCCollectors}}<tr><td>{{.Name}}</td><td>{{i64p .CollectionCount}}</td><td>{{i64p .CollectionTimeMillis}}</td></tr>{{end}}</tbody></table>{{end}}
  </div>
</details>
{{end}}
</div>
</details>
{{end}}

{{range .Groups}}
<h2>{{.Name}}</h2>
{{range .Results}}
<details class="card {{cls .Status}}"{{if isOpen .Status}} open{{end}}>
  <summary>{{badge .Status}} <span class="status-label">{{statusLabel .Status}}</span> {{.Title}} <span class="sm">— {{.Summary}}</span><span class="src">{{.Source}}</span></summary>
  <div class="body">
    {{if .Findings}}<h4>發現</h4><ul>{{range .Findings}}<li>{{.}}</li>{{end}}</ul>{{end}}
    {{if .RootCauses}}<h4>可能根因（假設）</h4><ul>{{range .RootCauses}}<li>{{.}}</li>{{end}}</ul>{{end}}
    {{if .Recommendations}}<h4>建議（唯讀引導）</h4><ul>{{range .Recommendations}}<li>{{if .Cmd}}<code>{{.Cmd}}</code> — {{end}}{{.Desc}}</li>{{end}}</ul>{{end}}
    {{if .RequiresExtra}}<p class="extra">⚠ 需額外條件：{{.ExtraReason}}</p>{{end}}
    {{if .VersionWarning}}<p class="vw">版本警告：{{.VersionWarning}}</p>{{end}}
    {{if .Docs}}<h4>官方文件</h4><ul>{{range .Docs}}<li><a href="{{.}}">{{.}}</a></li>{{end}}</ul>{{end}}
  </div>
</details>
{{end}}
{{end}}

<footer>{{.R.Disclaimer}}</footer>
</div>
</body>
</html>
`
