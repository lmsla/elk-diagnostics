package collector

import (
	"encoding/json"
	"strings"
)

// SlowlogEnabledIndices 回傳有設定 search slow log 門檻（非 -1）的 index 清單。
// 未開啟者不會出現在 flat_settings 回應中（未帶 include_defaults）。不加 filter_path、
// 逐鍵用 json.RawMessage 延遲解析的理由見 allocation.go 的 flatSettingString 註解
// ——同一組 bug 曾讓本函式無論實際設定為何永遠回傳空清單，真機建了真的開啟
// slow log 的 index 才測出來。
func (c *Client) SlowlogEnabledIndices() ([]string, error) {
	b, err := c.get("/_settings?flat_settings=true")
	if err != nil {
		return nil, err
	}
	var raw map[string]struct {
		Settings map[string]json.RawMessage `json:"settings"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	var out []string
	for idx, v := range raw {
		for k, rawVal := range v.Settings {
			if !strings.Contains(k, "search.slowlog.threshold") {
				continue
			}
			var val string
			if err := json.Unmarshal(rawVal, &val); err != nil {
				continue
			}
			if val != "" && val != "-1" {
				out = append(out, idx)
				break
			}
		}
	}
	return out, nil
}
