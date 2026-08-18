package reporter

import (
	"encoding/json"
	"strings"
	"testing"

	"elk-diagnostics/internal/diagnostic"
	"elk-diagnostics/internal/nodecontext"
)

func TestMetricsNDJSONUsesStructuredValues(t *testing.T) {
	heapPct := 42
	r := diagnostic.NewReport(diagnostic.Meta{
		GeneratedAt: "2026-08-15T01:00:01Z",
		CollectedAt: "2026-08-15T01:00:00Z",
		Cluster: diagnostic.ClusterMeta{
			UUID: "cluster-uuid", Name: "prod", Host: "https://secret.example:9200", ESVersion: "8.14.3",
		},
	}, []diagnostic.Result{{
		ID: "fielddata_memory", Title: "Fielddata", Category: "performance", Status: diagnostic.StatusWarning, Source: "raw_api",
		Measurements: []diagnostic.Measurement{{Metric: "elasticsearch.node.fielddata.evictions", Kind: "counter", Value: 2, Unit: "count", EntityType: "node", EntityID: "node-1", EntityName: "es-1"}},
	}})
	r.NodeContext = &nodecontext.Snapshot{Nodes: []nodecontext.Node{{ID: "node-1", Name: "es-1", JVM: nodecontext.JVM{HeapUsedPct: &heapPct}}}}

	b, err := MetricsNDJSON(r)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "secret.example") {
		t.Fatal("趨勢資料不得包含連線 host")
	}
	var observations []metricObservation
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		var observation metricObservation
		if err := json.Unmarshal([]byte(line), &observation); err != nil {
			t.Fatalf("NDJSON 無法解析: %v", err)
		}
		observations = append(observations, observation)
	}
	assertMetric := func(metric string, value float64) {
		t.Helper()
		for _, observation := range observations {
			if observation.Metric == metric && observation.Value != nil && *observation.Value == value {
				if observation.Timestamp != r.Meta.CollectedAt || observation.TimestampSource != "collected_at" || observation.SchemaVersion != "1" {
					t.Fatalf("%s metadata=%+v", metric, observation)
				}
				return
			}
		}
		t.Fatalf("找不到 %s=%v", metric, value)
	}
	assertMetric("elasticsearch.node.fielddata.evictions", 2)
	assertMetric("node.jvm.heap.used_pct", 42)
}

func TestMetricsNDJSONRejectsMissingTimestamp(t *testing.T) {
	if _, err := MetricsNDJSON(diagnostic.Report{}); err == nil {
		t.Fatal("缺少時間時不應產生可能誤導的趨勢資料")
	}
}
