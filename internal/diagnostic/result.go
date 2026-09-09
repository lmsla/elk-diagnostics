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

// CurrentState 是報告產生當下的叢集摘要，不是診斷卡。
// 指標使用 pointer／Known 區分「確實是 0」與「本次沒有足夠資料」，避免摘要製造假綠燈。
type CurrentState struct {
	SnapshotNote string                   `json:"snapshot_note"`
	Health       CurrentHealth            `json:"health"`
	Nodes        CurrentNodes             `json:"nodes"`
	Shards       CurrentShards            `json:"shards"`
	Disk         CurrentDisk              `json:"disk"`
	Master       CurrentMaster            `json:"master"`
	ILM          CurrentService           `json:"ilm"`
	License      CurrentLicense           `json:"license"`
	Kibana       *CurrentServiceInstances `json:"kibana,omitempty"`
	Logstash     *CurrentServiceInstances `json:"logstash,omitempty"`
}

type CurrentHealth struct {
	Known  bool   `json:"known"`
	Status string `json:"status"`
	Source string `json:"source,omitempty"`
}

type CurrentNodes struct {
	Expected     *int     `json:"expected,omitempty"`
	Responding   *int     `json:"responding,omitempty"`
	Missing      *int     `json:"missing,omitempty"`
	MissingKnown bool     `json:"missing_known"`
	MissingNames []string `json:"missing_names,omitempty"`
}

type CurrentShards struct {
	Total             *int     `json:"total,omitempty"`
	Active            *int     `json:"active,omitempty"`
	ActivePrimary     *int     `json:"active_primary,omitempty"`
	Unassigned        *int     `json:"unassigned,omitempty"`
	UnassignedPrimary *int     `json:"unassigned_primary,omitempty"`
	Relocating        *int     `json:"relocating,omitempty"`
	Initializing      *int     `json:"initializing,omitempty"`
	ActivePercent     *float64 `json:"active_percent,omitempty"`
	MaxPerNode        *int     `json:"max_per_node,omitempty"`
	MaxPerFrozenNode  *int     `json:"max_per_frozen_node,omitempty"`
	MaxTotal          *int     `json:"max_total,omitempty"`
	// Capacity 是依本次回應的非 frozen data node 數量計算出的叢集容量。
	// MaxTotal 保留作為舊版欄位相容別名；新呈現應使用 Capacity。
	Capacity            *int     `json:"capacity,omitempty"`
	CapacityNodeCount   *int     `json:"capacity_node_count,omitempty"`
	Remaining           *int     `json:"remaining,omitempty"`
	CapacityUsedPercent *float64 `json:"capacity_used_percent,omitempty"`
}

type CurrentDiskNode struct {
	Name           string `json:"name"`
	Missing        bool   `json:"missing,omitempty"`
	UsedPercent    *int   `json:"used_percent,omitempty"`
	UsedBytes      *int64 `json:"used_bytes,omitempty"`
	AvailableBytes *int64 `json:"available_bytes,omitempty"`
	TotalBytes     *int64 `json:"total_bytes,omitempty"`
}

type CurrentDisk struct {
	Known          bool              `json:"known"`
	NodeCount      int               `json:"node_count,omitempty"`
	Nodes          []CurrentDiskNode `json:"nodes,omitempty"`
	UsedBytes      *int64            `json:"used_bytes,omitempty"`
	AvailableBytes *int64            `json:"available_bytes,omitempty"`
	TotalBytes     *int64            `json:"total_bytes,omitempty"`
	UsedPercent    *int              `json:"used_percent,omitempty"`
	// MaxUsedPercent / MaxNode 保留作為舊版欄位相容別名；新呈現列出每個節點。
	MaxUsedPercent *int   `json:"max_used_percent,omitempty"`
	MaxNode        string `json:"max_node,omitempty"`
}

type CurrentMaster struct {
	EligibleCount *int     `json:"eligible_count,omitempty"`
	EligibleNames []string `json:"eligible_names,omitempty"`
}

type CurrentService struct {
	Known  bool   `json:"known"`
	Status string `json:"status,omitempty"`
}

type CurrentLicense struct {
	Known  bool   `json:"known"`
	Status string `json:"status,omitempty"`
	Type   string `json:"type,omitempty"`
}

type CurrentServiceInstances struct {
	Known         bool     `json:"known"`
	Total         *int     `json:"total,omitempty"`
	Available     *int     `json:"available,omitempty"`
	Degraded      *int     `json:"degraded,omitempty"`
	Unavailable   *int     `json:"unavailable,omitempty"`
	Unknown       *int     `json:"unknown,omitempty"`
	NotApplicable *int     `json:"not_applicable,omitempty"`
	Versions      []string `json:"versions,omitempty"`
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
	CurrentState      *CurrentState         `json:"current_state,omitempty"`
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
