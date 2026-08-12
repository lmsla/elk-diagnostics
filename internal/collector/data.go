package collector

import (
	"encoding/json"
	"strings"
)

// IndexFieldCount：單一 index 的 mapping 欄位數（leaf "type" 計數，近似值）。
type IndexFieldCount struct {
	Index      string
	FieldCount int
}

// MappingFieldCounts 取 GET /_mapping 並逐 index 計算欄位數，排除 ES/Kibana 內部系統
// index（如 .kibana*、.internal.alerts-*）。這些本來就常態性有上千個欄位（尤其 Kibana
// alerting 框架自建的 .internal.alerts-*），與客戶資料的 mapping 膨脹無關；真機驗證時
// 在全新、零資料的 8.14.3/9.0.0 叢集上就已重現這個誤報（見 docs/內部/實作進度.md）。
//
// **不能只用「.」開頭判斷**：data stream 的 backing index 也一律是「.」開頭（如
// .ds-logs-app-2026.07.15-000001），而且客戶用 logs-*-*/metrics-*-* 這類標準範本、
// Elastic Agent/Fleet 送資料時幾乎都是走 data stream——若無差別排除所有「.」開頭，
// 會把真正的客戶資料也一起濾掉，是比原本誤報更嚴重的漏判。ES 保證 data stream 的
// backing index 一律是 ".ds-" 開頭（真機建測試 data stream 驗證過），故只排除
// 「. 開頭但非 .ds- 開頭」者，兩者皆已用真機資料驗證。
func (c *Client) MappingFieldCounts() ([]IndexFieldCount, error) {
	b, err := c.get(EpMapping)
	if err != nil {
		return nil, err
	}
	var raw map[string]struct {
		Mappings map[string]interface{} `json:"mappings"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	out := make([]IndexFieldCount, 0, len(raw))
	for idx, m := range raw {
		if isSystemIndex(idx) {
			continue
		}
		out = append(out, IndexFieldCount{Index: idx, FieldCount: countTypes(m.Mappings)})
	}
	return out, nil
}

// isSystemIndex 判斷是否為 ES/Kibana 內部系統 index，而非客戶資料。見 MappingFieldCounts
// 註解：data stream backing index（.ds- 開頭）一律視為客戶資料，不排除。
func isSystemIndex(name string) bool {
	return strings.HasPrefix(name, ".") && !strings.HasPrefix(name, ".ds-")
}

// countTypes 遞迴計算 mapping 樹中 "type": "<string>" 的數量（近似欄位數）。
func countTypes(v interface{}) int {
	switch t := v.(type) {
	case map[string]interface{}:
		n := 0
		for k, val := range t {
			if k == "type" {
				if _, ok := val.(string); ok {
					n++
					continue
				}
			}
			n += countTypes(val)
		}
		return n
	case []interface{}:
		n := 0
		for _, e := range t {
			n += countTypes(e)
		}
		return n
	}
	return 0
}

// IngestPipeline：跨節點彙總的 pipeline 統計（count/failed 為自啟動累積）。
type IngestPipeline struct {
	Pipeline string
	Count    int64
	Failed   int64
}

// IngestPipelineStats 取 GET /_nodes/stats/ingest 並依 pipeline 彙總。
func (c *Client) IngestPipelineStats() ([]IngestPipeline, error) {
	b, err := c.get(EpNodesIngest)
	if err != nil {
		return nil, err
	}
	var r struct {
		Nodes map[string]struct {
			Ingest struct {
				Pipelines map[string]struct {
					Count  int64 `json:"count"`
					Failed int64 `json:"failed"`
				} `json:"pipelines"`
			} `json:"ingest"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, err
	}
	agg := map[string]*IngestPipeline{}
	for _, n := range r.Nodes {
		for name, p := range n.Ingest.Pipelines {
			e := agg[name]
			if e == nil {
				e = &IngestPipeline{Pipeline: name}
				agg[name] = e
			}
			e.Count += p.Count
			e.Failed += p.Failed
		}
	}
	out := make([]IngestPipeline, 0, len(agg))
	for _, e := range agg {
		out = append(out, *e)
	}
	return out, nil
}

// IndexHealth：cat indices 的健康與狀態。
type IndexHealth struct {
	Index  string
	Health string
	Status string
}

// CatIndicesHealth 取 GET /_cat/indices，排除 ES/Kibana 內部系統 index（理由同
// MappingFieldCounts，含 data stream backing index 不應被排除的說明）：系統 index
// 的健康狀態波動多半是 ES/Kibana 內部機制所致（如升版過渡期），與客戶資料毀損無關，
// 不該被 #32 當成資料毀損徵兆呈報；但客戶用 data stream 送的資料仍須檢查。
func (c *Client) CatIndicesHealth() ([]IndexHealth, error) {
	b, err := c.get(EpCatIndices)
	if err != nil {
		return nil, err
	}
	var raw []map[string]string
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	out := make([]IndexHealth, 0, len(raw))
	for _, m := range raw {
		if isSystemIndex(m["index"]) {
			continue
		}
		out = append(out, IndexHealth{Index: m["index"], Health: m["health"], Status: m["status"]})
	}
	return out, nil
}
