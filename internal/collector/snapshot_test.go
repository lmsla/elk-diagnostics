package collector

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRestoreProgress_FiltersToTypeSnapshot(t *testing.T) {
	body := `{
		"restored-idx": {
			"shards": [
				{"id": 0, "type": "SNAPSHOT", "stage": "INDEX", "index": {"size": {"percent": "42.3%"}}},
				{"id": 1, "type": "PEER", "stage": "DONE", "index": {"size": {"percent": "100.0%"}}}
			]
		},
		"normal-idx": {
			"shards": [
				{"id": 0, "type": "EMPTY_STORE", "stage": "DONE", "index": {"size": {"percent": "100.0%"}}}
			]
		}
	}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	c := &Client{hosts: []string{srv.URL}, hc: srv.Client(), base: srv.URL}
	ops, err := c.RestoreProgress()
	if err != nil {
		t.Fatalf("失敗: %v", err)
	}
	if len(ops) != 1 {
		t.Fatalf("結果數量 = %d, want 1（只有 type=SNAPSHOT 才算還原）", len(ops))
	}
	if ops[0].Index != "restored-idx" || ops[0].Shard != 0 || ops[0].Stage != "INDEX" || ops[0].Percent != "42.3%" {
		t.Errorf("解析不符: %+v", ops[0])
	}
}

func TestRestoreProgress_NoneInProgress(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := &Client{hosts: []string{srv.URL}, hc: srv.Client(), base: srv.URL}
	ops, err := c.RestoreProgress()
	if err != nil {
		t.Fatalf("失敗: %v", err)
	}
	if len(ops) != 0 {
		t.Errorf("結果數量 = %d, want 0", len(ops))
	}
}
