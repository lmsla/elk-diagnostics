package analyzer

import (
	"testing"

	"elk-diagnostics/internal/collector"
	"elk-diagnostics/internal/diagnostic"
)

func TestIlmTierMigration(t *testing.T) {
	t.Run("無遷移中", func(t *testing.T) {
		res := IlmTierMigration(nil)
		if res.Status != diagnostic.StatusPass {
			t.Errorf("Status = %q, want pass", res.Status)
		}
	})
	t.Run("有遷移候選", func(t *testing.T) {
		res := IlmTierMigration([]collector.IlmMigration{
			{Index: "logs-2026.01", Phase: "warm", Step: "migrate"},
		})
		if res.Status != diagnostic.StatusWarning {
			t.Fatalf("Status = %q, want warning", res.Status)
		}
		if !res.RequiresExtra {
			t.Error("RequiresExtra 應為 true（單次快照無法確認是否卡住）")
		}
		if len(res.Findings) != 1 {
			t.Errorf("Findings 數量 = %d, want 1", len(res.Findings))
		}
	})
}
