// golden_test.go：多版本 golden test（見 docs/PROGRESS.md §4）。
//
// 對每個 dev/phase0/fixtures/<cluster> 真機錄製組跑一次完整 `check`，比對輸出與
// dev/phase0/golden/<cluster>.json 是否一致，作為跨版本（8.14.3/9.0.0）、跨健康狀態
// （healthy/unhealthy）的回歸測試。不需要真實叢集——httptest server 直接回放錄製檔。
//
// 覆蓋範圍受限於 Phase 0 當時錄製的端點：allocation-enable/index-settings/mapping/
// recovery/write-thread-pool/monitoring-setting/slowlog-setting/ilm-explain 等較新的
// B 類端點未被錄製，對應診斷會如同真實叢集缺權限/端點時一樣被 check 的容錯邏輯跳過
// （見各 collector 方法呼叫處的 `if _, e := client.X(); e == nil`）。golden 檔如實反映
// 這個已知落差，非缺陷。
//
// 更新 golden 檔：go test ./cmd/elk-diagnostics/ -run TestGolden -update
package main

import (
	"encoding/json"
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"elk-diagnostics/internal/diagnostic"
)

var update = flag.Bool("update", false, "更新 golden 檔（dev/phase0/golden/）")

// fixtureEndpoints 對照各 collector 方法實際打的 path+query 到 Phase 0 錄製檔檔名
// （檔名於各 cluster fixture 目錄下一致）。未列出的端點該 cluster 未錄製，回 404，
// 讓 check 的容錯路徑跳過該診斷（如同真機缺權限）。
var fixtureEndpoints = map[string]string{
	"/":                                 "version.json",
	"/_health_report":                   "health_report.json",
	"/_cluster/health":                  "cluster_health.json",
	"/_nodes?filter_path=nodes.*.roles": "nodes_roles.json",
	"/_cluster/allocation/explain":      "allocation_explain.json",
	"/_ilm/status":                      "ilm_status.json",
	"/_nodes/stats?filter_path=nodes.*.name,nodes.*.jvm.mem.pools.old":                                         "nodes_stats_jvm.json",
	"/_nodes/stats/breaker?filter_path=nodes.*.name,nodes.*.breakers":                                          "nodes_stats_breaker.json",
	"/_cat/nodes?format=json&h=name,node.role,cpu,load_1m,allocated_processors,heap.percent,disk.used_percent": "cat_nodes.json",
	"/_cat/thread_pool?format=json&h=node_name,name,active,queue,rejected,completed":                           "cat_thread_pool.json",
	"/_nodes/stats/ingest?filter_path=nodes.*.ingest.pipelines":                                                "nodes_stats_ingest.json",
	"/_cat/indices?format=json&h=index,health,status":                                                          "cat_indices.json",
	"/_migration/deprecations": "migration_deprecations.json",
	"/_remote/info":            "remote_info.json",
	"/_transform/_stats":       "transform_stats.json",
	"/_watcher/stats":          "watcher_stats.json",
}

// fixtureServer 起一個回放指定 cluster 錄製檔的 httptest server；未錄製的端點回 404。
func fixtureServer(t *testing.T, clusterDir string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		file, ok := fixtureEndpoints[r.URL.RequestURI()]
		if !ok {
			t.Logf("golden fixture 未錄製端點（回 404，比照真機缺權限）: %s", r.URL.RequestURI())
			http.NotFound(w, r)
			return
		}
		b, err := os.ReadFile(filepath.Join("..", "..", "dev", "phase0", "fixtures", clusterDir, file))
		if err != nil {
			t.Fatalf("讀 fixture 失敗 %s/%s: %v", clusterDir, file, err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(b)
	}))
}

// newTestConnFlags 建構一份直接指向給定 hosts、其餘皆空值的 connFlags，繞過 cobra
// 解析（測試與 production code 同一個 package，可直接組結構）。username/password 可傳
// 空字串以外的值供密鑰遮蔽測試使用。
func newTestConnFlags(t *testing.T, hosts []string, username, password string) *connFlags {
	t.Helper()
	cfgPath := filepath.Join(t.TempDir(), "no-such-config.yaml")
	empty := ""
	falseVal := false
	zero := 0
	u, p := username, password
	return &connFlags{
		cfgPath: &cfgPath, hosts: &hosts,
		username: &u, password: &p, apiKey: &empty, caCert: &empty, rulesPath: &empty,
		insecure: &falseVal, timeout: &zero,
	}
}

// runGoldenCheck 對指定 cluster 的 fixture server 跑完整 check，回傳去除易變欄位後的報告。
func runGoldenCheck(t *testing.T, clusterDir string) diagnostic.Report {
	t.Helper()
	srv := fixtureServer(t, clusterDir)
	defer srv.Close()

	cf := newTestConnFlags(t, []string{srv.URL}, "", "")
	outFile := filepath.Join(t.TempDir(), "report.json")
	code := runCheck(cf, "", "json", outFile)
	t.Logf("cluster=%s exit_code=%d", clusterDir, code)

	b, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("讀輸出失敗: %v", err)
	}
	var report diagnostic.Report
	if err := json.Unmarshal(b, &report); err != nil {
		t.Fatalf("解析輸出失敗: %v", err)
	}
	// 易變欄位（每次執行都不同，不該進 golden 比對）。
	report.Meta.GeneratedAt = ""
	report.Meta.Cluster.Host = ""
	return report
}

func TestGolden(t *testing.T) {
	clusters := []string{"es8-health", "es8-unhealthy", "es9-healthy", "es9-unhealthy"}
	for _, cluster := range clusters {
		t.Run(cluster, func(t *testing.T) {
			got := runGoldenCheck(t, cluster)
			gotJSON, err := json.MarshalIndent(got, "", "  ")
			if err != nil {
				t.Fatalf("序列化失敗: %v", err)
			}
			goldenPath := filepath.Join("..", "..", "dev", "phase0", "golden", cluster+".json")

			if *update {
				if err := os.WriteFile(goldenPath, append(gotJSON, '\n'), 0644); err != nil {
					t.Fatalf("寫 golden 檔失敗: %v", err)
				}
				t.Logf("已更新 golden 檔: %s", goldenPath)
				return
			}

			want, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatalf("讀 golden 檔失敗（初次執行請加 -update 產生）: %v", err)
			}
			if string(gotJSON)+"\n" != string(want) {
				t.Errorf("輸出與 golden 檔不符 (%s)。若為預期變更，執行：\n  go test ./cmd/elk-diagnostics/ -run TestGolden -update\n\n--- got ---\n%s", goldenPath, gotJSON)
			}
		})
	}
}
