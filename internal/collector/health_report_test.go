package collector

import (
	"os"
	"testing"
)

// 用 dev/phase0/fixtures/ 的真機 fixture 驗證解析基座；A/B 類與未來 B 類都建立在
// 這份解析結果上，壞了會連帶影響所有讀 health_report 的診斷。
const fixtureDir = "../../dev/phase0/fixtures/"

func loadFixture(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(fixtureDir + path)
	if err != nil {
		t.Fatalf("讀 fixture 失敗: %v", err)
	}
	return b
}

func TestParseHealthReport_AllFixtures(t *testing.T) {
	cases := []struct {
		path       string
		wantStatus string
		wantKeys   []string // 至少要含這些 indicator
	}{
		{"es8-health/health_report_verbose.json", "green", []string{"shards_availability", "disk", "ilm"}},
		{"es8-unhealthy/health_report_verbose.json", "yellow", []string{"shards_availability"}},
		{"es9-healthy/health_report_verbose.json", "green", []string{"shards_availability", "file_settings"}},
		{"es9-unhealthy/health_report_verbose.json", "yellow", []string{"shards_availability", "file_settings"}},
	}
	for _, c := range cases {
		t.Run(c.path, func(t *testing.T) {
			hr, err := ParseHealthReport(loadFixture(t, c.path))
			if err != nil {
				t.Fatalf("解析失敗: %v", err)
			}
			if hr.Status != c.wantStatus {
				t.Errorf("top status = %q, want %q", hr.Status, c.wantStatus)
			}
			for _, k := range c.wantKeys {
				if _, ok := hr.Indicators[k]; !ok {
					t.Errorf("缺少 indicator %q", k)
				}
			}
		})
	}
}

// 9.0.0 多出 file_settings（8.14.3 沒有）；解析器必須容忍未知/新增 indicator，
// 不能因為欄位沒見過就漏解析其餘 indicator 或出錯。
func TestParseHealthReport_UnknownIndicatorTolerated(t *testing.T) {
	hr, err := ParseHealthReport(loadFixture(t, "es9-healthy/health_report_verbose.json"))
	if err != nil {
		t.Fatalf("解析失敗: %v", err)
	}
	fs, ok := hr.Indicators["file_settings"]
	if !ok {
		t.Fatal("file_settings 應存在於 9.x fixture")
	}
	if fs.Status != "green" {
		t.Errorf("file_settings.status = %q, want green", fs.Status)
	}
	// 其餘已知 indicator 不應被 file_settings 影響
	if _, ok := hr.Indicators["shards_availability"]; !ok {
		t.Error("file_settings 存在時，shards_availability 不應消失")
	}
}

func TestParseHealthReport_DiagnosisDetail(t *testing.T) {
	hr, err := ParseHealthReport(loadFixture(t, "es8-unhealthy/health_report_verbose.json"))
	if err != nil {
		t.Fatalf("解析失敗: %v", err)
	}
	sa := hr.Indicators["shards_availability"]
	if sa.Status != "yellow" {
		t.Fatalf("shards_availability.status = %q, want yellow", sa.Status)
	}
	if len(sa.Diagnosis) != 2 {
		t.Fatalf("diagnosis 數量 = %d, want 2", len(sa.Diagnosis))
	}
	if sa.Diagnosis[0].AffectedResources.Indices[0] != "elkdoctor-ilmerr" {
		t.Errorf("affected_resources.indices[0] = %q, want elkdoctor-ilmerr", sa.Diagnosis[0].AffectedResources.Indices[0])
	}
	if len(sa.Impacts) != 1 || sa.Impacts[0].Severity != 2 {
		t.Errorf("impacts 解析不符預期: %+v", sa.Impacts)
	}
}

func TestParseHealthReport_InvalidJSON(t *testing.T) {
	if _, err := ParseHealthReport([]byte("not json")); err == nil {
		t.Error("非法 JSON 應回傳 error，卻沒有")
	}
}
