package main

import (
	"testing"

	"elk-diagnostics/internal/collector"
	"elk-diagnostics/internal/diagnostic"
	"elk-diagnostics/rules"
)

func testThresholds() rules.Thresholds {
	t, _ := rules.Load("")
	return t
}

func TestSuggestSymptoms(t *testing.T) {
	th := testThresholds()

	t.Run("無異常特徵時不提示", func(t *testing.T) {
		results := []diagnostic.Result{{ID: "ilm_slm_status", Status: diagnostic.StatusPass}}
		hints := suggestSymptoms(results, nil, nil, th)
		if len(hints) != 0 {
			t.Errorf("hints = %v, want empty", hints)
		}
	})

	t.Run("ILM critical 提示 ilm-stuck", func(t *testing.T) {
		results := []diagnostic.Result{{ID: "ilm_slm_status", Status: diagnostic.StatusCritical}}
		hints := suggestSymptoms(results, nil, nil, th)
		if len(hints) != 1 || hints[0].Symptom != "ilm-stuck" {
			t.Errorf("hints = %v, want [ilm-stuck]", hints)
		}
	})

	t.Run("write-bottleneck 因果鏈成立時提示", func(t *testing.T) {
		cpus := []collector.NodeCPU{{Name: "n1", CPU: 20, AllocatedProcessors: 2}}
		pools := []collector.WritePoolRow{{Node: "n1", Queue: 5, Size: 2}}
		hints := suggestSymptoms(nil, cpus, pools, th)
		if len(hints) != 1 || hints[0].Symptom != "write-bottleneck" {
			t.Errorf("hints = %v, want [write-bottleneck]", hints)
		}
	})

	t.Run("兩者同時成立各自提示", func(t *testing.T) {
		results := []diagnostic.Result{{ID: "ilm_slm_status", Status: diagnostic.StatusCritical}}
		cpus := []collector.NodeCPU{{Name: "n1", CPU: 20, AllocatedProcessors: 2}}
		pools := []collector.WritePoolRow{{Node: "n1", Queue: 5, Size: 2}}
		hints := suggestSymptoms(results, cpus, pools, th)
		if len(hints) != 2 {
			t.Errorf("hints = %v, want 2 hints", hints)
		}
	})

	t.Run("write pool 資料缺失（採集失敗）時不誤判", func(t *testing.T) {
		cpus := []collector.NodeCPU{{Name: "n1", CPU: 20, AllocatedProcessors: 2}}
		hints := suggestSymptoms(nil, cpus, nil, th)
		if len(hints) != 0 {
			t.Errorf("hints = %v, want empty（pools 為空應視為資料不足，不強行判定）", hints)
		}
	})
}
