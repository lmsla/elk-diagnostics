package analyzer

import (
	"fmt"
	"sort"

	"elk-diagnostics/internal/collector"
	"elk-diagnostics/internal/diagnostic"
	"elk-diagnostics/rules"
)

const (
	docHotspot    = "https://www.elastic.co/docs/troubleshoot/elasticsearch/hotspotting"
	docUnbalanced = "https://www.elastic.co/docs/troubleshoot/elasticsearch/troubleshooting-unbalanced-cluster"
)

// HotSpotting #17：某節點 cpu/heap/disk 顯著高於同儕（需 ≥2 節點）。
func HotSpotting(nodes []collector.NodeCPU, t rules.Thresholds) diagnostic.Result {
	hotspotSpread := t.Balance.HotspotSpreadPct
	res := diagnostic.Result{ID: "hot_spotting", Title: "Hot spotting（資源分布不均）", Category: "performance", Source: "raw_api", Docs: []string{docHotspot}}
	if len(nodes) < 2 {
		return pass(res, "單節點或節點數不足，無從比較資源分布")
	}

	type metric struct {
		name  string
		floor int
		get   func(collector.NodeCPU) int
	}
	metrics := []metric{
		{"cpu", 50, func(n collector.NodeCPU) int { return n.CPU }},
		{"heap.percent", 50, func(n collector.NodeCPU) int { return n.HeapPercent }},
		{"disk.used_percent", 75, func(n collector.NodeCPU) int { return n.DiskPercent }},
	}

	var hits []string
	for _, m := range metrics {
		vals := make([]int, 0, len(nodes))
		for _, n := range nodes {
			vals = append(vals, m.get(n))
		}
		med := median(vals)
		for _, n := range nodes {
			v := m.get(n)
			if v >= m.floor && v-med >= hotspotSpread {
				hits = append(hits, fmt.Sprintf("%s：%s=%d（叢集中位數 %d）", n.Name, m.name, v, med))
			}
		}
	}

	if len(hits) == 0 {
		return pass(res, "各節點資源利用分布均衡")
	}
	res.Status, res.Conclusion = diagnostic.StatusWarning, diagnostic.ConclusionSuspected
	res.Summary = "偵測到節點資源利用顯著高於同儕（疑似 hot spotting）"
	res.Findings = hits
	res.RequiresExtra, res.ExtraReason = true, "為瞬時快照；偶發尖峰屬正常，需持續（如 >30 秒）才算問題，建議搭配時間序列佐證"
	res.Recommendations = []diagnostic.Recommendation{{Desc: "檢查硬體規格是否一致、shard 分布與寫入路由；write-heavy index 可設 index.routing.allocation.total_shards_per_node 分散"}}
	return res
}

// Unbalanced #18：cat allocation 的 shards.undesired（>0 表有待搬移 shard）。
func Unbalanced(rows []collector.AllocationRow) diagnostic.Result {
	res := diagnostic.Result{ID: "unbalanced_cluster", Title: "叢集 shard 分布不均", Category: "cluster", Source: "raw_api", Docs: []string{docUnbalanced}}
	if len(rows) < 2 {
		return pass(res, "單節點或節點數不足，無從評估分布平衡")
	}

	totalUndesired := 0
	var detail []string
	for _, r := range rows {
		totalUndesired += r.ShardsUndesired
		detail = append(detail, fmt.Sprintf("%s：shards=%d undesired=%d disk=%d%%", r.Node, r.Shards, r.ShardsUndesired, r.DiskPercent))
	}

	if totalUndesired == 0 {
		return pass(res, "所有 shard 皆在理想節點（無待搬移）")
	}
	res.Status, res.Conclusion = diagnostic.StatusWarning, diagnostic.ConclusionSuspected
	res.Summary = fmt.Sprintf("有 %d 個 shard 未在理想節點（rebalancing 進行中或偏離）", totalUndesired)
	res.Findings = detail
	res.RequiresExtra, res.ExtraReason = true, "節點重啟/設定變更後短暫不平衡屬正常；若待搬移數長時間（數小時）不降才需處理"
	res.Recommendations = []diagnostic.Recommendation{{Desc: "持續觀察 undesired 是否遞減；長期偏離可調 cluster.routing.allocation.balance.threshold 後重置 desired balance"}}
	return res
}

func median(v []int) int {
	if len(v) == 0 {
		return 0
	}
	s := append([]int(nil), v...)
	sort.Ints(s)
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}
