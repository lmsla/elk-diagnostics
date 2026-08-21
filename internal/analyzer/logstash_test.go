package analyzer

import (
	"testing"

	"elk-diagnostics/internal/collector"
	"elk-diagnostics/internal/diagnostic"
)

func logstashEvidence(root, health string, rootCode, healthCode int) collector.LogstashEvidence {
	return collector.LogstashEvidence{
		ID:               "ls-1",
		RootCode:         rootCode,
		RootBody:         []byte(root),
		HealthReportCode: healthCode,
		HealthReportBody: []byte(health),
	}
}

func TestLogstashStatusUsesReachabilityWhenRootHasNoStatus(t *testing.T) {
	got := LogstashStatus([]collector.LogstashEvidence{logstashEvidence(`{"name":"ls","version":{"number":"8.14.3"}}`, ``, 200, 404)})
	if got.Status != diagnostic.StatusPass {
		t.Fatalf("status=%s, want pass: %+v", got.Status, got)
	}
	if len(got.Measurements) == 0 {
		t.Fatal("expected status measurements")
	}
}

func TestLogstashStatusSupportsOfficialStringVersion(t *testing.T) {
	got := LogstashStatus([]collector.LogstashEvidence{logstashEvidence(`{"name":"ls","version":"8.14.3","status":"green"}`, ``, 200, 404)})
	if got.Status != diagnostic.StatusPass {
		t.Fatalf("status=%s, want pass: %+v", got.Status, got)
	}
	if len(got.Findings) != 1 || got.Findings[0] != "ls-1：root API 可用，version=8.14.3，status=green" {
		t.Fatalf("findings=%v, want official string version", got.Findings)
	}
}

func TestLogstashStatusLevels(t *testing.T) {
	for _, tc := range []struct {
		name, status string
		want         diagnostic.Status
	}{
		{"green", "green", diagnostic.StatusPass},
		{"yellow", "yellow", diagnostic.StatusWarning},
		{"red", "red", diagnostic.StatusCritical},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := LogstashStatus([]collector.LogstashEvidence{logstashEvidence(`{"version":{"number":"8.16.0"},"status":"`+tc.status+`"}`, ``, 200, 404)})
			if got.Status != tc.want {
				t.Fatalf("status=%s, want %s", got.Status, tc.want)
			}
		})
	}
}

func TestLogstashHealthReportUnsupportedVersionIsSkipped(t *testing.T) {
	got := LogstashHealthReport([]collector.LogstashEvidence{logstashEvidence(`{"version":{"number":"8.14.3"}}`, ``, 200, 404)})
	if got.Status != diagnostic.StatusSkipped {
		t.Fatalf("status=%s, want skipped: %+v", got.Status, got)
	}
}

func TestLogstashHealthReportOfficialStringVersionIsSkipped(t *testing.T) {
	got := LogstashHealthReport([]collector.LogstashEvidence{logstashEvidence(`{"version":"8.14.3"}`, ``, 200, 404)})
	if got.Status != diagnostic.StatusSkipped {
		t.Fatalf("status=%s, want skipped: %+v", got.Status, got)
	}
	if len(got.Findings) != 1 || got.Findings[0] != "ls-1：Logstash 8.14.3 未支援 /_health_report（8.16.0+ 才適用）" {
		t.Fatalf("findings=%v, want version-specific skip explanation", got.Findings)
	}
}

func TestLogstashHealthReportMissingOptionalFileIsSkipped(t *testing.T) {
	got := LogstashHealthReport([]collector.LogstashEvidence{{ID: "legacy", RootBody: []byte(`{"version":{"number":"8.14.3"}}`)}})
	if got.Status != diagnostic.StatusSkipped {
		t.Fatalf("status = %s, want skipped", got.Status)
	}
}

func TestLogstashHealthReportLevels(t *testing.T) {
	for _, tc := range []struct {
		name, status string
		want         diagnostic.Status
	}{
		{"green", "green", diagnostic.StatusPass},
		{"yellow", "yellow", diagnostic.StatusWarning},
		{"red", "red", diagnostic.StatusCritical},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := `{"status":"` + tc.status + `","indicators":{"pipelines":{"status":"` + tc.status + `","impacts":[{"severity":1,"description":"impact"}]}}}`
			got := LogstashHealthReport([]collector.LogstashEvidence{logstashEvidence(`{"version":{"number":"8.16.0"}}`, body, 200, 200)})
			if got.Status != tc.want {
				t.Fatalf("status=%s, want %s: %+v", got.Status, tc.want, got)
			}
		})
	}
}

func TestLogstashHealthReportForbiddenIsUnknown(t *testing.T) {
	got := LogstashHealthReport([]collector.LogstashEvidence{logstashEvidence(`{"version":{"number":"8.16.0"}}`, `{"error":"forbidden"}`, 200, 403)})
	if got.Status != diagnostic.StatusUnknown {
		t.Fatalf("status=%s, want unknown", got.Status)
	}
}

func TestLogstashPipelineStatsAreObservationOnly(t *testing.T) {
	e := logstashEvidence(`{"version":{"number":"8.14.3"}}`, ``, 200, 404)
	e.PipelineSample1Code = 200
	e.PipelineSample1Body = []byte(`{"pipelines":{"main":{"events":{"in":100,"out":90},"queue":{"events_count":10,"size_in_bytes":2048},"flow":{"input_throughput":{"current":12.5},"output_throughput":{"current":11.5}}}}}`)
	e.PipelineSample2Code = 200
	e.PipelineSample2Body = e.PipelineSample1Body
	got := LogstashPipelineStats([]collector.LogstashEvidence{e})
	if got.Status != diagnostic.StatusInfo || !got.RequiresExtra || len(got.Measurements) < 8 {
		t.Fatalf("result=%+v, want info with measurements", got)
	}
}

func TestLogstashPipelineStatsMissingIsSkipped(t *testing.T) {
	got := LogstashPipelineStats([]collector.LogstashEvidence{{ID: "ls-1", PipelineSample1Code: 403}})
	if got.Status != diagnostic.StatusSkipped {
		t.Fatalf("status=%s, want skipped", got.Status)
	}
}
