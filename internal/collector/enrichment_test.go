package collector

import "testing"

func TestEnrichmentCollectors(t *testing.T) {
	c := newStaticBundleClient(t, map[string]string{
		"ilm_policies.json":          `{"logs-policy":{"version":3,"modified_date_millis":1000,"policy":{"phases":{"hot":{"min_age":"0ms","actions":{"rollover":{"max_primary_shard_size":"50gb"}}},"delete":{"min_age":"30d","actions":{"delete":{}}}}},"in_use_by":{"indices":["logs-1"],"data_streams":["logs-app"],"composable_templates":["logs"]}}}`,
		"snapshot_repositories.json": `{"backup":{"type":"fs","uuid":"repo-1"}}`,
		"data_streams.json":          `{"data_streams":[{"name":"logs-app","status":"YELLOW","template":"logs","generation":2,"ilm_policy":"logs-policy","next_generation_managed_by":"Index Lifecycle Management","indices":[{"index_name":".ds-logs-app-000001","ilm_policy":"logs-policy","managed_by":"Index Lifecycle Management"}]}]}`,
		"nodes_fielddata.json":       `{"_nodes":{"total":2,"successful":2,"failed":0},"nodes":{"b":{"name":"n2","indices":{"fielddata":{"memory_size_in_bytes":2048,"evictions":1}}},"a":{"name":"n1","indices":{"fielddata":{"memory_size_in_bytes":1024,"evictions":0}}}}}`,
	})

	policies, err := c.ILMPolicies()
	if err != nil || len(policies) != 1 || policies[0].Name != "logs-policy" || len(policies[0].Phases) != 2 || policies[0].UsedDataStreams != 1 {
		t.Fatalf("ILMPolicies() = %+v, %v", policies, err)
	}
	repositories, err := c.SnapshotRepositories()
	if err != nil || len(repositories) != 1 || repositories[0].Name != "backup" || repositories[0].Type != "fs" {
		t.Fatalf("SnapshotRepositories() = %+v, %v", repositories, err)
	}
	streams, err := c.DataStreams()
	if err != nil || len(streams) != 1 || streams[0].Status != "YELLOW" || streams[0].ILMPolicy != "logs-policy" || streams[0].BackingIndices != 1 {
		t.Fatalf("DataStreams() = %+v, %v", streams, err)
	}
	fielddata, err := c.FielddataStats()
	if err != nil || !fielddata.Coverage.Complete() || len(fielddata.Nodes) != 2 || fielddata.Nodes[0].Name != "n1" || fielddata.Nodes[1].Evictions != 1 {
		t.Fatalf("FielddataStats() = %+v, %v", fielddata, err)
	}
}

func TestDataStreamsFallsBackToBackingIndexLifecycle(t *testing.T) {
	c := newStaticBundleClient(t, map[string]string{
		"data_streams.json": `{"data_streams":[{"name":"logs-app","status":"GREEN","indices":[{"index_name":".ds-logs-app-000001","ilm_policy":"logs-policy","managed_by":"Index Lifecycle Management"}]}]}`,
	})
	streams, err := c.DataStreams()
	if err != nil || len(streams) != 1 || streams[0].ILMPolicy != "logs-policy" || streams[0].ManagedBy != "Index Lifecycle Management" {
		t.Fatalf("DataStreams() fallback = %+v, %v", streams, err)
	}
}
