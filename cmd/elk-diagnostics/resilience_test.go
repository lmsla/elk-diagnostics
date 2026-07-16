// resilience_test.go：驗證 docs/workorders/wo-2026-07-16-trust.md T2/T3 的行為變化——
// _health_report 抓取失敗（bundle 缺檔/錯誤 body、連線模式 5xx）不再讓整份報告中止，
// A 類全數以 unknown（或版本不支援時 skipped）浮出；B/C 類照常執行；bundle 模式的
// unknown 措辭與連線模式不同（見 spec-resilience §1/§3、spec-cli §4）。
package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"elk-diagnostics/internal/analyzer"
	"elk-diagnostics/internal/diagnostic"
)

// copyFixtureBundle 複製 fixture 目錄到一份可任意破壞的臨時目錄，測試才能安全地
// 刪檔/竄改內容而不動到 checked-in 的 fixture 本體。
func copyFixtureBundle(t *testing.T, clusterDir string) string {
	t.Helper()
	src := fixtureDir(clusterDir)
	dst := t.TempDir()
	entries, err := os.ReadDir(src)
	if err != nil {
		t.Fatalf("讀 fixture 目錄失敗: %v", err)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(src, e.Name()))
		if err != nil {
			t.Fatalf("讀 %s 失敗: %v", e.Name(), err)
		}
		if err := os.WriteFile(filepath.Join(dst, e.Name()), b, 0644); err != nil {
			t.Fatalf("寫 %s 失敗: %v", e.Name(), err)
		}
	}
	return dst
}

// neutralizeMasterStabilityWarning 把 es9-healthy fixture 唯一的既有 warning
//（單一 master-eligible 節點）改成 3 節點，讓 baseline 是乾淨的 all-pass，
// 這樣「刪掉 health_report.json 後 exit code 變 3」才是乾淨的訊號，不與既有
// warning 混在一起判讀。
func neutralizeMasterStabilityWarning(t *testing.T, dir string) {
	t.Helper()
	writeJSON(t, filepath.Join(dir, "cluster_health.json"), map[string]any{
		"status": "green", "number_of_nodes": 3, "active_primary_shards": 0,
		"active_shards": 0, "relocating_shards": 0, "initializing_shards": 0,
		"unassigned_shards": 0, "unassigned_primary_shards": 0, "delayed_unassigned_shards": 0,
	})
	writeJSON(t, filepath.Join(dir, "nodes_roles.json"), map[string]any{
		"nodes": map[string]any{
			"n1": map[string]any{"name": "n1", "roles": []string{"master", "data"}},
			"n2": map[string]any{"name": "n2", "roles": []string{"master", "data"}},
			"n3": map[string]any{"name": "n3", "roles": []string{"master", "data"}},
		},
	})
}

func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("序列化失敗: %v", err)
	}
	if err := os.WriteFile(path, b, 0644); err != nil {
		t.Fatalf("寫 %s 失敗: %v", path, err)
	}
}

func runBundleCheck(t *testing.T, dir string) (diagnostic.Report, int) {
	t.Helper()
	cf := newTestConnFlags(t, nil, "", "")
	outFile := filepath.Join(t.TempDir(), "report.json")
	code := runCheck(cf, "", dir, "json", outFile, false)
	b, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("讀輸出失敗: %v", err)
	}
	var report diagnostic.Report
	if err := json.Unmarshal(b, &report); err != nil {
		t.Fatalf("解析輸出失敗: %v", err)
	}
	return report, code
}

// resultsByID 依 id 索引，方便逐項比對 B/C 類結果在 health_report 缺失前後是否一致。
func resultsByID(results []diagnostic.Result) map[string]diagnostic.Result {
	m := make(map[string]diagnostic.Result, len(results))
	for _, r := range results {
		m[r.ID] = r
	}
	return m
}

func aClassIDs() map[string]bool {
	ids := map[string]bool{}
	for _, id := range analyzer.HealthReportIndicatorIDs() {
		ids[id] = true
	}
	return ids
}

// TestCheck_BundleMissingHealthReport 驗收 T2 第一條：bundle 缺 health_report.json
// → 不中止（exit 3），A 類全數 unknown，B/C 類結果與刪檔前完全一致。
func TestCheck_BundleMissingHealthReport(t *testing.T) {
	dir := copyFixtureBundle(t, "es9-healthy")
	neutralizeMasterStabilityWarning(t, dir)

	// es9-healthy fixture 本身就缺幾個較新的 B 類端點檔案（見 golden_test.go 開頭註解），
	// 故 baseline 已有既有 unknown（與 health_report 無關）；這裡不假設 baseline 全線
	// pass，只用它當「刪檔前」的對照組，驗證刪檔不影響這些既有的 B/C 結果。
	before, _ := runBundleCheck(t, dir)

	if err := os.Remove(filepath.Join(dir, "health_report.json")); err != nil {
		t.Fatalf("刪檔失敗: %v", err)
	}
	after, afterCode := runBundleCheck(t, dir)

	if afterCode != 3 {
		t.Errorf("exit code = %d, want 3", afterCode)
	}
	if after.OverallStatus != diagnostic.StatusUnknown {
		t.Errorf("overall_status = %q, want unknown", after.OverallStatus)
	}

	aIDs := aClassIDs()
	afterByID := resultsByID(after.Results)
	for id := range aIDs {
		r, ok := afterByID[id]
		if !ok {
			t.Errorf("A 類 id %q 未出現在結果中", id)
			continue
		}
		if r.Status != diagnostic.StatusUnknown {
			t.Errorf("A 類 %q status = %q, want unknown", id, r.Status)
		}
	}

	beforeByID := resultsByID(before.Results)
	for id, b := range beforeByID {
		if aIDs[id] {
			continue // A 類本來就預期改變（pass/warning → unknown），不比對
		}
		if id == "index_allocation_blocked" {
			continue // #20 的輸入就是 health_report 的受影響 index 清單，刪檔後必然改變，另有專屬斷言（見下）
		}
		a, ok := afterByID[id]
		if !ok {
			t.Errorf("B/C 類 id %q 在刪檔後消失", id)
			continue
		}
		bj, _ := json.Marshal(b)
		aj, _ := json.Marshal(a)
		if string(bj) != string(aj) {
			t.Errorf("B/C 類 %q 刪檔前後不一致:\n前: %s\n後: %s", id, bj, aj)
		}
	}

	// #20 依賴 health_report 點名受影響 index；health_report 不可用時必須是 unknown，
	// 絕不可說出「shards_availability 目前正常」——沒有資料時宣稱正常正是
	// VERIFICATION.md §1.1 記載的假陰性模式（T2 讓 health_report 失敗不再中止後，
	// 此路徑首次真正可達）。
	iab, ok := afterByID["index_allocation_blocked"]
	if !ok {
		t.Fatal("index_allocation_blocked 未出現在結果中")
	}
	if iab.Status != diagnostic.StatusUnknown {
		t.Errorf("index_allocation_blocked status = %q, want unknown", iab.Status)
	}
	if strings.Contains(iab.Summary, "目前正常") {
		t.Errorf("index_allocation_blocked summary 不得含「目前正常」（health_report 不可用時無資料可佐證），got %q", iab.Summary)
	}
	if !strings.Contains(iab.Summary, "無法取得受影響 index 清單") {
		t.Errorf("index_allocation_blocked summary 應說明受影響 index 清單不可得，got %q", iab.Summary)
	}
	if !strings.Contains(iab.Summary, "bundle") {
		t.Errorf("bundle 模式的 summary 應沿用 T3 措辭慣例（提及 bundle），got %q", iab.Summary)
	}
}

// TestCheck_BundleHealthReport404 驗收 T2 第二條：health_report.json 是 404 錯誤 body
// （_status.txt 記 404）時行為與缺檔相同。
func TestCheck_BundleHealthReport404(t *testing.T) {
	dir := copyFixtureBundle(t, "es9-healthy")
	neutralizeMasterStabilityWarning(t, dir)

	if err := os.WriteFile(filepath.Join(dir, "health_report.json"), []byte(`{"error":{"type":"not_found"}}`), 0644); err != nil {
		t.Fatalf("寫檔失敗: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "_status.txt"), []byte("health_report.json 404\n"), 0644); err != nil {
		t.Fatalf("寫 _status.txt 失敗: %v", err)
	}

	report, code := runBundleCheck(t, dir)
	if code != 3 {
		t.Errorf("exit code = %d, want 3", code)
	}
	if report.OverallStatus != diagnostic.StatusUnknown {
		t.Errorf("overall_status = %q, want unknown", report.OverallStatus)
	}
	aIDs := aClassIDs()
	byID := resultsByID(report.Results)
	for id := range aIDs {
		r, ok := byID[id]
		if !ok || r.Status != diagnostic.StatusUnknown {
			t.Errorf("A 類 %q status = %+v, want unknown", id, r)
		}
	}
}

// TestCheck_BundleVersionUnsupported 驗收 T2 第三條：version.json 偽造成 7.17.0
// → A 類全 skipped、B/C 附 version_warning、JSON 有 version_notice、HTML 頁首有黃條。
func TestCheck_BundleVersionUnsupported(t *testing.T) {
	dir := copyFixtureBundle(t, "es9-healthy")
	neutralizeMasterStabilityWarning(t, dir)

	writeJSON(t, filepath.Join(dir, "version.json"), map[string]any{
		"name": "n1", "cluster_name": "docker-cluster", "cluster_uuid": "x",
		"version": map[string]any{"number": "7.17.0"},
		"tagline": "You Know, for Search",
	})

	report, code := runBundleCheck(t, dir)
	if code == 11 {
		t.Fatalf("不應中止連線（exit 11）")
	}
	if report.VersionNotice == "" {
		t.Error("report.VersionNotice 應非空")
	}
	if !strings.Contains(report.VersionNotice, "7.17.0") {
		t.Errorf("VersionNotice = %q，應提及實際版本", report.VersionNotice)
	}

	aIDs := aClassIDs()
	byID := resultsByID(report.Results)
	for id := range aIDs {
		r, ok := byID[id]
		if !ok {
			t.Errorf("A 類 id %q 消失", id)
			continue
		}
		if r.Status != diagnostic.StatusSkipped {
			t.Errorf("A 類 %q status = %q, want skipped", id, r.Status)
		}
	}
	bcWithWarning := 0
	for id, r := range byID {
		if aIDs[id] {
			continue
		}
		if r.VersionWarning != "" {
			bcWithWarning++
		}
	}
	if bcWithWarning == 0 {
		t.Error("B/C 類應至少有一項附 version_warning")
	}

	// HTML 頁首應有黃條。
	cf := newTestConnFlags(t, nil, "", "")
	outFile := filepath.Join(t.TempDir(), "report.html")
	htmlCode := runCheck(cf, "", dir, "html", outFile, false)
	if htmlCode == 11 {
		t.Fatalf("HTML 輸出時不應中止連線")
	}
	htmlBytes, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("讀 html 輸出失敗: %v", err)
	}
	html := string(htmlBytes)
	if !strings.Contains(html, "version-notice") {
		t.Error("HTML 應含 version-notice 區塊（黃色警告條）")
	}
	if !strings.Contains(html, "7.17.0") {
		t.Error("HTML version-notice 應提及實際版本 7.17.0")
	}
}

// TestCheck_ConnHealthReport500 驗收 T2 第四條（連線模式單元測試）：httptest 模擬
// _health_report 回 500 → 不中止，A 類 unknown。
func TestCheck_ConnHealthReport500(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"cluster_name":"test-cluster","version":{"number":"8.14.3"}}`)
		case "/_health_report":
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, `{"error":"internal"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cf := newTestConnFlags(t, []string{srv.URL}, "", "")
	outFile := filepath.Join(t.TempDir(), "report.json")
	code := runCheck(cf, "", "", "json", outFile, false)
	if code == 11 {
		t.Fatalf("_health_report 500 不應讓整個指令中止（exit 11）")
	}

	b, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("讀輸出失敗: %v", err)
	}
	var report diagnostic.Report
	if err := json.Unmarshal(b, &report); err != nil {
		t.Fatalf("解析輸出失敗: %v", err)
	}

	aIDs := aClassIDs()
	byID := resultsByID(report.Results)
	for id := range aIDs {
		r, ok := byID[id]
		if !ok {
			t.Errorf("A 類 id %q 消失", id)
			continue
		}
		if r.Status != diagnostic.StatusUnknown {
			t.Errorf("A 類 %q status = %q, want unknown", id, r.Status)
		}
		if r.Summary != "資料抓取失敗，無法判定" {
			t.Errorf("連線模式 summary = %q，措辭不應變（見 T3）", r.Summary)
		}
	}

	// #20 在連線模式 health_report 失敗時同樣必須是 unknown（不可宣稱「目前正常」），
	// 措辭用連線版而非 bundle 版。
	iab, ok := byID["index_allocation_blocked"]
	if !ok {
		t.Fatal("index_allocation_blocked 未出現在結果中")
	}
	if iab.Status != diagnostic.StatusUnknown {
		t.Errorf("index_allocation_blocked status = %q, want unknown", iab.Status)
	}
	if strings.Contains(iab.Summary, "目前正常") || strings.Contains(iab.Summary, "bundle") {
		t.Errorf("連線模式 index_allocation_blocked summary 不得含「目前正常」或 bundle 措辭，got %q", iab.Summary)
	}
}

// TestCheck_BundleUnknownWording 驗收 T3：--from-bundle 缺檔造成的 unknown 項，
// summary 為 bundle 專屬措辭，而非連線模式的「資料抓取失敗」。
func TestCheck_BundleUnknownWording(t *testing.T) {
	report, _ := runBundleCheck(t, fixtureDir("es9-unhealthy"))
	found := false
	for _, r := range report.Results {
		if r.Status != diagnostic.StatusUnknown {
			continue
		}
		found = true
		if strings.Contains(r.Summary, "抓取") {
			t.Errorf("%q 的 summary 不應含「抓取」字樣（bundle 模式沒有抓取動作）: %q", r.ID, r.Summary)
		}
		if !strings.Contains(r.Summary, "bundle") && r.ID != "index_allocation_blocked" {
			t.Errorf("%q 的 summary 應為 bundle 專屬措辭，got %q", r.ID, r.Summary)
		}
	}
	if !found {
		t.Fatal("es9-unhealthy fixture 預期至少有一項 unknown，測試前提不成立")
	}
}
