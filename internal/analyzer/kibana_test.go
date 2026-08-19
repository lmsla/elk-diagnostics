package analyzer

import (
	"testing"

	"elk-diagnostics/internal/collector"
	"elk-diagnostics/internal/diagnostic"
)

func TestKibanaStatusLevels(t *testing.T) {
	tests := []struct {
		name   string
		body   string
		code   int
		status diagnostic.Status
	}{
		{"available", `{"name":"kibana","version":{"number":"8.14.3"},"status":{"overall":{"level":"available","summary":"All services are available"}}}`, 200, diagnostic.StatusPass},
		{"degraded", `{"name":"kibana","version":{"number":"8.14.3"},"status":{"overall":{"level":"degraded","summary":"A plugin is degraded"}}}`, 200, diagnostic.StatusWarning},
		{"unavailable", `{"name":"kibana","version":{"number":"8.14.3"},"status":{"overall":{"level":"unavailable","summary":"Kibana is unavailable"}}}`, 200, diagnostic.StatusCritical},
		{"forbidden", `{"error":"forbidden"}`, 403, diagnostic.StatusUnknown},
		{"redacted", `{"status":{"overall":{}}}`, 200, diagnostic.StatusUnknown},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := KibanaStatus([]collector.KibanaEvidence{{ID: "k1", StatusCode: tc.code, StatusBody: []byte(tc.body)}})
			if got.Status != tc.status {
				t.Fatalf("status=%s, want %s; result=%+v", got.Status, tc.status, got)
			}
		})
	}
}

func TestKibanaStatsAreObservationOnly(t *testing.T) {
	got := KibanaStats([]collector.KibanaEvidence{{
		ID: "k1", StatsCode: 200, StatsBody: []byte(`{
  "process":{"memory":{"heap":{"used_bytes":10,"size_limit":100}},"event_loop_utilization":{"utilization":0.25}},
  "os":{"memory":{"used_bytes":20,"total_bytes":40},"load":{"1m":1.5}},
  "requests":{"total":7},"elasticsearch_client":{"total_queued_requests":2}
}`),
	}})
	if got.Status != diagnostic.StatusInfo || len(got.Measurements) < 5 {
		t.Fatalf("result=%+v, want info with measurements", got)
	}
	if got.RequiresExtra != true {
		t.Fatalf("RequiresExtra=false, want true for single-snapshot stats")
	}

	skipped := KibanaStats([]collector.KibanaEvidence{{ID: "k1", StatsCode: 403}})
	if skipped.Status != diagnostic.StatusSkipped {
		t.Fatalf("403 stats status=%s, want skipped", skipped.Status)
	}
}

func TestKibanaTaskManagerHealth(t *testing.T) {
	tests := []struct {
		name   string
		code   int
		body   string
		status diagnostic.Status
	}{
		{"正常", 200, `{"status":"OK","last_update":"2026-08-19T00:00:00Z"}`, diagnostic.StatusPass},
		{"降級", 200, `{"status":"WARN"}`, diagnostic.StatusWarning},
		{"錯誤", 200, `{"status":"ERROR"}`, diagnostic.StatusCritical},
		{"403 無法判定", 403, `{"error":"forbidden"}`, diagnostic.StatusUnknown},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := KibanaTaskManagerHealth([]collector.KibanaEvidence{{ID: "k1", TaskManagerCode: tc.code, TaskManagerBody: []byte(tc.body)}})
			if got.Status != tc.status {
				t.Fatalf("status=%s, want %s; result=%+v", got.Status, tc.status, got)
			}
		})
	}
	if got := KibanaTaskManagerHealth([]collector.KibanaEvidence{{ID: "k1", TaskManagerCode: 200}}); got.Status != diagnostic.StatusUnknown {
		t.Fatalf("missing task manager body status=%s, want unknown", got.Status)
	}
}

func TestKibanaAlertingHealth(t *testing.T) {
	okBody := `{"alerting_framework_health":{"decryption_health":{"status":"ok"},"execution_health":{"status":"ok"},"read_health":{"status":"ok"}},"has_permanent_encryption_key":true,"is_sufficiently_secure":true}`
	if got := KibanaAlertingHealth([]collector.KibanaEvidence{{ID: "k1", AlertingCode: 200, AlertingBody: []byte(okBody)}}); got.Status != diagnostic.StatusPass {
		t.Fatalf("normal status=%s, want pass", got.Status)
	}
	errorBody := `{"alerting_framework_health":{"decryption_health":{"status":"ok"},"execution_health":{"status":"error"},"read_health":{"status":"ok"}}}`
	if got := KibanaAlertingHealth([]collector.KibanaEvidence{{ID: "k1", AlertingCode: 200, AlertingBody: []byte(errorBody)}}); got.Status != diagnostic.StatusCritical {
		t.Fatalf("execution error status=%s, want critical", got.Status)
	}
	if got := KibanaAlertingHealth([]collector.KibanaEvidence{{ID: "k1", AlertingCode: 403, AlertingBody: []byte(`{"error":"forbidden"}`)}}); got.Status != diagnostic.StatusUnknown {
		t.Fatalf("403 status=%s, want unknown", got.Status)
	}
}
