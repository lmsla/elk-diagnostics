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
	docAddTier    = "https://www.elastic.co/docs/troubleshoot/elasticsearch/add-tier"
)

// DataTierAvailability #24：各標準 data tier 是否有對應節點。資訊性質——缺 tier
// 節點不必然是問題（許多叢集刻意不建置 warm/cold/frozen），是否構成 preferred tier
// 缺節點需對照 data_stream_lifecycle indicator 的診斷訊息（A 類已由 healthreport.go
// 產出）交叉確認，此處只提供結構性事實。
func DataTierAvailability(counts map[string]int) diagnostic.Result {
	res := diagnostic.Result{ID: "data_tier_availability", Title: "Data tier 節點分布", Category: "data", Source: "raw_api", Docs: []string{docAddTier}}
	var present, missing []string
	for _, tier := range []string{"data_content", "data_hot", "data_warm", "data_cold", "data_frozen"} {
		res.Measurements = append(res.Measurements, gauge("elasticsearch.data_tier.node.count", float64(counts[tier]), "count", "", "", "", tier))
		if counts[tier] > 0 {
			present = append(present, fmt.Sprintf("%s=%d", tier, counts[tier]))
		} else {
			missing = append(missing, tier)
		}
	}
	res.Status, res.Conclusion = diagnostic.StatusPass, diagnostic.ConclusionNormal
	if len(missing) == 0 {
		res.Summary = "所有標準 data tier 皆有對應節點"
		return res
	}
	res.Summary = fmt.Sprintf("叢集無 %v tier 的節點（資訊性；若 ILM/DSL 政策未使用這些 tier 屬正常）", missing)
	res.Findings = []string{fmt.Sprintf("有節點的 tier：%v", present), fmt.Sprintf("無節點的 tier：%v", missing)}
	res.RequiresExtra, res.ExtraReason = true, "缺 tier 節點是否構成問題，取決於 ILM/data stream lifecycle 政策是否指定該 tier；請對照 data_stream_lifecycle indicator 的診斷訊息（若有）確認是否為根因"
	return res
}

// MappingExplosion #11：mapping 欄位數逼近/超過 total_fields.limit。
func MappingExplosion(counts []collector.IndexFieldCount, t rules.Thresholds) diagnostic.Result {
	mappingLimit, mappingWarnFrac := t.Data.MappingLimitDefault, t.Data.MappingWarnFrac
	res := diagnostic.Result{ID: "mapping_explosion", Title: "Mapping 欄位膨脹", Category: "data", Source: "raw_api", Docs: []string{docMapping}}
	warnAt := mappingLimit * mappingWarnFrac / 100
	var crit, warn []string
	maxFields := 0
	for _, c := range counts {
		if c.FieldCount > maxFields {
			maxFields = c.FieldCount
		}
		switch {
		case c.FieldCount >= mappingLimit:
			res.Measurements = append(res.Measurements, gauge("elasticsearch.index.mapping.field.count", float64(c.FieldCount), "count", "index", c.Index, c.Index, ""))
			crit = append(crit, fmt.Sprintf("%s：%d 欄位（已達/超過上限 %d）", c.Index, c.FieldCount, mappingLimit))
		case c.FieldCount >= warnAt:
			res.Measurements = append(res.Measurements, gauge("elasticsearch.index.mapping.field.count", float64(c.FieldCount), "count", "index", c.Index, c.Index, ""))
			warn = append(warn, fmt.Sprintf("%s：%d 欄位（上限 %d 的 %d%%）", c.Index, c.FieldCount, mappingLimit, mappingWarnFrac))
		}
	}
	res.Measurements = append(res.Measurements,
		gauge("elasticsearch.index.mapping.scanned.count", float64(len(counts)), "count", "", "", "", ""),
		gauge("elasticsearch.index.mapping.field.max", float64(maxFields), "count", "", "", "", ""),
	)
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
		res.Measurements = append(res.Measurements,
			counter("elasticsearch.ingest.pipeline.processed", float64(p.Count), "count", "pipeline", p.Pipeline, p.Pipeline, ""),
			counter("elasticsearch.ingest.pipeline.failed", float64(p.Failed), "count", "pipeline", p.Pipeline, p.Pipeline, ""),
		)
		if p.Count > 0 {
			pct := int(100 * p.Failed / p.Count)
			res.Measurements = append(res.Measurements, gauge("elasticsearch.ingest.pipeline.failure_rate", float64(pct), "percent", "pipeline", p.Pipeline, p.Pipeline, ""))
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
	res.Measurements = append(res.Measurements,
		gauge("elasticsearch.index.health.scanned.count", float64(len(indices)), "count", "", "", "", ""),
		gauge("elasticsearch.index.health.red.count", float64(len(hits)), "count", "", "", "", ""),
	)
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
