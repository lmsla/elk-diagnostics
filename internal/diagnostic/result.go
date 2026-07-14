// Package diagnostic 定義所有 analyzer 共用的結果契約（見 docs/specs/spec-report.md §1）。
// analyzer 一律產出 Result，reporter 負責收斂與渲染——兩者解耦。
package diagnostic

import "time"

type Status string

const (
	StatusPass     Status = "pass"
	StatusWarning  Status = "warning"
	StatusCritical Status = "critical"
	StatusSkipped  Status = "skipped"
	StatusUnknown  Status = "unknown"
)

type Conclusion string

const (
	ConclusionNormal    Conclusion = "normal"
	ConclusionSuspected Conclusion = "suspected"
	ConclusionConfirmed Conclusion = "confirmed"
)

type Recommendation struct {
	Cmd  string `json:"cmd"`
	Desc string `json:"desc"`
}

// Result 是單一診斷項目的標準輸出。
type Result struct {
	ID              string           `json:"id"`
	Title           string           `json:"title"`
	Category        string           `json:"category"`
	Status          Status           `json:"status"`
	Conclusion      Conclusion       `json:"conclusion"`
	Summary         string           `json:"summary"`
	Findings        []string         `json:"findings"`
	RootCauses      []string         `json:"root_causes"`
	Recommendations []Recommendation `json:"recommendations"`
	Docs            []string         `json:"docs"`
	Source          string           `json:"source"` // health_report | raw_api | fallback
	RequiresExtra   bool             `json:"requires_extra"`
	ExtraReason     string           `json:"extra_reason,omitempty"`
	VersionWarning  string           `json:"version_warning,omitempty"`
}

type ClusterMeta struct {
	Name      string `json:"name"`
	Host      string `json:"host"`
	ESVersion string `json:"es_version"`
}

type Meta struct {
	ToolVersion string      `json:"tool_version"`
	GeneratedAt string      `json:"generated_at"`
	Cluster     ClusterMeta `json:"cluster"`
	Mode        string      `json:"mode"`
}

type Summary struct {
	Pass     int `json:"pass"`
	Warning  int `json:"warning"`
	Critical int `json:"critical"`
	Skipped  int `json:"skipped"`
	Unknown  int `json:"unknown"`
}

type Report struct {
	Meta          Meta     `json:"meta"`
	OverallStatus Status   `json:"overall_status"`
	Summary       Summary  `json:"summary"`
	Results       []Result `json:"results"`
	Disclaimer    string   `json:"disclaimer"`
}

const disclaimer = "本工具提供診斷引導，非根因確認。結論基於單次唯讀快照與預設閾值，請結合現場日誌、時間序列監控與業務脈絡綜合判斷。工具僅執行唯讀操作，任何修復指令均需人工確認後手動執行。"

// NewReport 組裝報告並依 spec-report §2 收斂 overall_status 與計數。
func NewReport(meta Meta, results []Result) Report {
	meta.GeneratedAt = time.Now().UTC().Format(time.RFC3339)
	r := Report{Meta: meta, Results: results, Disclaimer: disclaimer}

	for _, res := range results {
		switch res.Status {
		case StatusPass:
			r.Summary.Pass++
		case StatusWarning:
			r.Summary.Warning++
		case StatusCritical:
			r.Summary.Critical++
		case StatusSkipped:
			r.Summary.Skipped++
		case StatusUnknown:
			r.Summary.Unknown++
		}
	}

	// 收斂：critical > warning > unknown > pass；skipped 不影響。
	switch {
	case r.Summary.Critical > 0:
		r.OverallStatus = StatusCritical
	case r.Summary.Warning > 0:
		r.OverallStatus = StatusWarning
	case r.Summary.Unknown > 0:
		r.OverallStatus = StatusUnknown
	default:
		r.OverallStatus = StatusPass
	}
	return r
}

// ExitCode 依 spec-cli §3 對映 overall_status。
func (r Report) ExitCode() int {
	switch r.OverallStatus {
	case StatusPass:
		return 0
	case StatusWarning:
		return 1
	case StatusCritical:
		return 2
	case StatusUnknown:
		return 3
	default:
		return 3
	}
}
