package analyzer

import (
	"testing"

	"elk-diagnostics/internal/collector"
	"elk-diagnostics/internal/diagnostic"
)

func TestDataAllocationBlocked(t *testing.T) {
	cases := []struct {
		enable     string
		wantStatus diagnostic.Status
	}{
		{"all", diagnostic.StatusPass},
		{"", diagnostic.StatusPass}, // 未設定視同預設 all
		{"primaries", diagnostic.StatusCritical},
		{"none", diagnostic.StatusCritical},
	}
	for _, c := range cases {
		res := DataAllocationBlocked(c.enable)
		if res.Status != c.wantStatus {
			t.Errorf("enable=%q: Status = %q, want %q", c.enable, res.Status, c.wantStatus)
		}
	}
}

func TestIndexAllocationBlocked(t *testing.T) {
	t.Run("無受影響 index", func(t *testing.T) {
		res := IndexAllocationBlocked(map[string]string{})
		if res.Status != diagnostic.StatusPass {
			t.Errorf("Status = %q, want pass", res.Status)
		}
	})
	t.Run("皆正常", func(t *testing.T) {
		res := IndexAllocationBlocked(map[string]string{"idx-a": "all", "idx-b": ""})
		if res.Status != diagnostic.StatusPass {
			t.Errorf("Status = %q, want pass", res.Status)
		}
	})
	t.Run("部分被封鎖", func(t *testing.T) {
		res := IndexAllocationBlocked(map[string]string{"idx-a": "all", "idx-b": "none"})
		if res.Status != diagnostic.StatusWarning {
			t.Fatalf("Status = %q, want warning", res.Status)
		}
		if len(res.Findings) != 1 {
			t.Errorf("Findings 數量 = %d, want 1", len(res.Findings))
		}
	})
}

func TestAllocationGuidance(t *testing.T) {
	t.Run("無未分配 shard", func(t *testing.T) {
		res := AllocationGuidance(nil, false)
		if res.Status != diagnostic.StatusPass {
			t.Errorf("Status = %q, want pass", res.Status)
		}
	})
	t.Run("有 shard 但無 decider 封鎖", func(t *testing.T) {
		exp := &collector.AllocationExplanation{Index: "idx-a", Shard: 0, Primary: true}
		res := AllocationGuidance(exp, true)
		if res.Status != diagnostic.StatusPass {
			t.Errorf("Status = %q, want pass", res.Status)
		}
		if !res.RequiresExtra {
			t.Error("RequiresExtra 應為 true（代表性抽查，非窮舉）")
		}
	})
	t.Run("有 decider 拒絕", func(t *testing.T) {
		exp := &collector.AllocationExplanation{
			Index: "idx-a", Shard: 2, Primary: false,
			Deciders: []collector.AllocationDecider{
				{Decider: "disk_threshold", Decision: "NO", Explanation: "節點磁碟超過 watermark"},
			},
		}
		res := AllocationGuidance(exp, true)
		if res.Status != diagnostic.StatusWarning {
			t.Fatalf("Status = %q, want warning", res.Status)
		}
		if len(res.Findings) != 1 {
			t.Errorf("Findings 數量 = %d, want 1", len(res.Findings))
		}
	})
}
