package analyzer

import (
	"reflect"
	"testing"

	"elk-diagnostics/internal/collector"
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
