package analyzer

import (
	"fmt"

	"elk-doctor/internal/collector"
	"elk-doctor/internal/diagnostic"
)

// ILM 對映 spec #5。health_report 的 ilm indicator 會延遲（Phase 0 實測），故以
// _ilm/status + _ilm/explain 為準偵測 STOPPED / ERROR step。
func ILM(mode string, errs []collector.IlmError) diagnostic.Result {
	res := diagnostic.Result{
		ID:       "ilm_slm_status",
		Title:    "ILM 生命週期",
		Category: "management",
		Source:   "raw_api",
		Docs: []string{
			"https://www.elastic.co/docs/troubleshoot/elasticsearch/start-ilm",
			"https://www.elastic.co/docs/troubleshoot/elasticsearch/index-lifecycle-management-errors",
		},
	}

	switch {
	case mode == "STOPPED":
		res.Status = diagnostic.StatusCritical
		res.Conclusion = diagnostic.ConclusionConfirmed
		res.Summary = "ILM 已停止，生命週期動作不執行（SLM 亦連帶暫停）"
		res.Recommendations = []diagnostic.Recommendation{{Cmd: "POST _ilm/start", Desc: "維護完成後重啟 ILM"}}
	case len(errs) > 0:
		res.Status = diagnostic.StatusCritical
		res.Conclusion = diagnostic.ConclusionConfirmed
		res.Summary = fmt.Sprintf("%d 個 index 處於 ILM ERROR step", len(errs))
		for _, e := range errs {
			res.Findings = append(res.Findings, fmt.Sprintf("%s：failed_step=%s reason=%s", e.Index, e.FailedStep, e.Reason))
		}
		res.Recommendations = []diagnostic.Recommendation{{Cmd: "POST <index>/_ilm/retry", Desc: "依 reason 修正底層問題後重試該步驟"}}
	case mode == "STOPPING":
		res.Status = diagnostic.StatusWarning
		res.Conclusion = diagnostic.ConclusionSuspected
		res.Summary = "ILM 停止中（過渡狀態）"
	default:
		res.Status = diagnostic.StatusPass
		res.Conclusion = diagnostic.ConclusionNormal
		res.Summary = "ILM RUNNING，無 ERROR step"
	}
	return res
}

const (
	docWatcher    = "https://www.elastic.co/docs/troubleshoot/elasticsearch/watcher-troubleshooting"
	docTransform  = "https://www.elastic.co/docs/troubleshoot/elasticsearch/transform-troubleshooting"
	docMonitoring = "https://www.elastic.co/docs/troubleshoot/elasticsearch/monitoring-troubleshooting"
	docUpgrades   = "https://www.elastic.co/docs/troubleshoot/elasticsearch/troubleshooting-upgrades"
	docRemote     = "https://www.elastic.co/docs/troubleshoot/elasticsearch/remote-clusters"
)

// Watcher #27：Watcher 服務是否被手動停止。
func Watcher(manuallyStopped bool) diagnostic.Result {
	res := diagnostic.Result{ID: "watcher", Title: "Watcher", Category: "management", Source: "raw_api", Docs: []string{docWatcher}}
	if manuallyStopped {
		res.Status, res.Conclusion = diagnostic.StatusWarning, diagnostic.ConclusionSuspected
		res.Summary = "Watcher 已手動停止，watch 不會執行"
		res.Recommendations = []diagnostic.Recommendation{{Cmd: "POST _watcher/_start", Desc: "確認非維護中後重啟 Watcher"}}
		return res
	}
	return pass(res, "Watcher 運作中")
}

// Transforms #28：是否有 transform 處於 failed。
func Transforms(ts []collector.Transform) diagnostic.Result {
	res := diagnostic.Result{ID: "transforms", Title: "Transforms", Category: "management", Source: "raw_api", Docs: []string{docTransform}}
	if len(ts) == 0 {
		return pass(res, "未使用 transform（不適用）")
	}
	var failed []string
	for _, t := range ts {
		if t.State == "failed" {
			failed = append(failed, fmt.Sprintf("%s：state=failed", t.ID))
		}
	}
	if len(failed) == 0 {
		return pass(res, fmt.Sprintf("%d 個 transform 皆正常", len(ts)))
	}
	res.Status, res.Conclusion = diagnostic.StatusCritical, diagnostic.ConclusionConfirmed
	res.Summary = fmt.Sprintf("%d 個 transform 處於 failed", len(failed))
	res.Findings = failed
	res.Recommendations = []diagnostic.Recommendation{{Cmd: "GET _transform/<id>/_stats", Desc: "查 reason 後修正並 POST _transform/<id>/_start"}}
	return res
}

// RemoteClusters #35：已設定的 remote cluster 連線狀態。
func RemoteClusters(rcs []collector.RemoteCluster) diagnostic.Result {
	res := diagnostic.Result{ID: "remote_clusters", Title: "Remote clusters", Category: "management", Source: "raw_api", Docs: []string{docRemote}}
	if len(rcs) == 0 {
		return pass(res, "未設定 remote cluster（不適用）")
	}
	var down []string
	for _, r := range rcs {
		if !r.Connected {
			down = append(down, fmt.Sprintf("%s：未連線", r.Name))
		}
	}
	if len(down) == 0 {
		return pass(res, fmt.Sprintf("%d 個 remote cluster 皆已連線", len(rcs)))
	}
	res.Status, res.Conclusion = diagnostic.StatusWarning, diagnostic.ConclusionConfirmed
	res.Summary = fmt.Sprintf("%d 個 remote cluster 未連線", len(down))
	res.Findings = down
	res.Recommendations = []diagnostic.Recommendation{{Desc: "檢查網路連通、seed 節點設定與憑證；CCR/CCS 將受影響"}}
	return res
}

// UpgradeDeprecations #34：升版前 deprecation 警告。
func UpgradeDeprecations(deps []collector.Deprecation) diagnostic.Result {
	res := diagnostic.Result{ID: "upgrade_deprecations", Title: "升版 deprecation", Category: "management", Source: "raw_api", Docs: []string{docUpgrades}}
	var crit, warn []string
	for _, d := range deps {
		switch d.Level {
		case "critical":
			crit = append(crit, d.Message)
		case "warning":
			warn = append(warn, d.Message)
		}
	}
	res.Findings = append(crit, warn...)
	switch {
	case len(crit) > 0:
		res.Status, res.Conclusion = diagnostic.StatusCritical, diagnostic.ConclusionConfirmed
		res.Summary = fmt.Sprintf("%d 項 critical deprecation，升版前必須處理", len(crit))
		res.Recommendations = []diagnostic.Recommendation{{Desc: "依各 deprecation 的 details/url 於升版前修正"}}
	case len(warn) > 0:
		res.Status, res.Conclusion = diagnostic.StatusWarning, diagnostic.ConclusionSuspected
		res.Summary = fmt.Sprintf("%d 項 warning deprecation", len(warn))
	default:
		return pass(res, "無 deprecation 警告")
	}
	return res
}

// Monitoring #33：stack monitoring 收集是否啟用（資訊性）。
func Monitoring(enabled string) diagnostic.Result {
	res := diagnostic.Result{ID: "monitoring", Title: "Stack monitoring 收集", Category: "management", Source: "raw_api", Docs: []string{docMonitoring}}
	res.Status, res.Conclusion = diagnostic.StatusPass, diagnostic.ConclusionNormal
	if enabled == "false" {
		res.Summary = "stack monitoring 收集未啟用（巡檢工具本身不需，但無歷史監控資料可回溯時間序列）"
	} else {
		res.Summary = "stack monitoring 收集設定正常"
	}
	return res
}
