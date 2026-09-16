// Package nodecontext 定義 collector、analyzer 與 reporter 共用的節點環境快照。
// 這是內部領域模型：collector 只負責填值，analyzer 下判斷，reporter 只呈現。
package nodecontext

import (
	"sort"
	"strings"
)

// Coverage 是單一 Nodes API 的回應完整性。Available=false 表示回應沒有可驗證的
// _nodes 統計；此時其餘數值不得被解讀為 0 個失敗。
type Coverage struct {
	Available  bool `json:"available"`
	Total      int  `json:"total"`
	Successful int  `json:"successful"`
	Failed     int  `json:"failed"`
	Returned   int  `json:"returned"`
}

// Complete 只在 API 明確回報所有節點成功，且實際 nodes 數相符時為 true。
func (c Coverage) Complete() bool {
	return c.Available && c.Total > 0 && c.Failed == 0 &&
		c.Successful == c.Total && c.Returned == c.Successful
}

type Snapshot struct {
	StatsCoverage Coverage `json:"stats_coverage"`
	InfoCoverage  Coverage `json:"info_coverage"`
	Nodes         []Node   `json:"nodes"`
	MissingNodes  []string `json:"missing_nodes,omitempty"`
	Issues        []string `json:"issues,omitempty"`
}

// ExpectedNode 是人工維護的 ES 節點盤點項目。IP 是主要識別值；Name 只作為
// 顯示與舊版純 node.name 清單的相容欄位。
type ExpectedNode struct {
	Name string
	IP   string
}

// ParseExpectedNode 解析 "node.name|IP"；沒有 | 時保留舊版純 node.name 格式。
func ParseExpectedNode(value string) ExpectedNode {
	parts := strings.SplitN(strings.TrimSpace(value), "|", 2)
	node := ExpectedNode{Name: strings.TrimSpace(parts[0])}
	if len(parts) == 2 {
		node.IP = NormalizeIP(parts[1])
	}
	return node
}

func ExpectedNodeIdentity(value string) string {
	node := ParseExpectedNode(value)
	if node.IP != "" {
		return node.IP
	}
	return node.Name
}

func ExpectedNodeLabel(value string) string {
	node := ParseExpectedNode(value)
	switch {
	case node.Name != "" && node.IP != "":
		return node.Name + " (" + node.IP + ")"
	case node.Name != "":
		return node.Name
	default:
		return node.IP
	}
}

// NormalizeIP 去掉 Nodes API 常見的 transport port 與 IPv6 括號。
// 遮蔽後的 xx.xx.x.x:port 也能在 bundle 模式穩定比對。
func NormalizeIP(value string) string {
	s := strings.TrimSpace(value)
	if strings.HasPrefix(s, "[") {
		if end := strings.IndexByte(s, ']'); end > 0 {
			s = s[1:end]
		}
	} else if strings.Count(s, ":") == 1 {
		if colon := strings.LastIndexByte(s, ':'); colon > 0 {
			port := s[colon+1:]
			allDigits := port != ""
			for _, r := range port {
				if r < '0' || r > '9' {
					allDigits = false
					break
				}
			}
			if allDigits {
				s = s[:colon]
			}
		}
	}
	return strings.TrimSpace(strings.Trim(s, "[]"))
}

// NodeIdentity 使用 IP 作為主要識別值；舊版或缺少 IP 時退回 node ID/name。
func NodeIdentity(node Node) string {
	if ip := NormalizeIP(node.IP); ip != "" {
		return ip
	}
	if node.ID != "" {
		return node.ID
	}
	return node.Name
}

// MissingExpectedNames compares the expected inventory with Nodes Stats. Each
// entry is compared by IP when supplied, otherwise by node.name for backwards
// compatibility. Callers must check StatsCoverage.Complete before using the
// result; an incomplete response cannot prove a node is absent.
func MissingExpectedNames(expected []string, snapshot *Snapshot) []string {
	if len(expected) == 0 || snapshot == nil {
		return nil
	}
	observed := make(map[string]bool, len(snapshot.Nodes)*2)
	for _, node := range snapshot.Nodes {
		if identity := NodeIdentity(node); identity != "" {
			observed[identity] = true
		}
		if node.Name != "" {
			observed[node.Name] = true
		}
		if ip := NormalizeIP(node.IP); ip != "" {
			observed[ip] = true
		}
	}
	seen := make(map[string]bool, len(expected))
	missing := make([]string, 0, len(expected))
	for _, raw := range expected {
		entry := ParseExpectedNode(raw)
		identity := ExpectedNodeIdentity(raw)
		if identity == "" || seen[identity] {
			continue
		}
		seen[identity] = true
		if !observed[identity] {
			missing = append(missing, ExpectedNodeLabel(raw))
		} else if entry.IP == "" && entry.Name != "" && !observed[entry.Name] {
			missing = append(missing, ExpectedNodeLabel(raw))
		}
	}
	sort.Strings(missing)
	return missing
}

type Node struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	IP             string   `json:"ip,omitempty"`
	Roles          []string `json:"roles,omitempty"`
	StatsAvailable bool     `json:"stats_available"`
	InfoAvailable  bool     `json:"info_available"`
	// DiskUsedPercent 來自同次 _cat/nodes 快照；缺少時保留 nil，不把 0 當成已驗證。
	DiskUsedPercent *int       `json:"disk_used_percent,omitempty"`
	OS              OS         `json:"os"`
	Process         Process    `json:"process"`
	Filesystem      Filesystem `json:"filesystem"`
	JVM             JVM        `json:"jvm"`
}

type OS struct {
	Name                string   `json:"name,omitempty"`
	PrettyName          string   `json:"pretty_name,omitempty"`
	Version             string   `json:"version,omitempty"`
	Architecture        string   `json:"architecture,omitempty"`
	AvailableProcessors *int     `json:"available_processors,omitempty"`
	AllocatedProcessors *int     `json:"allocated_processors,omitempty"`
	CPUPercent          *int     `json:"cpu_percent,omitempty"`
	Load1m              *float64 `json:"load_1m,omitempty"`
	Load5m              *float64 `json:"load_5m,omitempty"`
	Load15m             *float64 `json:"load_15m,omitempty"`
	Memory              Memory   `json:"memory"`
	Swap                Memory   `json:"swap"`
	Cgroup              Cgroup   `json:"cgroup"`
}

type Memory struct {
	TotalBytes *int64 `json:"total_bytes,omitempty"`
	UsedBytes  *int64 `json:"used_bytes,omitempty"`
	FreeBytes  *int64 `json:"free_bytes,omitempty"`
	UsedPct    *int   `json:"used_pct,omitempty"`
	FreePct    *int   `json:"free_pct,omitempty"`
}

type Cgroup struct {
	CPU    CgroupCPU    `json:"cpu"`
	Memory CgroupMemory `json:"memory"`
}

type CgroupCPU struct {
	UsageNanos         *int64 `json:"usage_nanos,omitempty"`
	CFSPeriodMicros    *int64 `json:"cfs_period_micros,omitempty"`
	CFSQuotaMicros     *int64 `json:"cfs_quota_micros,omitempty"`
	ElapsedPeriods     *int64 `json:"elapsed_periods,omitempty"`
	TimesThrottled     *int64 `json:"times_throttled,omitempty"`
	TimeThrottledNanos *int64 `json:"time_throttled_nanos,omitempty"`
}

type CgroupMemory struct {
	UsageBytes     *uint64 `json:"usage_bytes,omitempty"`
	LimitBytes     *uint64 `json:"limit_bytes,omitempty"`
	LimitUnlimited *bool   `json:"limit_unlimited,omitempty"`
}

type Process struct {
	PID                     *int64 `json:"pid,omitempty"`
	MemoryLocked            *bool  `json:"memory_locked,omitempty"`
	CPUPercent              *int   `json:"cpu_percent,omitempty"`
	CPUTotalMillis          *int64 `json:"cpu_total_millis,omitempty"`
	TotalVirtualMemoryBytes *int64 `json:"total_virtual_memory_bytes,omitempty"`
	OpenFileDescriptors     *int64 `json:"open_file_descriptors,omitempty"`
	MaxFileDescriptors      *int64 `json:"max_file_descriptors,omitempty"`
}

type Filesystem struct {
	TotalBytes     *int64     `json:"total_bytes,omitempty"`
	FreeBytes      *int64     `json:"free_bytes,omitempty"`
	AvailableBytes *int64     `json:"available_bytes,omitempty"`
	DataPaths      []DataPath `json:"data_paths,omitempty"`
	Devices        []IODevice `json:"io_devices,omitempty"`
}

type DataPath struct {
	Path           string `json:"path"`
	Mount          string `json:"mount,omitempty"`
	Type           string `json:"type,omitempty"`
	TotalBytes     *int64 `json:"total_bytes,omitempty"`
	FreeBytes      *int64 `json:"free_bytes,omitempty"`
	AvailableBytes *int64 `json:"available_bytes,omitempty"`
}

// IODevice 的數值是自節點／核心開始計數的累積 counter，不是 latency 或 rate。
type IODevice struct {
	Name            string `json:"name"`
	Operations      *int64 `json:"operations,omitempty"`
	ReadOperations  *int64 `json:"read_operations,omitempty"`
	WriteOperations *int64 `json:"write_operations,omitempty"`
	ReadKilobytes   *int64 `json:"read_kilobytes,omitempty"`
	WriteKilobytes  *int64 `json:"write_kilobytes,omitempty"`
	IOTimeMillis    *int64 `json:"io_time_millis,omitempty"`
}

type JVM struct {
	UptimeMillis  *int64        `json:"uptime_millis,omitempty"`
	HeapUsedBytes *int64        `json:"heap_used_bytes,omitempty"`
	HeapMaxBytes  *int64        `json:"heap_max_bytes,omitempty"`
	HeapUsedPct   *int          `json:"heap_used_pct,omitempty"`
	OldUsedBytes  *int64        `json:"old_used_bytes,omitempty"`
	OldMaxBytes   *int64        `json:"old_max_bytes,omitempty"`
	GCCollectors  []GCCollector `json:"gc_collectors,omitempty"`
}

// GCCollector 的 count/time 是 JVM 啟動以來的累積 counter。
type GCCollector struct {
	Name                 string `json:"name"`
	CollectionCount      *int64 `json:"collection_count,omitempty"`
	CollectionTimeMillis *int64 `json:"collection_time_millis,omitempty"`
}
