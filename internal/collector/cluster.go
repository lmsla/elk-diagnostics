package collector

import (
	"encoding/json"
	"strings"
)

// ClusterHealth 是 GET /_cluster/health 的結構化快照。
// pointer 欄位保留「回應缺少欄位」與「實際值為 0」的差異。
type ClusterHealth struct {
	Status                  string
	NumberOfNodes           *int
	NumberOfDataNodes       *int
	ActivePrimaryShards     *int
	ActiveShards            *int
	RelocatingShards        *int
	InitializingShards      *int
	UnassignedShards        *int
	UnassignedPrimaryShards *int
	ActiveShardsPercent     *float64
}

// ParseClusterHealth 解析 GET /_cluster/health 回應，供連線與 bundle 模式共用。
func ParseClusterHealth(b []byte) (ClusterHealth, error) {
	var raw struct {
		Status                  string   `json:"status"`
		NumberOfNodes           *int     `json:"number_of_nodes"`
		NumberOfDataNodes       *int     `json:"number_of_data_nodes"`
		ActivePrimaryShards     *int     `json:"active_primary_shards"`
		ActiveShards            *int     `json:"active_shards"`
		RelocatingShards        *int     `json:"relocating_shards"`
		InitializingShards      *int     `json:"initializing_shards"`
		UnassignedShards        *int     `json:"unassigned_shards"`
		UnassignedPrimaryShards *int     `json:"unassigned_primary_shards"`
		ActiveShardsPercent     *float64 `json:"active_shards_percent_as_number"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return ClusterHealth{}, err
	}
	return ClusterHealth{
		Status:        strings.ToLower(strings.TrimSpace(raw.Status)),
		NumberOfNodes: nonNegativeInt(raw.NumberOfNodes), NumberOfDataNodes: nonNegativeInt(raw.NumberOfDataNodes),
		ActivePrimaryShards: nonNegativeInt(raw.ActivePrimaryShards), ActiveShards: nonNegativeInt(raw.ActiveShards),
		RelocatingShards: nonNegativeInt(raw.RelocatingShards), InitializingShards: nonNegativeInt(raw.InitializingShards),
		UnassignedShards: nonNegativeInt(raw.UnassignedShards), UnassignedPrimaryShards: nonNegativeInt(raw.UnassignedPrimaryShards),
		ActiveShardsPercent: nonNegativeFloat(raw.ActiveShardsPercent),
	}, nil
}

// ClusterHealth 取並解析 GET /_cluster/health。
func (c *Client) ClusterHealth() (ClusterHealth, error) {
	b, err := c.get(EpClusterHealth)
	if err != nil {
		return ClusterHealth{}, err
	}
	return ParseClusterHealth(b)
}

// ClusterNodeCounts 取 GET _cluster/health 的節點數（#30 用，佐證叢集規模）。
func (c *Client) ClusterNodeCounts() (numberOfNodes int, err error) {
	health, err := c.ClusterHealth()
	if err != nil {
		return 0, err
	}
	if health.NumberOfNodes == nil {
		return 0, nil
	}
	return *health.NumberOfNodes, nil
}

// dataTiers 是 data_stream_lifecycle / ILM 常用的標準 tier role 名稱。
var dataTiers = []string{"data_content", "data_hot", "data_warm", "data_cold", "data_frozen"}

// DataTierNodeCounts 取各 data tier 的節點數（GET _nodes）。#24 用：確認是否缺少
// 對應 tier 的節點，是 preferred tier 缺節點最直接的結構性根因。
func (c *Client) DataTierNodeCounts() (map[string]int, error) {
	b, err := c.get(EpNodesRoles)
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
	b, err := c.get(EpNodesRoles)
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
