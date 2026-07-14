package collector

import "encoding/json"

// ClusterNodeCounts 取 GET _cluster/health 的節點數（#30 用，佐證叢集規模）。
func (c *Client) ClusterNodeCounts() (numberOfNodes int, err error) {
	b, err := c.get("/_cluster/health")
	if err != nil {
		return 0, err
	}
	var r struct {
		NumberOfNodes int `json:"number_of_nodes"`
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return 0, err
	}
	return r.NumberOfNodes, nil
}

// dataTiers 是 data_stream_lifecycle / ILM 常用的標準 tier role 名稱。
var dataTiers = []string{"data_content", "data_hot", "data_warm", "data_cold", "data_frozen"}

// DataTierNodeCounts 取各 data tier 的節點數（GET _nodes）。#24 用：確認是否缺少
// 對應 tier 的節點，是 preferred tier 缺節點最直接的結構性根因。
func (c *Client) DataTierNodeCounts() (map[string]int, error) {
	b, err := c.get("/_nodes?filter_path=nodes.*.roles")
	if err != nil {
		return nil, err
	}
	var r struct {
		Nodes map[string]struct {
			Roles []string `json:"roles"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, err
	}
	counts := make(map[string]int, len(dataTiers))
	for _, t := range dataTiers {
		counts[t] = 0
	}
	for _, n := range r.Nodes {
		for _, role := range n.Roles {
			if _, ok := counts[role]; ok {
				counts[role]++
			}
		}
	}
	return counts, nil
}

// MasterEligibleCount 取具備 master role 的節點數（GET _nodes）。#30 用：
// master-eligible 節點數過少（尤其偶數或僅 1）是叢集不穩定最常見的結構性根因。
func (c *Client) MasterEligibleCount() (masterEligible int, err error) {
	b, err := c.get("/_nodes?filter_path=nodes.*.roles")
	if err != nil {
		return 0, err
	}
	var r struct {
		Nodes map[string]struct {
			Roles []string `json:"roles"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return 0, err
	}
	for _, n := range r.Nodes {
		for _, role := range n.Roles {
			if role == "master" {
				masterEligible++
				break
			}
		}
	}
	return masterEligible, nil
}
