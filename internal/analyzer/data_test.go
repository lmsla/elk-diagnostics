package analyzer

import (
	"testing"

	"elk-diagnostics/internal/collector"
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

func TestMappingExplosion(t *testing.T) {
	th := testThresholds()
	t.Run("遠低於門檻", func(t *testing.T) {
		res := MappingExplosion([]collector.IndexFieldCount{{Index: "idx1", FieldCount: 100}}, th)
		if res.Status != diagnostic.StatusPass {
			t.Errorf("Status = %q, want pass", res.Status)
		}
	})
	t.Run("逼近上限", func(t *testing.T) {
		res := MappingExplosion([]collector.IndexFieldCount{{Index: "idx1", FieldCount: 850}}, th)
		if res.Status != diagnostic.StatusWarning {
			t.Errorf("Status = %q, want warning", res.Status)
		}
	})
	t.Run("已達上限", func(t *testing.T) {
		res := MappingExplosion([]collector.IndexFieldCount{{Index: "idx1", FieldCount: 1000}}, th)
		if res.Status != diagnostic.StatusCritical {
			t.Errorf("Status = %q, want critical", res.Status)
		}
		if res.Conclusion != diagnostic.ConclusionConfirmed {
			t.Errorf("Conclusion = %q, want confirmed", res.Conclusion)
		}
	})
}

func TestIngestPipelineErrors(t *testing.T) {
	th := testThresholds()
	t.Run("失敗率正常", func(t *testing.T) {
		res := IngestPipelineErrors([]collector.IngestPipeline{{Pipeline: "p1", Count: 100, Failed: 1}}, th)
		if res.Status != diagnostic.StatusPass {
			t.Errorf("Status = %q, want pass", res.Status)
		}
	})
	t.Run("失敗率超過門檻", func(t *testing.T) {
		res := IngestPipelineErrors([]collector.IngestPipeline{{Pipeline: "p1", Count: 100, Failed: 20}}, th)
		if res.Status != diagnostic.StatusWarning {
			t.Errorf("Status = %q, want warning", res.Status)
		}
		if !res.RequiresExtra {
			t.Error("count/failed 為累積值，應要求額外確認")
		}
	})
	t.Run("count 為 0 不計算", func(t *testing.T) {
		res := IngestPipelineErrors([]collector.IngestPipeline{{Pipeline: "p1", Count: 0, Failed: 0}}, th)
		if res.Status != diagnostic.StatusPass {
			t.Errorf("Status = %q, want pass", res.Status)
		}
	})
}

func TestDataCorruption(t *testing.T) {
	t.Run("無 red index", func(t *testing.T) {
		res := DataCorruption([]collector.IndexHealth{{Index: "idx1", Health: "green", Status: "open"}})
		if res.Status != diagnostic.StatusPass {
			t.Errorf("Status = %q, want pass", res.Status)
		}
		if !res.RequiresExtra {
			t.Error("即使無 red，checksum 級毀損仍需另查 log，應標 RequiresExtra")
		}
	})
	t.Run("有 red index", func(t *testing.T) {
		res := DataCorruption([]collector.IndexHealth{{Index: "idx1", Health: "red", Status: "open"}})
		if res.Status != diagnostic.StatusWarning {
			t.Errorf("Status = %q, want warning", res.Status)
		}
		if len(res.RootCauses) == 0 {
			t.Error("red 時應給出可能根因")
		}
	})
}
