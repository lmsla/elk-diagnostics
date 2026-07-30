package collector

import (
	"encoding/json"
	"strconv"
)

// NodeJVM：JVM old pool 記憶體壓力（官方建議用 old pool used/max，比瞬時 heap% 準）。
type NodeJVM struct {
	Name        string
	UsedBytes   int64
	MaxBytes    int64
	PressurePct int
}

// NodesJVMOldPool 取 GET /_nodes/stats（只要 old pool）。
func (c *Client) NodesJVMOldPool() ([]NodeJVM, error) {
	b, err := c.nodeResourceStats()
	if err != nil {
		return nil, err
	}
	var r struct {
		Nodes map[string]struct {
			Name string `json:"name"`
			JVM  struct {
				Mem struct {
					Pools struct {
						Old struct {
							Used int64 `json:"used_in_bytes"`
							Max  int64 `json:"max_in_bytes"`
						} `json:"old"`
					} `json:"pools"`
				} `json:"mem"`
			} `json:"jvm"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, err
	}
	var out []NodeJVM
	for _, n := range r.Nodes {
		old := n.JVM.Mem.Pools.Old
		p := 0
		if old.Max > 0 {
			p = int(100 * old.Used / old.Max)
		}
		out = append(out, NodeJVM{Name: n.Name, UsedBytes: old.Used, MaxBytes: old.Max, PressurePct: p})
	}
	return out, nil
}

// NodeBreaker：單一節點單一 breaker 的 tripped 次數（自啟動起累積）。
type NodeBreaker struct {
	Node    string
	Breaker string
	Tripped int64
}

// NodesBreakers 取 GET /_nodes/stats/breaker。
func (c *Client) NodesBreakers() ([]NodeBreaker, error) {
	b, err := c.get(EpNodesBreakers)
	if err != nil {
		return nil, err
	}
	var r struct {
		Nodes map[string]struct {
			Name     string `json:"name"`
			Breakers map[string]struct {
				Tripped int64 `json:"tripped"`
			} `json:"breakers"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, err
	}
	var out []NodeBreaker
	for _, n := range r.Nodes {
		for name, br := range n.Breakers {
			out = append(out, NodeBreaker{Node: n.Name, Breaker: name, Tripped: br.Tripped})
		}
	}
	return out, nil
}

// NodeCPU：cat nodes 的資源利用（cpu/heap/disk 為瞬時值）。
type NodeCPU struct {
	Name                string
	Role                string
	CPU                 int
	HeapPercent         int
	DiskPercent         int
	Load1m              string
	AllocatedProcessors int
}

// CatNodesCPU 取 GET /_cat/nodes（含 heap/disk，供 HighCPU 與 HotSpotting 共用）。
func (c *Client) CatNodesCPU() ([]NodeCPU, error) {
	b, err := c.get(EpCatNodes)
	if err != nil {
		return nil, err
	}
	var raw []map[string]string
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	out := make([]NodeCPU, 0, len(raw))
	for _, m := range raw {
		out = append(out, NodeCPU{
			Name:                m["name"],
			Role:                m["node.role"],
			CPU:                 atoi(m["cpu"]),
			HeapPercent:         atoi(m["heap.percent"]),
			DiskPercent:         percentInt(m["disk.used_percent"]),
			Load1m:              m["load_1m"],
			AllocatedProcessors: atoi(m["allocated_processors"]),
		})
	}
	return out, nil
}

// AllocationRow：cat allocation 每節點的 shard 與磁碟分布（shards.undesired 為 8.6+）。
type AllocationRow struct {
	Node            string
	Shards          int
	ShardsUndesired int
	DiskPercent     int
}

// CatAllocation 取 GET /_cat/allocation。
func (c *Client) CatAllocation() ([]AllocationRow, error) {
	b, err := c.get(EpCatAllocation)
	if err != nil {
		return nil, err
	}
	var raw []map[string]string
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	out := make([]AllocationRow, 0, len(raw))
	for _, m := range raw {
		node := m["node"]
		if node == "" || node == "UNASSIGNED" {
			continue
		}
		out = append(out, AllocationRow{
			Node:            node,
			Shards:          atoi(m["shards"]),
			ShardsUndesired: atoi(m["shards.undesired"]),
			DiskPercent:     percentInt(m["disk.percent"]),
		})
	}
	return out, nil
}

// CAT APIs 的 disk percent 在不同版本／檔案系統可能回傳 "89" 或 "89.90"。
// 直接 strconv.Atoi 會把小數格式靜默吃成 0，導致 hot spotting 假綠燈。
func percentInt(s string) int {
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || v < 0 {
		return 0
	}
	return int(v)
}
