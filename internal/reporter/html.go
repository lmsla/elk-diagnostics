package reporter

import (
	"bytes"
	"html/template"
	"strings"

	"elk-diagnostics/internal/diagnostic"
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

var catOrder = []string{"cluster", "capacity", "data", "management", "performance", "snapshot"}
var catNames = map[string]string{
	"cluster": "叢集", "capacity": "容量", "data": "資料",
	"management": "管理", "performance": "效能", "snapshot": "快照",
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
	"isOpen": func(s diagnostic.Status) bool { return s != diagnostic.StatusPass && s != diagnostic.StatusSkipped },
	// isBundleHost 判斷本次分析是否來自 --from-bundle：Host 欄位在 bundle 模式固定帶
	// "(bundle) " 前綴（見 check.go）。只有 bundle 模式才需要提示「未含採集時間」，
	// 連線模式本來就沒有採集/分析時間差的問題。
	"isBundleHost": func(h string) bool { return strings.HasPrefix(h, "(bundle) ") },
}

const htmlTmpl = `<!DOCTYPE html>
<html lang="zh-Hant">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>elk-diagnostics 診斷報告</title>
<style>
  :root{--pass:#2e7d32;--warning:#ed6c02;--critical:#c62828;--skipped:#757575;--unknown:#455a64;}
  *{box-sizing:border-box}
  body{font-family:-apple-system,"Segoe UI","Microsoft JhengHei",sans-serif;margin:0;color:#1a1a1a;background:#f5f5f5;line-height:1.6}
  .wrap{max-width:960px;margin:0 auto;padding:24px}
  header h1{margin:0 0 4px;font-size:20px}
  header .meta{color:#555;font-size:13px}
  header .meta span{margin-right:16px}
  .banner{margin:16px 0;padding:16px 20px;border-radius:8px;color:#fff;font-size:18px;font-weight:700;display:flex;justify-content:space-between;align-items:center}
  .banner.pass{background:var(--pass)}.banner.warning{background:var(--warning)}.banner.critical{background:var(--critical)}.banner.unknown{background:var(--unknown)}
  .counts{font-size:13px;font-weight:400}
  .counts b{font-weight:700}
  .hints{margin:16px 0;padding:10px 16px;background:#fff8e1;border:1px solid #ffe082;border-radius:6px}
  .hints h4{margin:0 0 6px;font-size:13px;color:#795548}
  .hints ul{margin:0;padding-left:20px}
  .hints li{font-size:14px;margin:2px 0}
  h2{font-size:15px;margin:24px 0 8px;padding-bottom:4px;border-bottom:2px solid #ddd}
  .card{background:#fff;border:1px solid #e0e0e0;border-left-width:5px;border-radius:6px;margin:8px 0;overflow:hidden}
  .card.pass{border-left-color:var(--pass)}.card.warning{border-left-color:var(--warning)}.card.critical{border-left-color:var(--critical)}.card.skipped{border-left-color:var(--skipped)}.card.unknown{border-left-color:var(--unknown)}
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
  @media print{body{background:#fff}.card{break-inside:avoid}summary{cursor:default}}
</style>
</head>
<body>
<div class="wrap">
<header>
  <h1>elk-diagnostics 診斷報告</h1>
  <div class="meta">
    <span>叢集：{{.R.Meta.Cluster.Name}}</span>
    <span>{{.R.Meta.Cluster.Host}}</span>
    <span>ES {{.R.Meta.Cluster.ESVersion}}</span>
    <span>模式：{{.R.Meta.Mode}}</span>
    <span>{{.R.Meta.GeneratedAt}}</span>
    <span>工具 {{.R.Meta.ToolVersion}}</span>
    {{if .R.Meta.CollectedAt}}<span>採集時間：{{.R.Meta.CollectedAt}}（採集腳本 {{.R.Meta.CollectScriptVersion}}）</span>
    {{else if isBundleHost .R.Meta.Cluster.Host}}<span class="vw">bundle 未含採集時間（舊版採集腳本）</span>
    {{end}}
  </div>
</header>

<div class="banner {{cls .R.OverallStatus}}">
  <span>整體狀態：{{statusText .R.OverallStatus}}</span>
  <span class="counts">✅ <b>{{.R.Summary.Pass}}</b>　⚠️ <b>{{.R.Summary.Warning}}</b>　❌ <b>{{.R.Summary.Critical}}</b>　⏭️ <b>{{.R.Summary.Skipped}}</b>　❓ <b>{{.R.Summary.Unknown}}</b></span>
</div>

{{if .R.SuggestedSymptoms}}
<div class="hints">
  <h4>建議進一步排查</h4>
  <ul>
    {{range .R.SuggestedSymptoms}}<li>{{.Reason}} → <code>diagnose --symptom {{.Symptom}}</code></li>{{end}}
  </ul>
</div>
{{end}}

{{range .Groups}}
<h2>{{.Name}}</h2>
{{range .Results}}
<details class="card {{cls .Status}}"{{if isOpen .Status}} open{{end}}>
  <summary>{{badge .Status}} {{.Title}} <span class="sm">— {{.Summary}}</span><span class="src">{{.Source}}</span></summary>
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
