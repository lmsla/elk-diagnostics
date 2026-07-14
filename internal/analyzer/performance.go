package analyzer

import (
	"fmt"

	"elk-diagnostics/internal/collector"
	"elk-diagnostics/internal/diagnostic"
)

// 閾值暫於程式內定義；後續規則引擎（spec-rules）會外部化為 default.yaml。
const (
	docRejected    = "https://www.elastic.co/docs/troubleshoot/elasticsearch/rejected-requests"
	docJVM         = "https://www.elastic.co/docs/troubleshoot/elasticsearch/high-jvm-memory-pressure"
	docBreaker     = "https://www.elastic.co/docs/troubleshoot/elasticsearch/circuit-breaker-errors"
	docCPU         = "https://www.elastic.co/docs/troubleshoot/elasticsearch/high-cpu-usage"
	docTaskBacklog = "https://www.elastic.co/docs/troubleshoot/elasticsearch/task-queue-backlog"
	docSlowlog     = "https://www.elastic.co/docs/troubleshoot/elasticsearch/troubleshooting-searches"

	jvmWarnPct   = 85 // 官方：持續 >85% 應紓解
	jvmCritPct   = 95 // parent circuit breaker 預設跳閘點
	cpuWarnPct   = 85 // 官方：>95% 持續才算問題；85 取較保守的快照預警
	queueBacklog = 50 // thread pool queue 積壓預警（瞬時值）
)

var watchPools = map[string]bool{"search": true, "write": true}

// RejectedRequests #6：thread pool 請求拒絕（rejected 累積值）。
func RejectedRequests(rows []collector.ThreadPoolRow) diagnostic.Result {
	res := diagnostic.Result{ID: "rejected_requests", Title: "請求拒絕 (thread pool)", Category: "performance", Source: "raw_api", Docs: []string{docRejected}}
	var hits []string
	for _, r := range rows {
		if watchPools[r.Name] && r.Rejected > 0 {
			hits = append(hits, fmt.Sprintf("%s / %s：rejected=%d completed=%d", r.Node, r.Name, r.Rejected, r.Completed))
		}
	}
	if len(hits) == 0 {
		return pass(res, "search / write thread pool 無拒絕")
	}
	res.Status, res.Conclusion = diagnostic.StatusWarning, diagnostic.ConclusionSuspected
	res.Summary = "search / write thread pool 出現拒絕（累積值，需間隔取樣比對差值確認是否持續）"
	res.Findings = hits
	res.RequiresExtra, res.ExtraReason = true, "rejected 為自節點啟動起的累積值；以 --interval 雙取樣比對差值才能確認當下是否持續"
	res.Recommendations = []diagnostic.Recommendation{{Desc: "降低 bulk / 搜尋批次大小、紓解 CPU/JVM 壓力、清理 task backlog"}}
	return res
}

// JVMPressure #7：以 old pool used/max 計算記憶體壓力（比瞬時 heap% 準）。
func JVMPressure(nodes []collector.NodeJVM) diagnostic.Result {
	res := diagnostic.Result{ID: "jvm_memory_pressure", Title: "JVM 記憶體壓力", Category: "performance", Source: "raw_api", Docs: []string{docJVM}}
	var crit, warn []string
	for _, n := range nodes {
		switch {
		case n.PressurePct >= jvmCritPct:
			crit = append(crit, fmt.Sprintf("%s：old pool 壓力 %d%%", n.Name, n.PressurePct))
		case n.PressurePct >= jvmWarnPct:
			warn = append(warn, fmt.Sprintf("%s：old pool 壓力 %d%%", n.Name, n.PressurePct))
		}
	}
	res.Findings = append(crit, warn...)
	switch {
	case len(crit) > 0:
		res.Status, res.Conclusion = diagnostic.StatusCritical, diagnostic.ConclusionConfirmed
		res.Summary = fmt.Sprintf("%d 個節點 JVM old pool 壓力 ≥%d%%，恐觸發 circuit breaker", len(crit), jvmCritPct)
		res.Recommendations = []diagnostic.Recommendation{{Desc: "紓解 heap：減少 fielddata/昂貴查詢、降 shard 數、必要時擴記憶體"}}
	case len(warn) > 0:
		res.Status, res.Conclusion = diagnostic.StatusWarning, diagnostic.ConclusionSuspected
		res.Summary = fmt.Sprintf("%d 個節點 JVM old pool 壓力 ≥%d%%", len(warn), jvmWarnPct)
		res.Recommendations = []diagnostic.Recommendation{{Desc: "檢視昂貴查詢與 fielddata；持續偏高建議擴記憶體"}}
	default:
		return pass(res, fmt.Sprintf("各節點 JVM old pool 壓力 <%d%%", jvmWarnPct))
	}
	return res
}

// CircuitBreaker #8：breaker tripped 次數（累積值）。
func CircuitBreaker(nodes []collector.NodeBreaker) diagnostic.Result {
	res := diagnostic.Result{ID: "circuit_breaker", Title: "Circuit breaker", Category: "performance", Source: "raw_api", Docs: []string{docBreaker}}
	var hits []string
	for _, n := range nodes {
		if n.Tripped > 0 {
			hits = append(hits, fmt.Sprintf("%s / %s breaker：tripped=%d", n.Node, n.Breaker, n.Tripped))
		}
	}
	if len(hits) == 0 {
		return pass(res, "circuit breaker 無跳閘紀錄")
	}
	res.Status, res.Conclusion = diagnostic.StatusWarning, diagnostic.ConclusionSuspected
	res.Summary = "circuit breaker 曾跳閘（累積值，無法確認是否仍在發生）"
	res.Findings = hits
	res.RequiresExtra, res.ExtraReason = true, "tripped 為自啟動起的累積值；需間隔取樣或查 log 時間戳確認當下是否持續"
	res.Recommendations = []diagnostic.Recommendation{{Desc: "多與高 JVM 壓力相關（見 jvm_memory_pressure）；降低單次請求記憶體用量"}}
	return res
}

// HighCPU #9：cat nodes 的 cpu（瞬時值），高 CPU 建議以 hot_threads 定位。
func HighCPU(nodes []collector.NodeCPU) diagnostic.Result {
	res := diagnostic.Result{ID: "high_cpu", Title: "CPU 使用率", Category: "performance", Source: "raw_api", Docs: []string{docCPU}}
	var hits []string
	for _, n := range nodes {
		if n.CPU >= cpuWarnPct {
			hits = append(hits, fmt.Sprintf("%s（%s）：cpu=%d%% load_1m=%s allocated_processors=%d", n.Name, n.Role, n.CPU, n.Load1m, n.AllocatedProcessors))
		}
	}
	if len(hits) == 0 {
		return pass(res, fmt.Sprintf("各節點 CPU <%d%%", cpuWarnPct))
	}
	res.Status, res.Conclusion = diagnostic.StatusWarning, diagnostic.ConclusionSuspected
	res.Summary = fmt.Sprintf("%d 個節點 CPU ≥%d%%（瞬時值）", len(hits), cpuWarnPct)
	res.Findings = hits
	res.RequiresExtra, res.ExtraReason = true, "cpu 為瞬時快照；官方建議 >95% 持續一段時間才算異常，持續性需時間序列佐證"
	res.Recommendations = []diagnostic.Recommendation{{Cmd: "GET _nodes/hot_threads", Desc: "定位熱點執行緒（搜尋/索引/merge/GC）"}}
	return res
}

// TaskBacklog #12：thread pool queue 積壓（瞬時值）。
func TaskBacklog(rows []collector.ThreadPoolRow) diagnostic.Result {
	res := diagnostic.Result{ID: "task_backlog", Title: "Thread pool 佇列積壓", Category: "performance", Source: "raw_api", Docs: []string{docTaskBacklog}}
	var hits []string
	for _, r := range rows {
		if r.Queue >= queueBacklog {
			hits = append(hits, fmt.Sprintf("%s / %s：queue=%d active=%d", r.Node, r.Name, r.Queue, r.Active))
		}
	}
	if len(hits) == 0 {
		return pass(res, fmt.Sprintf("各 thread pool queue <%d", queueBacklog))
	}
	res.Status, res.Conclusion = diagnostic.StatusWarning, diagnostic.ConclusionSuspected
	res.Summary = fmt.Sprintf("%d 個 thread pool 佇列積壓 ≥%d（瞬時值）", len(hits), queueBacklog)
	res.Findings = hits
	res.RequiresExtra, res.ExtraReason = true, "queue 為瞬時值；持續積壓需間隔觀察，長任務可查 GET /_tasks?detailed 的 running_time_in_nanos"
	res.Recommendations = []diagnostic.Recommendation{{Desc: "定位長任務/昂貴查詢；必要時取消卡住的 cancellable 任務"}}
	return res
}

// SlowLog #31：引導型。偵測 search slow log 是否開啟；未開則輸出開啟方式，不臆測慢查詢。
func SlowLog(enabledIndices []string) diagnostic.Result {
	res := diagnostic.Result{ID: "search_slow_log", Title: "Search slow log", Category: "performance", Source: "raw_api", Docs: []string{docSlowlog}}
	if len(enabledIndices) > 0 {
		res = pass(res, fmt.Sprintf("已於 %d 個 index 開啟 search slow log", len(enabledIndices)))
		res.Findings = enabledIndices
		res.Recommendations = []diagnostic.Recommendation{{Desc: "可至節點 slow log 檔（*_index_search_slowlog.json）檢視慢查詢，或用 search profiler 分析"}}
		return res
	}
	// 未開啟並非錯誤，但無法回溯慢查詢——標為需額外條件，給開啟方式。
	res = pass(res, "未開啟 search slow log，無法回溯慢查詢")
	res.RequiresExtra = true
	res.ExtraReason = "slow log 需事先開啟才能記錄慢查詢；工具無法回溯歷史請求"
	res.Recommendations = []diagnostic.Recommendation{{
		Cmd:  `PUT <index>/_settings {"index.search.slowlog.threshold.query.warn":"10s","index.search.slowlog.threshold.query.info":"5s"}`,
		Desc: "對目標 index 設定查詢門檻以開啟 slow log，之後再回頭分析",
	}}
	return res
}

func pass(res diagnostic.Result, summary string) diagnostic.Result {
	res.Status, res.Conclusion, res.Summary = diagnostic.StatusPass, diagnostic.ConclusionNormal, summary
	return res
}
