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
	res := diagnostic.Result{
		ID: "hot_spotting", Title: "Hot spotting（資源分布不均）", Category: "performance", Source: "raw_api", Docs: []string{docHotspot},
		JudgmentGuide: []diagnostic.JudgmentGuide{
			{Condition: "單次節點差距", Interpretation: "可能只是瞬時變化，不能單獨判定故障"},
			{Condition: "同一節點、同一指標在時間序列中持續偏高", Interpretation: "需觀察，請以 Monitoring 或前後兩次採集確認"},
			{Condition: "差距同時伴隨 queue、rejected、延遲或 hot threads 上升", Interpretation: "提高處置優先級"},
			{Condition: "429、circuit breaker、red 或 node unavailable", Interpretation: "由對應診斷卡判定故障"},
		},
	}
	if len(nodes) < 2 {
		res.Measurements = append(res.Measurements, gauge("elasticsearch.cluster.node.count", float64(len(nodes)), "count", "", "", "", ""))
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

	// 只在相同 node.role 內比較，避免把不同 tier／角色的合理資源差異誤判為
	// hot spotting。角色缺失時退回所有缺失角色節點同組，並在報告中揭露比較群組。
	groups := make(map[string][]collector.NodeCPU)
	var groupOrder []string
	for _, n := range nodes {
		group := peerGroup(n.Role)
		if _, ok := groups[group]; !ok {
			groupOrder = append(groupOrder, group)
		}
		groups[group] = append(groups[group], n)
	}

	var hits []string
	singletonGroups := 0
	for _, group := range groupOrder {
		members := groups[group]
		if len(members) < 2 {
			singletonGroups++
		}
		for _, m := range metrics {
			vals := make([]int, 0, len(members))
			for _, n := range members {
				vals = append(vals, m.get(n))
			}
			med := median(vals)
			if len(members) >= 2 {
				res.Measurements = append(res.Measurements, gaugeInPeerGroup("elasticsearch.cluster.resource.median", med, "percent", "cluster", "", "同類節點中位數", m.name, group))
			}
			for _, n := range members {
				v := m.get(n)
				res.Measurements = append(res.Measurements, gaugeInPeerGroup("elasticsearch.node.resource.current", float64(v), "percent", "node", n.Name, n.Name, m.name, group))
				if len(members) < 2 {
					continue
				}
				diff := float64(v) - med
				res.Measurements = append(res.Measurements, gaugeInPeerGroup("elasticsearch.node.resource.deviation_from_median", diff, "percentage_point", "node", n.Name, n.Name, m.name, group))
				if v >= m.floor && diff >= float64(hotspotSpread) {
					hits = append(hits, fmt.Sprintf("%s（%s）：%s=%d%%（同類節點中位數 %.1f%%，差距 %+.1f 個百分點）", n.Name, group, m.name, v, med, diff))
				}
			}
		}
	}

	if len(hits) == 0 && singletonGroups == 0 {
		return pass(res, "各節點資源利用分布均衡")
	}
	res.Status, res.Conclusion = diagnostic.StatusInfo, diagnostic.ConclusionNormal
	if len(hits) > 0 {
		res.Summary = "偵測到節點資源利用分布不均，需觀察是否持續"
	} else {
		res.Summary = "部分同類節點不足，Hot spotting 無法完整判定"
	}
	res.Findings = hits
	if singletonGroups > 0 {
		res.Findings = append(res.Findings, fmt.Sprintf("%d 個 node.role 比較群組只有 1 個節點，無法計算同類中位數", singletonGroups))
	}
	res.RequiresExtra = true
	res.ExtraReason = "本次為單次快照；資源差距不能單獨判定 hot spotting，請以 Monitoring 或前後兩次採集確認是否持續"
	res.Recommendations = []diagnostic.Recommendation{{Desc: "檢查硬體規格是否一致、shard 分布與寫入路由；write-heavy index 可設 index.routing.allocation.total_shards_per_node 分散"}}
	return res
}

// Unbalanced #18：cat allocation 的 shards.undesired（>0 表有待搬移 shard）。
func Unbalanced(rows []collector.AllocationRow) diagnostic.Result {
	res := diagnostic.Result{ID: "unbalanced_cluster", Title: "叢集 shard 分布不均", Category: "cluster", Source: "raw_api", Docs: []string{docUnbalanced}}
	for _, row := range rows {
		res.Measurements = append(res.Measurements,
			gauge("elasticsearch.node.shard.count", float64(row.Shards), "count", "node", row.Node, row.Node, ""),
			gauge("elasticsearch.node.shard.undesired", float64(row.ShardsUndesired), "count", "node", row.Node, row.Node, ""),
			gauge("elasticsearch.node.disk.used", float64(row.DiskPercent), "percent", "node", row.Node, row.Node, ""),
		)
	}
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

func peerGroup(role string) string {
	if role == "" {
		return "node.role=未提供"
	}
	return "node.role=" + role
}

func median(v []int) float64 {
	if len(v) == 0 {
		return 0
	}
	s := append([]int(nil), v...)
	sort.Ints(s)
	n := len(s)
	if n%2 == 1 {
		return float64(s[n/2])
	}
	return float64(s[n/2-1]+s[n/2]) / 2
}
