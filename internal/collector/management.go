package collector

import (
	"encoding/json"
	"sort"
)

// WatcherNodeStats 是 _watcher/stats 回應中的單節點摘要。
type WatcherNodeStats struct {
	NodeID         string
	WatcherState   string
	WatchCount     int
	QueueSize      int
	MaxQueueSize   int
	CurrentWatches int
	QueuedWatches  int
}

// WatcherStats 取 GET /_watcher/stats 的服務與節點執行摘要。
// 端點本身一定回傳基本 metrics；部分版本或權限只會提供 manually_stopped，
// 因此欄位不足時保留零值，交由 analyzer 以「無法判定」或相容的基本判定處理。
type WatcherStats struct {
	ManuallyStopped bool
	NodesTotal      int
	NodesSuccessful int
	NodesFailed     int
	Nodes           []WatcherNodeStats
}

// WatcherStats 取 GET /_watcher/stats。
func (c *Client) WatcherStats() (WatcherStats, error) {
	b, err := c.get(EpWatcherStats)
	if err != nil {
		return WatcherStats{}, err
	}
	var raw struct {
		Nodes struct {
			Total      int `json:"total"`
			Successful int `json:"successful"`
			Failed     int `json:"failed"`
		} `json:"_nodes"`
		ManuallyStopped bool `json:"manually_stopped"`
		Stats           []struct {
			NodeID              string `json:"node_id"`
			WatcherState        string `json:"watcher_state"`
			WatchCount          int    `json:"watch_count"`
			ExecutionThreadPool struct {
				QueueSize int `json:"queue_size"`
				MaxSize   int `json:"max_size"`
			} `json:"execution_thread_pool"`
			CurrentWatches []json.RawMessage `json:"current_watches"`
			QueuedWatches  []json.RawMessage `json:"queued_watches"`
		} `json:"stats"`
		WatcherState        string `json:"watcher_state"`
		WatchCount          int    `json:"watch_count"`
		ExecutionThreadPool struct {
			QueueSize int `json:"queue_size"`
			MaxSize   int `json:"max_size"`
		} `json:"execution_thread_pool"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return WatcherStats{}, err
	}
	out := WatcherStats{
		ManuallyStopped: raw.ManuallyStopped,
		NodesTotal:      raw.Nodes.Total,
		NodesSuccessful: raw.Nodes.Successful,
		NodesFailed:     raw.Nodes.Failed,
	}
	for _, node := range raw.Stats {
		out.Nodes = append(out.Nodes, WatcherNodeStats{
			NodeID:         node.NodeID,
			WatcherState:   node.WatcherState,
			WatchCount:     node.WatchCount,
			QueueSize:      node.ExecutionThreadPool.QueueSize,
			MaxQueueSize:   node.ExecutionThreadPool.MaxSize,
			CurrentWatches: len(node.CurrentWatches),
			QueuedWatches:  len(node.QueuedWatches),
		})
	}
	if len(out.Nodes) == 0 && (raw.WatcherState != "" || raw.WatchCount != 0 || raw.ExecutionThreadPool.QueueSize != 0) {
		out.Nodes = append(out.Nodes, WatcherNodeStats{
			WatcherState: raw.WatcherState,
			WatchCount:   raw.WatchCount,
			QueueSize:    raw.ExecutionThreadPool.QueueSize,
			MaxQueueSize: raw.ExecutionThreadPool.MaxSize,
		})
	}
	return out, nil
}

// WatcherManuallyStopped 保留既有呼叫介面，取 GET /_watcher/stats 的 manually_stopped。
func (c *Client) WatcherManuallyStopped() (stopped bool, err error) {
	stats, err := c.WatcherStats()
	return stats.ManuallyStopped, err
}

// Transform：單一 transform 的狀態。
type Transform struct {
	ID    string
	State string
}

// Transforms 取 GET /_transform/_stats。
func (c *Client) Transforms() ([]Transform, error) {
	b, err := c.get(EpTransformStats)
	if err != nil {
		return nil, err
	}
	var r struct {
		Transforms []struct {
			ID    string `json:"id"`
			State string `json:"state"`
		} `json:"transforms"`
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, err
	}
	out := make([]Transform, 0, len(r.Transforms))
	for _, t := range r.Transforms {
		out = append(out, Transform{ID: t.ID, State: t.State})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// RemoteCluster：遠端叢集連線狀態。
type RemoteCluster struct {
	Name      string
	Connected bool
}

// RemoteInfo 取 GET /_remote/info（空物件表未設定 remote cluster）。
func (c *Client) RemoteInfo() ([]RemoteCluster, error) {
	b, err := c.get(EpRemoteInfo)
	if err != nil {
		return nil, err
	}
	var raw map[string]struct {
		Connected bool `json:"connected"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	out := make([]RemoteCluster, 0, len(raw))
	for name, r := range raw {
		out = append(out, RemoteCluster{Name: name, Connected: r.Connected})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Deprecation：升版 deprecation 警告。
type Deprecation struct {
	Level   string
	Message string
}

// Deprecations 取 GET /_migration/deprecations（彙整四個層級的陣列）。
func (c *Client) Deprecations() ([]Deprecation, error) {
	b, err := c.get(EpMigrationDeprecations)
	if err != nil {
		return nil, err
	}
	type rawDep struct {
		Level   string `json:"level"`
		Message string `json:"message"`
	}
	var rr struct {
		ClusterSettings []rawDep            `json:"cluster_settings"`
		NodeSettings    []rawDep            `json:"node_settings"`
		IndexSettings   map[string][]rawDep `json:"index_settings"`
		MLSettings      []rawDep            `json:"ml_settings"`
	}
	if err := json.Unmarshal(b, &rr); err != nil {
		return nil, err
	}
	var out []Deprecation
	add := func(ds []rawDep) {
		for _, d := range ds {
			out = append(out, Deprecation{Level: d.Level, Message: d.Message})
		}
	}
	add(rr.ClusterSettings)
	add(rr.NodeSettings)
	add(rr.MLSettings)
	for _, ds := range rr.IndexSettings {
		add(ds)
	}
	return out, nil
}

// MonitoringCollectionEnabled 取 stack monitoring 收集設定（"" 表未明確設定）。
// 不加 filter_path、不硬解 map[string]string 的理由見 allocation.go 的
// flatSettingString 註解（同一組 bug：filter_path 對 flat_settings 比對不到，
// defaults 區塊混雜非字串型別會讓整段解析失敗又被吞掉）。
func (c *Client) MonitoringCollectionEnabled() (string, error) {
	b, err := c.get(EpClusterSettings)
	if err != nil {
		return "", err
	}
	return flatSettingString(b, "xpack.monitoring.collection.enabled", ""), nil
}
