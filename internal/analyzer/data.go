package analyzer

import (
	"fmt"

	"elk-diagnostics/internal/collector"
	"elk-diagnostics/internal/diagnostic"
	"elk-diagnostics/rules"
)

const (
	docMapping    = "https://www.elastic.co/docs/troubleshoot/elasticsearch/mapping-explosion"
	docIngest     = "https://www.elastic.co/docs/troubleshoot/elasticsearch/troubleshoot-ingest-pipelines"
	docCorruption = "https://www.elastic.co/docs/troubleshoot/elasticsearch/corruption-troubleshooting"
)

// MappingExplosion #11：mapping 欄位數逼近/超過 total_fields.limit。
func MappingExplosion(counts []collector.IndexFieldCount, t rules.Thresholds) diagnostic.Result {
	mappingLimit, mappingWarnFrac := t.Data.MappingLimitDefault, t.Data.MappingWarnFrac
	res := diagnostic.Result{ID: "mapping_explosion", Title: "Mapping 欄位膨脹", Category: "data", Source: "raw_api", Docs: []string{docMapping}}
	warnAt := mappingLimit * mappingWarnFrac / 100
	var crit, warn []string
	for _, c := range counts {
		switch {
		case c.FieldCount >= mappingLimit:
			crit = append(crit, fmt.Sprintf("%s：%d 欄位（已達/超過上限 %d）", c.Index, c.FieldCount, mappingLimit))
		case c.FieldCount >= warnAt:
			warn = append(warn, fmt.Sprintf("%s：%d 欄位（上限 %d 的 %d%%）", c.Index, c.FieldCount, mappingLimit, mappingWarnFrac))
		}
	}
	res.Findings = append(crit, warn...)
	switch {
	case len(crit) > 0:
		res.Status, res.Conclusion = diagnostic.StatusCritical, diagnostic.ConclusionConfirmed
		res.Summary = fmt.Sprintf("%d 個 index 欄位數達/超過上限 %d，新欄位寫入將被拒", len(crit), mappingLimit)
	case len(warn) > 0:
		res.Status, res.Conclusion = diagnostic.StatusWarning, diagnostic.ConclusionSuspected
		res.Summary = fmt.Sprintf("%d 個 index 欄位數逼近上限", len(warn))
	default:
		return pass(res, fmt.Sprintf("各 index 欄位數均低於上限 %d 的 %d%%", mappingLimit, mappingWarnFrac))
	}
	res.RootCauses = []string{"多為上游資料格式問題或動態 mapping 失控（runaway dynamic mapping）"}
	res.Recommendations = []diagnostic.Recommendation{{Desc: "關閉動態 mapping、改用 flattened 型別、或 reindex 至修正後的 mapping；勿盲目調高 total_fields.limit"}}
	res.RequiresExtra, res.ExtraReason = true, "欄位數為 mapping 樹 leaf type 之近似計數；跨 index 合併膨脹（如 data view）需另以 field_caps 確認"
	return res
}

// IngestPipelineErrors #13：ingest pipeline failed 比例過高。
func IngestPipelineErrors(pipes []collector.IngestPipeline, t rules.Thresholds) diagnostic.Result {
	ingestFailWarn := t.Data.IngestFailWarnPct
	res := diagnostic.Result{ID: "ingest_pipeline_errors", Title: "Ingest pipeline 失敗", Category: "data", Source: "raw_api", Docs: []string{docIngest}}
	var hits []string
	for _, p := range pipes {
		if p.Count > 0 {
			pct := int(100 * p.Failed / p.Count)
			if pct > ingestFailWarn {
				hits = append(hits, fmt.Sprintf("%s：failed=%d / count=%d（%d%%）", p.Pipeline, p.Failed, p.Count, pct))
			}
		}
	}
	if len(hits) == 0 {
		return pass(res, "各 ingest pipeline 失敗率正常")
	}
	res.Status, res.Conclusion = diagnostic.StatusWarning, diagnostic.ConclusionSuspected
	res.Summary = fmt.Sprintf("%d 個 ingest pipeline 失敗率 >%d%%（累積值）", len(hits), ingestFailWarn)
	res.Findings = hits
	res.RequiresExtra, res.ExtraReason = true, "count/failed 為自啟動起的累積值；需間隔取樣或查 log 確認是否持續"
	res.Recommendations = []diagnostic.Recommendation{{Desc: "定位失敗 processor（常為 script/painless 或 mapping 衝突）；檢查 on_failure 設計"}}
	return res
}

// DataCorruption #32：偵測 index 異常狀態徵兆（checksum 級驗證屬 ES 內部，工具不做）。
func DataCorruption(indices []collector.IndexHealth) diagnostic.Result {
	res := diagnostic.Result{ID: "data_corruption", Title: "資料毀損徵兆", Category: "data", Source: "raw_api", Docs: []string{docCorruption}}
	var hits []string
	for _, idx := range indices {
		if idx.Health == "red" {
			hits = append(hits, fmt.Sprintf("%s：health=red status=%s", idx.Index, idx.Status))
		}
	}
	if len(hits) == 0 {
		res = pass(res, "無 index 處於 red 狀態")
		res.RequiresExtra, res.ExtraReason = true, "工具僅能偵測狀態異常徵兆；checksum/translog 級 corruption 需由 ES 內部於 merge/recovery/snapshot 時偵測，請留意節點 log 的 CorruptIndexException 等例外"
		return res
	}
	res.Status, res.Conclusion = diagnostic.StatusWarning, diagnostic.ConclusionSuspected
	res.Summary = fmt.Sprintf("%d 個 index 處於 red，可能為毀損徵兆之一", len(hits))
	res.Findings = hits
	res.RootCauses = []string{"red 可能源自未分配 shard（見 cluster_health）或實際資料毀損；後者多由儲存層/檔案系統/韌體問題引起"}
	res.RequiresExtra, res.ExtraReason = true, "請查節點 log 是否有 CorruptIndexException / TranslogCorruptedException / 檔案缺失等例外以區分；checksum 驗證需 ES 內部機制"
	res.Recommendations = []diagnostic.Recommendation{{Desc: "查節點 log 確認是否為 corruption；若是，從 snapshot 還原並排查儲存層（fio/stress-ng 驗 I/O 完整性）"}}
	return res
}
