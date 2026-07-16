package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"elk-diagnostics/internal/collector"
)

// 這支腳本是交到客戶手上、在客戶正式環境執行的東西，且它的全部價值就是「資安人員
// 讀得懂、敢放行」，因此語法與內容都要鎖住。

// checkedInScript 是 repo 根目錄下 checked in 的那份 collect.sh。
const checkedInScript = "../../collect.sh"

// TestCollectScript_CheckedInCopyIsFresh 確保 checked in 的 collect.sh 與端點表同步。
//
// 產生檔 checked in 的代價就是會過期；不擋住的話，交付出去的腳本可能少採幾個端點，
// 而少採的後果是對應診斷在分析時變成 unknown——等於健檢有洞卻不自知。
// 這裡沿用 golden 檔的模式：checked in 一份可供 review／diff，測試負責擋過期。
func TestCollectScript_CheckedInCopyIsFresh(t *testing.T) {
	want, err := renderCollectScript()
	if err != nil {
		t.Fatalf("renderCollectScript() 失敗: %v", err)
	}
	got, err := os.ReadFile(checkedInScript)
	if err != nil {
		t.Fatalf("讀 collect.sh 失敗（請執行 make generate）: %v", err)
	}
	if string(got) != want {
		t.Errorf("collect.sh 已過期（端點表或範本已變動）。請執行：\n  make generate")
	}
}

func TestCollectScript_CheckedInCopyIsExecutable(t *testing.T) {
	fi, err := os.Stat(checkedInScript)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm()&0o111 == 0 {
		t.Error("collect.sh 應具執行權限（客戶拿到後要能直接 ./collect.sh）")
	}
}

func TestCollectScript_IsValidPOSIXShell(t *testing.T) {
	script := filepath.Join(t.TempDir(), "collect.sh")
	s, err := renderCollectScript()
	if err != nil {
		t.Fatalf("renderCollectScript() 失敗: %v", err)
	}
	if err := os.WriteFile(script, []byte(s), 0o755); err != nil {
		t.Fatal(err)
	}

	// sh -n 只做語法檢查、不執行，故不會真的送出任何請求。
	for _, shell := range []string{"sh", "dash", "bash"} {
		path, err := exec.LookPath(shell)
		if err != nil {
			t.Logf("略過 %s（本機未安裝）", shell)
			continue
		}
		out, err := exec.Command(path, "-n", script).CombinedOutput()
		if err != nil {
			t.Errorf("%s -n 失敗: %v\n%s", shell, err, out)
		}
	}
}

func TestCollectScript_CoversEveryEndpoint(t *testing.T) {
	s, err := renderCollectScript()
	if err != nil {
		t.Fatal(err)
	}
	// 少抓一個端點，對應診斷就會在離線分析時變成 unknown——腳本必須與端點表等價。
	for _, e := range collector.Endpoints {
		want := "fetch '" + e.Path + "' '" + e.File + "'"
		if !strings.Contains(s, want) {
			t.Errorf("腳本未涵蓋端點: %s", e.Path)
		}
	}
	if got := strings.Count(s, "\nfetch '"); got != len(collector.Endpoints) {
		t.Errorf("fetch 呼叫數 = %d, want %d（多出來的可能是重複採集）", got, len(collector.Endpoints))
	}
}

func TestCollectScript_IsReadOnly(t *testing.T) {
	s, err := renderCollectScript()
	if err != nil {
		t.Fatal(err)
	}
	// 唯讀是本工具的核心保證（specs 鐵律 1）。腳本在客戶正式環境執行，
	// 任何寫入方法都不可接受。
	for _, forbidden := range []string{"-XPOST", "-XPUT", "-XDELETE", "-X POST", "-X PUT", "-X DELETE", "--request"} {
		if strings.Contains(s, forbidden) {
			t.Errorf("腳本含寫入方法 %q，違反唯讀保證", forbidden)
		}
	}
}

func TestCollectScript_DoesNotPutCredentialsOnCommandLine(t *testing.T) {
	s, err := renderCollectScript()
	if err != nil {
		t.Fatal(err)
	}
	// 命令列參數會出現在 ps 輸出，同機其他使用者看得到；認證資訊必須走 curl 設定檔。
	for _, bad := range []string{"curl -u", "-u \"$", "-u $", "--user "} {
		if strings.Contains(s, bad) {
			t.Errorf("腳本可能把認證資訊放上命令列: %q", bad)
		}
	}
	if !strings.Contains(s, "umask 077") {
		t.Error("寫入認證資訊的暫存檔前應先 umask 077")
	}
	if !strings.Contains(s, `trap 'rm -f "$CURL_CFG"' EXIT`) {
		t.Error("認證暫存檔應在結束時清除")
	}
}

// TestCollectScript_WritesParsableManifest 對映 spec-bundle §4.2 驗收條件：跑過
// sh -n 只驗語法，這裡實際執行一次採集，確認 _manifest.json 存在且可被
// json.Unmarshal 解析、欄位值正確。
func TestCollectScript_WritesParsableManifest(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("本機未安裝 sh")
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 任何路徑都回一個能被 fetch 寫入且不影響本測試判定的最小 body。
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"cluster_name":"test","version":{"number":"8.14.3"}}`))
	}))
	defer srv.Close()

	script := filepath.Join(t.TempDir(), "collect.sh")
	s, err := renderCollectScript()
	if err != nil {
		t.Fatalf("renderCollectScript() 失敗: %v", err)
	}
	if err := os.WriteFile(script, []byte(s), 0o755); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(t.TempDir(), "bundle")
	cmd := exec.Command(sh, script, "-h", srv.URL, "-o", out)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("執行 collect.sh 失敗: %v\n%s", err, b)
	}

	manifestPath := filepath.Join(out, collector.BundleManifestFile)
	b, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("讀 %s 失敗: %v", manifestPath, err)
	}
	var manifest struct {
		CollectScriptVersion string `json:"collect_script_version"`
		CollectedAt          string `json:"collected_at"`
		Host                 string `json:"host"`
		EndpointsTotal       int    `json:"endpoints_total"`
	}
	if err := json.Unmarshal(b, &manifest); err != nil {
		t.Fatalf("_manifest.json 應可被 json.Unmarshal 解析: %v\n內容:\n%s", err, b)
	}
	if manifest.CollectScriptVersion != toolVersion {
		t.Errorf("collect_script_version = %q, want %q", manifest.CollectScriptVersion, toolVersion)
	}
	if manifest.Host != srv.URL {
		t.Errorf("host = %q, want %q", manifest.Host, srv.URL)
	}
	if manifest.EndpointsTotal != len(collector.Endpoints) {
		t.Errorf("endpoints_total = %d, want %d", manifest.EndpointsTotal, len(collector.Endpoints))
	}
	if manifest.CollectedAt == "" {
		t.Error("collected_at 不應為空")
	}
	if !strings.HasSuffix(manifest.CollectedAt, "Z") {
		t.Errorf("collected_at = %q, 應為 UTC（Z 結尾）", manifest.CollectedAt)
	}
}

func TestCollectScript_RecordsHTTPStatus(t *testing.T) {
	s, err := renderCollectScript()
	if err != nil {
		t.Fatal(err)
	}
	// 沒有狀態碼，分析端無法區分「正常回應」與「ES 的錯誤訊息」，
	// 權限不足的 403 會被解析成零值而回報「一切正常」（實測確認過）。
	if !strings.Contains(s, collector.BundleStatusFile) {
		t.Errorf("腳本未產生 %s", collector.BundleStatusFile)
	}
	if strings.Contains(s, "curl -sS -f") || strings.Contains(s, "--fail") {
		t.Error("不可用 curl -f：部分端點以 4xx 表達語意（allocation/explain 的「無未分配 shard」），body 必須保留")
	}
}
