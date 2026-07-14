package analyzer

import (
	"testing"

	"elk-diagnostics/internal/collector"
	"elk-diagnostics/internal/diagnostic"
)

func TestRestoreStatus(t *testing.T) {
	t.Run("無還原操作", func(t *testing.T) {
		res := RestoreStatus(nil)
		if res.Status != diagnostic.StatusPass {
			t.Errorf("Status = %q, want pass", res.Status)
		}
		if res.RequiresExtra {
			t.Error("無還原操作時不應要求額外確認")
		}
	})
	t.Run("有還原進行中", func(t *testing.T) {
		res := RestoreStatus([]collector.RestoreOperation{
			{Index: "restored-idx", Shard: 0, Stage: "INDEX", Percent: "42.3%"},
		})
		if res.Status != diagnostic.StatusPass {
			t.Errorf("Status = %q, want pass（資訊性）", res.Status)
		}
		if !res.RequiresExtra {
			t.Error("有還原操作時應提示需間隔重查")
		}
		if len(res.Findings) != 1 {
			t.Errorf("Findings 數量 = %d, want 1", len(res.Findings))
		}
	})
}
