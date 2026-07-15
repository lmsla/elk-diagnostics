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
