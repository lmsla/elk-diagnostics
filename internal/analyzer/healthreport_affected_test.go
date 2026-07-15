package analyzer

import (
	"reflect"
	"testing"

	"elk-diagnostics/internal/collector"
	"elk-diagnostics/internal/diagnostic"
)

func TestAffectedIndices(t *testing.T) {
	hr := &collector.HealthReport{
		Indicators: map[string]collector.HRIndicator{
			"shards_availability": {
				Diagnosis: []collector.HRDiagnosis{
					{AffectedResources: collector.HRAffectedRsrc{Indices: []string{"a", "b"}}},
					{AffectedResources: collector.HRAffectedRsrc{Indices: []string{"b", "c"}}}, // b 重複
				},
			},
		},
	}
	got := AffectedIndices(hr, "shards_availability")
	want := []string{"a", "b", "c"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v（應去重且保留出現順序）", got, want)
	}
}

func TestAffectedIndices_MissingIndicator(t *testing.T) {
	hr := &collector.HealthReport{Indicators: map[string]collector.HRIndicator{}}
	if got := AffectedIndices(hr, "shards_availability"); got != nil {
		t.Errorf("got %v, want nil", got)
	}
}

func TestHealthReportIndicator(t *testing.T) {
	hr := &collector.HealthReport{
		Indicators: map[string]collector.HRIndicator{
			"disk": {Status: "red", Symptom: "flood-stage watermark exceeded"},
		},
	}
	t.Run("已知 id 命中", func(t *testing.T) {
		res, ok := HealthReportIndicator(hr, "disk")
		if !ok {
			t.Fatal("want ok=true")
		}
		if res.ID != "disk" || res.Status != diagnostic.StatusCritical {
			t.Errorf("got id=%q status=%q, want id=disk status=critical", res.ID, res.Status)
		}
	})
	t.Run("未知 id 不命中", func(t *testing.T) {
		if _, ok := HealthReportIndicator(hr, "no_such_indicator"); ok {
			t.Error("want ok=false")
		}
	})
	t.Run("已知 id 但叢集未提供該 indicator", func(t *testing.T) {
		res, ok := HealthReportIndicator(hr, "shards_capacity")
		if !ok {
			t.Fatal("want ok=true（id 存在於對照表，只是本次未提供）")
		}
		if res.Status != diagnostic.StatusSkipped {
			t.Errorf("Status = %q, want skipped", res.Status)
		}
	})
}
