// Package diagnostic 定義所有 analyzer 共用的結果契約（見 docs/內部/規格/診斷報告規格.md §1）。
// analyzer 一律產出 Result，reporter 負責收斂與渲染——兩者解耦。
package diagnostic

import (
	"time"

	"elk-diagnostics/internal/nodecontext"
)

type Status string

const (
	StatusPass     Status = "pass"
	StatusInfo     Status = "info"
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

// JudgmentGuide 是診斷卡提供給讀者的簡短判讀對照，不是 analyzer 的判定輸入。
type JudgmentGuide struct {
	Condition      string `json:"condition"`
	Interpretation string `json:"interpretation"`
}

// Measurement 是可供時間序列保存的結構化觀測值。Findings 仍供人閱讀，
// 任何趨勢輸出不得反向解析 Findings 文字。
type Measurement struct {
	Metric     string  `json:"metric"`
	Kind       string  `json:"kind"` // gauge | counter
	Value      float64 `json:"value"`
	Unit       string  `json:"unit,omitempty"`
	EntityType string  `json:"entity_type,omitempty"`
	EntityID   string  `json:"entity_id,omitempty"`
	EntityName string  `json:"entity_name,omitempty"`
	Component  string  `json:"component,omitempty"`
	// PeerGroup 是需要同類節點比較的觀測值所屬群組；空值代表不適用。
	PeerGroup string `json:"peer_group,omitempty"`
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
	Measurements    []Measurement    `json:"measurements,omitempty"`
	JudgmentGuide   []JudgmentGuide  `json:"judgment_guide,omitempty"`
}

type ClusterMeta struct {
	Name      string `json:"name"`
	UUID      string `json:"uuid,omitempty"`
	Host      string `json:"host"`
	ESVersion string `json:"es_version"`
}

type Meta struct {
	ToolVersion string      `json:"tool_version"`
	GeneratedAt string      `json:"generated_at"`
	Cluster     ClusterMeta `json:"cluster"`
	Mode        string      `json:"mode"`

	// CollectedAt / CollectScriptVersion：bundle 採集開始時間與採集腳本版本
	// （見 docs/內部/規格/採集包規格.md §4.2，2026-07-16 新增）。GeneratedAt 是分析時間，
	// 這兩個是採集時間——bundle 可能在採集數天後才被分析，不可混同。僅 --from-bundle
	// 且 bundle 含 _manifest.json 時才有值；省略代表舊版採集腳本產出的 bundle，不得用
	// mtime 或目錄名猜測。
	CollectedAt          string   `json:"collected_at,omitempty"`
	CollectScriptVersion string   `json:"collect_script_version,omitempty"`
	BundleSchemaVersion  int      `json:"bundle_schema_version,omitempty"`
	CollectedServices    []string `json:"collected_services,omitempty"`
}

type Summary struct {
	Pass     int `json:"pass"`
	Info     int `json:"info,omitempty"`
	Warning  int `json:"warning"`
	Critical int `json:"critical"`
	Skipped  int `json:"skipped"`
	Unknown  int `json:"unknown"`
}

// SymptomHint 是 check 巡檢時偵測到特定症狀特徵組合後的反向觸發提示
// （見 症狀診斷規格 §3），非診斷結論，僅建議下一步指令。
type SymptomHint struct {
	Symptom string `json:"symptom"`
	Reason  string `json:"reason"`
}

type Report struct {
	Meta              Meta                  `json:"meta"`
	OverallStatus     Status                `json:"overall_status"`
	Summary           Summary               `json:"summary"`
	VersionNotice     string                `json:"version_notice,omitempty"` // 見 診斷報告規格 §3；目標版本不受支援時的全域提示（見 buildReport 呼叫端設值）
	Results           []Result              `json:"results"`
	NodeContext       *nodecontext.Snapshot `json:"node_context,omitempty"`
	SuggestedSymptoms []SymptomHint         `json:"suggested_symptoms,omitempty"`
	Disclaimer        string                `json:"disclaimer"`
}

const disclaimer = "本工具提供診斷引導，非根因確認。結論基於單次唯讀快照與預設閾值，請結合現場日誌、時間序列監控與業務脈絡綜合判斷。工具僅執行唯讀操作，任何修復指令均需人工確認後手動執行。"

// NewReport 組裝報告並依 診斷報告規格 §2 收斂 overall_status 與計數。
func NewReport(meta Meta, results []Result) Report {
	meta.GeneratedAt = time.Now().UTC().Format(time.RFC3339)
	r := Report{Meta: meta, Results: results, Disclaimer: disclaimer}

	for _, res := range results {
		switch res.Status {
		case StatusPass:
			r.Summary.Pass++
		case StatusInfo:
			r.Summary.Info++
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

	// 收斂：critical > warning > unknown > pass；info 與 skipped 不影響。
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

// ExitCode 依 命令列規格 §3 對映 overall_status。
func (r Report) ExitCode() int {
	switch r.OverallStatus {
	case StatusPass:
		return 0
	case StatusInfo:
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
