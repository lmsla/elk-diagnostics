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

// TestCheckStaticHealthFromBundle 驗證新增的單次快照檢查只依賴 bundle 檔案，
// 不會在離線分析階段偷偷回連 Elasticsearch。
func TestCheckStaticHealthFromBundle(t *testing.T) {
	bundle := copyFixtureBundle(t, "es8-health")
	writeJSON(t, filepath.Join(bundle, "cluster_pending_tasks.json"), map[string]any{"tasks": []any{}})
	writeJSON(t, filepath.Join(bundle, "running_tasks.json"), map[string]any{"tasks": map[string]any{}})
	writeJSON(t, filepath.Join(bundle, "cat_shards_sizing.json"), []map[string]any{{
		"index": "logs", "shard": "0", "prirep": "p", "state": "STARTED", "node": "n1", "store": "10737418240", "docs": "1000",
	}})
	writeJSON(t, filepath.Join(bundle, "slm_policies.json"), map[string]any{})
	writeJSON(t, filepath.Join(bundle, "nodes_runtime.json"), map[string]any{
		"_nodes": map[string]any{"total": 1, "successful": 1, "failed": 0},
		"nodes": map[string]any{"a": map[string]any{
			"name": "n1", "roles": []string{"data_hot"}, "version": "8.14.3", "build_hash": "aaa",
			"jvm":     map[string]any{"version": "21", "vm_version": "21", "mem": map[string]any{"heap_init_in_bytes": 1073741824, "heap_max_in_bytes": 1073741824}},
			"plugins": []any{},
		}},
	})
	writeJSON(t, filepath.Join(bundle, "ssl_certificates.json"), []map[string]any{{
		"path": "http.p12", "subject_dn": "CN=n1", "issuer": "CN=ca", "expiry": "2099-01-01T00:00:00Z", "has_private_key": true,
	}})
	writeJSON(t, filepath.Join(bundle, "license.json"), map[string]any{"license": map[string]any{"status": "active", "type": "basic", "issued_to": "lab"}})
	writeJSON(t, filepath.Join(bundle, "all_settings.json"), map[string]any{"logs": map[string]any{"settings": map[string]any{"index.number_of_replicas": "1"}}})
	writeJSON(t, filepath.Join(bundle, "cluster_settings.json"), map[string]any{"persistent": map[string]any{}, "transient": map[string]any{}, "defaults": map[string]any{}})
	writeJSON(t, filepath.Join(bundle, "nodes_topology.json"), map[string]any{
		"_nodes": map[string]any{"total": 1, "successful": 1, "failed": 0},
		"nodes":  map[string]any{"a": map[string]any{"name": "n1", "roles": []string{"data_hot"}, "attributes": map[string]any{}}},
	})

	report, _ := runBundleCheck(t, bundle)
	byID := resultsByID(report.Results)
	want := map[string]diagnostic.Status{
		"cluster_pending_tasks":    diagnostic.StatusPass,
		"long_running_tasks":       diagnostic.StatusPass,
		"shard_sizing":             diagnostic.StatusPass,
		"snapshot_freshness":       diagnostic.StatusSkipped,
		"node_runtime_consistency": diagnostic.StatusPass,
		"tls_certificate_expiry":   diagnostic.StatusPass,
		"license_expiry":           diagnostic.StatusPass,
		"replica_resilience":       diagnostic.StatusPass,
		"allocation_awareness":     diagnostic.StatusSkipped,
	}
	for id, status := range want {
		if got, ok := byID[id]; !ok || got.Status != status {
			t.Errorf("%s = %+v, want status=%s", id, got, status)
		}
	}
}

// TestCheckExtendedHealthFromBundle 驗證 ES-GAP-07～12 的分析階段只讀 bundle，且
// optional feature「未使用」不會污染 overall health。
func TestCheckExtendedHealthFromBundle(t *testing.T) {
	bundle := copyFixtureBundle(t, "es8-health")
	writeJSON(t, filepath.Join(bundle, "nodes_stats_jvm.json"), map[string]any{
		"_nodes": map[string]any{"total": 1, "successful": 1, "failed": 0},
		"nodes": map[string]any{"a": map[string]any{
			"name": "n1", "roles": []string{"data_hot"},
			"os":  map[string]any{"swap": map[string]any{"total_in_bytes": 0, "used_in_bytes": 0, "free_in_bytes": 0}},
			"jvm": map[string]any{"uptime_in_millis": 7200000},
		}},
	})
	writeJSON(t, filepath.Join(bundle, "nodes_info_os_process.json"), map[string]any{
		"_nodes": map[string]any{"total": 1, "successful": 1, "failed": 0},
		"nodes": map[string]any{"a": map[string]any{
			"name": "n1", "roles": []string{"data_hot"}, "os": map[string]any{"name": "Linux", "available_processors": 2, "allocated_processors": 2},
			"process": map[string]any{"id": 1, "mlockall": false},
		}},
	})
	writeJSON(t, filepath.Join(bundle, "nodes_indexing_pressure.json"), map[string]any{
		"_nodes": map[string]any{"total": 1, "successful": 1, "failed": 0},
		"nodes": map[string]any{"a": map[string]any{"name": "n1", "indexing_pressure": map[string]any{"memory": map[string]any{
			"current": map[string]any{"combined_coordinating_and_primary_in_bytes": 10, "replica_in_bytes": 0, "all_in_bytes": 10}, "limit_in_bytes": 1000,
		}}}},
	})
	writeJSON(t, filepath.Join(bundle, "all_settings.json"), map[string]any{"logs": map[string]any{"settings": map[string]any{"index.number_of_replicas": "1"}}})
	writeJSON(t, filepath.Join(bundle, "ccr_stats.json"), map[string]any{"auto_follow_stats": map[string]any{}, "follow_stats": map[string]any{"indices": []any{}}})
	writeJSON(t, filepath.Join(bundle, "ml_job_stats.json"), map[string]any{"count": 0, "jobs": []any{}})
	writeJSON(t, filepath.Join(bundle, "ml_datafeed_stats.json"), map[string]any{"count": 0, "datafeeds": []any{}})
	writeJSON(t, filepath.Join(bundle, "planned_shutdown.json"), map[string]any{"nodes": []any{}})
	writeJSON(t, filepath.Join(bundle, "voting_exclusions.json"), map[string]any{"metadata": map[string]any{"cluster_coordination": map[string]any{"voting_config_exclusions": []any{}}}})

	report, _ := runBundleCheck(t, bundle)
	byID := resultsByID(report.Results)
	want := map[string]diagnostic.Status{
		"indexing_pressure":        diagnostic.StatusPass,
		"index_read_write_blocks":  diagnostic.StatusPass,
		"recent_node_restart":      diagnostic.StatusPass,
		"node_memory_lock":         diagnostic.StatusPass,
		"ccr_health":               diagnostic.StatusSkipped,
		"ml_jobs_datafeeds":        diagnostic.StatusSkipped,
		"planned_shutdown":         diagnostic.StatusPass,
		"voting_config_exclusions": diagnostic.StatusPass,
	}
	for id, status := range want {
		if got, ok := byID[id]; !ok || got.Status != status {
			t.Errorf("%s = %+v, want status=%s", id, got, status)
		}
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
