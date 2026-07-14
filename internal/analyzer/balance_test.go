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
		if res.Status != diagnostic.StatusWarning {
			t.Errorf("Status = %q, want warning", res.Status)
		}
		if !res.RequiresExtra {
			t.Error("為瞬時快照，應要求額外確認")
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
		want int
	}{
		{"empty", nil, 0},
		{"odd", []int{3, 1, 2}, 2},
		{"even", []int{4, 1, 2, 3}, 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := median(c.in); got != c.want {
				t.Errorf("median(%v) = %d, want %d", c.in, got, c.want)
			}
		})
	}
}
