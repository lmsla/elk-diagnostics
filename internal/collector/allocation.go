package collector

import "encoding/json"

// flatSettingString 從 flat_settings=true 的 _cluster/settings 回應中，依
// persistent > transient > defaults 優先序取出單一設定鍵的字串值；找不到回 defaultVal。
//
// 不能對 flat_settings=true 的回應套用 filter_path=**.a.b.c 這種巢狀路徑寫法：
// flat_settings 把設定壓成單一字串 key（如 "cluster.routing.allocation.enable"），
// key 本身帶的點不是 JSON 巢狀層級，filter_path 卻是照巢狀層級比對，兩者語意衝突，
// 會直接比對不到、永遠回空物件——真機驗證時曾被這個 bug 坑過。
//
// 也不能把整個 persistent/transient/defaults 硬解成 map[string]string：defaults
// 區塊混雜大量非字串型別值（如 network.host 是陣列、xpack.security.user 是 null），
// 一旦其中任一鍵型別不符，整個 json.Unmarshal 直接失敗；若呼叫端把這個錯誤吞掉當
// 「查無設定」處理，會產生比原本 filter_path bug 更隱蔽的假陰性（明明有值卻讀不到，
// 真機驗證也曾實際踩到）。故用 map[string]json.RawMessage 延遲解析，只在確定拿到
// 目標鍵之後才嘗試字串解析，其餘鍵的型別問題不影響。
func flatSettingString(b []byte, key, defaultVal string) string {
	var generic map[string]map[string]json.RawMessage
	if err := json.Unmarshal(b, &generic); err != nil {
		return defaultVal
	}
	for _, layer := range []string{"persistent", "transient", "defaults"} {
		m, ok := generic[layer]
		if !ok {
			continue
		}
		raw, ok := m[key]
		if !ok {
			continue
		}
		var v string
		if err := json.Unmarshal(raw, &v); err == nil {
			return v
		}
	}
	return defaultVal
}

// ClusterAllocationEnable 取 cluster.routing.allocation.enable 的生效值
// （persistent > transient > defaults 優先序；預設 "all"）。#19 用。
func (c *Client) ClusterAllocationEnable() (string, error) {
	b, err := c.get("/_cluster/settings?include_defaults=true&flat_settings=true")
	if err != nil {
		return "", err
	}
	return flatSettingString(b, "cluster.routing.allocation.enable", "all"), nil
}

// IndexAllocationEnable 取單一 index 的 index.routing.allocation.enable 生效值
// （預設 "all"）。#20 用。不加 filter_path、不硬解 map[string]string 的理由見
// flatSettingString 註解。
func (c *Client) IndexAllocationEnable(index string) (string, error) {
	b, err := c.get("/" + index + "/_settings?include_defaults=true&flat_settings=true")
	if err != nil {
		return "", err
	}
	var raw map[string]struct {
		Settings map[string]json.RawMessage `json:"settings"`
		Defaults map[string]json.RawMessage `json:"defaults"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return "all", nil
	}
	for _, idx := range raw {
		for _, m := range []map[string]json.RawMessage{idx.Settings, idx.Defaults} {
			raw, ok := m["index.routing.allocation.enable"]
			if !ok {
				continue
			}
			var v string
			if err := json.Unmarshal(raw, &v); err == nil {
				return v, nil
			}
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
