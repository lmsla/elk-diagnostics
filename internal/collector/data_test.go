package collector

import (
	"net/http"
	"testing"
)

const mappingBody = `{
  "customer-logs-2026.01": {"mappings": {"properties": {"a": {"type": "keyword"}, "b": {"type": "long"}}}},
  ".ds-logs-app-default-2026.07.15-000001": {"mappings": {"properties": {"e": {"type": "keyword"}}}},
  ".internal.alerts-observability.logs.alerts-default-000001": {"mappings": {"properties": {"c": {"type": "keyword"}}}},
  ".kibana-observability-ai-assistant-conversations-000001": {"mappings": {"properties": {"d": {"type": "keyword"}}}}
}`

func TestMappingFieldCounts_ExcludesSystemIndicesButKeepsDataStreams(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			w.Write([]byte(versionBody))
			return
		}
		w.Write([]byte(mappingBody))
	})

	got, err := c.MappingFieldCounts()
	if err != nil {
		t.Fatalf("MappingFieldCounts() 失敗: %v", err)
	}
	byIndex := map[string]int{}
	for _, r := range got {
		byIndex[r.Index] = r.FieldCount
	}
	if len(got) != 2 {
		t.Fatalf("got %d indices, want 2（排除 .internal.alerts-*/.kibana-*，保留一般 index 與 .ds- data stream backing index）: %+v", len(got), got)
	}
	if fc, ok := byIndex["customer-logs-2026.01"]; !ok || fc != 2 {
		t.Errorf("customer-logs-2026.01 應保留，FieldCount=2，got ok=%v fc=%d", ok, fc)
	}
	if fc, ok := byIndex[".ds-logs-app-default-2026.07.15-000001"]; !ok || fc != 1 {
		t.Errorf(".ds- data stream backing index 應保留（客戶用 data stream 送的資料），got ok=%v fc=%d", ok, fc)
	}
}

const catIndicesBody = `[
  {"index":"customer-logs-2026.01","health":"green","status":"open"},
  {"index":".ds-logs-app-default-2026.07.15-000001","health":"yellow","status":"open"},
  {"index":".internal.alerts-security.alerts-default-000001","health":"red","status":"open"},
  {"index":".kibana","health":"red","status":"open"}
]`

func TestCatIndicesHealth_ExcludesSystemIndicesButKeepsDataStreams(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			w.Write([]byte(versionBody))
			return
		}
		w.Write([]byte(catIndicesBody))
	})

	got, err := c.CatIndicesHealth()
	if err != nil {
		t.Fatalf("CatIndicesHealth() 失敗: %v", err)
	}
	byIndex := map[string]IndexHealth{}
	for _, r := range got {
		byIndex[r.Index] = r
	}
	if len(got) != 2 {
		t.Fatalf("got %d indices, want 2（系統 index 即使 red 也不該混入客戶資料毀損判定；.ds- data stream backing index 仍須檢查）: %+v", len(got), got)
	}
	if r, ok := byIndex["customer-logs-2026.01"]; !ok || r.Health != "green" {
		t.Errorf("customer-logs-2026.01 應保留且為 green，got ok=%v %+v", ok, r)
	}
	if r, ok := byIndex[".ds-logs-app-default-2026.07.15-000001"]; !ok || r.Health != "yellow" {
		t.Errorf(".ds- data stream backing index 應保留（客戶資料），got ok=%v %+v", ok, r)
	}
}

func TestIsSystemIndex(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"customer-logs-2026.01", false},
		{".ds-logs-app-default-2026.07.15-000001", false},
		{".internal.alerts-security.alerts-default-000001", true},
		{".kibana", true},
		{".kibana-observability-ai-assistant-conversations-000001", true},
	}
	for _, c := range cases {
		if got := isSystemIndex(c.name); got != c.want {
			t.Errorf("isSystemIndex(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}
