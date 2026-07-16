// Package analyzer 解讀採集資料、產出 diagnostic.Result。
//
// healthreport.go：通用 indicator analyzer。_health_report 的各 indicator 結構相同
// （status/symptom/diagnosis/impacts），故以一張驅動表產出多條結果，不為每個 A 類項目
// 複製 analyzer（見 docs/specs/spec-health-report.md）。ILM 因有延遲，另由 management.go
// 搭 _ilm/explain 補強；thread pool 等缺口由各自 analyzer 處理。
package analyzer

import (
	"fmt"

	"elk-diagnostics/internal/collector"
	"elk-diagnostics/internal/diagnostic"
)

const docRedYellow = "https://www.elastic.co/docs/troubleshoot/elasticsearch/red-yellow-cluster-status"

type indicatorSpec struct {
	indicator string
	id        string
	title     string
	category  string
	docs      []string
}

// 由 _health_report indicator 直接吸收的 A/B 類項目（ilm 除外，特例處理）。
var healthReportIndicators = []indicatorSpec{
	{"shards_availability", "cluster_health", "叢集健康 / 未分配 shard", "cluster",
		[]string{docRedYellow}},
	{"disk", "disk", "磁碟容量 / watermark", "capacity",
		[]string{"https://www.elastic.co/docs/troubleshoot/elasticsearch/fix-watermark-errors"}},
	{"shards_capacity", "shards_capacity", "Shard 容量上限", "capacity",
		[]string{"https://www.elastic.co/docs/troubleshoot/elasticsearch/troubleshooting-shards-capacity-issues"}},
	{"repository_integrity", "snapshot_repository", "Snapshot repository 完整性", "snapshot",
		[]string{"https://www.elastic.co/docs/troubleshoot/elasticsearch/add-repository"}},
	{"slm", "slm", "SLM 快照政策", "snapshot",
		[]string{"https://www.elastic.co/docs/troubleshoot/elasticsearch/repeated-snapshot-failures"}},
	{"master_is_stable", "master_stability", "Master 穩定性", "cluster",
		[]string{"https://www.elastic.co/docs/troubleshoot/elasticsearch/troubleshooting-unstable-cluster"}},
	{"data_stream_lifecycle", "data_stream_lifecycle", "Data stream 生命週期 / tier", "data",
		[]string{"https://www.elastic.co/docs/troubleshoot/elasticsearch/add-tier"}},
}

// FromHealthReport 對表中每個 indicator 產出一條結果。未知/新增 indicator 一律忽略（容錯）。
func FromHealthReport(hr *collector.HealthReport) []diagnostic.Result {
	out := make([]diagnostic.Result, 0, len(healthReportIndicators))
	for _, s := range healthReportIndicators {
		out = append(out, mapIndicator(hr, s))
	}
	return out
}

func mapIndicator(hr *collector.HealthReport, s indicatorSpec) diagnostic.Result {
	res := diagnostic.Result{
		ID:       s.id,
		Title:    s.title,
		Category: s.category,
		Source:   "health_report",
		Docs:     append([]string{}, s.docs...),
	}

	ind, ok := hr.Indicators[s.indicator]
	if !ok {
		res.Status = diagnostic.StatusSkipped
		res.Conclusion = diagnostic.ConclusionNormal
		res.Summary = fmt.Sprintf("此版本未提供 %q indicator", s.indicator)
		return res
	}

	switch ind.Status {
	case "green":
		res.Status = diagnostic.StatusPass
		res.Conclusion = diagnostic.ConclusionNormal
		res.Summary = symptomOr(ind, "正常")
	case "yellow":
		res.Status = diagnostic.StatusWarning
		res.Conclusion = diagnostic.ConclusionSuspected
		res.Summary = symptomOr(ind, "偵測到風險")
	case "red":
		res.Status = diagnostic.StatusCritical
		res.Conclusion = diagnostic.ConclusionConfirmed
		res.Summary = symptomOr(ind, "偵測到嚴重問題")
	default:
		res.Status = diagnostic.StatusUnknown
		res.Conclusion = diagnostic.ConclusionNormal
		res.Summary = fmt.Sprintf("%s 狀態未知：%q", s.indicator, ind.Status)
	}

	seenAction := map[string]bool{}
	seenDoc := map[string]bool{}
	for _, d := range res.Docs {
		seenDoc[d] = true
	}
	for _, d := range ind.Diagnosis {
		if d.Cause != "" {
			cause := d.Cause
			if len(d.AffectedResources.Indices) > 0 {
				cause += fmt.Sprintf("（受影響 index：%v）", d.AffectedResources.Indices)
			}
			res.RootCauses = append(res.RootCauses, cause)
		}
		if d.Action != "" && !seenAction[d.Action] {
			seenAction[d.Action] = true
			res.Recommendations = append(res.Recommendations, diagnostic.Recommendation{Desc: d.Action})
		}
		if d.HelpURL != "" && !seenDoc[d.HelpURL] {
			seenDoc[d.HelpURL] = true
			res.Docs = append(res.Docs, d.HelpURL)
		}
	}
	for _, im := range ind.Impacts {
		if im.Description != "" {
			res.Findings = append(res.Findings, im.Description)
		}
	}
	return res
}

// HealthReportIndicatorIDs 回傳 A 類（healthReportIndicators 驅動表）全部 id，依表中順序。
// 供 check.go 判斷某個 diagnostic.Result 是否屬於 A 類（例如套用版本警告時要排除 A 類，
// 因為 A 類已經是 skipped，不需要再疊加 version_warning），單一來源導出、不得另建清單
// （見 docs/workorders/wo-2026-07-16-trust.md T2）。
func HealthReportIndicatorIDs() []string {
	ids := make([]string, len(healthReportIndicators))
	for i, s := range healthReportIndicators {
		ids[i] = s.id
	}
	return ids
}

// HealthReportFetchFailed 在無法取得 _health_report 時（連線逾時/4xx/5xx；bundle 缺檔或
// 錯誤 body），讓 A 類全數以 status=unknown 浮出，而非讓整份報告中止
//（見 spec-resilience §1，2026-07-16 修訂）。summary/findings 由呼叫端組裝——措辭依連線/
// bundle 模式而異（見 spec-resilience §3），屬 CLI 層決策，不在 analyzer 內判斷模式。
// A 類清單一律從 healthReportIndicators 這張單一驅動表導出，不另建對照表。
func HealthReportFetchFailed(summary string, findings []string) []diagnostic.Result {
	out := make([]diagnostic.Result, 0, len(healthReportIndicators))
	for _, s := range healthReportIndicators {
		out = append(out, diagnostic.Result{
			ID:         s.id,
			Title:      s.title,
			Category:   s.category,
			Source:     "health_report",
			Docs:       append([]string{}, s.docs...),
			Status:     diagnostic.StatusUnknown,
			Conclusion: diagnostic.ConclusionNormal,
			Summary:    summary,
			Findings:   append([]string{}, findings...),
		})
	}
	return out
}

// HealthReportVersionUnsupported 在目標 ES 版本 < 8.4（無 _health_report）時，讓 A 類全數
// status=skipped（不是 unknown——這是「此環境不適用」，不是「該有而拿不到」，見
// spec-resilience §1、spec-cli §4）。
func HealthReportVersionUnsupported(esVersion string) []diagnostic.Result {
	out := make([]diagnostic.Result, 0, len(healthReportIndicators))
	summary := fmt.Sprintf("ES %s < 8.4 無 _health_report，本項不適用", esVersion)
	for _, s := range healthReportIndicators {
		out = append(out, diagnostic.Result{
			ID:         s.id,
			Title:      s.title,
			Category:   s.category,
			Source:     "health_report",
			Docs:       append([]string{}, s.docs...),
			Status:     diagnostic.StatusSkipped,
			Conclusion: diagnostic.ConclusionNormal,
			Summary:    summary,
		})
	}
	return out
}

// HealthReportIndicator 取單一 indicator 的判定結果（依 id，如 "cluster_health"、"disk"）。
// 供症狀診斷樹依因果順序挑選特定環節，不重跑 FromHealthReport 全量（見 spec-diagnose-symptoms）。
func HealthReportIndicator(hr *collector.HealthReport, id string) (diagnostic.Result, bool) {
	for _, s := range healthReportIndicators {
		if s.id == id {
			return mapIndicator(hr, s), true
		}
	}
	return diagnostic.Result{}, false
}

// AffectedIndices 彙整指定 indicator 的 diagnosis 中所有受影響 index（去重、依出現順序）。
// #20 用：找出需要逐一查 index.routing.allocation.enable 的候選 index。
func AffectedIndices(hr *collector.HealthReport, indicator string) []string {
	if hr == nil {
		return nil
	}
	ind, ok := hr.Indicators[indicator]
	if !ok {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, d := range ind.Diagnosis {
		for _, idx := range d.AffectedResources.Indices {
			if !seen[idx] {
				seen[idx] = true
				out = append(out, idx)
			}
		}
	}
	return out
}

func symptomOr(ind collector.HRIndicator, def string) string {
	if ind.Symptom != "" {
		return ind.Symptom
	}
	return def
}
