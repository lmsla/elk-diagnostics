package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"elk-diagnostics/internal/diagnostic"
)

// TestCheckFromBundle 用真機錄製的 fixture 目錄當 bundle，跑一次完整 check。
//
// 這是「採集與判斷分離」的端到端驗證：客戶環境只跑採集腳本產出 bundle，二進位檔在
// 自己機器上分析——本測試證明後半段跟連線模式走的是同一套診斷邏輯。
//
// Phase 0 的 fixture 只錄了部分端點，正好同時驗到兩條路徑：有錄到的正常判定、
// 沒錄到的落 unknown（而不是被當成 pass）。
func TestCheckFromBundle(t *testing.T) {
	bundle := fixtureDir("es8-health")
	outFile := filepath.Join(t.TempDir(), "report.json")

	code := runCheck(newTestConnFlags(t, nil, "", ""), "", bundle, "json", outFile, false)
	t.Logf("exit_code=%d", code)

	b, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("讀輸出失敗: %v", err)
	}
	var report diagnostic.Report
	if err := json.Unmarshal(b, &report); err != nil {
		t.Fatalf("解析輸出失敗: %v", err)
	}

	if report.Meta.Cluster.ESVersion != "8.14.3" {
		t.Errorf("ESVersion = %q, want 8.14.3（應取自 bundle 的 version.json）", report.Meta.Cluster.ESVersion)
	}
	if report.Meta.Cluster.Host != "(bundle) "+bundle {
		t.Errorf("Host = %q，應標明資料來自 bundle 而非某個叢集", report.Meta.Cluster.Host)
	}

	byID := map[string]diagnostic.Result{}
	for _, r := range report.Results {
		byID[r.ID] = r
	}

	// 有錄到的端點：正常判定，不因為離線就退化。
	if got := byID["cluster_health"].Status; got != diagnostic.StatusPass {
		t.Errorf("cluster_health = %q, want pass（health_report.json 有錄到）", got)
	}
	if got := byID["watcher"].Status; got != diagnostic.StatusPass {
		t.Errorf("watcher = %q, want pass（watcher_stats.json 有錄到）", got)
	}

	// 沒錄到的端點：必須是 unknown。若是 pass，代表「查不到」又被講成「沒問題」。
	for _, id := range []string{"mapping_explosion", "restore_status", "data_allocation_blocked"} {
		r, ok := byID[id]
		if !ok {
			t.Errorf("%s 未出現在報告中（缺資料的診斷不該整條消失）", id)
			continue
		}
		if r.Status != diagnostic.StatusUnknown {
			t.Errorf("%s = %q, want unknown（bundle 未含該端點資料）", id, r.Status)
		}
	}

	if report.Summary.Unknown == 0 {
		t.Error("Summary.Unknown = 0，但 bundle 明顯缺端點——unknown 沒有被正確計數")
	}
}

// TestCheckFromBundleAndFileMutuallyExclusive 兩個離線來源同時給是使用者錯誤，
// 應明確拒絕而不是默默只用其中一個。
func TestCheckFromBundleAndFileMutuallyExclusive(t *testing.T) {
	code := runCheck(newTestConnFlags(t, nil, "", ""), "some.json", "some-dir", "json", "", false)
	if code != 10 {
		t.Errorf("exit code = %d, want 10（設定錯誤）", code)
	}
}
