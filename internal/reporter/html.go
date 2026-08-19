package reporter

import (
	"bytes"
	"fmt"
	"html/template"
	"math"
	"path/filepath"
	"strings"
	"time"

	"elk-diagnostics/internal/diagnostic"
	"elk-diagnostics/internal/nodecontext"
)

// HTML 產出離線可渲染報告（診斷報告規格 §5）：單一檔、CSS 全內嵌、零外部 CDN、
// 用原生 <details> 折疊（免 JS）、可列印。
func HTML(r diagnostic.Report) ([]byte, error) {
	var esResults, kibanaResults []diagnostic.Result
	for _, res := range r.Results {
		// 目前 category=service 代表 Kibana；ES 的所有既有分類維持原樣。
		// 未來新增其他服務診斷時，應在這裡再建立對應的服務區塊。
		if res.Category == "service" {
			kibanaResults = append(kibanaResults, res)
		} else {
			esResults = append(esResults, res)
		}
	}
	esGroups := categoryGroups(esResults)
	kibanaGroups := categoryGroups(kibanaResults)

	data := struct {
		R            diagnostic.Report
		ESGroups     []htmlGroup
		KibanaGroups []htmlGroup
	}{r, esGroups, kibanaGroups}

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
	for c, rs := range byCat { // 未知/新增分類，照原 key 附在後面
		groups = append(groups, htmlGroup{c, c, rs})
	}
	return groups
}

type htmlGroup struct {
	Key     string
	Name    string
	Results []diagnostic.Result
}

var catOrder = []string{"cluster", "capacity", "data", "management", "performance", "snapshot", "node", "service"}
var catNames = map[string]string{
	"cluster": "叢集", "capacity": "容量", "data": "資料",
	"management": "管理", "performance": "效能", "snapshot": "快照", "node": "節點環境診斷", "service": "服務診斷",
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

const htmlTmpl = `{{define "diagnostic-groups"}}
{{range .}}
<h2>{{.Name}}</h2>
{{range .Results}}
<details class="card {{cls .Status}}"{{if isOpen .Status}} open{{end}}>
  <summary>{{badge .Status}} <span class="status-label">{{statusLabel .Status}}</span> {{.Title}} <span class="sm">— {{.Summary}}</span><span class="src">{{.Source}}</span></summary>
  <div class="body">
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
    {{if .Docs}}<h4>官方文件</h4><ul>{{range .Docs}}<li><a href="{{.}}">{{.}}</a></li>{{end}}</ul>{{end}}
  </div>
</details>
{{end}}
{{end}}
{{end}}
<!DOCTYPE html>
<html lang="zh-Hant">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Elasticsearch 叢集健康診斷報告</title>
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
      {{if .R.Meta.BundleSchemaVersion}}<dt>採集包格式</dt><dd>schema v{{.R.Meta.BundleSchemaVersion}}</dd>{{end}}
      {{if .R.Meta.CollectedServices}}<dt>採集模組</dt><dd>{{range $i, $service := .R.Meta.CollectedServices}}{{if $i}}、{{end}}{{$service}}{{end}}</dd>{{end}}
    </dl>
  </details>
</header>

<div class="banner {{cls .R.OverallStatus}}">
  <span>整體狀態：{{statusText .R.OverallStatus}}</span>
  <span class="counts">✅ <b>{{.R.Summary.Pass}}</b>　<span class="info-count">ℹ️ <b>{{.R.Summary.Info}}</b></span>　⚠️ <b>{{.R.Summary.Warning}}</b>　❌ <b>{{.R.Summary.Critical}}</b>　⏭️ <b>{{.R.Summary.Skipped}}</b>　❓ <b>{{.R.Summary.Unknown}}</b></span>
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

<nav class="service-nav" aria-label="服務區塊導覽">
  <span class="service-nav-label">服務區塊</span>
  <a class="service-nav-link elasticsearch" href="#service-elasticsearch">Elasticsearch <span>{{len .ESGroups}} 類</span></a>
  {{if .KibanaGroups}}<a class="service-nav-link kibana" href="#service-kibana">Kibana <span>{{len .KibanaGroups}} 類</span></a>{{end}}
</nav>

<section id="service-elasticsearch" class="service-section elasticsearch">
  <div class="service-section-heading">
    <div><h2>Elasticsearch <span class="section-en">Cluster</span></h2><p>叢集、節點與 Elasticsearch 診斷</p></div>
    <span class="service-section-count">{{len .ESGroups}} 類診斷</span>
  </div>

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
{{end}}

{{template "diagnostic-groups" .ESGroups}}
</section>

{{if .KibanaGroups}}
<section id="service-kibana" class="service-section kibana">
  <div class="service-section-heading">
    <div><h2>Kibana <span class="section-en">Service</span></h2><p>Kibana 核心服務與執行狀態</p></div>
    <span class="service-section-count">{{len .KibanaGroups}} 類診斷</span>
  </div>
{{template "diagnostic-groups" .KibanaGroups}}
</section>
{{end}}

<footer>{{.R.Disclaimer}}</footer>
</div>
</body>
</html>
`
