package analyzer

import (
	"testing"

	"elk-diagnostics/internal/collector"
	"elk-diagnostics/internal/diagnostic"
)

func TestHotSpotting(t *testing.T) {
	th := testThresholds()
	t.Run("單節點無從比較", func(t *testing.T) {
		res := HotSpotting([]collector.NodeCPU{{Name: "n1", CPU: 90}}, th)
		if res.Status != diagnostic.StatusPass {
			t.Errorf("Status = %q, want pass", res.Status)
		}
	})
	t.Run("分布均衡", func(t *testing.T) {
		nodes := []collector.NodeCPU{
			{Name: "n1", CPU: 40, HeapPercent: 40, DiskPercent: 50},
			{Name: "n2", CPU: 42, HeapPercent: 41, DiskPercent: 52},
		}
		res := HotSpotting(nodes, th)
		if res.Status != diagnostic.StatusPass {
			t.Errorf("Status = %q, want pass", res.Status)
		}
	})
	t.Run("單節點顯著高於同儕", func(t *testing.T) {
		nodes := []collector.NodeCPU{
			{Name: "n1", CPU: 95, HeapPercent: 40, DiskPercent: 50},
			{Name: "n2", CPU: 20, HeapPercent: 40, DiskPercent: 50},
		}
		res := HotSpotting(nodes, th)
		if res.Status != diagnostic.StatusInfo {
			t.Errorf("Status = %q, want info", res.Status)
		}
		if !res.RequiresExtra {
			t.Error("為瞬時快照，應要求額外確認")
		}
	})
	t.Run("只在同類角色內比較", func(t *testing.T) {
		nodes := []collector.NodeCPU{
			{Name: "hot", Role: "data", HeapPercent: 95},
			{Name: "peer", Role: "data", HeapPercent: 20},
			{Name: "other-role", Role: "master", HeapPercent: 95},
		}
		res := HotSpotting(nodes, th)
		if res.Status != diagnostic.StatusInfo {
			t.Fatalf("Status = %q, want info", res.Status)
		}
		found := false
		for _, f := range res.Findings {
			if f == "hot（node.role=data）：heap.percent=95%（同類節點中位數 57.5%，差距 +37.5 個百分點）" {
				found = true
			}
		}
		if !found {
			t.Error("應在 data 角色群組內辨識出 hot 節點")
		}
		for _, m := range res.Measurements {
			if m.Metric == "elasticsearch.node.resource.current" && m.EntityName == "other-role" && m.PeerGroup != "node.role=master" {
				t.Errorf("other-role PeerGroup = %q", m.PeerGroup)
			}
		}
	})
}

func TestUnbalanced(t *testing.T) {
	t.Run("單節點無從評估", func(t *testing.T) {
		res := Unbalanced([]collector.AllocationRow{{Node: "n1", Shards: 10}})
		if res.Status != diagnostic.StatusPass {
			t.Errorf("Status = %q, want pass", res.Status)
		}
	})
	t.Run("無 undesired shard", func(t *testing.T) {
		rows := []collector.AllocationRow{
			{Node: "n1", Shards: 10, ShardsUndesired: 0},
			{Node: "n2", Shards: 10, ShardsUndesired: 0},
		}
		res := Unbalanced(rows)
		if res.Status != diagnostic.StatusPass {
			t.Errorf("Status = %q, want pass", res.Status)
		}
	})
	t.Run("有待搬移 shard", func(t *testing.T) {
		rows := []collector.AllocationRow{
			{Node: "n1", Shards: 10, ShardsUndesired: 5},
			{Node: "n2", Shards: 10, ShardsUndesired: 0},
		}
		res := Unbalanced(rows)
		if res.Status != diagnostic.StatusWarning {
			t.Errorf("Status = %q, want warning", res.Status)
		}
		if !res.RequiresExtra {
			t.Error("應提示短暫不平衡屬正常，需持續觀察")
		}
	})
}

func TestMedian(t *testing.T) {
	cases := []struct {
		name string
		in   []int
		want float64
	}{
		{"empty", nil, 0},
		{"odd", []int{3, 1, 2}, 2},
		{"even", []int{4, 1, 2, 3}, 2.5},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := median(c.in); got != c.want {
				t.Errorf("median(%v) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}
