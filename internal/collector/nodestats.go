package collector

import "encoding/json"

// NodeJVM：JVM old pool 記憶體壓力（官方建議用 old pool used/max，比瞬時 heap% 準）。
type NodeJVM struct {
	Name        string
	UsedBytes   int64
	MaxBytes    int64
	PressurePct int
}

// NodesJVMOldPool 取 GET /_nodes/stats（只要 old pool）。
func (c *Client) NodesJVMOldPool() ([]NodeJVM, error) {
	b, err := c.get("/_nodes/stats?filter_path=nodes.*.name,nodes.*.jvm.mem.pools.old")
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
	b, err := c.get("/_nodes/stats/breaker?filter_path=nodes.*.name,nodes.*.breakers")
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
	b, err := c.get("/_cat/nodes?format=json&h=name,node.role,cpu,load_1m,allocated_processors,heap.percent,disk.used_percent")
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
			DiskPercent:         atoi(m["disk.used_percent"]),
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
	b, err := c.get("/_cat/allocation?format=json&h=node,shards,shards.undesired,disk.percent")
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
			DiskPercent:     atoi(m["disk.percent"]),
		})
	}
	return out, nil
}
