package analyzer

import (
	"os"
	"strings"
	"testing"

	"elk-diagnostics/internal/collector"
	"elk-diagnostics/internal/diagnostic"
)

const fixtureDir = "../../dev/phase0/fixtures/"

func loadHR(t *testing.T, path string) *collector.HealthReport {
	t.Helper()
	b, err := os.ReadFile(fixtureDir + path)
	if err != nil {
		t.Fatalf("讀 fixture 失敗: %v", err)
	}
	hr, err := collector.ParseHealthReport(b)
	if err != nil {
		t.Fatalf("解析失敗: %v", err)
	}
	return hr
}

func resultByID(results []diagnostic.Result, id string) *diagnostic.Result {
	for i := range results {
		if results[i].ID == id {
			return &results[i]
		}
	}
	return nil
}

func TestFromHealthReport_AllGreen(t *testing.T) {
	hr := loadHR(t, "es8-health/health_report_verbose.json")
	results := FromHealthReport(hr)
	if len(results) != len(healthReportIndicators) {
		t.Fatalf("結果數量 = %d, want %d", len(results), len(healthReportIndicators))
	}
	for _, r := range results {
		if r.Status != diagnostic.StatusPass {
			t.Errorf("%s.Status = %q, want pass（全綠 fixture 不應有其他狀態）", r.ID, r.Status)
		}
		if r.Conclusion != diagnostic.ConclusionNormal {
			t.Errorf("%s.Conclusion = %q, want normal", r.ID, r.Conclusion)
		}
	}
}

// shards_availability=yellow 時，cluster_health 應映射為 warning/suspected，
// 且逐條 diagnosis 的根因、去重後的建議、受影響 index 都要正確帶出。
func TestFromHealthReport_YellowMapsToWarning(t *testing.T) {
	hr := loadHR(t, "es8-unhealthy/health_report_verbose.json")
	results := FromHealthReport(hr)

	ch := resultByID(results, "cluster_health")
	if ch == nil {
		t.Fatal("找不到 cluster_health 結果")
	}
	if ch.Status != diagnostic.StatusWarning {
		t.Errorf("Status = %q, want warning", ch.Status)
	}
	if ch.Conclusion != diagnostic.ConclusionSuspected {
		t.Errorf("Conclusion = %q, want suspected", ch.Conclusion)
	}
	if len(ch.RootCauses) != 2 {
		t.Fatalf("RootCauses 數量 = %d, want 2", len(ch.RootCauses))
	}
	for _, c := range ch.RootCauses {
		if !strings.Contains(c, "受影響 index") {
			t.Errorf("RootCause 應內嵌受影響 index，實得: %q", c)
		}
	}
	// 兩筆 diagnosis 的 action 文字相同，應去重成 1 筆建議
	if len(ch.Recommendations) != 1 {
		t.Errorf("Recommendations 數量 = %d, want 1（相同 action 應去重）", len(ch.Recommendations))
	}
	if len(ch.Findings) != 1 {
		t.Errorf("Findings（impacts）數量 = %d, want 1", len(ch.Findings))
	}

	// 其餘 A 類項目（全綠部分）不應被 shards_availability 的異常影響
	disk := resultByID(results, "disk")
	if disk == nil || disk.Status != diagnostic.StatusPass {
		t.Errorf("disk 應維持 pass，實得: %+v", disk)
	}
}

// indicator 在該 ES 版本不存在時（如 <8.4 fallback 或未來欄位變動），應標 skipped 而非崩潰。
func TestFromHealthReport_MissingIndicatorSkipped(t *testing.T) {
	hr := &collector.HealthReport{
		Status:     "green",
		Indicators: map[string]collector.HRIndicator{}, // 空，模擬完全沒有 indicator
	}
	results := FromHealthReport(hr)
	if len(results) != len(healthReportIndicators) {
		t.Fatalf("結果數量 = %d, want %d（即使 indicators 全缺也要為每個項目產出結果）", len(results), len(healthReportIndicators))
	}
	for _, r := range results {
		if r.Status != diagnostic.StatusSkipped {
			t.Errorf("%s.Status = %q, want skipped", r.ID, r.Status)
		}
	}
}

// indicator status 為未知字串（非 green/yellow/red）時應標 unknown，不可誤判為其他狀態。
func TestFromHealthReport_UnknownStatus(t *testing.T) {
	hr := &collector.HealthReport{
		Status: "green",
		Indicators: map[string]collector.HRIndicator{
			"shards_availability": {Status: "weird_future_status"},
		},
	}
	results := FromHealthReport(hr)
	ch := resultByID(results, "cluster_health")
	if ch == nil {
		t.Fatal("找不到 cluster_health 結果")
	}
	if ch.Status != diagnostic.StatusUnknown {
		t.Errorf("Status = %q, want unknown", ch.Status)
	}
}

// 9.x 多出的 file_settings 不在 driver table 內，FromHealthReport 只依自己的表遍歷，
// 不應該因為 hr.Indicators 多了未知 key 就出錯或多生結果。
func TestFromHealthReport_ExtraIndicatorIgnored(t *testing.T) {
	hr := loadHR(t, "es9-healthy/health_report_verbose.json")
	results := FromHealthReport(hr)
	if len(results) != len(healthReportIndicators) {
		t.Errorf("結果數量 = %d, want %d（file_settings 不應多產出結果）", len(results), len(healthReportIndicators))
	}
	if resultByID(results, "file_settings") != nil {
		t.Error("file_settings 不在 driver table 內，不應出現在結果中")
	}
}
