package analyzer

import (
	"testing"

	"elk-diagnostics/internal/collector"
	"elk-diagnostics/internal/diagnostic"
)

func TestWriteBottleneck(t *testing.T) {
	th := testThresholds()

	t.Run("無 queue 積壓", func(t *testing.T) {
		cpus := []collector.NodeCPU{{Name: "n1", CPU: 80, AllocatedProcessors: 8}}
		pools := []collector.WritePoolRow{{Node: "n1", Queue: 0, Size: 8}}
		res := WriteBottleneck(cpus, pools, th)
		if res.Status != diagnostic.StatusPass {
			t.Errorf("Status = %q, want pass", res.Status)
		}
		if res.Conclusion != diagnostic.ConclusionNormal {
			t.Errorf("Conclusion = %q, want normal", res.Conclusion)
		}
	})

	t.Run("完整因果鏈成立：CPU低+queue積壓+processors偏低", func(t *testing.T) {
		cpus := []collector.NodeCPU{{Name: "n1", CPU: 20, AllocatedProcessors: 2}}
		pools := []collector.WritePoolRow{{Node: "n1", Queue: 5, Size: 2}}
		res := WriteBottleneck(cpus, pools, th)
		if res.Status != diagnostic.StatusCritical {
			t.Errorf("Status = %q, want critical", res.Status)
		}
		if res.Conclusion != diagnostic.ConclusionConfirmed {
			t.Errorf("Conclusion = %q, want confirmed", res.Conclusion)
		}
		if len(res.RootCauses) == 0 {
			t.Error("因果鏈確認時應給出 RootCauses")
		}
	})

	t.Run("僅 queue 積壓，因果鏈未完全成立", func(t *testing.T) {
		cpus := []collector.NodeCPU{{Name: "n1", CPU: 90, AllocatedProcessors: 16}}
		pools := []collector.WritePoolRow{{Node: "n1", Queue: 5, Size: 16}}
		res := WriteBottleneck(cpus, pools, th)
		if res.Status != diagnostic.StatusWarning {
			t.Errorf("Status = %q, want warning", res.Status)
		}
		if res.Conclusion != diagnostic.ConclusionSuspected {
			t.Errorf("Conclusion = %q, want suspected", res.Conclusion)
		}
		if !res.RequiresExtra {
			t.Error("queue 為瞬時值，應要求雙取樣確認")
		}
	})
}

func TestYn(t *testing.T) {
	if yn(true) != "是" {
		t.Error(`yn(true) want "是"`)
	}
	if yn(false) != "否" {
		t.Error(`yn(false) want "否"`)
	}
}
