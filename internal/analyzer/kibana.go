package analyzer

import (
	"encoding/json"
	"fmt"
	"strings"

	"elk-diagnostics/internal/collector"
	"elk-diagnostics/internal/diagnostic"
)

const (
	docKibanaStatus      = "https://www.elastic.co/docs/api/doc/kibana/operation/operation-get-status"
	docKibanaStats       = "https://www.elastic.co/docs/reference/integrations/kibana/"
	docKibanaTaskManager = "https://www.elastic.co/docs/api/doc/kibana/operation/operation-task-manager-health"
	docKibanaAlerting    = "https://www.elastic.co/docs/api/doc/kibana/operation/operation-getalertinghealth"
)

type kibanaStatusDocument struct {
	Name    string `json:"name"`
	UUID    string `json:"uuid"`
	Version struct {
		Number string `json:"number"`
	} `json:"version"`
	Status struct {
		Overall kibanaStatusEntry            `json:"overall"`
		Core    map[string]kibanaStatusEntry `json:"core"`
		Plugins map[string]kibanaStatusEntry `json:"plugins"`
	} `json:"status"`
}

type kibanaStatusEntry struct {
	Level   string `json:"level"`
	Summary string `json:"summary"`
}

// KibanaStatus 分析 GET /api/status。只有 overall level 能明確表示 available／
// degraded／unavailable 才會分級；403、逾時、遮罩或格式錯誤一律 unknown。
func KibanaStatus(evidence []collector.KibanaEvidence) diagnostic.Result {
	res := diagnostic.Result{
		ID: "kibana_status", Title: "Kibana 核心健康", Category: "service", Source: "raw_api", Docs: []string{docKibanaStatus},
	}
	if len(evidence) == 0 {
		return kibanaUnknown(res, "bundle 未包含 Kibana 採集資料", "找不到 kibana/<instance-id>/status.json")
	}

	var passCount, degradedCount, unavailableCount, unknownCount int
	var findings []string
	for _, ev := range evidence {
		res.Measurements = append(res.Measurements, gauge("kibana.instance.response.status", float64(ev.StatusCode), "count", "service", ev.ID, ev.ID, "status"))
		if ev.StatusCode < 200 || ev.StatusCode >= 300 {
			unknownCount++
			findings = append(findings, fmt.Sprintf("%s：/api/status HTTP %d，無法判定 Kibana 健康狀態", ev.ID, ev.StatusCode))
			continue
		}
		var doc kibanaStatusDocument
		if len(ev.StatusBody) == 0 {
			unknownCount++
			findings = append(findings, fmt.Sprintf("%s：缺少 status.json，無法判定 Kibana 健康狀態", ev.ID))
			continue
		}
		if err := json.Unmarshal(ev.StatusBody, &doc); err != nil {
			unknownCount++
			findings = append(findings, fmt.Sprintf("%s：status.json 格式錯誤：%v", ev.ID, err))
			continue
		}
		level := strings.ToLower(strings.TrimSpace(doc.Status.Overall.Level))
		if level == "" || (doc.Name == "" && doc.UUID == "" && doc.Version.Number == "" && len(doc.Status.Core) == 0 && len(doc.Status.Plugins) == 0) {
			unknownCount++
			findings = append(findings, fmt.Sprintf("%s：status 回應缺少可判讀欄位，可能是未授權或遮罩回應", ev.ID))
			continue
		}
		summary := doc.Status.Overall.Summary
		if summary == "" {
			summary = "未提供摘要"
		}
		findings = append(findings, fmt.Sprintf("%s：overall=%s，%s", ev.ID, level, summary))
		switch level {
		case "available", "green", "ok":
			passCount++
		case "degraded", "yellow", "warning":
			degradedCount++
		case "unavailable", "red", "critical", "error":
			unavailableCount++
		default:
			unknownCount++
		}
	}
	res.Measurements = append(res.Measurements,
		gauge("kibana.instance.count", float64(len(evidence)), "count", "cluster", "", "Kibana", ""),
		gauge("kibana.instance.available.count", float64(passCount), "count", "cluster", "", "Kibana", ""),
		gauge("kibana.instance.degraded.count", float64(degradedCount), "count", "cluster", "", "Kibana", ""),
		gauge("kibana.instance.unavailable.count", float64(unavailableCount), "count", "cluster", "", "Kibana", ""),
		gauge("kibana.instance.unknown.count", float64(unknownCount), "count", "cluster", "", "Kibana", ""),
	)
	res.Findings = findings
	switch {
	case unavailableCount > 0:
		res.Status, res.Conclusion = diagnostic.StatusCritical, diagnostic.ConclusionConfirmed
		res.Summary = fmt.Sprintf("%d/%d 個 Kibana instance 不可用", unavailableCount, len(evidence))
		res.Recommendations = []diagnostic.Recommendation{{Desc: "確認 Kibana 服務、Elasticsearch 連線與 Saved Objects migration 狀態"}}
	case degradedCount > 0:
		res.Status, res.Conclusion = diagnostic.StatusWarning, diagnostic.ConclusionSuspected
		res.Summary = fmt.Sprintf("%d/%d 個 Kibana instance 降級", degradedCount, len(evidence))
		res.Recommendations = []diagnostic.Recommendation{{Desc: "依 status 摘要檢查 Elasticsearch、Saved Objects 與受影響 plugin"}}
	case unknownCount > 0:
		return kibanaUnknownWith(res, "部分 Kibana instance 無法判定", findings)
	default:
		res.Status, res.Conclusion = diagnostic.StatusPass, diagnostic.ConclusionNormal
		res.Summary = fmt.Sprintf("%d 個 Kibana instance 均可用", passCount)
	}
	return res
}

// KibanaStats 只保存可供趨勢使用的 runtime 觀測值，不把單次 stats 快照升級成故障。
// stats 缺失、403 或格式不支援時使用 skipped，避免選配端點污染 ES 健康結論。
func KibanaStats(evidence []collector.KibanaEvidence) diagnostic.Result {
	res := diagnostic.Result{ID: "kibana_stats", Title: "Kibana 執行觀測", Category: "service", Source: "raw_api", Docs: []string{docKibanaStats}}
	if len(evidence) == 0 {
		return statsSkipped(res, "bundle 未包含 Kibana 採集資料")
	}
	for _, ev := range evidence {
		if ev.StatsCode < 200 || ev.StatsCode >= 300 || len(ev.StatsBody) == 0 {
			continue
		}
		var raw map[string]any
		if err := json.Unmarshal(ev.StatsBody, &raw); err != nil {
			continue
		}
		appendKibanaStatsMeasurements(&res, ev.ID, raw)
	}
	if len(res.Measurements) == 0 {
		return statsSkipped(res, "未取得可解析的 Kibana stats；未納入健康判定")
	}
	res.Status, res.Conclusion = diagnostic.StatusInfo, diagnostic.ConclusionNormal
	res.Summary = fmt.Sprintf("已取得 %d 個 Kibana runtime 觀測值（僅供趨勢，不作故障判定）", len(res.Measurements))
	res.Findings = []string{"stats.json 的數值是單次快照，請以多次採集或 Kibana Monitoring 觀察趨勢"}
	res.RequiresExtra, res.ExtraReason = true, "單次 Kibana stats 不能單獨判定服務故障"
	return res
}

// KibanaTaskManagerHealth 分析 GET /api/task_manager/_health。
// Task Manager 回報的是 Kibana 自己最近的健康檢查，不把單次 workload 數值另行推論成故障。
func KibanaTaskManagerHealth(evidence []collector.KibanaEvidence) diagnostic.Result {
	res := diagnostic.Result{ID: "kibana_task_manager", Title: "Kibana Task Manager 健康", Category: "service", Source: "raw_api", Docs: []string{docKibanaTaskManager}}
	if len(evidence) == 0 {
		return kibanaEndpointSkipped(res, "未取得 Kibana Task Manager 採集資料", "task_manager_health.json")
	}

	var passCount, warningCount, criticalCount, unknownCount, missingCount int
	for _, ev := range evidence {
		res.Measurements = append(res.Measurements, gauge("kibana.task_manager.response.status", float64(ev.TaskManagerCode), "count", "service", ev.ID, ev.ID, "status"))
		if ev.TaskManagerCode < 200 || ev.TaskManagerCode >= 300 {
			unknownCount++
			res.Findings = append(res.Findings, fmt.Sprintf("%s：/api/task_manager/_health HTTP %d，無法判定 Task Manager 健康狀態", ev.ID, ev.TaskManagerCode))
			continue
		}
		if len(ev.TaskManagerBody) == 0 {
			missingCount++
			continue
		}
		var doc struct {
			Status     string `json:"status"`
			Timestamp  string `json:"timestamp"`
			LastUpdate string `json:"last_update"`
			Stats      map[string]struct {
				Status string `json:"status"`
			} `json:"stats"`
		}
		if err := json.Unmarshal(ev.TaskManagerBody, &doc); err != nil {
			unknownCount++
			res.Findings = append(res.Findings, fmt.Sprintf("%s：task_manager_health.json 格式錯誤：%v", ev.ID, err))
			continue
		}
		level := strings.TrimSpace(doc.Status)
		if level == "" {
			level = taskManagerSubstatus(doc.Stats)
		}
		status := kibanaHealthStatus(level)
		if status == diagnostic.StatusUnknown {
			unknownCount++
			res.Findings = append(res.Findings, fmt.Sprintf("%s：Task Manager 回應缺少可判讀 status", ev.ID))
			continue
		}
		res.Findings = append(res.Findings, fmt.Sprintf("%s：status=%s last_update=%s", ev.ID, strings.ToLower(level), kibanaValueOr(doc.LastUpdate, "未提供")))
		switch status {
		case diagnostic.StatusPass:
			passCount++
		case diagnostic.StatusWarning:
			warningCount++
		case diagnostic.StatusCritical:
			criticalCount++
		default:
			unknownCount++
		}
	}
	res.Measurements = append(res.Measurements,
		gauge("kibana.task_manager.instance.count", float64(len(evidence)), "count", "cluster", "", "Kibana", "instances"),
		gauge("kibana.task_manager.warning.count", float64(warningCount), "count", "cluster", "", "Kibana", "warning"),
		gauge("kibana.task_manager.error.count", float64(criticalCount), "count", "cluster", "", "Kibana", "error"),
	)
	if missingCount > 0 {
		unknownCount++
		res.Findings = append(res.Findings, fmt.Sprintf("%d 個 Kibana instance 未包含 task_manager_health.json，無法完整判讀", missingCount))
	}
	res.Measurements = append(res.Measurements, gauge("kibana.task_manager.unknown.count", float64(unknownCount), "count", "cluster", "", "Kibana", "unknown"))
	switch {
	case criticalCount > 0:
		res.Status, res.Conclusion = diagnostic.StatusCritical, diagnostic.ConclusionConfirmed
		res.Summary = fmt.Sprintf("%d/%d 個 Kibana Task Manager 回報 error", criticalCount, len(evidence))
		res.Recommendations = []diagnostic.Recommendation{{Desc: "檢查 Task Manager 的 worker、drift、workload 與 Kibana server log"}}
	case warningCount > 0:
		res.Status, res.Conclusion = diagnostic.StatusWarning, diagnostic.ConclusionSuspected
		res.Summary = fmt.Sprintf("%d/%d 個 Kibana Task Manager 回報 warn", warningCount, len(evidence))
		res.Recommendations = []diagnostic.Recommendation{{Desc: "檢查 Task Manager runtime、workload 與容量設定"}}
	case unknownCount > 0:
		return kibanaUnknownWith(res, "部分 Kibana Task Manager 無法判定", res.Findings)
	default:
		res.Status, res.Conclusion = diagnostic.StatusPass, diagnostic.ConclusionNormal
		res.Summary = fmt.Sprintf("%d 個 Kibana Task Manager 健康檢查正常", passCount)
	}
	return res
}

// KibanaAlertingHealth 分析 GET /api/alerting/_health。
// Alerting 的三個子狀態與加密/TLS 條件都保留在卡片，避免只看單一 overall 值。
func KibanaAlertingHealth(evidence []collector.KibanaEvidence) diagnostic.Result {
	res := diagnostic.Result{ID: "kibana_alerting", Title: "Kibana Alerting 健康", Category: "service", Source: "raw_api", Docs: []string{docKibanaAlerting}}
	if len(evidence) == 0 {
		return kibanaEndpointSkipped(res, "未取得 Kibana Alerting 採集資料", "alerting_health.json")
	}

	var passCount, warningCount, criticalCount, unknownCount, missingCount int
	for _, ev := range evidence {
		res.Measurements = append(res.Measurements, gauge("kibana.alerting.response.status", float64(ev.AlertingCode), "count", "service", ev.ID, ev.ID, "status"))
		if ev.AlertingCode < 200 || ev.AlertingCode >= 300 {
			unknownCount++
			res.Findings = append(res.Findings, fmt.Sprintf("%s：/api/alerting/_health HTTP %d，無法判定 Alerting 健康狀態", ev.ID, ev.AlertingCode))
			continue
		}
		if len(ev.AlertingBody) == 0 {
			missingCount++
			continue
		}
		var doc struct {
			Framework struct {
				Decryption healthState `json:"decryption_health"`
				Execution  healthState `json:"execution_health"`
				Read       healthState `json:"read_health"`
			} `json:"alerting_framework_health"`
			HasPermanentEncryptionKey *bool `json:"has_permanent_encryption_key"`
			IsSufficientlySecure      *bool `json:"is_sufficiently_secure"`
		}
		if err := json.Unmarshal(ev.AlertingBody, &doc); err != nil {
			unknownCount++
			res.Findings = append(res.Findings, fmt.Sprintf("%s：alerting_health.json 格式錯誤：%v", ev.ID, err))
			continue
		}
		states := []struct {
			name  string
			state healthState
		}{
			{"decryption", doc.Framework.Decryption},
			{"execution", doc.Framework.Execution},
			{"read", doc.Framework.Read},
		}
		if doc.Framework.Decryption.Status == "" || doc.Framework.Execution.Status == "" || doc.Framework.Read.Status == "" {
			unknownCount++
			res.Findings = append(res.Findings, fmt.Sprintf("%s：Alerting 回應缺少 decryption/execution/read health", ev.ID))
			continue
		}
		instanceStatus := diagnostic.StatusPass
		for _, item := range states {
			status := kibanaHealthStatus(item.state.Status)
			res.Findings = append(res.Findings, fmt.Sprintf("%s：%s=%s", ev.ID, item.name, strings.ToLower(item.state.Status)))
			instanceStatus = worseKibanaStatus(instanceStatus, status)
		}
		if doc.HasPermanentEncryptionKey != nil && !*doc.HasPermanentEncryptionKey {
			instanceStatus = worseKibanaStatus(instanceStatus, diagnostic.StatusWarning)
			res.Findings = append(res.Findings, fmt.Sprintf("%s：未設定 permanent encryption key", ev.ID))
		}
		if doc.IsSufficientlySecure != nil && !*doc.IsSufficientlySecure {
			instanceStatus = worseKibanaStatus(instanceStatus, diagnostic.StatusWarning)
			res.Findings = append(res.Findings, fmt.Sprintf("%s：安全性不足（security 啟用但 TLS 未完整保護）", ev.ID))
		}
		switch instanceStatus {
		case diagnostic.StatusPass:
			passCount++
		case diagnostic.StatusWarning:
			warningCount++
		case diagnostic.StatusCritical:
			criticalCount++
		default:
			unknownCount++
		}
	}
	res.Measurements = append(res.Measurements,
		gauge("kibana.alerting.instance.count", float64(len(evidence)), "count", "cluster", "", "Kibana", "instances"),
		gauge("kibana.alerting.warning.count", float64(warningCount), "count", "cluster", "", "Kibana", "warning"),
		gauge("kibana.alerting.error.count", float64(criticalCount), "count", "cluster", "", "Kibana", "error"),
	)
	if missingCount > 0 {
		unknownCount++
		res.Findings = append(res.Findings, fmt.Sprintf("%d 個 Kibana instance 未包含 alerting_health.json，無法完整判讀", missingCount))
	}
	res.Measurements = append(res.Measurements, gauge("kibana.alerting.unknown.count", float64(unknownCount), "count", "cluster", "", "Kibana", "unknown"))
	switch {
	case criticalCount > 0:
		res.Status, res.Conclusion = diagnostic.StatusCritical, diagnostic.ConclusionConfirmed
		res.Summary = fmt.Sprintf("%d/%d 個 Kibana Alerting 子系統回報 error", criticalCount, len(evidence))
		res.Recommendations = []diagnostic.Recommendation{{Desc: "檢查 Alerting rule execution、decryption、read health 與 Kibana server log"}}
	case warningCount > 0:
		res.Status, res.Conclusion = diagnostic.StatusWarning, diagnostic.ConclusionSuspected
		res.Summary = fmt.Sprintf("%d/%d 個 Kibana Alerting instance 需要注意", warningCount, len(evidence))
		res.Recommendations = []diagnostic.Recommendation{{Desc: "確認加密金鑰、TLS 與 Alerting rule 執行狀態"}}
	case unknownCount > 0:
		return kibanaUnknownWith(res, "部分 Kibana Alerting 無法判定", res.Findings)
	default:
		res.Status, res.Conclusion = diagnostic.StatusPass, diagnostic.ConclusionNormal
		res.Summary = fmt.Sprintf("%d 個 Kibana Alerting instance 健康檢查正常", passCount)
	}
	return res
}

type healthState struct {
	Status    string `json:"status"`
	Timestamp string `json:"timestamp"`
}

func taskManagerSubstatus(stats map[string]struct {
	Status string `json:"status"`
}) string {
	for _, name := range []string{"runtime", "workload", "capacity_estimation", "configuration"} {
		if state := strings.TrimSpace(stats[name].Status); state != "" {
			return state
		}
	}
	return ""
}

func kibanaValueOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func kibanaHealthStatus(level string) diagnostic.Status {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "ok", "available", "green", "pass":
		return diagnostic.StatusPass
	case "warn", "warning", "degraded", "yellow":
		return diagnostic.StatusWarning
	case "error", "critical", "unavailable", "red":
		return diagnostic.StatusCritical
	default:
		return diagnostic.StatusUnknown
	}
}

func worseKibanaStatus(current, candidate diagnostic.Status) diagnostic.Status {
	priority := func(status diagnostic.Status) int {
		switch status {
		case diagnostic.StatusCritical:
			return 3
		case diagnostic.StatusWarning:
			return 2
		case diagnostic.StatusUnknown:
			return 1
		default:
			return 0
		}
	}
	if priority(candidate) > priority(current) {
		return candidate
	}
	return current
}

func kibanaEndpointSkipped(res diagnostic.Result, summary, file string) diagnostic.Result {
	res.Status, res.Conclusion = diagnostic.StatusSkipped, diagnostic.ConclusionNormal
	res.Summary = summary
	res.Findings = []string{fmt.Sprintf("未找到 %s；本次未執行或為舊版 Kibana bundle", file)}
	return res
}

func appendKibanaStatsMeasurements(res *diagnostic.Result, id string, raw map[string]any) {
	add := func(metric string, value float64, unit, component string) {
		res.Measurements = append(res.Measurements, gauge(metric, value, unit, "service", id, id, component))
	}
	for _, item := range []struct {
		path      []string
		metric    string
		unit      string
		component string
	}{
		{[]string{"process", "memory", "heap", "used_bytes"}, "kibana.process.heap.used", "bytes", "heap.used"},
		{[]string{"process", "memory", "heap", "size_limit"}, "kibana.process.heap.limit", "bytes", "heap.limit"},
		{[]string{"process", "resident_set_size_bytes"}, "kibana.process.resident_set_size", "bytes", "resident_set_size"},
		{[]string{"process", "uptime_ms"}, "kibana.process.uptime", "milliseconds", "uptime"},
		{[]string{"process", "event_loop_delay"}, "kibana.process.event_loop_delay", "milliseconds", "event_loop_delay"},
		{[]string{"os", "memory", "used_bytes"}, "kibana.os.memory.used", "bytes", "os.memory.used"},
		{[]string{"os", "memory", "total_bytes"}, "kibana.os.memory.total", "bytes", "os.memory.total"},
		{[]string{"os", "load", "1m"}, "kibana.os.load.1m", "count", "os.load.1m"},
		{[]string{"os", "load", "5m"}, "kibana.os.load.5m", "count", "os.load.5m"},
		{[]string{"os", "load", "15m"}, "kibana.os.load.15m", "count", "os.load.15m"},
		{[]string{"requests", "total"}, "kibana.requests.total", "count", "requests.total"},
		{[]string{"requests", "disconnects"}, "kibana.requests.disconnects", "count", "requests.disconnects"},
		{[]string{"elasticsearch_client", "total_active_sockets"}, "kibana.elasticsearch.active_sockets", "count", "elasticsearch.active_sockets"},
		{[]string{"elasticsearch_client", "total_idle_sockets"}, "kibana.elasticsearch.idle_sockets", "count", "elasticsearch.idle_sockets"},
		{[]string{"elasticsearch_client", "total_queued_requests"}, "kibana.elasticsearch.queued_requests", "count", "elasticsearch.queued_requests"},
	} {
		if value, ok := numberAt(raw, item.path...); ok {
			add(item.metric, value, item.unit, item.component)
		}
	}
	if value, ok := numberAt(raw, "process", "event_loop_utilization", "utilization"); ok {
		add("kibana.process.event_loop_utilization", value*100, "percent", "event_loop_utilization")
	}
}

func numberAt(raw map[string]any, path ...string) (float64, bool) {
	var current any = raw
	for _, key := range path {
		object, ok := current.(map[string]any)
		if !ok {
			return 0, false
		}
		current, ok = object[key]
		if !ok {
			return 0, false
		}
	}
	switch value := current.(type) {
	case float64:
		return value, true
	case json.Number:
		v, err := value.Float64()
		return v, err == nil
	default:
		return 0, false
	}
}

func kibanaUnknown(res diagnostic.Result, summary, finding string) diagnostic.Result {
	return kibanaUnknownWith(res, summary, []string{finding})
}

func kibanaUnknownWith(res diagnostic.Result, summary string, findings []string) diagnostic.Result {
	res.Status, res.Conclusion, res.Summary = diagnostic.StatusUnknown, diagnostic.ConclusionNormal, summary
	res.Findings, res.Recommendations, res.Measurements = findings, nil, nil
	return res
}

func statsSkipped(res diagnostic.Result, summary string) diagnostic.Result {
	res.Status, res.Conclusion, res.Summary = diagnostic.StatusSkipped, diagnostic.ConclusionNormal, summary
	res.Findings = []string{"Kibana stats 未納入健康分級；請確認採集端點狀態碼與權限"}
	return res
}
