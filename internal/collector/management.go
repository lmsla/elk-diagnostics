package collector

import "encoding/json"

// WatcherManuallyStopped 取 GET /_watcher/stats 的 manually_stopped。
func (c *Client) WatcherManuallyStopped() (stopped bool, err error) {
	b, err := c.get(EpWatcherStats)
	if err != nil {
		return false, err
	}
	var r struct {
		ManuallyStopped bool `json:"manually_stopped"`
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return false, err
	}
	return r.ManuallyStopped, nil
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
