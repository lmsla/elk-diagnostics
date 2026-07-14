package collector

import "encoding/json"

// HealthReport 對映 GET /_health_report（見 docs/specs/spec-health-report.md）。
// Indicators 為 map，刻意容忍未知/新增 indicator（9.x 多了 file_settings，Phase 0 實測）。
type HealthReport struct {
	Status     string                 `json:"status"`
	Indicators map[string]HRIndicator `json:"indicators"`
}

type HRIndicator struct {
	Status    string        `json:"status"`
	Symptom   string        `json:"symptom"`
	Diagnosis []HRDiagnosis `json:"diagnosis"`
	Impacts   []HRImpact    `json:"impacts"`
}

type HRDiagnosis struct {
	Cause             string         `json:"cause"`
	Action            string         `json:"action"`
	HelpURL           string         `json:"help_url"`
	AffectedResources HRAffectedRsrc `json:"affected_resources"`
}

type HRAffectedRsrc struct {
	Indices []string `json:"indices"`
}

type HRImpact struct {
	Severity    int    `json:"severity"`
	Description string `json:"description"`
}

// ParseHealthReport 解析回應位元組。HTTP 模式與 --from-file 模式共用此函式。
func ParseHealthReport(b []byte) (*HealthReport, error) {
	var hr HealthReport
	if err := json.Unmarshal(b, &hr); err != nil {
		return nil, err
	}
	return &hr, nil
}
