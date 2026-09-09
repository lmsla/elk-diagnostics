package collector

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClusterNodeCounts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"number_of_nodes": 5}`))
	}))
	defer srv.Close()

	c := &Client{hosts: []string{srv.URL}, hc: srv.Client(), base: srv.URL}
	n, err := c.ClusterNodeCounts()
	if err != nil {
		t.Fatalf("失敗: %v", err)
	}
	if n != 5 {
		t.Errorf("number_of_nodes = %d, want 5", n)
	}
}

func TestParseClusterHealth(t *testing.T) {
	health, err := ParseClusterHealth([]byte(`{
		"status":"YELLOW","number_of_nodes":3,"number_of_data_nodes":3,
		"active_primary_shards":12,"active_shards":24,"relocating_shards":0,
		"initializing_shards":0,"unassigned_shards":2,"unassigned_primary_shards":1,
		"active_shards_percent_as_number":92.3
	}`))
	if err != nil {
		t.Fatalf("ParseClusterHealth() 失敗: %v", err)
	}
	if health.Status != "yellow" || health.NumberOfNodes == nil || *health.NumberOfNodes != 3 || health.NumberOfDataNodes == nil || *health.NumberOfDataNodes != 3 {
		t.Fatalf("health status/nodes = %+v", health)
	}
	if health.ActiveShards == nil || *health.ActiveShards != 24 || health.UnassignedShards == nil || *health.UnassignedShards != 2 || health.ActiveShardsPercent == nil || *health.ActiveShardsPercent != 92.3 {
		t.Fatalf("health shard metrics = %+v", health)
	}
}

func TestDataTierNodeCounts(t *testing.T) {
	body := `{
		"nodes": {
			"node1": {"roles": ["data_content", "data_hot", "master"]},
			"node2": {"roles": ["data_hot", "data_warm"]},
			"node3": {"roles": ["ingest"]}
		}
	}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	c := &Client{hosts: []string{srv.URL}, hc: srv.Client(), base: srv.URL}
	counts, err := c.DataTierNodeCounts()
	if err != nil {
		t.Fatalf("失敗: %v", err)
	}
	want := map[string]int{"data_content": 1, "data_hot": 2, "data_warm": 1, "data_cold": 0, "data_frozen": 0}
	for tier, wantN := range want {
		if counts[tier] != wantN {
			t.Errorf("counts[%q] = %d, want %d", tier, counts[tier], wantN)
		}
	}
}

func TestMasterEligibleCount(t *testing.T) {
	body := `{
		"nodes": {
			"node1": {"roles": ["master", "data"]},
			"node2": {"roles": ["master"]},
			"node3": {"roles": ["data", "ingest"]}
		}
	}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	c := &Client{hosts: []string{srv.URL}, hc: srv.Client(), base: srv.URL}
	n, err := c.MasterEligibleCount()
	if err != nil {
		t.Fatalf("失敗: %v", err)
	}
	if n != 2 {
		t.Errorf("master-eligible 數 = %d, want 2", n)
	}
}
