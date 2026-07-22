package reporter

import (
	"bytes"
	"fmt"
	"html/template"
	"strings"

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
	"isOpen": func(s diagnostic.Status) bool { return s != diagnostic.StatusPass && s != diagnostic.StatusSkipped },
	// isBundleHost 判斷本次分析是否來自 --from-bundle：Host 欄位在 bundle 模式固定帶
	// "(bundle) " 前綴（見 check.go）。只有 bundle 模式才需要提示「未含採集時間」，
	// 連線模式本來就沒有採集/分析時間差的問題。
	"isBundleHost": func(h string) bool { return strings.HasPrefix(h, "(bundle) ") },
	"coverage": func(c nodecontext.Coverage) string {
		if !c.Available {
			return fmt.Sprintf("不可驗證（returned=%d）", c.Returned)
		}
		return fmt.Sprintf("%d/%d 成功，%d 失敗，%d 回傳", c.Successful, c.Total, c.Failed, c.Returned)
	},
	"roles": func(v []string) string {
		if len(v) == 0 {
			return "—"
		}
		return strings.Join(v, ", ")
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
  .version-notice{margin:16px 0;padding:10px 16px;background:#fff8e1;border:1px solid #ffca28;border-radius:6px;color:#795548;font-size:13px}
  .hints{margin:16px 0;padding:10px 16px;background:#fff8e1;border:1px solid #ffe082;border-radius:6px}
  .hints h4{margin:0 0 6px;font-size:13px;color:#795548}
  .hints ul{margin:0;padding-left:20px}
  .hints li{font-size:14px;margin:2px 0}
  h2{font-size:15px;margin:24px 0 8px;padding-bottom:4px;border-bottom:2px solid #ddd}
	.node-coverage{background:#fff;border:1px solid #e0e0e0;border-radius:6px;padding:10px 14px;font-size:13px;margin:8px 0}
	.node-issues{color:var(--unknown);margin:6px 0 0;padding-left:20px}
	.node-card{background:#fff;border:1px solid #d8dee4;border-radius:6px;margin:8px 0;overflow:hidden}
	.node-card summary{background:#f8fafc}
	.node-body{padding:10px 14px 14px;border-top:1px solid #e8e8e8}
	.node-body h4{font-size:13px;margin:12px 0 4px}
	.node-grid{display:grid;grid-template-columns:180px 1fr;gap:3px 12px;font-size:13px}
	.node-grid dt{font-weight:600;color:#444}.node-grid dd{margin:0;word-break:break-word}
	.node-table{width:100%;border-collapse:collapse;font-size:12px}.node-table th,.node-table td{border:1px solid #ddd;padding:4px 6px;text-align:left}.node-table th{background:#f5f5f5}
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
<h2>節點環境（Node Context）</h2>
<div class="node-coverage">
  <strong>Nodes Stats：</strong>{{coverage .StatsCoverage}}　<strong>Nodes Info：</strong>{{coverage .InfoCoverage}}
  {{if .Issues}}<ul class="node-issues">{{range .Issues}}<li>{{.}}</li>{{end}}</ul>{{end}}
</div>
{{range .Nodes}}
<details class="node-card">
  <summary>{{if .Name}}{{.Name}}{{else}}{{.ID}}{{end}} <span class="sm">— {{roles .Roles}} ｜ CPU {{pct .OS.CPUPercent}} ｜ RAM {{pct .OS.Memory.UsedPct}} ｜ Swap {{bytesI .OS.Swap.UsedBytes}} ｜ FD {{fdRatio .Process.OpenFileDescriptors .Process.MaxFileDescriptors}} ｜ Heap {{pct .JVM.HeapUsedPct}}</span></summary>
  <div class="node-body">
    <dl class="node-grid">
      <dt>Node ID / 資料來源</dt><dd>{{.ID}} ｜ Stats={{.StatsAvailable}} Info={{.InfoAvailable}}</dd>
      <dt>OS</dt><dd>{{if .OS.PrettyName}}{{.OS.PrettyName}}{{else}}{{.OS.Name}}{{end}} {{.OS.Version}} ｜ {{.OS.Architecture}} ｜ processors available={{intp .OS.AvailableProcessors}} allocated={{intp .OS.AllocatedProcessors}}</dd>
      <dt>OS CPU / load</dt><dd>{{pct .OS.CPUPercent}} ｜ load 1m={{load .OS.Load1m}} 5m={{load .OS.Load5m}} 15m={{load .OS.Load15m}}</dd>
      <dt>OS memory</dt><dd>used={{bytesI .OS.Memory.UsedBytes}} / total={{bytesI .OS.Memory.TotalBytes}}（{{pct .OS.Memory.UsedPct}}）｜ swap={{bytesI .OS.Swap.UsedBytes}} / {{bytesI .OS.Swap.TotalBytes}}</dd>
      <dt>Process</dt><dd>PID={{i64p .Process.PID}} ｜ memory locked={{boolp .Process.MemoryLocked}} ｜ CPU={{pct .Process.CPUPercent}} ｜ virtual={{bytesI .Process.TotalVirtualMemoryBytes}} ｜ FD={{fdRatio .Process.OpenFileDescriptors .Process.MaxFileDescriptors}}</dd>
      <dt>Cgroup memory</dt><dd>{{memoryRatio .OS.Cgroup.Memory.UsageBytes .OS.Cgroup.Memory.LimitBytes}} ｜ unlimited={{boolp .OS.Cgroup.Memory.LimitUnlimited}}</dd>
      <dt>Cgroup CPU 累積值</dt><dd>usage_ns={{i64p .OS.Cgroup.CPU.UsageNanos}} ｜ throttled={{i64p .OS.Cgroup.CPU.TimesThrottled}} 次 / {{i64p .OS.Cgroup.CPU.TimeThrottledNanos}} ns</dd>
      <dt>Filesystem</dt><dd>available={{bytesI .Filesystem.AvailableBytes}} / total={{bytesI .Filesystem.TotalBytes}}</dd>
      <dt>JVM</dt><dd>heap={{bytesI .JVM.HeapUsedBytes}} / {{bytesI .JVM.HeapMaxBytes}}（{{pct .JVM.HeapUsedPct}}）｜ old={{bytesI .JVM.OldUsedBytes}} / {{bytesI .JVM.OldMaxBytes}} ｜ uptime_ms={{i64p .JVM.UptimeMillis}}</dd>
    </dl>
    {{if .Filesystem.DataPaths}}<h4>Data paths</h4><table class="node-table"><thead><tr><th>Path</th><th>Mount</th><th>Type</th><th>Available / Total</th></tr></thead><tbody>{{range .Filesystem.DataPaths}}<tr><td>{{.Path}}</td><td>{{.Mount}}</td><td>{{.Type}}</td><td>{{bytesI .AvailableBytes}} / {{bytesI .TotalBytes}}</td></tr>{{end}}</tbody></table>{{end}}
    {{if .Filesystem.Devices}}<h4>Filesystem I/O 累積值</h4><table class="node-table"><thead><tr><th>Device</th><th>Operations</th><th>Read ops / KiB</th><th>Write ops / KiB</th><th>I/O time ms</th></tr></thead><tbody>{{range .Filesystem.Devices}}<tr><td>{{.Name}}</td><td>{{i64p .Operations}}</td><td>{{i64p .ReadOperations}} / {{i64p .ReadKilobytes}}</td><td>{{i64p .WriteOperations}} / {{i64p .WriteKilobytes}}</td><td>{{i64p .IOTimeMillis}}</td></tr>{{end}}</tbody></table>{{end}}
    {{if .JVM.GCCollectors}}<h4>JVM GC 累積值</h4><table class="node-table"><thead><tr><th>Collector</th><th>Count</th><th>Time ms</th></tr></thead><tbody>{{range .JVM.GCCollectors}}<tr><td>{{.Name}}</td><td>{{i64p .CollectionCount}}</td><td>{{i64p .CollectionTimeMillis}}</td></tr>{{end}}</tbody></table>{{end}}
  </div>
</details>
{{end}}
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
