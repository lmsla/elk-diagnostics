package collector

import (
	"encoding/json"
	"strings"
)

// SlowlogEnabledIndices 回傳有設定 search slow log 門檻（非 -1）的 index 清單。
// 未開啟者不會出現在 flat_settings 回應中（未帶 include_defaults）。
func (c *Client) SlowlogEnabledIndices() ([]string, error) {
	b, err := c.get("/_settings?flat_settings=true&filter_path=**.index.search.slowlog.threshold*")
	if err != nil {
		return nil, err
	}
	var raw map[string]struct {
		Settings map[string]string `json:"settings"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	var out []string
	for idx, v := range raw {
		for k, val := range v.Settings {
			if strings.Contains(k, "search.slowlog.threshold") && val != "" && val != "-1" {
				out = append(out, idx)
				break
			}
		}
	}
	return out, nil
}
