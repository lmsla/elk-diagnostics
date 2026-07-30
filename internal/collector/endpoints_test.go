package collector

import (
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

// Endpoints 是 bundle 讀取、golden test 回放、後續採集腳本產生的共同事實來源，
// 重複的 Path 或 File 會讓其中一項悄悄覆蓋另一項，故直接鎖住。
func TestEndpoints_NoDuplicates(t *testing.T) {
	seenPath := map[string]bool{}
	seenFile := map[string]bool{}
	for _, e := range Endpoints {
		if seenPath[e.Path] {
			t.Errorf("Path 重複: %s", e.Path)
		}
		if seenFile[e.File] {
			t.Errorf("File 重複: %s（會蓋掉另一個端點的資料）", e.File)
		}
		if e.Purpose == "" {
			t.Errorf("%s 缺 Purpose（客戶導入審查的 API 清單要靠它自我說明）", e.Path)
		}
		seenPath[e.Path] = true
		seenFile[e.File] = true
	}
}

func TestNodeFanOutEndpointsHaveServerSideTimeout(t *testing.T) {
	paths := []string{
		EpNodesRoles,
		EpNodesResourceStats,
		EpNodesResourceInfo,
		EpNodesBreakers,
		EpNodesIngest,
		EpRunningTasks,
		EpNodesRuntime,
		EpNodesTopology,
		EpNodesIndexingPressure,
	}
	for _, path := range paths {
		u, err := url.Parse(path)
		if err != nil {
			t.Fatalf("解析端點失敗 %q: %v", path, err)
		}
		if got := u.Query().Get("timeout"); got != "5s" {
			t.Errorf("%s timeout=%q, want 5s", path, got)
		}
	}
}

func TestFileForEndpoint(t *testing.T) {
	if f, ok := FileForEndpoint(EpHealthReport); !ok || f != "health_report.json" {
		t.Errorf("FileForEndpoint(EpHealthReport) = %q, %v", f, ok)
	}
	// 動態端點不在表中——bundle 無法預先採集，必須明確回 false 讓呼叫端走 unknown。
	if _, ok := FileForEndpoint(EpIndexSettings("some-index")); ok {
		t.Error("動態的 per-index settings 不該出現在 Endpoints 表中")
	}
}

// writeBundle 建一個只含指定檔案的 bundle 目錄。
func writeBundle(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestNewFromBundle(t *testing.T) {
	t.Run("讀得到版本與 cluster_name", func(t *testing.T) {
		dir := writeBundle(t, map[string]string{"version.json": versionBody})
		c, err := NewFromBundle(dir)
		if err != nil {
			t.Fatalf("NewFromBundle() 失敗: %v", err)
		}
		if c.Version() != "8.14.3" || c.ClusterName() != "test" {
			t.Errorf("version=%q cluster=%q", c.Version(), c.ClusterName())
		}
	})

	t.Run("缺 version.json 直接失敗，不產生半殘的 client", func(t *testing.T) {
		if _, err := NewFromBundle(writeBundle(t, nil)); err == nil {
			t.Fatal("want error（bundle 不完整時應明確失敗）")
		}
	})

	t.Run("缺檔的端點回錯誤而非空值", func(t *testing.T) {
		dir := writeBundle(t, map[string]string{"version.json": versionBody})
		c, err := NewFromBundle(dir)
		if err != nil {
			t.Fatal(err)
		}
		// health_report.json 不存在：必須回錯誤，讓 check 判成 unknown 而非 pass。
		if _, err := c.HealthReport(); err == nil {
			t.Fatal("want error（缺檔不能被當成「查過、沒問題」）")
		}
	})

	t.Run("動態端點回明確錯誤", func(t *testing.T) {
		dir := writeBundle(t, map[string]string{"version.json": versionBody})
		c, err := NewFromBundle(dir)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := c.IndexAllocationEnable("idx-a"); err == nil {
			t.Fatal("want error（bundle 無法涵蓋 per-index 動態端點）")
		}
	})

	// 這是 _status.txt 存在的唯一理由，實測確認過：若不記錄狀態碼，403 的錯誤 body
	// 會被當成 200 解析成零值，讓診斷回報「Watcher 運作中」——正是本專案一路在
	// 消滅的假綠燈（見 VERIFICATION.md §1）。
	t.Run("_status.txt 讓 4xx 回放成錯誤而非資料", func(t *testing.T) {
		dir := writeBundle(t, map[string]string{
			"version.json":       versionBody,
			"watcher_stats.json": `{"error":{"reason":"unauthorized"},"status":403}`,
			BundleStatusFile:     "version.json 200\nwatcher_stats.json 403\n",
		})
		c, err := NewFromBundle(dir)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := c.WatcherManuallyStopped(); err == nil {
			t.Fatal("want error（403 的錯誤 body 不可被當成正常資料解析）")
		}
	})

	t.Run("_status.txt 讓語意化的 4xx 仍能判讀", func(t *testing.T) {
		// 叢集健康時 allocation/explain 回 400，那個 body 就是「無未分配 shard」的答案。
		dir := writeBundle(t, map[string]string{
			"version.json":            versionBody,
			"allocation_explain.json": `{"error":{"reason":"unable to find any unassigned shards to explain"}}`,
			BundleStatusFile:          "version.json 200\nallocation_explain.json 400\n",
		})
		c, err := NewFromBundle(dir)
		if err != nil {
			t.Fatal(err)
		}
		exp, found, err := c.AllocationExplain()
		if err != nil {
			t.Fatalf("不該回錯誤（400 在此是語意而非失敗）: %v", err)
		}
		if found || exp != nil {
			t.Error("應判定為「無未分配 shard 可解釋」")
		}
	})

	t.Run("無 _status.txt 時 allocation/explain 仍能判讀", func(t *testing.T) {
		// 直接拿 fixture 目錄當 bundle 的情境：沒有狀態檔，一律視為 200，
		// 故 AllocationExplain 需自行辨識錯誤 body。
		dir := writeBundle(t, map[string]string{
			"version.json":            versionBody,
			"allocation_explain.json": `{"error":{"reason":"unable to find any unassigned shards to explain"}}`,
		})
		c, err := NewFromBundle(dir)
		if err != nil {
			t.Fatal(err)
		}
		_, found, err := c.AllocationExplain()
		if err != nil || found {
			t.Errorf("應判定為「無未分配 shard」，got found=%v err=%v", found, err)
		}
	})

	t.Run("bundle 模式全程不連線", func(t *testing.T) {
		dir := writeBundle(t, map[string]string{
			"version.json":        versionBody,
			"cluster_health.json": `{"number_of_nodes": 3}`,
		})
		c, err := NewFromBundle(dir)
		if err != nil {
			t.Fatal(err)
		}
		// hc 為 nil：任何走 HTTP 的路徑都會 panic，所以這裡能跑完就證明沒有連線。
		if c.hc != nil {
			t.Error("bundle 模式不該持有 http.Client")
		}
		n, err := c.ClusterNodeCounts()
		if err != nil || n != 3 {
			t.Errorf("ClusterNodeCounts() = %d, %v", n, err)
		}
	})
}
