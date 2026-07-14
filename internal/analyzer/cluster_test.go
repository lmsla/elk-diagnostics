package analyzer

import (
	"testing"

	"elk-diagnostics/internal/diagnostic"
)

func TestMasterStabilityContext(t *testing.T) {
	cases := []struct {
		name          string
		total, master int
		wantStatus    diagnostic.Status
	}{
		{"3 master-eligible（健康）", 5, 3, diagnostic.StatusPass},
		{"1 master-eligible（單點故障）", 3, 1, diagnostic.StatusWarning},
		{"2 master-eligible（偶數風險）", 4, 2, diagnostic.StatusWarning},
		{"0 master-eligible（無法選舉）", 3, 0, diagnostic.StatusCritical},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res := MasterStabilityContext(c.total, c.master)
			if res.Status != c.wantStatus {
				t.Errorf("Status = %q, want %q", res.Status, c.wantStatus)
			}
		})
	}
}
