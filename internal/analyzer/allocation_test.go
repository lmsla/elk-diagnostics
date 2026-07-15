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
		res := IndexAllocationBlocked(map[string]string{}, nil)
		if res.Status != diagnostic.StatusPass {
			t.Errorf("Status = %q, want pass", res.Status)
		}
	})
	t.Run("皆正常", func(t *testing.T) {
		res := IndexAllocationBlocked(map[string]string{"idx-a": "all", "idx-b": ""}, nil)
		if res.Status != diagnostic.StatusPass {
			t.Errorf("Status = %q, want pass", res.Status)
		}
	})
	t.Run("部分被封鎖", func(t *testing.T) {
		res := IndexAllocationBlocked(map[string]string{"idx-a": "all", "idx-b": "none"}, nil)
		if res.Status != diagnostic.StatusWarning {
			t.Fatalf("Status = %q, want warning", res.Status)
		}
		if len(res.Findings) != 1 {
			t.Errorf("Findings 數量 = %d, want 1", len(res.Findings))
		}
	})

	// 查不到 ≠ 沒問題：以下兩個案例鎖住 2026-07-15 那批 bug 的共同模式。
	t.Run("全部查不到時判 unknown，不可宣稱正常", func(t *testing.T) {
		res := IndexAllocationBlocked(map[string]string{}, []string{"idx-a", "idx-b"})
		if res.Status != diagnostic.StatusUnknown {
			t.Fatalf("Status = %q, want unknown（查不到不等於正常）", res.Status)
		}
		if !res.RequiresExtra {
			t.Error("應提示為何查不到（權限不足或 bundle 模式）")
		}
	})
	t.Run("已查的皆正常但仍有查不到的，判 unknown", func(t *testing.T) {
		res := IndexAllocationBlocked(map[string]string{"idx-a": "all"}, []string{"idx-b"})
		if res.Status != diagnostic.StatusUnknown {
			t.Errorf("Status = %q, want unknown（idx-b 未查，不能替它背書）", res.Status)
		}
	})
	t.Run("已找到封鎖時仍揭露查不到的部分", func(t *testing.T) {
		res := IndexAllocationBlocked(map[string]string{"idx-a": "none"}, []string{"idx-b"})
		if res.Status != diagnostic.StatusWarning {
			t.Fatalf("Status = %q, want warning（已確認的封鎖優先於未知）", res.Status)
		}
		if len(res.Findings) != 2 {
			t.Errorf("Findings = %v, want 2（封鎖項 + 查不到的揭露）", res.Findings)
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
