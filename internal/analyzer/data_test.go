package analyzer

import (
	"testing"

	"elk-diagnostics/internal/diagnostic"
)

func TestDataTierAvailability(t *testing.T) {
	t.Run("全部 tier 皆有節點", func(t *testing.T) {
		counts := map[string]int{"data_content": 3, "data_hot": 3, "data_warm": 2, "data_cold": 1, "data_frozen": 1}
		res := DataTierAvailability(counts)
		if res.Status != diagnostic.StatusPass {
			t.Errorf("Status = %q, want pass", res.Status)
		}
		if res.RequiresExtra {
			t.Error("全部 tier 皆有節點時不應要求額外確認")
		}
	})
	t.Run("缺 warm/cold/frozen（常見於小叢集，仍是資訊性）", func(t *testing.T) {
		counts := map[string]int{"data_content": 3, "data_hot": 3}
		res := DataTierAvailability(counts)
		if res.Status != diagnostic.StatusPass {
			t.Errorf("Status = %q, want pass（資訊性，不應直接判異常）", res.Status)
		}
		if !res.RequiresExtra {
			t.Error("缺 tier 時應提示需對照 indicator 診斷交叉確認")
		}
		if len(res.Findings) != 2 {
			t.Errorf("Findings 數量 = %d, want 2", len(res.Findings))
		}
	})
}
