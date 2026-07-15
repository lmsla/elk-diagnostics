package collector

import (
	"net/http"
	"testing"
)

// 真機驗證抓到的 bug：見 allocation_test.go 開頭註解，同一個 filter_path/flat_settings
// 衝突讓本函式無論實際設定為何永遠回傳空清單。這裡刻意用未經 filter_path 縮減的
// 真實 flat_settings 結構驗證。
const slowlogSettingsBody = `{
  "slowlog-test": {
    "settings": {
      "index.search.slowlog.threshold.query.warn": "10s",
      "index.number_of_shards": "1",
      "index.query.default_field": ["*"]
    }
  },
  "no-slowlog-test": {
    "settings": {
      "index.number_of_shards": "1"
    }
  },
  "disabled-slowlog-test": {
    "settings": {
      "index.search.slowlog.threshold.query.warn": "-1"
    }
  }
}`

func TestSlowlogEnabledIndices(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			w.Write([]byte(versionBody))
			return
		}
		w.Write([]byte(slowlogSettingsBody))
	})
	got, err := c.SlowlogEnabledIndices()
	if err != nil {
		t.Fatalf("SlowlogEnabledIndices() 失敗: %v", err)
	}
	if len(got) != 1 || got[0] != "slowlog-test" {
		t.Errorf("got %v, want [slowlog-test]（未設定與明確 -1 皆不應算已開啟）", got)
	}
}
