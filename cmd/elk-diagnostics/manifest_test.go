package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"elk-diagnostics/internal/collector"
	"elk-diagnostics/internal/diagnostic"
)

// copyFixtureBundle 由 resilience_test.go 提供（兩批工單各自需要同一個 helper，合併時去重）。

// TestCheckFromBundle_ManifestPresent 對映驗收條件：含 manifest 的 bundle →
// JSON meta 有兩個新欄位、HTML 頁首顯示採集時間。
func TestCheckFromBundle_ManifestPresent(t *testing.T) {
	bundle := copyFixtureBundle(t, "es9-unhealthy")
	manifest := `{
  "collect_script_version": "0.0.5",
  "collected_at": "2026-07-16T02:10:00Z",
  "host": "https://es.example.local:9200",
  "endpoints_total": 24
}
`
	if err := os.WriteFile(filepath.Join(bundle, collector.BundleManifestFile), []byte(manifest), 0644); err != nil {
		t.Fatal(err)
	}

	jsonOut := filepath.Join(t.TempDir(), "report.json")
	runCheck(newTestConnFlags(t, nil, "", ""), "", bundle, "json", jsonOut, false)
	b, err := os.ReadFile(jsonOut)
	if err != nil {
		t.Fatal(err)
	}
	var report diagnostic.Report
	if err := json.Unmarshal(b, &report); err != nil {
		t.Fatal(err)
	}
	if report.Meta.CollectedAt != "2026-07-16T02:10:00Z" {
		t.Errorf("Meta.CollectedAt = %q, want 2026-07-16T02:10:00Z", report.Meta.CollectedAt)
	}
	if report.Meta.CollectScriptVersion != "0.0.5" {
		t.Errorf("Meta.CollectScriptVersion = %q, want 0.0.5", report.Meta.CollectScriptVersion)
	}

	htmlOut := filepath.Join(t.TempDir(), "report.html")
	runCheck(newTestConnFlags(t, nil, "", ""), "", bundle, "html", htmlOut, false)
	hb, err := os.ReadFile(htmlOut)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(hb), "2026-07-16T02:10:00Z") {
		t.Error("HTML 頁首應顯示採集時間")
	}
}

// TestCheckFromBundle_ManifestAbsent 對映驗收條件：舊 bundle（fixture 目錄）→
// 欄位省略、HTML 有註明、不報錯。
func TestCheckFromBundle_ManifestAbsent(t *testing.T) {
	bundle := fixtureDir("es9-unhealthy") // checked-in fixture，本來就沒有 _manifest.json

	jsonOut := filepath.Join(t.TempDir(), "report.json")
	code := runCheck(newTestConnFlags(t, nil, "", ""), "", bundle, "json", jsonOut, false)
	t.Logf("exit_code=%d", code)
	b, err := os.ReadFile(jsonOut)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), `"collected_at"`) || strings.Contains(string(b), `"collect_script_version"`) {
		t.Error("無 manifest 時 JSON 不應出現 collected_at/collect_script_version 欄位（omitempty）")
	}
	var report diagnostic.Report
	if err := json.Unmarshal(b, &report); err != nil {
		t.Fatalf("仍應可正常解析、不報錯: %v", err)
	}
	if report.Meta.CollectedAt != "" || report.Meta.CollectScriptVersion != "" {
		t.Error("無 manifest 時欄位應為空")
	}

	htmlOut := filepath.Join(t.TempDir(), "report.html")
	runCheck(newTestConnFlags(t, nil, "", ""), "", bundle, "html", htmlOut, false)
	hb, err := os.ReadFile(htmlOut)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(hb), "bundle 未含採集時間") {
		t.Error("無 manifest 時 HTML 應註明「bundle 未含採集時間（舊版採集腳本）」")
	}
}

// TestManifestDoesNotAffectAPIsOutput 對映驗收條件：manifest 不進 collector.Endpoints，
// apis 輸出與 docs/api-inventory.md 不受影響。
func TestManifestDoesNotAffectAPIsOutput(t *testing.T) {
	for _, e := range collector.Endpoints {
		if e.File == collector.BundleManifestFile {
			t.Errorf("_manifest.json 不應出現在 collector.Endpoints：%+v", e)
		}
	}
	out := apisMarkdown()
	if strings.Contains(out, collector.BundleManifestFile) {
		t.Error("apis 輸出不應提及 _manifest.json（它不是端點）")
	}
}
