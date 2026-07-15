// security_test.go：唯讀保證與密鑰遮蔽的回歸測試（見 docs/PROGRESS.md §4「安全與非功能」）。
//
// 這兩項本來就已經是現況（collector 只曝露 http.MethodGet；沒有任何地方把
// password/api-key 寫進 log 或報告），但都只是「沒人寫錯」的巧合式安全，沒有測試鎖住。
// 這裡把它們變成會在未來有人不慎違反時直接讓 CI 失敗的硬保證。
package main

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"elk-diagnostics/internal/collector"
)

// TestCheckIsReadOnly 驗證整輪 check（涵蓋所有已知端點）只送出 GET 請求，
// 對應 spec-config.md「唯讀：client 只允許 GET/HEAD；實作層應封死寫入方法」。
func TestCheckIsReadOnly(t *testing.T) {
	var nonGET []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			nonGET = append(nonGET, r.Method+" "+r.URL.RequestURI())
		}
		file, ok := collector.FileForEndpoint(r.URL.RequestURI())
		if !ok {
			http.NotFound(w, r)
			return
		}
		b, err := os.ReadFile(filepath.Join(fixtureDir("es9-unhealthy"), file))
		if err != nil {
			http.NotFound(w, r) // Phase 0 未錄製，比照真機缺權限
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(b)
	}))
	defer srv.Close()

	cf := newTestConnFlags(t, []string{srv.URL}, "", "")
	outFile := filepath.Join(t.TempDir(), "report.json")
	runCheck(cf, "", "", "json", outFile)

	if len(nonGET) > 0 {
		t.Errorf("check 送出了非 GET 請求（違反唯讀保證）: %v", nonGET)
	}
}

// TestCheckDoesNotLeakSecretsOnSuccess 驗證認證成功跑完整輪 check 後，密碼明文與其
// Basic auth base64 編碼都不會出現在報告輸出裡（報告可能會被交給客戶或留存稽核）。
func TestCheckDoesNotLeakSecretsOnSuccess(t *testing.T) {
	const username = "svc-account"
	const secret = "S3cr3t-Pa55w0rd-should-not-leak"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		file, ok := collector.FileForEndpoint(r.URL.RequestURI())
		if !ok {
			http.NotFound(w, r)
			return
		}
		b, err := os.ReadFile(filepath.Join(fixtureDir("es9-unhealthy"), file))
		if err != nil {
			http.NotFound(w, r) // Phase 0 未錄製，比照真機缺權限
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(b)
	}))
	defer srv.Close()

	cf := newTestConnFlags(t, []string{srv.URL}, username, secret)
	outFile := filepath.Join(t.TempDir(), "report.json")
	runCheck(cf, "", "", "json", outFile)

	b, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("讀輸出失敗: %v", err)
	}
	assertNoSecretLeak(t, string(b), username, secret, "報告輸出")

	// 也確認整份輸出仍是合法 JSON（沒有因為插入遮蔽邏輯而弄壞格式）。
	var report map[string]any
	if err := json.Unmarshal(b, &report); err != nil {
		t.Fatalf("輸出不是合法 JSON: %v", err)
	}
}

// TestCheckDoesNotLeakSecretsOnConnectFailure 驗證連線失敗（密鑰最容易被夾帶進錯誤
// 訊息的路徑）時，stderr 也不會洩漏密碼明文或其編碼。
func TestCheckDoesNotLeakSecretsOnConnectFailure(t *testing.T) {
	const username = "svc-account"
	const secret = "S3cr3t-Pa55w0rd-should-not-leak"

	cf := newTestConnFlags(t, []string{"http://127.0.0.1:1"}, username, secret)
	outFile := filepath.Join(t.TempDir(), "report.json")

	stderr := captureStderr(t, func() {
		runCheck(cf, "", "", "json", outFile)
	})
	assertNoSecretLeak(t, stderr, username, secret, "stderr")
}

func assertNoSecretLeak(t *testing.T, haystack, username, secret, where string) {
	t.Helper()
	if strings.Contains(haystack, secret) {
		t.Errorf("%s 洩漏了密碼明文", where)
	}
	encoded := base64.StdEncoding.EncodeToString([]byte(username + ":" + secret))
	if strings.Contains(haystack, encoded) {
		t.Errorf("%s 洩漏了 Basic auth 的 base64 編碼", where)
	}
}

// captureStderr 暫時把 os.Stderr 導到 pipe，回傳 fn 執行期間寫入的全部內容。
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("建立 pipe 失敗: %v", err)
	}
	orig := os.Stderr
	os.Stderr = w
	defer func() { os.Stderr = orig }()

	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()

	fn()
	w.Close()
	return <-done
}
