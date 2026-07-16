package main

import (
	"errors"
	"testing"

	"elk-diagnostics/internal/collector"
	"elk-diagnostics/internal/diagnostic"
	"elk-diagnostics/rules"
)

func testThresholds() rules.Thresholds {
	t, _ := rules.Load("")
	return t
}

func TestUnknownFrom(t *testing.T) {
	zero := diagnostic.Result{
		ID: "mapping_explosion", Title: "Mapping 欄位膨脹", Category: "data", Source: "raw_api",
		Docs:            []string{"https://example.com/doc"},
		Status:          diagnostic.StatusPass,
		Conclusion:      diagnostic.ConclusionNormal,
		Summary:         "各 index 欄位數均低於上限",
		RootCauses:      []string{"不應保留"},
		Recommendations: []diagnostic.Recommendation{{Desc: "不應保留"}},
		RequiresExtra:   true,
		ExtraReason:     "不應保留",
	}
	err := errors.New("ES 回應 404: /_mapping")

	got := unknownFrom(zero, err, false)

	if got.ID != "mapping_explosion" || got.Title != zero.Title || got.Category != zero.Category {
		t.Errorf("id/title/category 應保留自 zero，got %+v", got)
	}
	if len(got.Docs) != 1 || got.Docs[0] != zero.Docs[0] {
		t.Errorf("Docs 應保留自 zero，got %v", got.Docs)
	}
	if got.Status != diagnostic.StatusUnknown {
		t.Errorf("Status = %q, want unknown", got.Status)
	}
	if got.Conclusion != diagnostic.ConclusionNormal {
		t.Errorf("Conclusion = %q, want normal", got.Conclusion)
	}
	if len(got.Findings) != 1 || got.Findings[0] != err.Error() {
		t.Errorf("Findings 應含錯誤訊息，got %v", got.Findings)
	}
	if got.RootCauses != nil || got.Recommendations != nil {
		t.Error("失敗時不該保留來自 zero-call 的假設性根因/建議")
	}
	if got.RequiresExtra {
		t.Error("失敗時不該保留 zero-call 遺留的 RequiresExtra")
	}
	if got.Summary != "資料抓取失敗，無法判定" {
		t.Errorf("連線模式 Summary = %q，want 資料抓取失敗，無法判定", got.Summary)
	}
}

// TestUnknownFromBundleWording 驗證 T3：bundle 模式沒有「抓取」這個動作，措辭需與連線模式
// 不同，但 findings（含檔名）照舊保留完整錯誤訊息（見 spec-resilience §3）。
func TestUnknownFromBundleWording(t *testing.T) {
	zero := diagnostic.Result{ID: "cluster_settings", Title: "叢集設定", Category: "cluster"}
	err := errors.New("bundle 缺少 cluster_settings.json（採集腳本未執行此項或該端點當時失敗）")

	got := unknownFrom(zero, err, true)

	if got.Status != diagnostic.StatusUnknown {
		t.Errorf("Status = %q, want unknown", got.Status)
	}
	if got.Summary != "bundle 缺少該端點資料，無法判定" {
		t.Errorf("bundle 模式 Summary = %q，want bundle 缺少該端點資料，無法判定", got.Summary)
	}
	if len(got.Findings) != 1 || got.Findings[0] != err.Error() {
		t.Errorf("Findings 應保留完整錯誤訊息（含檔名），got %v", got.Findings)
	}
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
