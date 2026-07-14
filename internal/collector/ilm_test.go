package collector

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestIlmMigrating_FiltersToActionMigrate(t *testing.T) {
	body := `{
		"indices": {
			"logs-warm-2026.01": {"managed": true, "phase": "warm", "action": "migrate", "step": "migrate"},
			"logs-hot-2026.02":  {"managed": true, "phase": "hot", "action": "rollover", "step": "check-rollover-ready"},
			"logs-unmanaged":    {"managed": false, "phase": "", "action": "", "step": ""}
		}
	}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	c := &Client{hosts: []string{srv.URL}, hc: srv.Client(), base: srv.URL}
	out, err := c.IlmMigrating()
	if err != nil {
		t.Fatalf("IlmMigrating 失敗: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("結果數量 = %d, want 1（只有 action=migrate 且 managed=true 才算）", len(out))
	}
	if out[0].Index != "logs-warm-2026.01" || out[0].Phase != "warm" || out[0].Step != "migrate" {
		t.Errorf("解析不符: %+v", out[0])
	}
}
