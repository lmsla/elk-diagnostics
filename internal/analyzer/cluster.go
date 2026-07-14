// cluster.go：master_is_stable 的 B 類加深（#30，見 spec-health-report.md）。
// A 類（master_stability，green/yellow/red）已由 healthreport.go 的通用 driver table
// 產出；這裡補充節點拓樸的結構性根因——master-eligible 節點數過少或為偶數，是
// 「叢集不穩定」最常見的成因，且不需要等到真的發生選舉問題才能發現。
package analyzer

import (
	"fmt"

	"elk-diagnostics/internal/diagnostic"
)

const docUnstableCluster = "https://www.elastic.co/docs/troubleshoot/elasticsearch/troubleshooting-unstable-cluster"

// MasterStabilityContext #30：master-eligible 節點數與叢集規模的結構性檢查。
func MasterStabilityContext(totalNodes, masterEligible int) diagnostic.Result {
	res := diagnostic.Result{ID: "master_stability_context", Title: "Master 穩定性結構檢查", Category: "cluster", Source: "raw_api", Docs: []string{docUnstableCluster}}

	switch {
	case masterEligible == 0:
		res.Status, res.Conclusion = diagnostic.StatusCritical, diagnostic.ConclusionConfirmed
		res.Summary = "無 master-eligible 節點，叢集無法選出 master"
	case masterEligible == 1:
		res.Status, res.Conclusion = diagnostic.StatusWarning, diagnostic.ConclusionSuspected
		res.Summary = fmt.Sprintf("僅 1 個 master-eligible 節點（共 %d 個節點），此節點故障將導致無法選舉，屬單點故障", totalNodes)
	case masterEligible%2 == 0:
		res.Status, res.Conclusion = diagnostic.StatusWarning, diagnostic.ConclusionSuspected
		res.Summary = fmt.Sprintf("%d 個 master-eligible 節點為偶數（共 %d 個節點），網路分區時有選舉風險", masterEligible, totalNodes)
	default:
		return pass(res, fmt.Sprintf("%d 個 master-eligible 節點（共 %d 個節點），符合建議的奇數配置", masterEligible, totalNodes))
	}

	res.RootCauses = []string{"master-eligible 節點數建議為奇數且 ≥3，避免單點故障與 split-brain"}
	res.RequiresExtra, res.ExtraReason = true, "節點拓樸為結構性風險而非當下故障；是否曾實際發生選舉問題需查 master_is_stable indicator 與節點 log"
	res.Recommendations = []diagnostic.Recommendation{{Desc: "調整 master-eligible 節點數為奇數且 ≥3（小型叢集可加 dedicated master 或 voting-only 節點）"}}
	return res
}
