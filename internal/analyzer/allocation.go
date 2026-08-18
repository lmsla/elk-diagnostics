// allocation.go：shards_availability 的 B 類加深（#19/#20/#37，見 健康報告規格.md）。
// A 類（cluster_health，涵蓋 #1/#2/#21）已由 healthreport.go 的通用 driver table 產出；
// 這裡只在該 indicator 非 green 時，額外查 allocation.enable 設定與 decider 級根因。
package analyzer

import (
	"fmt"

	"elk-diagnostics/internal/collector"
	"elk-diagnostics/internal/diagnostic"
)

const (
	docUnassigned   = "https://www.elastic.co/docs/troubleshoot/elasticsearch/diagnose-unassigned-shards"
	docAllocExplain = "https://www.elastic.co/docs/troubleshoot/elasticsearch/cluster-allocation-api-examples"
)

// DataAllocationBlocked #19：cluster.routing.allocation.enable 非 "all" 時，
// 叢集層級封鎖了部分/全部 shard 分配，是最常被忽略的根因（常見於維護後忘記復原）。
func DataAllocationBlocked(clusterEnable string) diagnostic.Result {
	res := diagnostic.Result{ID: "data_allocation_blocked", Title: "叢集層級 shard 分配封鎖", Category: "cluster", Source: "raw_api", Docs: []string{docUnassigned}}
	if clusterEnable == "" || clusterEnable == "all" {
		return pass(res, "cluster.routing.allocation.enable = all，無叢集層級封鎖")
	}
	res.Status, res.Conclusion = diagnostic.StatusCritical, diagnostic.ConclusionConfirmed
	res.Summary = fmt.Sprintf("cluster.routing.allocation.enable = %q，叢集層級的 shard 分配被限制或封鎖", clusterEnable)
	res.RootCauses = []string{"常見於維護操作（如節點下線）後忘記將設定復原為 all"}
	res.Recommendations = []diagnostic.Recommendation{{
		Cmd:  `PUT _cluster/settings {"persistent":{"cluster.routing.allocation.enable":"all"}}`,
		Desc: "確認非維護中後復原為 all；若為刻意限制（如 primaries-only 遷移中）則此為預期狀態",
	}}
	return res
}

// IndexAllocationBlocked #20：受影響 index 的 index.routing.allocation.enable 非 "all"。
// enables 為 index → 生效值；只需傳入 shards_availability 診斷點名的受影響 index。
// unprobed 為「該查但查不到」的 index（權限不足、或 bundle 模式無法涵蓋動態端點）。
//
// unprobed 必須參與判定，不能當成空集合忽略：查不到不等於沒問題，把「沒查到封鎖」
// 講成「正常」正是 2026-07-15 抓到那批 bug 的共同模式（見 驗證狀態.md §1）。
func IndexAllocationBlocked(enables map[string]string, unprobed []string) diagnostic.Result {
	res := diagnostic.Result{ID: "index_allocation_blocked", Title: "Index 層級 shard 分配封鎖", Category: "cluster", Source: "raw_api", Docs: []string{docUnassigned}}
	var blocked []string
	for idx, v := range enables {
		if v != "" && v != "all" {
			blocked = append(blocked, fmt.Sprintf("%s：index.routing.allocation.enable=%q", idx, v))
		}
	}
	res.Measurements = append(res.Measurements,
		gauge("elasticsearch.index.allocation.checked.count", float64(len(enables)), "count", "", "", "", ""),
		gauge("elasticsearch.index.allocation.blocked.count", float64(len(blocked)), "count", "", "", "", ""),
		gauge("elasticsearch.index.allocation.unprobed.count", float64(len(unprobed)), "count", "", "", "", ""),
	)
	if len(blocked) == 0 {
		// 有 index 查不到就無法宣稱正常——已查的都乾淨，不代表查不到的那些也乾淨。
		if len(unprobed) > 0 {
			res.Status, res.Conclusion = diagnostic.StatusUnknown, diagnostic.ConclusionNormal
			res.Summary = fmt.Sprintf("%d 個受影響 index 無法查詢 allocation 設定，無法判定", len(unprobed))
			res.Findings = []string{fmt.Sprintf("查不到設定的 index：%v", unprobed)}
			if len(enables) > 0 {
				res.Findings = append(res.Findings, fmt.Sprintf("另有 %d 個 index 已查，皆為 all", len(enables)))
			}
			res.RequiresExtra, res.ExtraReason = true, "可能是權限不足，或以 --from-bundle 離線分析（bundle 無法涵蓋逐 index 的動態端點）；請確認該 index 的 index.routing.allocation.enable"
			return res
		}
		if len(enables) == 0 {
			return pass(res, "無受影響 index 需檢查（shards_availability 目前正常）")
		}
		return pass(res, fmt.Sprintf("已檢查 %d 個受影響 index，index.routing.allocation.enable 皆為 all", len(enables)))
	}
	res.Status, res.Conclusion = diagnostic.StatusWarning, diagnostic.ConclusionConfirmed
	res.Summary = fmt.Sprintf("%d 個 index 的層級分配設定被限制或封鎖", len(blocked))
	res.Findings = blocked
	// 已找到封鎖時仍要揭露查不到的部分——結論成立不代表證據完整。
	if len(unprobed) > 0 {
		res.Findings = append(res.Findings, fmt.Sprintf("另有 %d 個受影響 index 查不到設定，未納入判定：%v", len(unprobed), unprobed))
	}
	res.Recommendations = []diagnostic.Recommendation{{
		Cmd:  `PUT <index>/_settings {"index.routing.allocation.enable":"all"}`,
		Desc: "確認非刻意限制後復原為 all",
	}}
	return res
}

// AllocationGuidance #37：GET _cluster/allocation/explain 的代表性 decider 說明。
// found=false 表無未分配 shard 可解釋（叢集當下無此問題，非錯誤）。
func AllocationGuidance(exp *collector.AllocationExplanation, found bool) diagnostic.Result {
	res := diagnostic.Result{ID: "allocation_guidance", Title: "Shard 分配根因（decider 級）", Category: "cluster", Source: "raw_api", Docs: []string{docAllocExplain}}
	if !found || exp == nil {
		res.Measurements = append(res.Measurements, gauge("elasticsearch.shard.allocation.rejected_decider.count", 0, "count", "", "", "", ""))
		return pass(res, "無未分配 shard 可供 allocation/explain 解釋")
	}
	res.Measurements = append(res.Measurements, gauge("elasticsearch.shard.allocation.rejected_decider.count", float64(len(exp.Deciders)), "count", "index", exp.Index, exp.Index, fmt.Sprintf("shard-%d", exp.Shard)))
	if len(exp.Deciders) == 0 {
		res = pass(res, fmt.Sprintf("%s shard %d（primary=%v）：無 decider 封鎖，可能為暫時性 rebalancing", exp.Index, exp.Shard, exp.Primary))
		res.RequiresExtra, res.ExtraReason = true, "僅代表性抽查一個未分配 shard，非窮舉；其餘 shard 可能有不同根因"
		return res
	}
	res.Status, res.Conclusion = diagnostic.StatusWarning, diagnostic.ConclusionSuspected
	res.Summary = fmt.Sprintf("%s shard %d（primary=%v）：%d 個 decider 拒絕分配", exp.Index, exp.Shard, exp.Primary, len(exp.Deciders))
	for _, d := range exp.Deciders {
		res.Findings = append(res.Findings, fmt.Sprintf("%s（%s）：%s", d.Decider, d.Decision, d.Explanation))
	}
	res.RequiresExtra, res.ExtraReason = true, "僅代表性抽查一個未分配 shard，非窮舉（規格原定上限 20 逐一查）；其餘 shard 可能有不同根因，必要時對特定 index/shard 重複呼叫 allocation/explain"
	res.Recommendations = []diagnostic.Recommendation{{Cmd: "POST _cluster/reroute?retry_failed", Desc: "排除 decider 指出的問題後，可嘗試重新觸發分配"}}
	return res
}
