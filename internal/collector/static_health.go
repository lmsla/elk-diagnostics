package collector

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"time"

	"elk-diagnostics/internal/nodecontext"
)

// PendingClusterTask 是尚未套用的 cluster-state update。
type PendingClusterTask struct {
	Priority           string
	Source             string
	QueueTimeMillis    int64
	CurrentlyExecuting bool
}

func (c *Client) PendingClusterTasks() ([]PendingClusterTask, error) {
	b, err := c.get(EpPendingTasks)
	if err != nil {
		return nil, err
	}
	var raw struct {
		Tasks []struct {
			Priority           string `json:"priority"`
			Source             string `json:"source"`
			QueueTimeMillis    int64  `json:"time_in_queue_millis"`
			CurrentlyExecuting bool   `json:"executing"`
		} `json:"tasks"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	out := make([]PendingClusterTask, 0, len(raw.Tasks))
	for _, task := range raw.Tasks {
		out = append(out, PendingClusterTask{
			Priority: task.Priority, Source: task.Source,
			QueueTimeMillis: task.QueueTimeMillis, CurrentlyExecuting: task.CurrentlyExecuting,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].QueueTimeMillis > out[j].QueueTimeMillis })
	return out, nil
}

// RunningTask 是 Task Management API 的單一執行中工作。Headers/status 刻意不保留，
// 避免 bundle 收進 opaque id、query 細節或版本相依的大型狀態物件。
type RunningTask struct {
	ID           string
	Node         string
	Type         string
	Action       string
	Description  string
	RunningNanos int64
	Cancellable  bool
}

func (c *Client) RunningTasks() ([]RunningTask, error) {
	b, err := c.get(EpRunningTasks)
	if err != nil {
		return nil, err
	}
	type rawTask struct {
		Node         string `json:"node"`
		Type         string `json:"type"`
		Action       string `json:"action"`
		Description  string `json:"description"`
		RunningNanos int64  `json:"running_time_in_nanos"`
		Cancellable  bool   `json:"cancellable"`
	}
	var raw struct {
		Tasks map[string]rawTask `json:"tasks"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	out := make([]RunningTask, 0, len(raw.Tasks))
	for id, task := range raw.Tasks {
		out = append(out, RunningTask{
			ID: id, Node: task.Node, Type: task.Type, Action: task.Action,
			Description: task.Description, RunningNanos: task.RunningNanos, Cancellable: task.Cancellable,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RunningNanos > out[j].RunningNanos })
	return out, nil
}

type ShardSize struct {
	Index      string
	Shard      int
	Primary    bool
	State      string
	Node       string
	StoreBytes int64
	Docs       int64
}

func (c *Client) ShardSizes() ([]ShardSize, error) {
	b, err := c.get(EpCatShardsSizing)
	if err != nil {
		return nil, err
	}
	var raw []map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	out := make([]ShardSize, 0, len(raw))
	for _, row := range raw {
		index := rawString(row["index"])
		if index == "" || isSystemIndex(index) {
			continue
		}
		store, ok := rawInt64(row["store"])
		if !ok || store < 0 {
			continue
		}
		out = append(out, ShardSize{
			Index: index, Shard: int(rawInt64Default(row["shard"])), Primary: rawString(row["prirep"]) == "p",
			State: rawString(row["state"]), Node: rawString(row["node"]), StoreBytes: store,
			Docs: rawInt64Default(row["docs"]),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Index != out[j].Index {
			return out[i].Index < out[j].Index
		}
		if out[i].Shard != out[j].Shard {
			return out[i].Shard < out[j].Shard
		}
		return out[i].Primary && !out[j].Primary
	})
	return out, nil
}

type SLMPolicy struct {
	Name                string
	Repository          string
	ModifiedMillis      int64
	NextExecutionMillis int64
	LastSuccessMillis   int64
	LastSuccessSnapshot string
	LastFailureMillis   int64
	LastFailureSnapshot string
	SnapshotsTaken      int64
	SnapshotsFailed     int64
}

func (c *Client) SLMPolicies() ([]SLMPolicy, error) {
	b, err := c.get(EpSLMPolicies)
	if err != nil {
		return nil, err
	}
	var raw map[string]struct {
		Policy struct {
			Repository string `json:"repository"`
		} `json:"policy"`
		ModifiedMillis      int64 `json:"modified_date_millis"`
		NextExecutionMillis int64 `json:"next_execution_millis"`
		LastSuccess         struct {
			Snapshot string          `json:"snapshot_name"`
			Time     json.RawMessage `json:"time"`
		} `json:"last_success"`
		LastFailure struct {
			Snapshot string          `json:"snapshot_name"`
			Time     json.RawMessage `json:"time"`
		} `json:"last_failure"`
		Stats struct {
			Taken  int64 `json:"snapshots_taken"`
			Failed int64 `json:"snapshots_failed"`
		} `json:"stats"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	out := make([]SLMPolicy, 0, len(raw))
	for name, policy := range raw {
		out = append(out, SLMPolicy{
			Name: name, Repository: policy.Policy.Repository, ModifiedMillis: policy.ModifiedMillis, NextExecutionMillis: policy.NextExecutionMillis,
			LastSuccessMillis: epochMillis(policy.LastSuccess.Time), LastSuccessSnapshot: policy.LastSuccess.Snapshot,
			LastFailureMillis: epochMillis(policy.LastFailure.Time), LastFailureSnapshot: policy.LastFailure.Snapshot,
			SnapshotsTaken: policy.Stats.Taken, SnapshotsFailed: policy.Stats.Failed,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

type NodePlugin struct {
	Name    string
	Version string
}

type NodeRuntime struct {
	ID            string
	Name          string
	Roles         []string
	ESVersion     string
	BuildHash     string
	JVMVersion    string
	VMVersion     string
	HeapInitBytes int64
	HeapMaxBytes  int64
	Plugins       []NodePlugin
}

type NodeRuntimeSnapshot struct {
	Coverage nodecontext.Coverage
	Nodes    []NodeRuntime
}

func (c *Client) NodeRuntimes() (*NodeRuntimeSnapshot, error) {
	b, err := c.get(EpNodesRuntime)
	if err != nil {
		return nil, err
	}
	var raw struct {
		Coverage rawCoverage `json:"_nodes"`
		Nodes    map[string]struct {
			Name      string   `json:"name"`
			Roles     []string `json:"roles"`
			Version   string   `json:"version"`
			BuildHash string   `json:"build_hash"`
			JVM       struct {
				Version   string `json:"version"`
				VMVersion string `json:"vm_version"`
				Mem       struct {
					HeapInit int64 `json:"heap_init_in_bytes"`
					HeapMax  int64 `json:"heap_max_in_bytes"`
				} `json:"mem"`
			} `json:"jvm"`
			Plugins []struct {
				Name    string `json:"name"`
				Version string `json:"version"`
			} `json:"plugins"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	out := &NodeRuntimeSnapshot{Coverage: coverageOf(raw.Coverage, len(raw.Nodes))}
	for id, node := range raw.Nodes {
		runtime := NodeRuntime{ID: id, Name: node.Name, Roles: append([]string(nil), node.Roles...), ESVersion: node.Version, BuildHash: node.BuildHash, JVMVersion: node.JVM.Version, VMVersion: node.JVM.VMVersion, HeapInitBytes: node.JVM.Mem.HeapInit, HeapMaxBytes: node.JVM.Mem.HeapMax}
		for _, plugin := range node.Plugins {
			runtime.Plugins = append(runtime.Plugins, NodePlugin{Name: plugin.Name, Version: plugin.Version})
		}
		sort.Strings(runtime.Roles)
		sort.Slice(runtime.Plugins, func(i, j int) bool {
			if runtime.Plugins[i].Name != runtime.Plugins[j].Name {
				return runtime.Plugins[i].Name < runtime.Plugins[j].Name
			}
			return runtime.Plugins[i].Version < runtime.Plugins[j].Version
		})
		out.Nodes = append(out.Nodes, runtime)
	}
	sort.Slice(out.Nodes, func(i, j int) bool {
		if out.Nodes[i].Name != out.Nodes[j].Name {
			return out.Nodes[i].Name < out.Nodes[j].Name
		}
		return out.Nodes[i].ID < out.Nodes[j].ID
	})
	return out, nil
}

type TLSCertificate struct {
	Path          string
	Alias         string
	Subject       string
	Issuer        string
	Expiry        string
	HasPrivateKey bool
}

func (c *Client) TLSCertificates() ([]TLSCertificate, error) {
	b, err := c.get(EpSSLCertificates)
	if err != nil {
		return nil, err
	}
	var raw []struct {
		Path          string `json:"path"`
		Alias         string `json:"alias"`
		Subject       string `json:"subject_dn"`
		Issuer        string `json:"issuer"`
		Expiry        string `json:"expiry"`
		HasPrivateKey bool   `json:"has_private_key"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	out := make([]TLSCertificate, 0, len(raw))
	for _, cert := range raw {
		out = append(out, TLSCertificate{Path: cert.Path, Alias: cert.Alias, Subject: cert.Subject, Issuer: cert.Issuer, Expiry: cert.Expiry, HasPrivateKey: cert.HasPrivateKey})
	}
	return out, nil
}

type LicenseInfo struct {
	Status       string
	Type         string
	IssuedTo     string
	ExpiryMillis int64
}

func (c *Client) LicenseInfo() (LicenseInfo, error) {
	b, err := c.get(EpLicense)
	if err != nil {
		return LicenseInfo{}, err
	}
	var raw struct {
		License struct {
			Status       string `json:"status"`
			Type         string `json:"type"`
			IssuedTo     string `json:"issued_to"`
			ExpiryMillis int64  `json:"expiry_date_in_millis"`
		} `json:"license"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return LicenseInfo{}, err
	}
	return LicenseInfo{Status: raw.License.Status, Type: raw.License.Type, IssuedTo: raw.License.IssuedTo, ExpiryMillis: raw.License.ExpiryMillis}, nil
}

type IndexReplica struct {
	Index      string
	Replicas   int
	AutoExpand string
}

func (c *Client) IndexReplicas() ([]IndexReplica, error) {
	b, err := c.get(EpAllSettings)
	if err != nil {
		return nil, err
	}
	var raw map[string]struct {
		Settings map[string]json.RawMessage `json:"settings"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	out := make([]IndexReplica, 0, len(raw))
	for index, entry := range raw {
		if isSystemIndex(index) {
			continue
		}
		replicas := 1
		if value, ok := rawInt64(entry.Settings["index.number_of_replicas"]); ok {
			replicas = int(value)
		}
		out = append(out, IndexReplica{Index: index, Replicas: replicas, AutoExpand: rawString(entry.Settings["index.auto_expand_replicas"])})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Index < out[j].Index })
	return out, nil
}

type TopologyNode struct {
	ID         string
	Name       string
	Roles      []string
	Attributes map[string]string
}

type NodeTopologySnapshot struct {
	Coverage nodecontext.Coverage
	Nodes    []TopologyNode
}

func (c *Client) NodeTopology() (*NodeTopologySnapshot, error) {
	b, err := c.get(EpNodesTopology)
	if err != nil {
		return nil, err
	}
	var raw struct {
		Coverage rawCoverage `json:"_nodes"`
		Nodes    map[string]struct {
			Name       string            `json:"name"`
			Roles      []string          `json:"roles"`
			Attributes map[string]string `json:"attributes"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	out := &NodeTopologySnapshot{Coverage: coverageOf(raw.Coverage, len(raw.Nodes))}
	for id, node := range raw.Nodes {
		out.Nodes = append(out.Nodes, TopologyNode{ID: id, Name: node.Name, Roles: append([]string(nil), node.Roles...), Attributes: node.Attributes})
	}
	sort.Slice(out.Nodes, func(i, j int) bool { return out.Nodes[i].Name < out.Nodes[j].Name })
	return out, nil
}

func (c *Client) AllocationAwarenessAttributes() ([]string, error) {
	b, err := c.get(EpClusterSettings)
	if err != nil {
		return nil, err
	}

	var generic map[string]map[string]json.RawMessage
	if err := json.Unmarshal(b, &generic); err != nil {
		return nil, err
	}
	for _, layer := range []string{"persistent", "transient", "defaults"} {
		raw, ok := generic[layer]["cluster.routing.allocation.awareness.attributes"]
		if !ok {
			continue
		}

		var values []string
		if json.Unmarshal(raw, &values) != nil {
			var value string
			if json.Unmarshal(raw, &value) != nil {
				continue
			}
			values = strings.Split(value, ",")
		}

		var out []string
		for _, item := range values {
			if item = strings.TrimSpace(item); item != "" {
				out = append(out, item)
			}
		}
		return out, nil
	}
	return nil, nil
}

func rawString(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return strings.Trim(string(raw), `"`)
}

func rawInt64(raw json.RawMessage) (int64, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return 0, false
	}
	var n int64
	if json.Unmarshal(raw, &n) == nil {
		return n, true
	}
	s := rawString(raw)
	value, err := strconv.ParseInt(s, 10, 64)
	return value, err == nil
}

func rawInt64Default(raw json.RawMessage) int64 {
	n, _ := rawInt64(raw)
	return n
}

func epochMillis(raw json.RawMessage) int64 {
	if n, ok := rawInt64(raw); ok {
		return n
	}
	if parsed, err := time.Parse(time.RFC3339Nano, rawString(raw)); err == nil {
		return parsed.UnixMilli()
	}
	return 0
}
