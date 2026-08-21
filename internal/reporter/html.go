package reporter

import (
	"bytes"
	_ "embed"
	"fmt"
	"html/template"
	"math"
	"net/url"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"elk-diagnostics/internal/diagnostic"
	"elk-diagnostics/internal/nodecontext"
)

//go:embed webcomm_logo.svg
var webcommLogoSVG string

// HTML 產出離線可渲染報告（診斷報告規格 §5）：單一檔、CSS 全內嵌、零外部 CDN、
// 用原生 <details> 折疊（免 JS）、可列印。
func HTML(r diagnostic.Report) ([]byte, error) {
	var esResults, kibanaResults, logstashResults []diagnostic.Result
	for _, res := range r.Results {
		if res.Category == "service" {
			switch {
			case strings.HasPrefix(res.ID, "logstash_"):
				logstashResults = append(logstashResults, res)
			default:
				// 既有 Kibana 結果與未帶服務前綴的舊 bundle 都歸入 Kibana。
				kibanaResults = append(kibanaResults, res)
			}
		} else {
			esResults = append(esResults, res)
		}
	}
	esGroups := categoryGroups(esResults)
	kibanaGroups := categoryGroups(kibanaResults)
	logstashGroups := categoryGroups(logstashResults)
	esStart := 1
	if r.NodeContext != nil {
		esStart = 2
	}

	data := struct {
		R              diagnostic.Report
		ESGroups       []htmlGroup
		KibanaGroups   []htmlGroup
		LogstashGroups []htmlGroup
		ES             htmlGroupSet
		Kibana         htmlGroupSet
		Logstash       htmlGroupSet
	}{
		R:              r,
		ESGroups:       esGroups,
		KibanaGroups:   kibanaGroups,
		LogstashGroups: logstashGroups,
		ES:             htmlGroupSet{Prefix: "es", Start: esStart, Groups: esGroups},
		Kibana:         htmlGroupSet{Prefix: "kibana", Start: 1, Groups: kibanaGroups},
		Logstash:       htmlGroupSet{Prefix: "logstash", Start: 1, Groups: logstashGroups},
	}

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

func categoryGroups(results []diagnostic.Result) []htmlGroup {
	byCat := map[string][]diagnostic.Result{}
	for _, res := range results {
		byCat[res.Category] = append(byCat[res.Category], res)
	}
	var groups []htmlGroup
	for _, c := range catOrder {
		if rs, ok := byCat[c]; ok {
			groups = append(groups, htmlGroup{c, catNames[c], rs})
			delete(byCat, c)
		}
	}
	keys := make([]string, 0, len(byCat))
	for c := range byCat {
		keys = append(keys, c)
	}
	sort.Strings(keys)
	for _, c := range keys { // 未知/新增分類，按 key 排序後附在後面
		rs := byCat[c]
		groups = append(groups, htmlGroup{c, c, rs})
	}
	return groups
}

type htmlGroup struct {
	Key     string
	Name    string
	Results []diagnostic.Result
}

type htmlGroupSet struct {
	Prefix string
	Start  int
	Groups []htmlGroup
}

func groupStatus(results []diagnostic.Result) diagnostic.Status {
	status := diagnostic.StatusPass
	for _, result := range results {
		if statusRank(result.Status) > statusRank(status) {
			status = result.Status
		}
	}
	return status
}

func serviceStatus(groups []htmlGroup) diagnostic.Status {
	status := diagnostic.StatusPass
	for _, group := range groups {
		if candidate := groupStatus(group.Results); statusRank(candidate) > statusRank(status) {
			status = candidate
		}
	}
	return status
}

func hasNonSkipped(results []diagnostic.Result) bool {
	for _, result := range results {
		if result.Status != diagnostic.StatusSkipped {
			return true
		}
	}
	return false
}

func serviceNavigable(groups []htmlGroup) bool {
	for _, group := range groups {
		if hasNonSkipped(group.Results) {
			return true
		}
	}
	return false
}

func resultSummary(results []diagnostic.Result) diagnostic.Summary {
	var summary diagnostic.Summary
	for _, result := range results {
		switch result.Status {
		case diagnostic.StatusPass:
			summary.Pass++
		case diagnostic.StatusInfo:
			summary.Info++
		case diagnostic.StatusWarning:
			summary.Warning++
		case diagnostic.StatusCritical:
			summary.Critical++
		case diagnostic.StatusSkipped:
			summary.Skipped++
		default:
			summary.Unknown++
		}
	}
	return summary
}

func serviceSummary(groups []htmlGroup) diagnostic.Summary {
	var results []diagnostic.Result
	for _, group := range groups {
		results = append(results, group.Results...)
	}
	return resultSummary(results)
}

func nodeContextStatus(snapshot *nodecontext.Snapshot) diagnostic.Status {
	if snapshot == nil {
		return diagnostic.StatusUnknown
	}
	if len(snapshot.MissingNodes) > 0 {
		return diagnostic.StatusCritical
	}
	if !snapshot.StatsCoverage.Complete() || !snapshot.InfoCoverage.Complete() {
		return diagnostic.StatusUnknown
	}
	return diagnostic.StatusPass
}

func totalSummary(summary diagnostic.Summary) int {
	return summary.Pass + summary.Info + summary.Warning + summary.Critical + summary.Skipped + summary.Unknown
}

func statusRank(status diagnostic.Status) int {
	switch status {
	case diagnostic.StatusCritical:
		return 5
	case diagnostic.StatusWarning:
		return 4
	case diagnostic.StatusUnknown:
		return 3
	case diagnostic.StatusInfo:
		return 2
	case diagnostic.StatusSkipped:
		return 1
	default:
		return 0
	}
}

func sectionID(prefix, key string) string {
	var b strings.Builder
	b.WriteString("section-")
	b.WriteString(prefix)
	b.WriteByte('-')
	lastDash := false
	for _, r := range strings.ToLower(key) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.TrimSuffix(b.String(), "-")
}

var catOrder = []string{"cluster", "capacity", "data", "management", "performance", "snapshot", "node", "security", "replication", "machine_learning", "service"}
var catNames = map[string]string{
	"cluster": "叢集", "capacity": "容量", "data": "資料",
	"management": "管理", "performance": "效能", "snapshot": "快照", "node": "節點環境診斷",
	"security": "Security", "replication": "Replication", "machine_learning": "Machine Learning", "service": "服務診斷",
}

var htmlFuncs = template.FuncMap{
	"cls": func(s diagnostic.Status) string { return string(s) },
	"badge": func(s diagnostic.Status) string {
		switch s {
		case diagnostic.StatusPass:
			return "✅"
		case diagnostic.StatusInfo:
			return "ℹ️"
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
		case diagnostic.StatusInfo:
			return "需觀察"
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
		case diagnostic.StatusInfo:
			return "INFO"
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
	"extraLead": func(s diagnostic.Status) string {
		if s == diagnostic.StatusInfo {
			return "ⓘ 需觀察"
		}
		return "⚠ 需額外條件"
	},
	"isOpen":       func(s diagnostic.Status) bool { return s != diagnostic.StatusPass && s != diagnostic.StatusSkipped },
	"groupStatus":  groupStatus,
	"groupVisible": hasNonSkipped,
	"groupSummary": resultSummary,
	"sectionID":    sectionID,
	"sectionNumber": func(index, start int) string {
		return fmt.Sprintf("%02d", index+start)
	},
	"pctOf": func(value, total int) int {
		if total <= 0 || value <= 0 {
			return 0
		}
		return int(math.Round(float64(value) * 100 / float64(total)))
	},
	"addCounts":      totalSummary,
	"serviceStatus":  serviceStatus,
	"serviceVisible": serviceNavigable,
	"serviceSummary": serviceSummary,
	"nodeStatus":     nodeContextStatus,
	"docLabel":       documentLabel,
	"statusIcon": func(status diagnostic.Status) string {
		switch status {
		case diagnostic.StatusPass:
			return "i-check-circle"
		case diagnostic.StatusInfo:
			return "i-info"
		case diagnostic.StatusWarning:
			return "i-alert-triangle"
		case diagnostic.StatusCritical:
			return "i-alert-octagon"
		case diagnostic.StatusSkipped:
			return "i-skip-forward"
		default:
			return "i-help-circle"
		}
	},
	"brandLogo": func() template.HTML { return template.HTML(webcommLogoSVG) },
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
	"joinNodes": func(v []string) string { return strings.Join(v, "、") },
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
	"measurementLabel": func(m diagnostic.Measurement) string {
		if m.Metric == "elasticsearch.node.resource.deviation_from_median" {
			switch m.Component {
			case "cpu":
				return "CPU 相對叢集中位數差距"
			case "heap.percent":
				return "JVM Heap 相對叢集中位數差距"
			case "disk.used_percent":
				return "磁碟使用率相對叢集中位數差距"
			}
		}
		if label, ok := measurementLabels[m.Metric]; ok {
			return label
		}
		return m.Metric
	},
	"measurementTarget": func(m diagnostic.Measurement) string {
		switch {
		case m.EntityName != "":
			return m.EntityName
		case m.EntityID != "":
			return m.EntityID
		case m.Component != "":
			return m.Component
		default:
			return "叢集"
		}
	},
	"measurementValue": formatMeasurementValue,
	"measurementKind": func(kind string) string {
		if kind == "counter" {
			return "累積值"
		}
		return "當下值"
	},
	"measurementKindClass": func(kind string) string {
		if kind == "counter" {
			return "counter"
		}
		return "gauge"
	},
	"hasCounter": func(values []diagnostic.Measurement) bool {
		for _, value := range values {
			if value.Kind == "counter" {
				return true
			}
		}
		return false
	},
	"hasHotspotMeasurements": hasHotspotMeasurements,
	"hotspotBaselines":       hotspotBaselines,
	"hotspotRows":            hotspotRows,
}

type hotspotBaselineRow struct {
	Label     string
	PeerGroup string
	Value     string
}

type hotspotObservationRow struct {
	Label      string
	Node       string
	PeerGroup  string
	Current    string
	Median     string
	Difference string
	Nature     string
}

func hasHotspotMeasurements(id string, values []diagnostic.Measurement) bool {
	if id != "hot_spotting" {
		return false
	}
	var current, median bool
	for _, value := range values {
		switch value.Metric {
		case "elasticsearch.node.resource.current":
			current = true
		case "elasticsearch.cluster.resource.median":
			median = true
		}
	}
	return current && median
}

func hotspotMetricLabel(component string) string {
	switch component {
	case "cpu":
		return "CPU 使用率"
	case "heap.percent":
		return "JVM Heap 使用率"
	case "disk.used_percent":
		return "磁碟使用率"
	default:
		return component
	}
}

func hotspotPeerGroup(value diagnostic.Measurement) string {
	if strings.TrimSpace(value.PeerGroup) == "" {
		return "所有節點"
	}
	return value.PeerGroup
}

func hotspotBaselines(values []diagnostic.Measurement) []hotspotBaselineRow {
	rows := make([]hotspotBaselineRow, 0)
	seen := make(map[string]bool)
	for _, value := range values {
		if value.Metric != "elasticsearch.cluster.resource.median" {
			continue
		}
		group := hotspotPeerGroup(value)
		key := group + "\x00" + value.Component
		if seen[key] {
			continue
		}
		seen[key] = true
		rows = append(rows, hotspotBaselineRow{
			Label: hotspotMetricLabel(value.Component), PeerGroup: group, Value: formatMeasurementValue(value),
		})
	}
	return rows
}

func hotspotRows(values []diagnostic.Measurement) []hotspotObservationRow {
	type observation struct {
		current, difference *diagnostic.Measurement
	}
	items := make(map[string]*observation)
	var order []string
	for i := range values {
		value := &values[i]
		if value.Metric != "elasticsearch.node.resource.current" && value.Metric != "elasticsearch.node.resource.deviation_from_median" {
			continue
		}
		group := hotspotPeerGroup(*value)
		key := group + "\x00" + value.Component + "\x00" + value.EntityName
		item, ok := items[key]
		if !ok {
			item = &observation{}
			items[key] = item
			order = append(order, key)
		}
		if value.Metric == "elasticsearch.node.resource.current" {
			item.current = value
		} else {
			item.difference = value
		}
	}
	medians := make(map[string]*diagnostic.Measurement)
	for i := range values {
		value := &values[i]
		if value.Metric != "elasticsearch.cluster.resource.median" {
			continue
		}
		key := hotspotPeerGroup(*value) + "\x00" + value.Component
		if _, ok := medians[key]; !ok {
			medians[key] = value
		}
	}
	rows := make([]hotspotObservationRow, 0, len(order))
	for _, key := range order {
		item := items[key]
		if item.current == nil {
			continue
		}
		group := hotspotPeerGroup(*item.current)
		row := hotspotObservationRow{
			Label: hotspotMetricLabel(item.current.Component), Node: item.current.EntityName, PeerGroup: group,
			Current: formatMeasurementValue(*item.current), Nature: "原始快照（同類節點不足）",
		}
		if median, ok := medians[group+"\x00"+item.current.Component]; ok {
			row.Median = formatMeasurementValue(*median)
			row.Nature = "原始快照＋衍生比較"
		}
		if item.difference != nil {
			row.Difference = formatMeasurementValue(*item.difference)
		}
		rows = append(rows, row)
	}
	return rows
}

var measurementLabels = map[string]string{
	"elasticsearch.health_report.diagnosis.count":                    "診斷原因數量",
	"elasticsearch.health_report.impact.count":                       "影響項目數量",
	"elasticsearch.health_report.affected_index.count":               "受影響 Index 數量",
	"elasticsearch.cluster.node.count":                               "叢集節點總數",
	"elasticsearch.nodes.expected":                                   "預期 ES 節點數",
	"elasticsearch.nodes.observed":                                   "目前回應 ES 節點數",
	"elasticsearch.nodes.missing":                                    "缺失 ES 節點數",
	"elasticsearch.cluster.master_eligible_node.count":               "Master-eligible 節點數",
	"elasticsearch.cluster.pending_task.count":                       "Pending task 數量",
	"elasticsearch.cluster.pending_task.max_queue_time":              "最長排隊時間",
	"elasticsearch.cluster.voting_exclusion.count":                   "Voting exclusion 數量",
	"elasticsearch.index.allocation.checked.count":                   "已檢查 Index 數量",
	"elasticsearch.index.allocation.blocked.count":                   "分配受限 Index 數量",
	"elasticsearch.index.allocation.unprobed.count":                  "無法檢查 Index 數量",
	"elasticsearch.shard.allocation.rejected_decider.count":          "拒絕分配的 Decider 數量",
	"elasticsearch.node.shard.count":                                 "節點 Shard 數量",
	"elasticsearch.node.shard.undesired":                             "非理想位置 Shard 數量",
	"elasticsearch.node.disk.used":                                   "節點磁碟使用率",
	"elasticsearch.node.resource.deviation_from_median":              "相對叢集中位數差距",
	"elasticsearch.data_tier.node.count":                             "Data tier 節點數",
	"elasticsearch.index.mapping.scanned.count":                      "已檢查 Mapping 的 Index 數量",
	"elasticsearch.index.mapping.field.max":                          "單一 Index 最大欄位數",
	"elasticsearch.index.mapping.field.count":                        "Index 欄位數",
	"elasticsearch.ingest.pipeline.processed":                        "Ingest pipeline 處理總數",
	"elasticsearch.ingest.pipeline.failed":                           "Ingest pipeline 失敗總數",
	"elasticsearch.ingest.pipeline.failure_rate":                     "Ingest pipeline 失敗率",
	"elasticsearch.index.health.scanned.count":                       "已檢查 Index 數量",
	"elasticsearch.index.health.red.count":                           "Red Index 數量",
	"elasticsearch.index.blocked.count":                              "存在 Block 的 Index 數量",
	"elasticsearch.index.replica.evaluated.count":                    "已檢查 Replica 的 Index 數量",
	"elasticsearch.index.replica.minimum":                            "最低 Replica 數",
	"elasticsearch.index.replica.zero.count":                         "零 Replica Index 數量",
	"elasticsearch.index.replica.auto_expand_all.count":              "Auto-expand 上限為 all 的 Index 數量",
	"elasticsearch.index.search_slowlog.enabled.count":               "已啟用 Search slow log 的 Index 數量",
	"elasticsearch.ilm.policy.count":                                 "ILM policy 總數",
	"elasticsearch.ilm.policy.in_use.count":                          "使用中的 ILM policy",
	"elasticsearch.ilm.error_index.count":                            "ILM ERROR Index 數量",
	"elasticsearch.ilm.migrating_index.count":                        "Tier 遷移中 Index 數量",
	"elasticsearch.slm.policy.count":                                 "SLM policy 總數",
	"elasticsearch.slm.freshness.evaluated_policy.count":             "已檢查新鮮度的 SLM policy 數量",
	"elasticsearch.slm.snapshot.taken":                               "Snapshot 成功累積數",
	"elasticsearch.slm.snapshot.failed":                              "Snapshot 失敗累積數",
	"elasticsearch.slm.snapshot.last_success_age":                    "距最後成功 Snapshot 時間",
	"elasticsearch.snapshot.repository.count":                        "Snapshot repository 總數",
	"elasticsearch.slm.missing_repository.count":                     "缺少 repository 的 SLM policy",
	"elasticsearch.snapshot.restore.active_shard.count":              "Snapshot 還原中 Shard 數量",
	"elasticsearch.data_stream.count":                                "Data stream 總數",
	"elasticsearch.data_stream.backing_index.count":                  "Backing index 數量",
	"elasticsearch.data_stream.status.count":                         "Data stream 狀態數量",
	"elasticsearch.transform.count":                                  "Transform 總數",
	"elasticsearch.transform.failed.count":                           "失敗 Transform 數量",
	"elasticsearch.remote_cluster.count":                             "Remote cluster 總數",
	"elasticsearch.remote_cluster.disconnected.count":                "未連線 Remote cluster 數量",
	"elasticsearch.deprecation.count":                                "Deprecation 總數",
	"elasticsearch.deprecation.critical.count":                       "Critical deprecation 數量",
	"elasticsearch.deprecation.warning.count":                        "Warning deprecation 數量",
	"elasticsearch.node.thread_pool.rejected":                        "Thread pool 拒絕累積數",
	"elasticsearch.node.thread_pool.completed":                       "Thread pool 完成累積數",
	"elasticsearch.node.thread_pool.queue":                           "Thread pool Queue",
	"elasticsearch.thread_pool.queue.max":                            "最大 Thread pool Queue",
	"elasticsearch.thread_pool.active.max":                           "最大 Active thread 數",
	"elasticsearch.node.jvm.old_pool.used":                           "JVM Old pool 已用",
	"elasticsearch.node.jvm.old_pool.max":                            "JVM Old pool 上限",
	"elasticsearch.node.jvm.old_pool.pressure":                       "JVM Old pool 壓力",
	"elasticsearch.node.breaker.tripped":                             "Circuit breaker 跳閘累積數",
	"elasticsearch.node.cpu":                                         "節點 CPU 使用率",
	"elasticsearch.node.allocated_processors":                        "Elasticsearch 可用處理器數",
	"elasticsearch.task.long_running.count":                          "長時間執行 Task 數量",
	"elasticsearch.task.long_running.max_duration":                   "最長 Task 執行時間",
	"elasticsearch.shard.primary.count":                              "Primary shard 數量",
	"elasticsearch.shard.primary.max_store":                          "最大 Primary shard 容量",
	"elasticsearch.shard.primary.large.count":                        "大型 Primary shard 數量",
	"elasticsearch.shard.primary.small.count":                        "小型 Primary shard 數量",
	"elasticsearch.nodes.indexing_pressure.total":                    "Indexing pressure API 節點總數",
	"elasticsearch.nodes.indexing_pressure.successful":               "Indexing pressure API 成功節點",
	"elasticsearch.nodes.indexing_pressure.failed":                   "Indexing pressure API 失敗節點",
	"elasticsearch.nodes.indexing_pressure.returned":                 "Indexing pressure API 回傳節點",
	"elasticsearch.node.indexing_pressure.combined":                  "Coordinating + Primary 使用量",
	"elasticsearch.node.indexing_pressure.replica":                   "Replica 使用量",
	"elasticsearch.node.indexing_pressure.limit":                     "Indexing pressure 上限",
	"elasticsearch.node.indexing_pressure.combined_pct":              "Coordinating + Primary 使用率",
	"elasticsearch.node.indexing_pressure.replica_pct":               "Replica 使用率",
	"elasticsearch.ccr.follower.count":                               "CCR Follower 數量",
	"elasticsearch.ccr.follower.checkpoint_lag":                      "CCR Checkpoint lag",
	"elasticsearch.ccr.follower.fatal_error.count":                   "CCR Fatal error 數量",
	"elasticsearch.ccr.follower.read_error.count":                    "CCR Read error 數量",
	"elasticsearch.ccr.auto_follow.failed_indices":                   "Auto-follow 失敗 Index 累積數",
	"elasticsearch.ccr.auto_follow.failed_remote_state":              "Remote state 失敗累積數",
	"elasticsearch.ccr.auto_follow.recent_error.count":               "近期 Auto-follow error 數量",
	"elasticsearch.ml.job.count":                                     "ML Job 數量",
	"elasticsearch.ml.datafeed.count":                                "ML Datafeed 數量",
	"elasticsearch.ml.state.count":                                   "ML 狀態數量",
	"elasticsearch.planned_shutdown.count":                           "Planned shutdown 登記數量",
	"elasticsearch.node.planned_shutdown.shard_migrations_remaining": "剩餘 Shard 遷移數",
	"elasticsearch.nodes.runtime.total":                              "Runtime API 節點總數",
	"elasticsearch.nodes.runtime.successful":                         "Runtime API 成功節點",
	"elasticsearch.nodes.runtime.failed":                             "Runtime API 失敗節點",
	"elasticsearch.nodes.runtime.returned":                           "Runtime API 回傳節點",
	"elasticsearch.node.runtime.heap_init":                           "JVM 初始 Heap",
	"elasticsearch.node.runtime.heap_max":                            "JVM 最大 Heap",
	"elasticsearch.node.runtime.plugin.count":                        "Plugin 數量",
	"elasticsearch.tls.certificate.count":                            "TLS Certificate 數量",
	"elasticsearch.tls.certificate.days_remaining":                   "TLS Certificate 剩餘天數",
	"elasticsearch.license.days_remaining":                           "License 剩餘天數",
	"elasticsearch.allocation.awareness.attribute.count":             "Awareness attribute 數量",
	"elasticsearch.allocation.awareness.data_node.count":             "Data node 數量",
	"elasticsearch.allocation.awareness.value.count":                 "Failure domain 數量",
	"elasticsearch.allocation.awareness.missing_node.count":          "缺少 Awareness attribute 的節點數",
	"elasticsearch.node.write_bottleneck.cpu":                        "寫入節點 CPU 使用率",
	"elasticsearch.node.write_bottleneck.queue":                      "Write Queue",
	"elasticsearch.node.write_bottleneck.allocated_processors":       "寫入節點可用處理器數",
	"elasticsearch.node.write_bottleneck.pool_size":                  "Write thread pool 大小",
	"elasticsearch.nodes.fielddata.total":                            "Fielddata API 節點總數",
	"elasticsearch.nodes.fielddata.successful":                       "Fielddata API 成功節點",
	"elasticsearch.nodes.fielddata.failed":                           "Fielddata API 失敗節點",
	"elasticsearch.nodes.fielddata.returned":                         "Fielddata API 回傳節點",
	"elasticsearch.node.fielddata.memory":                            "節點 Fielddata 記憶體",
	"elasticsearch.node.fielddata.evictions":                         "節點 Fielddata eviction",
	"elasticsearch.fielddata.memory":                                 "Fielddata 記憶體總量",
	"kibana.instance.count":                                          "Kibana instance 總數",
	"kibana.instance.response.status":                                "Kibana status HTTP 狀態碼",
	"kibana.instance.available.count":                                "Kibana 可用 instance 數量",
	"kibana.instance.degraded.count":                                 "Kibana 降級 instance 數量",
	"kibana.instance.unavailable.count":                              "Kibana 不可用 instance 數量",
	"kibana.instance.unknown.count":                                  "Kibana 無法判定 instance 數量",
	"kibana.process.heap.used":                                       "Kibana Heap 已用",
	"kibana.process.heap.limit":                                      "Kibana Heap 上限",
	"kibana.process.resident_set_size":                               "Kibana Resident Set Size",
	"kibana.process.uptime":                                          "Kibana 程序 uptime",
	"kibana.process.event_loop_delay":                                "Kibana Event loop delay",
	"kibana.process.event_loop_utilization":                          "Kibana Event loop utilization",
	"kibana.os.memory.used":                                          "Kibana OS 記憶體已用",
	"kibana.os.memory.total":                                         "Kibana OS 記憶體總量",
	"kibana.os.load.1m":                                              "Kibana OS Load 1m",
	"kibana.os.load.5m":                                              "Kibana OS Load 5m",
	"kibana.os.load.15m":                                             "Kibana OS Load 15m",
	"kibana.requests.total":                                          "Kibana 請求總數",
	"kibana.requests.disconnects":                                    "Kibana 連線中斷數",
	"kibana.elasticsearch.active_sockets":                            "Kibana Elasticsearch active sockets",
	"kibana.elasticsearch.idle_sockets":                              "Kibana Elasticsearch idle sockets",
	"kibana.elasticsearch.queued_requests":                           "Kibana Elasticsearch queued requests",
	"logstash.instance.count":                                        "Logstash instance 總數",
	"logstash.instance.response.status":                              "Logstash root HTTP 狀態碼",
	"logstash.instance.available.count":                              "Logstash 可用 instance 數量",
	"logstash.instance.degraded.count":                               "Logstash 降級 instance 數量",
	"logstash.instance.unavailable.count":                            "Logstash 不可用 instance 數量",
	"logstash.instance.unknown.count":                                "Logstash 無法判定 instance 數量",
	"logstash.health_report.response.status":                         "Logstash Health Report HTTP 狀態碼",
	"logstash.health_report.instance.count":                          "Logstash Health Report instance 數量",
	"logstash.health_report.indicator.count":                         "Logstash Health Report indicator 數量",
	"logstash.health_report.impact.count":                            "Logstash Health Report impact 數量",
	"logstash.health_report.skipped.count":                           "Logstash Health Report 不適用數量",
	"logstash.pipeline.sample.count":                                 "Logstash pipeline 取樣次數",
	"logstash.pipeline.count":                                        "Logstash pipeline 觀測數",
	"logstash.pipeline.events.in":                                    "Logstash pipeline 輸入事件累積數",
	"logstash.pipeline.events.out":                                   "Logstash pipeline 輸出事件累積數",
	"logstash.pipeline.queue.events":                                 "Logstash pipeline Queue 事件數",
	"logstash.pipeline.queue.size":                                   "Logstash pipeline Queue 大小",
	"logstash.pipeline.flow.input.current":                           "Logstash pipeline 目前輸入流量",
	"logstash.pipeline.flow.output.current":                          "Logstash pipeline 目前輸出流量",
}

func formatMeasurementValue(m diagnostic.Measurement) string {
	switch m.Unit {
	case "bytes":
		if m.Value < 0 {
			return fmt.Sprintf("%.0f B", m.Value)
		}
		return humanBytes(uint64(m.Value))
	case "percent":
		return fmt.Sprintf("%s%%", formatMeasurementNumber(m.Value))
	case "percentage_point":
		return fmt.Sprintf("%s 個百分點", formatMeasurementNumber(m.Value))
	case "milliseconds":
		return time.Duration(m.Value * float64(time.Millisecond)).String()
	case "nanoseconds":
		return time.Duration(m.Value).String()
	case "hours":
		return fmt.Sprintf("%s 小時", formatMeasurementNumber(m.Value))
	case "days":
		return fmt.Sprintf("%s 天", formatMeasurementNumber(m.Value))
	case "count", "":
		return formatMeasurementNumber(m.Value)
	default:
		return fmt.Sprintf("%s %s", formatMeasurementNumber(m.Value), m.Unit)
	}
}

func formatMeasurementNumber(v float64) string {
	if math.Trunc(v) == v {
		return fmt.Sprintf("%.0f", v)
	}
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.2f", v), "0"), ".")
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

func documentLabel(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return raw
	}
	cleanPath := strings.TrimRight(u.Path, "/")
	if cleanPath == "" {
		return u.Host
	}
	label := path.Base(cleanPath)
	if label == "." || label == "/" || label == "" {
		return u.Host
	}
	return label
}

const htmlTmpl = `{{define "diagnostic-results"}}
{{range .}}
<details class="card {{cls .Status}} check" data-status="{{cls .Status}}"{{if isOpen .Status}} open{{end}}>
  <summary>
    <svg class="ic ic-16 chev"><use href="#i-chevron-right"/></svg>
    <span class="check-head"><span class="status-label">{{statusLabel .Status}}</span><span class="check-title">{{.Title}}</span><span class="check-src">{{.Source}}</span></span>
    <span class="check-desc">{{.Summary}}</span>
  </summary>
  <div class="body check-body">
    {{if .Findings}}<h4>發現</h4><ul>{{range .Findings}}<li>{{.}}</li>{{end}}</ul>{{end}}
    {{if hasHotspotMeasurements .ID .Measurements}}
    <h4>本次比較基準</h4><div class="measurement-wrap"><table class="measurement-table hotspot-table"><thead><tr><th>指標</th><th>比較群組</th><th>同類節點中位數</th></tr></thead><tbody>{{range hotspotBaselines .Measurements}}<tr><td>{{.Label}}</td><td>{{.PeerGroup}}</td><td class="measurement-number">{{.Value}}</td></tr>{{end}}</tbody></table></div>
    <h4>節點觀測值</h4><div class="measurement-wrap"><table class="measurement-table hotspot-table"><thead><tr><th>指標</th><th>節點</th><th>比較群組</th><th>當下值</th><th>同類節點中位數</th><th>差距（百分點）</th><th>資料屬性</th></tr></thead><tbody>{{range hotspotRows .Measurements}}<tr><td>{{.Label}}</td><td>{{.Node}}</td><td>{{.PeerGroup}}</td><td class="measurement-number">{{.Current}}</td><td class="measurement-number">{{if .Median}}{{.Median}}{{else}}—{{end}}</td><td class="measurement-number">{{if .Difference}}{{.Difference}}{{else}}—{{end}}</td><td>{{.Nature}}</td></tr>{{end}}</tbody></table></div><p class="measurement-note">中位數是本次同類節點的比較基準；差距以百分點表示。單次快照不能單獨判定 hot spotting。</p>
    {{else if .Measurements}}<h4>本次觀測值</h4><div class="measurement-wrap"><table class="measurement-table"><thead><tr><th>指標</th><th>對象</th><th>數值</th><th>性質</th></tr></thead><tbody>{{range .Measurements}}<tr><td>{{measurementLabel .}}</td><td>{{measurementTarget .}}</td><td class="measurement-number">{{measurementValue .}}</td><td><span class="measurement-kind {{measurementKindClass .Kind}}">{{measurementKind .Kind}}</span></td></tr>{{end}}</tbody></table></div>{{if hasCounter .Measurements}}<p class="measurement-note">累積值需以前後兩次採集的差值判讀；節點或程序重啟後計數可能歸零。</p>{{end}}{{end}}
    {{if .JudgmentGuide}}<h4>判定方式</h4><div class="judgment-wrap"><table class="judgment-table"><thead><tr><th>情況</th><th>判讀</th></tr></thead><tbody>{{range .JudgmentGuide}}<tr><td>{{.Condition}}</td><td>{{.Interpretation}}</td></tr>{{end}}</tbody></table></div>{{end}}
    {{if .RootCauses}}<h4>可能根因（假設）</h4><ul>{{range .RootCauses}}<li>{{.}}</li>{{end}}</ul>{{end}}
    {{if .Recommendations}}<h4>建議（唯讀引導）</h4><ul>{{range .Recommendations}}<li>{{if .Cmd}}<code>{{.Cmd}}</code> — {{end}}{{.Desc}}</li>{{end}}</ul>{{end}}
    {{if .RequiresExtra}}<p class="extra {{cls .Status}}">{{extraLead .Status}}：{{.ExtraReason}}</p>{{end}}
    {{if .VersionWarning}}<p class="vw">版本警告：{{.VersionWarning}}</p>{{end}}
    {{if .Docs}}<h4>官方文件</h4><ul class="doclist">{{range .Docs}}<li><a class="doclink" href="{{.}}" title="{{.}}"><svg class="ic ic-12"><use href="#i-external-link"/></svg><span>{{docLabel .}}</span></a></li>{{end}}</ul>{{end}}
  </div>
</details>
{{end}}
{{end}}
{{define "diagnostic-groups"}}
{{range $i, $group := .Groups}}
{{$summary := groupSummary .Results}}
<section id="{{sectionID $.Prefix .Key}}" class="section diagnostic-section" data-status="{{cls (groupStatus .Results)}}">
  <header class="section-head">
    <div class="section-heading"><span class="section-id">{{sectionNumber $i $.Start}}</span><h2 class="section-title">{{.Name}}</h2><span class="section-en">{{.Key}}</span></div>
    <div class="section-counts">
      {{if $summary.Critical}}<span class="count-chip" data-status="critical"><svg class="ic ic-12"><use href="#i-x-circle"/></svg><b>{{$summary.Critical}}</b></span>{{end}}
      {{if $summary.Warning}}<span class="count-chip" data-status="warning"><svg class="ic ic-12"><use href="#i-alert-triangle"/></svg><b>{{$summary.Warning}}</b></span>{{end}}
      {{if $summary.Info}}<span class="count-chip" data-status="info"><svg class="ic ic-12"><use href="#i-info"/></svg><b>{{$summary.Info}}</b></span>{{end}}
      {{if $summary.Pass}}<span class="count-chip" data-status="pass"><svg class="ic ic-12"><use href="#i-check-circle"/></svg><b>{{$summary.Pass}}</b></span>{{end}}
      {{if $summary.Skipped}}<span class="count-chip" data-status="skipped"><svg class="ic ic-12"><use href="#i-skip-forward"/></svg><b>{{$summary.Skipped}}</b></span>{{end}}
      {{if $summary.Unknown}}<span class="count-chip" data-status="unknown"><svg class="ic ic-12"><use href="#i-help-circle"/></svg><b>{{$summary.Unknown}}</b></span>{{end}}
    </div>
  </header>
  <div class="section-body">{{template "diagnostic-results" .Results}}</div>
</section>
{{end}}
{{end}}
<!DOCTYPE html>
<html lang="zh-Hant">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>ELK 服務健康診斷報告</title>
<style>
  :root{--pass:#2e7d32;--info:#1565c0;--warning:#ed6c02;--critical:#c62828;--skipped:#757575;--unknown:#455a64;}
  *{box-sizing:border-box}
  body{font-family:-apple-system,"Segoe UI","Microsoft JhengHei",sans-serif;margin:0;color:#1a1a1a;background:#f5f5f5;line-height:1.6}
  .wrap{max-width:1180px;margin:0 auto;padding:24px}
  header{background:#fff;border:1px solid #e0e0e0;border-radius:8px;padding:20px}
  .header-title{display:flex;align-items:flex-start;justify-content:space-between;gap:16px}
  header h1{margin:0;font-size:24px;line-height:1.35}
  .report-status{flex:none;border-radius:999px;padding:4px 12px;font-size:12px;font-weight:700;letter-spacing:.03em}
  .report-status.pass{background:#e8f5e9;color:var(--pass)}.report-status.info{background:#e3f2fd;color:var(--info)}.report-status.warning{background:#fff3e0;color:#a04b00}
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
  .banner.pass{background:var(--pass)}.banner.info{background:var(--info)}.banner.warning{background:var(--warning)}.banner.critical{background:var(--critical)}.banner.unknown{background:var(--unknown)}
  .counts{font-size:13px;font-weight:400}
  .counts .info-count{color:#bbdefb}
  .counts b{font-weight:700}
  .version-notice{margin:16px 0;padding:10px 16px;background:#fff8e1;border:1px solid #ffca28;border-radius:6px;color:#795548;font-size:13px}
  .hints{margin:16px 0;padding:10px 16px;background:#fff8e1;border:1px solid #ffe082;border-radius:6px}
  .hints h4{margin:0 0 6px;font-size:13px;color:#795548}
  .hints ul{margin:0;padding-left:20px}
  .hints li{font-size:14px;margin:2px 0}
	.service-nav{display:flex;flex-wrap:wrap;align-items:center;gap:8px;margin:20px 0 12px;padding:10px 12px;background:#fff;border:1px solid #d8dee4;border-radius:8px}
	.service-nav-label{color:#555;font-size:12px;font-weight:700;margin-right:2px}
	.service-nav-link{display:inline-flex;align-items:center;gap:6px;padding:4px 10px;border-radius:999px;text-decoration:none;font-size:12px;font-weight:700}
	.service-nav-link.elasticsearch{background:#eceff1;color:#263238}.service-nav-link.kibana{background:#e3f2fd;color:#0d47a1}
	.service-nav-link span{font-weight:600;opacity:.8}
	.service-section{margin:22px 0 30px;padding:0 14px 14px;background:#fff;border:1px solid #d8dee4;border-top:5px solid #263238;border-radius:8px}
	.service-section.kibana{border-top-color:#1565c0;background:#fbfdff}
	.service-section.logstash{border-top-color:#2a6191;background:#fbfdff}
	.service-section-heading{display:flex;justify-content:space-between;align-items:flex-start;gap:16px;margin:0 -14px 12px;padding:12px 14px 10px;border-bottom:1px solid #e4e9ee;background:#f7f9fb}
	.service-section.kibana .service-section-heading{background:#eef7ff}
	.service-section-heading h2{margin:0;padding:0;border:0;font-size:20px}
	.service-section-heading p{margin:2px 0 0;color:#66717a;font-size:12px}
	.service-section-count{flex:none;color:#66717a;font-size:12px;font-weight:600;white-space:nowrap}
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
	.node-missing-alert{margin:8px 0 0;padding:8px 10px;background:#ffebee;border:1px solid #ef9a9a;border-radius:6px;color:var(--critical);font-weight:700}
	.node-overview-wrap{overflow-x:auto;background:#fff;border:1px solid #d8dee4;border-radius:8px;margin-top:10px}
  .node-overview{width:100%;border-collapse:collapse;font-size:13px;min-width:820px}
  .node-overview th,.node-overview td{padding:11px 12px;text-align:left;border-bottom:1px solid #e8edf2;vertical-align:middle}
  .node-overview th{background:#f6f8fa;color:#555;font-size:11px;text-transform:uppercase;letter-spacing:.03em;white-space:nowrap}
	.node-overview tbody tr:last-child td{border-bottom:0}
	.node-overview tbody tr:hover{background:#fafbfd}
	.node-overview .node-missing-row{background:#fff5f5}
	.node-overview .node-missing-row td{color:var(--critical);font-weight:700}
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
  .card.pass{border-left-color:var(--pass)}.card.info{border-left-color:var(--info)}.card.warning{border-left-color:var(--warning)}.card.critical{border-left-color:var(--critical)}.card.skipped{border-left-color:var(--skipped)}.card.unknown{border-left-color:var(--unknown)}
  .status-label{display:inline-block;margin-right:4px;padding:1px 6px;border-radius:4px;font-size:10px;font-weight:700;letter-spacing:.04em;vertical-align:1px}
  .card.pass .status-label{background:#e8f5e9;color:var(--pass)}
  .card.info .status-label{background:#e3f2fd;color:var(--info)}
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
  .measurement-wrap{overflow-x:auto;margin-top:4px;border:1px solid #e0e0e0;border-radius:6px}
  .measurement-table{width:100%;border-collapse:collapse;font-size:13px;min-width:560px}
  .measurement-table th,.measurement-table td{padding:7px 9px;text-align:left;border-bottom:1px solid #eee;vertical-align:middle}
  .measurement-table th{background:#f6f8fa;color:#555;font-size:11px;white-space:nowrap}
  .measurement-table tbody tr:last-child td{border-bottom:0}
  .measurement-table .measurement-number{font-variant-numeric:tabular-nums;font-weight:650;white-space:nowrap}
  .measurement-kind{display:inline-block;padding:1px 7px;border-radius:999px;font-size:11px;white-space:nowrap;background:#e8f5e9;color:var(--pass)}
  .measurement-kind.counter{background:#fff3e0;color:#a04b00}
  .measurement-note{font-size:12px;color:#795548;margin:7px 0 0}
  .hotspot-table{min-width:820px}
  .judgment-wrap{overflow-x:auto;margin-top:4px;border:1px solid #e0e0e0;border-radius:6px}
  .judgment-table{width:100%;border-collapse:collapse;font-size:13px;min-width:560px}
  .judgment-table th,.judgment-table td{padding:7px 9px;text-align:left;border-bottom:1px solid #eee;vertical-align:top}
  .judgment-table th{background:#f6f8fa;color:#555;font-size:11px;white-space:nowrap}
  .judgment-table tbody tr:last-child td{border-bottom:0}
  code{background:#f0f0f0;padding:1px 6px;border-radius:4px;font-size:13px}
  .extra,.vw{font-size:13px;color:var(--warning);margin:8px 0 0}.extra.info{color:var(--info)}
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

  /* UIUX 模板：維持單檔離線輸出，將模板的版面語彙套用到動態診斷資料。 */
  :root{
    --ui-brand:#2a6191;--ui-ink:#222b45;--ui-muted:#62748e;--ui-subtle:#8f9bb3;
    --ui-line:#e4e9f2;--ui-soft:#eef1f6;--ui-canvas:#f7f9fc;--ui-link:#0095ff;
    --ui-shadow:0 1px 3px 1px rgba(0,0,0,.15),0 1px 2px rgba(0,0,0,.12);
  }
  body{font-family:-apple-system,BlinkMacSystemFont,"PingFang TC","Noto Sans TC","Microsoft JhengHei","Segoe UI",Roboto,"Helvetica Neue",Arial,sans-serif;font-size:14px;line-height:1.625;color:var(--ui-ink);background:var(--ui-canvas);-webkit-font-smoothing:antialiased}
  .wrap{max-width:1276px;margin:0 auto;padding:0 24px}
  .topbar{position:sticky;top:0;z-index:30;height:60px;display:flex;align-items:center;justify-content:center;margin:0 -24px;padding:0 20px;background:#fff;border:0;border-bottom:1px solid var(--ui-line);border-radius:0;box-shadow:var(--ui-shadow)}
  .topbar-inner{display:flex;align-items:center;gap:16px}
  .logo{display:block;width:160px;height:41.374px}
  .topbar-rule{width:1px;height:20px;background:var(--ui-line)}
  .topbar-title{font-size:16px;font-weight:500;line-height:1.5;color:var(--ui-brand)}
  .report-meta{margin-top:24px;padding:20px;background:#fff;border:1px solid var(--ui-line);border-radius:8px}.report-meta .header-title{display:flex;align-items:flex-start;justify-content:space-between;gap:16px}.report-meta h1{margin:0;color:var(--ui-ink);font-size:24px;line-height:1.35}.report-meta .cluster-name{margin-top:12px;color:var(--ui-ink);font-size:18px;font-weight:650}.report-meta .cluster-facts{color:var(--ui-muted);font-size:13px}.report-meta .meta-grid{border-top:1px solid var(--ui-line)}
  .section-nav{position:sticky;top:60px;z-index:20;margin:0 -24px;background:#fff;border-bottom:1px solid var(--ui-line)}
  .section-nav-inner{display:flex;justify-content:center;gap:4px;height:51px;padding:0 24px;overflow-x:auto;scrollbar-width:none}
  .section-nav-inner::-webkit-scrollbar{display:none}
  .nav-item{position:relative;display:flex;align-items:center;gap:8px;flex:none;padding:0 16px;color:var(--ui-muted);font-size:15px;font-weight:500;white-space:nowrap;text-decoration:none}
  .nav-item:hover{color:var(--ui-brand);text-decoration:none}.nav-dot{width:6px;height:6px;border-radius:999px;background:var(--ui-subtle);opacity:.7}.nav-item[data-status="critical"] .nav-dot{background:#ff3d71}.nav-item[data-status="warning"] .nav-dot{background:#e0930a}.nav-item[data-status="pass"] .nav-dot{background:#00b383}.nav-item[data-status="info"] .nav-dot{background:#8f9bb3}
  .nav-item::after{content:"";position:absolute;left:8px;right:8px;bottom:0;height:3px;border-radius:999px 999px 0 0;background:var(--ui-brand);opacity:0}.nav-item:hover::after,.nav-item:focus-visible::after{opacity:.35}
  .banner{margin:24px 0 0;padding:0;background:#dbe1ec;color:var(--ui-ink);border:1px solid var(--ui-line);border-radius:8px;overflow:hidden;display:block;font-size:inherit;font-weight:400}
  .banner-body{padding:12px 16px 16px}.banner-head{display:flex;gap:12px;align-items:center}.banner-icon{width:36px;height:36px;display:grid;place-items:center;border-radius:999px;background:#e4e9f2;color:#62748e}.banner.pass{background:#cdefe2}.banner.pass .banner-icon{background:#e4f7f0;color:#00875a}.banner.info{background:#dbe1ec}.banner.warning{background:#fbe6c4}.banner.critical{background:#ffd6d9}.banner.unknown{background:#d5dae6}
  .banner-title{margin:0;font-size:15px;font-weight:700;color:#48566f}.banner.warning .banner-title{color:#8a5708}.banner.critical .banner-title{color:#b81d5b}.banner.pass .banner-title{color:#00694a}.banner-counts{display:flex;flex-wrap:wrap;align-items:center;gap:4px 12px;margin:2px 0 0;color:var(--ui-muted);font-size:12px;line-height:1.5}.banner-counts .count{display:inline-flex;align-items:center;gap:4px}.banner-counts .count b{font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;font-weight:600}.banner-counts .count-total{color:var(--ui-muted)}
  .deflist{display:flex;flex-wrap:wrap;gap:10px 24px;margin:16px 0 0}.deflist>div{min-width:0}.deflist dt{font-size:11px;color:var(--ui-muted);line-height:1.5}.deflist dd{margin:0;font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;font-size:12px;color:var(--ui-ink);overflow-wrap:anywhere}.technical-info{margin-top:12px;padding-top:12px;border-top:1px solid rgba(34,43,69,.10)}
  .kpi-row{display:flex;flex-wrap:wrap;gap:16px;margin-top:16px}.kpi{display:flex;flex-direction:column;justify-content:space-between;min-width:0;min-height:120px;padding:16px;background:#fff;border:1px solid var(--ui-line);border-radius:8px}.kpi-rate{width:188px;flex:none}.kpi-stat{flex:1 1 180px}.kpi-label{display:flex;align-items:center;gap:8px;color:var(--ui-muted);font-size:14px}.kpi-rate .kpi-label{font-weight:700}.kpi-icon{width:24px;height:24px;display:grid;place-items:center;border-radius:6px;background:#e4e9f2;color:#62748e}.kpi-figure{display:flex;align-items:flex-end;justify-content:flex-end;gap:8px;padding-top:12px}.kpi-value{font-size:32px;font-weight:700;line-height:1;color:#62748e}.kpi-unit{font-size:13px;color:var(--ui-subtle)}.kpi-track{height:6px;margin-top:12px;border-radius:999px;background:var(--ui-line);overflow:hidden}.kpi-fill{height:100%;border-radius:999px;background:#8f9bb3}.kpi[data-status="pass"] .kpi-value{color:#00875a}.kpi[data-status="pass"] .kpi-fill{background:#00b383}.kpi[data-status="warning"] .kpi-value{color:#b5730a}.kpi[data-status="warning"] .kpi-fill{background:#e0930a}.kpi[data-status="critical"] .kpi-value{color:#db2c5b}.kpi[data-status="critical"] .kpi-fill{background:#ff3d71}
  .service-nav{display:none}
  .service-section{margin:16px 0 24px;padding:0;background:#fff;border:1px solid var(--ui-line);border-radius:8px;overflow:visible}.service-section.kibana,.service-section.logstash{background:#fbfdff;border-top:0}.service-section-heading{display:flex;justify-content:space-between;align-items:flex-start;gap:16px;margin:0;padding:14px 20px;background:var(--ui-brand);color:#fff;border:0;border-radius:8px 8px 0 0}.service-section.kibana .service-section-heading,.service-section.logstash .service-section-heading{background:#2a6191}.service-section-heading h2{margin:0;padding:0;border:0;color:#fff;font-size:16px}.service-section-heading p{margin:2px 0 0;color:rgba(255,255,255,.78);font-size:12px}.service-section-count{color:rgba(255,255,255,.85);font-size:12px;font-weight:600}
  .diagnostic-section{margin:16px 20px;background:#fff;border:1px solid var(--ui-line);border-radius:8px;overflow:hidden;scroll-margin-top:120px}.diagnostic-section .section-head{padding:12px 16px;background:#f7f9fc;color:var(--ui-ink);border-bottom:1px solid var(--ui-line)}.diagnostic-section .section-title{font-size:16px;color:var(--ui-ink)}.diagnostic-section .section-id{font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;color:var(--ui-subtle);font-size:12px}.diagnostic-section .section-en{color:var(--ui-subtle);font-size:11px}.diagnostic-section .section-counts{color:var(--ui-muted)}.diagnostic-section .count-chip{display:inline-flex;align-items:center;gap:4px;padding:2px 8px;border-radius:999px;background:#eef1f6;color:var(--ui-muted);font-size:12px}.diagnostic-section .section-body{padding:0 16px}
  .check{border:0;border-bottom:1px solid var(--ui-line);border-left:0;border-radius:0;margin:0;overflow:visible;padding:16px 0}.check:last-child{border-bottom:0}.check>summary{display:grid;grid-template-columns:16px minmax(0,1fr);gap:4px 8px;align-items:start;padding:0;font-weight:400;list-style:none;cursor:pointer}.check>summary::-webkit-details-marker{display:none}.check>summary .chev{margin-top:3px;color:var(--ui-subtle);font-size:20px;line-height:1;transition:transform .15s ease}.check[open]>summary .chev{transform:rotate(90deg)}.check-head{grid-column:2;display:flex;flex-wrap:wrap;align-items:center;gap:6px}.check-desc{grid-column:2;max-width:768px;color:var(--ui-muted);font-size:14px;line-height:1.625}.check-title{color:var(--ui-ink);font-size:15px;font-weight:500;line-height:1.5}.check-src{display:inline-flex;align-items:center;font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;color:var(--ui-subtle);font-size:11px;line-height:1.5}.status-label{display:inline-flex;align-items:center;padding:2px 8px;border-radius:999px;background:#eef1f6;color:#62748e;font-size:11px;font-weight:600;letter-spacing:.02em;line-height:1.5}.check.pass .status-label{background:#e4f7f0;color:#00875a}.check.info .status-label{background:#e4e9f2;color:#62748e}.check.warning .status-label{background:#fdf1dc;color:#b5730a}.check.critical .status-label{background:#ffe6ee;color:#db2c5b}.check.skipped .status-label{background:#f2f5f9;color:#a8b3c7}.check.unknown .status-label{background:#dfe3ec;color:#465272}
  .body.check-body{padding:16px 0 0 24px;border:0}.body.check-body h4{margin:16px 0 6px;color:var(--ui-ink);font-size:13px}.body.check-body h4:first-child{margin-top:0}.body.check-body li{color:var(--ui-muted);font-size:14px}.measurement-wrap,.judgment-wrap{border:1px solid var(--ui-line);border-radius:6px;overflow-x:auto}.measurement-table,.judgment-table{font-size:13px;min-width:560px}.measurement-table th,.judgment-table th{padding:8px 12px;background:var(--ui-brand);color:#fff;font-size:12px;font-weight:500}.measurement-table td,.judgment-table td{padding:8px 12px;border-top:1px solid var(--ui-line);color:var(--ui-ink)}.measurement-table .measurement-number{font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace}.measurement-kind{display:inline-flex;padding:1px 8px;border-radius:999px;background:#eef1f6;color:var(--ui-muted);font-size:11px}.measurement-kind.counter{background:#fdf1dc;color:#b5730a}.measurement-note,.extra,.vw{color:var(--ui-muted);font-size:12px}.extra.info{color:#62748e}
  .footer{margin:24px 0;padding:16px 0;border-top:1px solid var(--ui-line);background:transparent;color:var(--ui-subtle);font-size:12px;line-height:1.75}
  @media(max-width:980px){.wrap{padding:0 16px}.topbar,.section-nav{margin-left:-16px;margin-right:-16px}.section-nav-inner{justify-content:flex-start}.kpi-rate{width:100%}.diagnostic-section{margin-left:12px;margin-right:12px}.body.check-body{padding-left:0}}
  @media(max-width:600px){.topbar{height:auto;padding:8px 16px}.topbar-inner{flex-wrap:wrap;justify-content:center;row-gap:4px}.section-nav{top:auto}.banner-head{align-items:flex-start}.deflist{gap:8px 16px}}
  @media print{.topbar{position:static;box-shadow:none}.section-nav{display:none}.check,.diagnostic-section,.service-section{break-inside:avoid;page-break-inside:avoid}.wrap{max-width:none;padding:0}.node-mobile-list{display:none}.node-overview-wrap{display:block}}

  /* 與核准模板一致的最終版面規則。前述既有樣式保留供舊元件相容，以下規則優先。 */
  :root{
    --brand:#2a6191;--ink:#222b45;--ink-2:#62748e;--ink-3:#8f9bb3;
    --line:#e4e9f2;--line-soft:#eef1f6;--surface:#fff;--canvas:#f7f9fc;--link:#0095ff;
    --shell:1276px;--topbar-h:60px;--nav-h:51px;
  }
  [data-status="pass"]{--s-bg:#e4f7f0;--s-fg:#00875a;--s-bar:#00b383;--s-strong:#cdefe2;--s-deep:#00694a}
  [data-status="info"]{--s-bg:#e4e9f2;--s-fg:#62748e;--s-bar:#8f9bb3;--s-strong:#dbe1ec;--s-deep:#48566f}
  [data-status="warning"]{--s-bg:#fdf1dc;--s-fg:#b5730a;--s-bar:#e0930a;--s-strong:#fbe6c4;--s-deep:#8a5708}
  [data-status="critical"]{--s-bg:#ffe6ee;--s-fg:#db2c5b;--s-bar:#ff3d71;--s-strong:#ffd6d9;--s-deep:#b81d5b}
  [data-status="skipped"]{--s-bg:#f2f5f9;--s-fg:#a8b3c7;--s-bar:#c3cbd9;--s-strong:#eaeff5;--s-deep:#75808f}
  [data-status="unknown"]{--s-bg:#dfe3ec;--s-fg:#465272;--s-bar:#7d8aa3;--s-strong:#d5dae6;--s-deep:#333d57}
  .ic{fill:none;stroke:currentColor;stroke-width:2;stroke-linecap:round;stroke-linejoin:round;flex:none}
  .ic-12{width:12px;height:12px;stroke-width:2.25}.ic-14{width:14px;height:14px;stroke-width:2.15}.ic-16{width:16px;height:16px}.ic-20{width:20px;height:20px}
  .shell{max-width:var(--shell);margin:0 auto;padding:0 24px}
  .topbar{height:var(--topbar-h);margin:0;padding:0 20px;background:var(--surface);border-bottom:1px solid var(--line);box-shadow:var(--ui-shadow)}
  .section-nav{top:var(--topbar-h);margin:0;background:var(--surface);border-bottom:1px solid var(--line)}
  .section-nav-inner{height:var(--nav-h);padding:0 24px}
  .nav-dot{background:var(--s-bar);opacity:.55}.nav-item:hover .nav-dot{opacity:1}
  .banner{margin-top:24px;background:var(--s-strong);border:1px solid var(--line)}
  .banner-icon{background:var(--s-bg);color:var(--s-fg)}.banner-title{color:var(--s-deep)}
  .banner-counts .count{color:var(--s-deep)}
  .tech>summary{display:flex;align-items:center;gap:6px;color:var(--s-deep);list-style:none;cursor:pointer}.tech>summary::-webkit-details-marker{display:none}
  .kpi{min-height:142px}.kpi-rate-body{display:flex;align-items:flex-end;justify-content:space-between;gap:8px;padding-top:8px}.kpi-rate-value{font-size:40px;font-weight:700;line-height:.9;color:var(--brand)}.kpi-rate-note{font-size:12px;color:var(--ink-2)}
  .kpi-icon{background:var(--s-bg);color:var(--s-fg)}.kpi-value{color:var(--s-fg)}.kpi-fill{background:var(--s-bar)}
  .section,.service-section,.diagnostic-section{margin:16px 0 0;padding:0;background:var(--surface);border:1px solid var(--line);border-radius:8px;overflow:hidden;scroll-margin-top:calc(var(--topbar-h) + var(--nav-h) + 8px)}
  .section-head,.diagnostic-section .section-head{display:flex;align-items:center;justify-content:space-between;gap:12px;padding:14px 20px;background:var(--brand);color:#fff;border:0}
  .section-heading{display:flex;align-items:baseline;gap:8px;min-width:0;flex-wrap:wrap}.section-id,.diagnostic-section .section-id{font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;font-size:13px;color:inherit;opacity:1}.section-title,.diagnostic-section .section-title{margin:0;padding-bottom:0;border-bottom:0;color:#fff;font-size:16px;font-weight:700;line-height:1.5}.section-en,.diagnostic-section .section-en{color:#fff;font-size:11px;opacity:.75}.section-counts{display:flex;align-items:center;gap:6px;flex:none}.count-chip,.diagnostic-section .count-chip{display:inline-flex;align-items:center;gap:4px;padding:2px 8px;border-radius:999px;background:var(--s-bg);color:var(--s-fg);font-size:12px;font-weight:500;line-height:1.5}
  .section-body,.diagnostic-section .section-body{padding:0 20px}.node-context-body{padding-bottom:16px}
	.service-section.kibana,.service-section.logstash{margin-top:16px;background:var(--surface);border-top:1px solid var(--line)}
  .check{padding:16px 0}.check>summary{gap:8px}.check>summary .chev{font-size:inherit}.check-head{gap:10px}.body.check-body{padding:12px 0 0 24px;max-width:1024px}
  .doclist{display:flex;flex-direction:column;gap:4px;list-style:none;margin:0;padding:0}.doclink{display:inline-flex;align-items:center;gap:6px;font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;font-size:12px;line-height:1.5;overflow-wrap:anywhere}.doclink .ic{color:var(--link)}
  .node-overview{border-collapse:collapse}.node-overview thead th{background:var(--brand);color:#fff}.node-missing-row{background:#fff2f6;color:#db2c5b}
  @media(max-width:980px){.shell{padding:0 16px}.topbar,.section-nav{margin:0}.section-nav-inner{justify-content:flex-start}.diagnostic-section{margin-left:0;margin-right:0}}
  @media print{.shell{max-width:none;padding:0}.topbar{position:static}.section,.check{break-inside:avoid;page-break-inside:avoid}}
</style>
</head>
<body>
<svg width="0" height="0" aria-hidden="true" focusable="false" style="position:absolute">
  <symbol id="i-check-circle" viewBox="0 0 24 24"><path d="M22 11.08V12a10 10 0 1 1-5.93-9.14"></path><polyline points="22 4 12 14.01 9 11.01"></polyline></symbol>
  <symbol id="i-info" viewBox="0 0 24 24"><circle cx="12" cy="12" r="10"></circle><line x1="12" y1="16" x2="12" y2="12"></line><line x1="12" y1="8" x2="12.01" y2="8"></line></symbol>
  <symbol id="i-alert-triangle" viewBox="0 0 24 24"><path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"></path><line x1="12" y1="9" x2="12" y2="13"></line><line x1="12" y1="17" x2="12.01" y2="17"></line></symbol>
  <symbol id="i-x-circle" viewBox="0 0 24 24"><circle cx="12" cy="12" r="10"></circle><line x1="15" y1="9" x2="9" y2="15"></line><line x1="9" y1="9" x2="15" y2="15"></line></symbol>
  <symbol id="i-skip-forward" viewBox="0 0 24 24"><polygon points="5 4 15 12 5 20 5 4"></polygon><line x1="19" y1="5" x2="19" y2="19"></line></symbol>
  <symbol id="i-help-circle" viewBox="0 0 24 24"><circle cx="12" cy="12" r="10"></circle><path d="M9.09 9a3 3 0 0 1 5.83 1c0 2-3 3-3 3"></path><line x1="12" y1="17" x2="12.01" y2="17"></line></symbol>
  <symbol id="i-alert-octagon" viewBox="0 0 24 24"><polygon points="7.86 2 16.14 2 22 7.86 22 16.14 16.14 22 7.86 22 2 16.14 2 7.86 7.86 2"></polygon><line x1="12" y1="8" x2="12" y2="12"></line><line x1="12" y1="16" x2="12.01" y2="16"></line></symbol>
  <symbol id="i-external-link" viewBox="0 0 24 24"><path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6"></path><polyline points="15 3 21 3 21 9"></polyline><line x1="10" y1="14" x2="21" y2="3"></line></symbol>
  <symbol id="i-chevron-right" viewBox="0 0 24 24"><polyline points="9 18 15 12 9 6"></polyline></symbol>
</svg>

<header class="topbar">
  <div class="topbar-inner">
    {{brandLogo}}
    <span class="topbar-rule"></span>
    <span class="topbar-title">ELK 服務健康診斷報告</span>
  </div>
</header>

<nav class="section-nav" aria-label="報告區段導覽">
  <div class="section-nav-inner">
    {{with .R.NodeContext}}<a class="nav-item" href="#section-node-context" data-status="{{cls (nodeStatus .)}}"><span class="nav-dot"></span>節點概況</a>{{end}}
    {{range .ESGroups}}{{if groupVisible .Results}}<a class="nav-item" href="#{{sectionID "es" .Key}}" data-status="{{cls (groupStatus .Results)}}"><span class="nav-dot"></span>{{.Name}}</a>{{end}}{{end}}
    {{if serviceVisible .KibanaGroups}}<a class="nav-item" href="#service-kibana" data-status="{{cls (serviceStatus .KibanaGroups)}}"><span class="nav-dot"></span>Kibana</a>{{end}}
    {{if serviceVisible .LogstashGroups}}<a class="nav-item" href="#service-logstash" data-status="{{cls (serviceStatus .LogstashGroups)}}"><span class="nav-dot"></span>Logstash</a>{{end}}
  </div>
</nav>

<main class="shell">

<section class="banner {{cls .R.OverallStatus}}" data-status="{{cls .R.OverallStatus}}">
  <div class="banner-body">
    <div class="banner-head">
      <span class="banner-icon" aria-hidden="true"><svg class="ic ic-20"><use href="#{{statusIcon .R.OverallStatus}}"/></svg></span>
      <div>
        <h1 class="banner-title">整體狀態：{{statusText .R.OverallStatus}}</h1>
        <p class="banner-counts">
          <span class="count-total">共 <b>{{addCounts .R.Summary}}</b> 項檢查</span>
          <span class="count" data-status="pass"><svg class="ic ic-12"><use href="#i-check-circle"/></svg>通過 <b>{{.R.Summary.Pass}}</b></span>
          <span class="count" data-status="info"><svg class="ic ic-12"><use href="#i-info"/></svg>資訊 <b>{{.R.Summary.Info}}</b></span>
          <span class="count" data-status="warning"><svg class="ic ic-12"><use href="#i-alert-triangle"/></svg>警告 <b>{{.R.Summary.Warning}}</b></span>
          <span class="count" data-status="critical"><svg class="ic ic-12"><use href="#i-x-circle"/></svg>失敗 <b>{{.R.Summary.Critical}}</b></span>
          <span class="count" data-status="skipped"><svg class="ic ic-12"><use href="#i-skip-forward"/></svg>略過 <b>{{.R.Summary.Skipped}}</b></span>
          <span class="count" data-status="unknown"><svg class="ic ic-12"><use href="#i-help-circle"/></svg>未知 <b>{{.R.Summary.Unknown}}</b></span>
        </p>
      </div>
    </div>
    <dl class="deflist">
      <div><dt>叢集名稱</dt><dd>{{clusterName .R.Meta.Cluster.Name}}</dd></div>
      <div><dt>ES 版本</dt><dd>{{.R.Meta.Cluster.ESVersion}}</dd></div>
      {{with .R.NodeContext}}<div><dt>節點數</dt><dd>{{len .Nodes}} 個節點</dd></div>{{end}}
      <div><dt>診斷模式</dt><dd>{{analysisMode .R.Meta.Mode .R.Meta.Cluster.Host}}</dd></div>
      {{if .R.Meta.CollectedAt}}<div><dt>資料採集時間</dt><dd><time datetime="{{.R.Meta.CollectedAt}}">{{localTime .R.Meta.CollectedAt}}</time></dd></div>{{else if isBundleHost .R.Meta.Cluster.Host}}<div><dt>資料採集時間</dt><dd class="vw">bundle 未含採集時間（舊版採集腳本）</dd></div>{{end}}
      <div><dt>報告產生時間</dt><dd><time datetime="{{.R.Meta.GeneratedAt}}">{{localTime .R.Meta.GeneratedAt}}</time></dd></div>
      <div><dt>工具版本</dt><dd>elk-diagnostics {{.R.Meta.ToolVersion}}</dd></div>
      {{if .R.Meta.CollectScriptVersion}}<div><dt>採集器版本</dt><dd>{{.R.Meta.CollectScriptVersion}}</dd></div>{{end}}
    </dl>
    <details class="tech technical-info">
      <summary><svg class="ic ic-12 chev"><use href="#i-chevron-right"/></svg>技術資訊</summary>
      <dl class="tech-grid technical-grid">
        <dt>診斷模式</dt><dd>{{analysisMode .R.Meta.Mode .R.Meta.Cluster.Host}}</dd>
        <dt>資料來源</dt><dd><span title="{{sourcePath .R.Meta.Cluster.Host}}">{{sourceShort .R.Meta.Cluster.Host}}</span></dd>
        <dt>完整來源</dt><dd>{{sourcePath .R.Meta.Cluster.Host}}</dd>
        {{if .R.Meta.BundleSchemaVersion}}<dt>採集包格式</dt><dd>schema v{{.R.Meta.BundleSchemaVersion}}</dd>{{end}}
        {{if .R.Meta.CollectedServices}}<dt>採集模組</dt><dd>{{range $i, $service := .R.Meta.CollectedServices}}{{if $i}}、{{end}}{{$service}}{{end}}</dd>{{end}}
      </dl>
    </details>
  </div>
</section>

<section class="kpi-row" aria-label="檢查結果統計">
  <div class="kpi kpi-rate"><span class="kpi-label">通過率</span><div class="kpi-rate-body"><span class="kpi-rate-value">{{pctOf .R.Summary.Pass (addCounts .R.Summary)}}%</span><span class="kpi-rate-note">{{.R.Summary.Pass}} / {{addCounts .R.Summary}} 項</span></div></div>
  <div class="kpi kpi-stat" data-status="pass"><span class="kpi-label"><span class="kpi-icon"><svg class="ic ic-14"><use href="#i-check-circle"/></svg></span>通過</span><div><div class="kpi-figure"><span class="kpi-value">{{.R.Summary.Pass}}</span><span class="kpi-unit">項 · {{pctOf .R.Summary.Pass (addCounts .R.Summary)}}%</span></div><div class="kpi-track"><div class="kpi-fill" style="width:{{pctOf .R.Summary.Pass (addCounts .R.Summary)}}%"></div></div></div></div>
  <div class="kpi kpi-stat" data-status="warning"><span class="kpi-label"><span class="kpi-icon"><svg class="ic ic-14"><use href="#i-alert-triangle"/></svg></span>警告</span><div><div class="kpi-figure"><span class="kpi-value">{{.R.Summary.Warning}}</span><span class="kpi-unit">項 · {{pctOf .R.Summary.Warning (addCounts .R.Summary)}}%</span></div><div class="kpi-track"><div class="kpi-fill" style="width:{{pctOf .R.Summary.Warning (addCounts .R.Summary)}}%"></div></div></div></div>
  <div class="kpi kpi-stat" data-status="critical"><span class="kpi-label"><span class="kpi-icon"><svg class="ic ic-14"><use href="#i-x-circle"/></svg></span>失敗</span><div><div class="kpi-figure"><span class="kpi-value">{{.R.Summary.Critical}}</span><span class="kpi-unit">項 · {{pctOf .R.Summary.Critical (addCounts .R.Summary)}}%</span></div><div class="kpi-track"><div class="kpi-fill" style="width:{{pctOf .R.Summary.Critical (addCounts .R.Summary)}}%"></div></div></div></div>
</section>

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
<section id="section-node-context" class="section node-context-section" data-status="{{cls (nodeStatus .)}}">
<header class="section-head">
  <div class="section-heading"><span class="section-id">01</span><h2 class="section-title">節點概況</h2><span class="section-en">Node Context</span></div>
  <div class="section-counts">{{if .MissingNodes}}<span class="count-chip" data-status="critical"><svg class="ic ic-12"><use href="#i-x-circle"/></svg><b>{{len .MissingNodes}}</b></span>{{end}}{{if .Nodes}}<span class="count-chip" data-status="pass"><svg class="ic ic-12"><use href="#i-check-circle"/></svg><b>{{len .Nodes}}</b></span>{{end}}</div>
</header>
<div class="section-body node-context-body">
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
  {{if .MissingNodes}}<div class="node-missing-alert">❌ 缺失節點：{{joinNodes .MissingNodes}}</div>{{end}}
</div>
<div class="node-overview-wrap">
  <table class="node-overview">
    <thead><tr><th>節點</th><th>IP</th><th>角色</th><th>CPU</th><th>OS RAM*</th><th>JVM Heap</th><th>Swap</th><th>FD</th></tr></thead>
    <tbody>
    {{range .MissingNodes}}
      <tr class="node-missing-row">
        <td class="node-name-cell">{{.}}</td>
        <td colspan="7">❌ 缺失（Nodes API 未回應）</td>
      </tr>
    {{end}}
    {{range .Nodes}}
      <tr>
        <td class="node-name-cell">{{nodeName .}}</td>
        <td>{{if .IP}}{{.IP}}{{else}}—{{end}}</td>
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
{{range .MissingNodes}}
  <div class="node-mobile-card node-missing-card">
    <div class="node-mobile-head"><span class="node-mobile-name">{{.}}</span></div>
    <div class="node-missing-alert">❌ 缺失（Nodes API 未回應）</div>
  </div>
{{end}}
{{range .Nodes}}
  <div class="node-mobile-card">
    <div class="node-mobile-head"><span class="node-mobile-name">{{nodeName .}}</span></div>
    <div class="sm">IP：{{if .IP}}{{.IP}}{{else}}—{{end}}</div>
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
      <dt>Node ID / IP / 資料來源</dt><dd>{{.ID}} ｜ {{if .IP}}{{.IP}}{{else}}—{{end}} ｜ Stats={{.StatsAvailable}} Info={{.InfoAvailable}}</dd>
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
</div>
</section>
{{end}}

{{template "diagnostic-groups" .ES}}

{{if .KibanaGroups}}
{{$summary := serviceSummary .KibanaGroups}}
<section id="service-kibana" class="section service-section kibana" data-status="{{cls (serviceStatus .KibanaGroups)}}">
  <header class="section-head">
    <div class="section-heading"><h2 class="section-title">Kibana</h2><span class="section-en">Service</span></div>
    <div class="section-counts">
      {{if $summary.Critical}}<span class="count-chip" data-status="critical"><svg class="ic ic-12"><use href="#i-x-circle"/></svg><b>{{$summary.Critical}}</b></span>{{end}}
      {{if $summary.Warning}}<span class="count-chip" data-status="warning"><svg class="ic ic-12"><use href="#i-alert-triangle"/></svg><b>{{$summary.Warning}}</b></span>{{end}}
      {{if $summary.Info}}<span class="count-chip" data-status="info"><svg class="ic ic-12"><use href="#i-info"/></svg><b>{{$summary.Info}}</b></span>{{end}}
      {{if $summary.Pass}}<span class="count-chip" data-status="pass"><svg class="ic ic-12"><use href="#i-check-circle"/></svg><b>{{$summary.Pass}}</b></span>{{end}}
      {{if $summary.Skipped}}<span class="count-chip" data-status="skipped"><svg class="ic ic-12"><use href="#i-skip-forward"/></svg><b>{{$summary.Skipped}}</b></span>{{end}}
      {{if $summary.Unknown}}<span class="count-chip" data-status="unknown"><svg class="ic ic-12"><use href="#i-help-circle"/></svg><b>{{$summary.Unknown}}</b></span>{{end}}
    </div>
  </header>
  <div class="section-body">{{range .KibanaGroups}}{{template "diagnostic-results" .Results}}{{end}}</div>
</section>
{{end}}

{{if .LogstashGroups}}
{{$summary := serviceSummary .LogstashGroups}}
<section id="service-logstash" class="section service-section logstash" data-status="{{cls (serviceStatus .LogstashGroups)}}">
  <header class="section-head">
    <div class="section-heading"><h2 class="section-title">Logstash</h2><span class="section-en">Service</span></div>
    <div class="section-counts">
      {{if $summary.Critical}}<span class="count-chip" data-status="critical"><svg class="ic ic-12"><use href="#i-x-circle"/></svg><b>{{$summary.Critical}}</b></span>{{end}}
      {{if $summary.Warning}}<span class="count-chip" data-status="warning"><svg class="ic ic-12"><use href="#i-alert-triangle"/></svg><b>{{$summary.Warning}}</b></span>{{end}}
      {{if $summary.Info}}<span class="count-chip" data-status="info"><svg class="ic ic-12"><use href="#i-info"/></svg><b>{{$summary.Info}}</b></span>{{end}}
      {{if $summary.Pass}}<span class="count-chip" data-status="pass"><svg class="ic ic-12"><use href="#i-check-circle"/></svg><b>{{$summary.Pass}}</b></span>{{end}}
      {{if $summary.Skipped}}<span class="count-chip" data-status="skipped"><svg class="ic ic-12"><use href="#i-skip-forward"/></svg><b>{{$summary.Skipped}}</b></span>{{end}}
      {{if $summary.Unknown}}<span class="count-chip" data-status="unknown"><svg class="ic ic-12"><use href="#i-help-circle"/></svg><b>{{$summary.Unknown}}</b></span>{{end}}
    </div>
  </header>
  <div class="section-body">{{range .LogstashGroups}}{{template "diagnostic-results" .Results}}{{end}}</div>
</section>
{{end}}

<footer class="footer">{{.R.Disclaimer}}</footer>
</main>
</body>
</html>
`
