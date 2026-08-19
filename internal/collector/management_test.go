package collector

import (
	"net/http"
	"testing"
)

// 真機驗證抓到的 bug：見 allocation_test.go 開頭註解，同一個 filter_path/flat_settings
// 衝突讓本函式的 filter_path 永遠比對不到任何內容。
const monitoringSettingsBody = `{
  "persistent": {"xpack.monitoring.collection.enabled": "true"},
  "transient": {},
  "defaults": {
    "xpack.monitoring.collection.enabled": "false",
    "network.host": ["0.0.0.0"],
    "xpack.security.user": null
  }
}`

func TestMonitoringCollectionEnabled(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			w.Write([]byte(versionBody))
			return
		}
		w.Write([]byte(monitoringSettingsBody))
	})
	got, err := c.MonitoringCollectionEnabled()
	if err != nil {
		t.Fatalf("MonitoringCollectionEnabled() 失敗: %v", err)
	}
	if got != "true" {
		t.Errorf("got %q, want %q（應讀到 persistent 層，優先於 defaults）", got, "true")
	}
}

func TestWatcherStats(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			w.Write([]byte(versionBody))
			return
		}
		if r.URL.Path != EpWatcherStats {
			t.Fatalf("path = %q, want %q", r.URL.Path, EpWatcherStats)
		}
		w.Write([]byte(`{
  "_nodes":{"total":2,"successful":1,"failed":1},
  "manually_stopped":false,
  "stats":[{"node_id":"n1","watcher_state":"started","watch_count":3,
    "execution_thread_pool":{"queue_size":2,"max_size":10},
    "current_watches":[{}],"queued_watches":[{},{}]}]
}`))
	})
	got, err := c.WatcherStats()
	if err != nil {
		t.Fatalf("WatcherStats() 失敗: %v", err)
	}
	if got.NodesTotal != 2 || got.NodesSuccessful != 1 || got.NodesFailed != 1 || len(got.Nodes) != 1 {
		t.Fatalf("coverage/nodes = %+v", got)
	}
	n := got.Nodes[0]
	if n.NodeID != "n1" || n.WatcherState != "started" || n.WatchCount != 3 || n.QueueSize != 2 || n.MaxQueueSize != 10 || n.CurrentWatches != 1 || n.QueuedWatches != 2 {
		t.Fatalf("node stats = %+v", n)
	}
}
