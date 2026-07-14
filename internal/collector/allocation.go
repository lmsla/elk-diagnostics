package collector

import "encoding/json"

// ClusterAllocationEnable 取 cluster.routing.allocation.enable 的生效值
// （persistent > transient > defaults 優先序；預設 "all"）。#19 用。
func (c *Client) ClusterAllocationEnable() (string, error) {
	b, err := c.get("/_cluster/settings?include_defaults=true&flat_settings=true&filter_path=**.cluster.routing.allocation.enable")
	if err != nil {
		return "", err
	}
	var generic map[string]map[string]string
	if err := json.Unmarshal(b, &generic); err != nil {
		return "all", nil
	}
	for _, layer := range []string{"persistent", "transient", "defaults"} {
		if m, ok := generic[layer]; ok {
			if v, ok := m["cluster.routing.allocation.enable"]; ok {
				return v, nil
			}
		}
	}
	return "all", nil
}

// IndexAllocationEnable 取單一 index 的 index.routing.allocation.enable 生效值
// （預設 "all"）。#20 用。
func (c *Client) IndexAllocationEnable(index string) (string, error) {
	b, err := c.get("/" + index + "/_settings?include_defaults=true&flat_settings=true&filter_path=**.index.routing.allocation.enable")
	if err != nil {
		return "", err
	}
	var raw map[string]struct {
		Settings map[string]string `json:"settings"`
		Defaults map[string]string `json:"defaults"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return "all", nil
	}
	for _, idx := range raw {
		if v, ok := idx.Settings["index.routing.allocation.enable"]; ok {
			return v, nil
		}
		if v, ok := idx.Defaults["index.routing.allocation.enable"]; ok {
			return v, nil
		}
	}
	return "all", nil
}

// AllocationDecider：單一 decider 的判定與說明（decision=NO/THROTTLE 才有診斷價值）。
type AllocationDecider struct {
	Decider     string `json:"decider"`
	Decision    string `json:"decision"`
	Explanation string `json:"explanation"`
}

// AllocationExplanation 對映 GET _cluster/allocation/explain 的精簡結果。
type AllocationExplanation struct {
	Index    string
	Shard    int
	Primary  bool
	Deciders []AllocationDecider
}

// AllocationExplain 取 GET _cluster/allocation/explain（不帶 body）。ES 在無 body
// 時會自動挑一個未分配 shard 說明——本工具是唯讀診斷引導，不逐 shard 窮舉（spec
// 原定上限 20 逐一查），取一個代表性範例已足以判斷 decider 類型；若叢集無未分配
// shard 可解釋，ES 回 400，視為「無可解釋對象」而非錯誤。
func (c *Client) AllocationExplain() (*AllocationExplanation, bool, error) {
	b, err := c.get("/_cluster/allocation/explain")
	if err != nil {
		// ES 對「無未分配 shard」回 400；get() 仍會把 body 一併回傳，直接檢查內容。
		if len(b) > 0 && isNoUnassignedShardError(b) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return parseAllocationExplain(b)
}

func isNoUnassignedShardError(b []byte) bool {
	var probe struct {
		Error struct {
			Reason string `json:"reason"`
		} `json:"error"`
	}
	return json.Unmarshal(b, &probe) == nil && probe.Error.Reason != ""
}

// parseAllocationExplain 是 AllocationExplain 的純解析邏輯，脫離 HTTP 層方便直接用
// fixture 測試。
func parseAllocationExplain(b []byte) (*AllocationExplanation, bool, error) {
	var top struct {
		Index                   string `json:"index"`
		Shard                   int    `json:"shard"`
		Primary                 bool   `json:"primary"`
		NodeAllocationDecisions []struct {
			Deciders []AllocationDecider `json:"deciders"`
		} `json:"node_allocation_decisions"`
	}
	if err := json.Unmarshal(b, &top); err != nil {
		return nil, false, err
	}
	out := &AllocationExplanation{Index: top.Index, Shard: top.Shard, Primary: top.Primary}
	for _, nd := range top.NodeAllocationDecisions {
		for _, d := range nd.Deciders {
			if d.Decision != "YES" {
				out.Deciders = append(out.Deciders, d)
			}
		}
	}
	return out, true, nil
}
