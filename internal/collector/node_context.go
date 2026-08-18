package collector

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"

	"elk-diagnostics/internal/nodecontext"
)

// NodeContextSnapshot 取得所有回應節點的資源快照。Stats 是主資料；Info 失敗時仍回傳
// 已取得的 Stats，並把原因放進 Issues，避免單一輔助端點失敗使所有節點資料消失。
func (c *Client) NodeContextSnapshot() (*nodecontext.Snapshot, error) {
	statsBody, err := c.nodeResourceStats()
	if err != nil {
		return nil, err
	}
	snapshot, nodes, err := parseNodeResourceStats(statsBody)
	if err != nil {
		return nil, fmt.Errorf("解析 node resource stats 失敗: %w", err)
	}

	infoBody, infoErr := c.get(EpNodesResourceInfo)
	if infoErr != nil {
		snapshot.Issues = append(snapshot.Issues, "Nodes Info 不可用: "+infoErr.Error())
	} else {
		if err := mergeNodeResourceInfo(snapshot, nodes, infoBody); err != nil {
			snapshot.Issues = append(snapshot.Issues, "Nodes Info 解析失敗: "+err.Error())
		}
	}

	appendCoverageIssue(snapshot, "Nodes Stats", snapshot.StatsCoverage)
	appendCoverageIssue(snapshot, "Nodes Info", snapshot.InfoCoverage)
	finalizeNodeSnapshot(snapshot, nodes)
	return snapshot, nil
}

type rawCoverage struct {
	Total      *int `json:"total"`
	Successful *int `json:"successful"`
	Failed     *int `json:"failed"`
}

type rawStatsNode struct {
	Name  string   `json:"name"`
	IP    string   `json:"ip"`
	Roles []string `json:"roles"`
	OS    struct {
		CPU struct {
			Percent *int `json:"percent"`
			Load    struct {
				One     *float64 `json:"1m"`
				Five    *float64 `json:"5m"`
				Fifteen *float64 `json:"15m"`
			} `json:"load_average"`
		} `json:"cpu"`
		// Load 保留對未來／非標準回應的相容；ES 8/9 正式回應位於 cpu.load_average。
		Load struct {
			One     *float64 `json:"1m"`
			Five    *float64 `json:"5m"`
			Fifteen *float64 `json:"15m"`
		} `json:"load_average"`
		Mem struct {
			Total       *int64 `json:"total_in_bytes"`
			Free        *int64 `json:"free_in_bytes"`
			Used        *int64 `json:"used_in_bytes"`
			FreePercent *int   `json:"free_percent"`
			UsedPercent *int   `json:"used_percent"`
		} `json:"mem"`
		Swap struct {
			Total *int64 `json:"total_in_bytes"`
			Free  *int64 `json:"free_in_bytes"`
			Used  *int64 `json:"used_in_bytes"`
		} `json:"swap"`
		Cgroup struct {
			CPUAcct struct {
				Usage *int64 `json:"usage_nanos"`
			} `json:"cpuacct"`
			CPU struct {
				Period *int64 `json:"cfs_period_micros"`
				Quota  *int64 `json:"cfs_quota_micros"`
				Stat   struct {
					Elapsed     *int64 `json:"number_of_elapsed_periods"`
					Throttled   *int64 `json:"number_of_times_throttled"`
					ThrottledNS *int64 `json:"time_throttled_nanos"`
				} `json:"stat"`
			} `json:"cpu"`
			Memory struct {
				Limit json.RawMessage `json:"limit_in_bytes"`
				Usage json.RawMessage `json:"usage_in_bytes"`
			} `json:"memory"`
		} `json:"cgroup"`
	} `json:"os"`
	Process struct {
		OpenFD *int64 `json:"open_file_descriptors"`
		MaxFD  *int64 `json:"max_file_descriptors"`
		CPU    struct {
			Percent *int   `json:"percent"`
			Total   *int64 `json:"total_in_millis"`
		} `json:"cpu"`
		Mem struct {
			TotalVirtual *int64 `json:"total_virtual_in_bytes"`
		} `json:"mem"`
	} `json:"process"`
	FS struct {
		Total struct {
			Total     *int64 `json:"total_in_bytes"`
			Free      *int64 `json:"free_in_bytes"`
			Available *int64 `json:"available_in_bytes"`
		} `json:"total"`
		Data []struct {
			Path      string `json:"path"`
			Mount     string `json:"mount"`
			Type      string `json:"type"`
			Total     *int64 `json:"total_in_bytes"`
			Free      *int64 `json:"free_in_bytes"`
			Available *int64 `json:"available_in_bytes"`
		} `json:"data"`
		IOStats struct {
			Devices []struct {
				Name            string `json:"device_name"`
				Operations      *int64 `json:"operations"`
				ReadOperations  *int64 `json:"read_operations"`
				WriteOperations *int64 `json:"write_operations"`
				ReadKB          *int64 `json:"read_kilobytes"`
				WriteKB         *int64 `json:"write_kilobytes"`
				IOTime          *int64 `json:"io_time_in_millis"`
			} `json:"devices"`
		} `json:"io_stats"`
	} `json:"fs"`
	JVM struct {
		Uptime *int64 `json:"uptime_in_millis"`
		Mem    struct {
			HeapUsed    *int64 `json:"heap_used_in_bytes"`
			HeapMax     *int64 `json:"heap_max_in_bytes"`
			HeapUsedPct *int   `json:"heap_used_percent"`
			Pools       struct {
				Old struct {
					Used *int64 `json:"used_in_bytes"`
					Max  *int64 `json:"max_in_bytes"`
				} `json:"old"`
			} `json:"pools"`
		} `json:"mem"`
		GC struct {
			Collectors map[string]struct {
				Count *int64 `json:"collection_count"`
				Time  *int64 `json:"collection_time_in_millis"`
			} `json:"collectors"`
		} `json:"gc"`
	} `json:"jvm"`
}

func parseNodeResourceStats(body []byte) (*nodecontext.Snapshot, map[string]*nodecontext.Node, error) {
	var raw struct {
		Coverage rawCoverage             `json:"_nodes"`
		Nodes    map[string]rawStatsNode `json:"nodes"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, nil, err
	}

	snapshot := &nodecontext.Snapshot{StatsCoverage: coverageOf(raw.Coverage, len(raw.Nodes))}
	nodes := make(map[string]*nodecontext.Node, len(raw.Nodes))
	for id, r := range raw.Nodes {
		n := &nodecontext.Node{ID: id, Name: r.Name, IP: r.IP, Roles: append([]string(nil), r.Roles...), StatsAvailable: true}
		n.OS.CPUPercent = nonNegativeInt(r.OS.CPU.Percent)
		n.OS.Load1m = firstFloat(r.OS.CPU.Load.One, r.OS.Load.One)
		n.OS.Load5m = firstFloat(r.OS.CPU.Load.Five, r.OS.Load.Five)
		n.OS.Load15m = firstFloat(r.OS.CPU.Load.Fifteen, r.OS.Load.Fifteen)
		n.OS.Memory = nodecontext.Memory{TotalBytes: nonNegativeInt64(r.OS.Mem.Total), UsedBytes: nonNegativeInt64(r.OS.Mem.Used), FreeBytes: nonNegativeInt64(r.OS.Mem.Free), UsedPct: nonNegativeInt(r.OS.Mem.UsedPercent), FreePct: nonNegativeInt(r.OS.Mem.FreePercent)}
		n.OS.Swap = nodecontext.Memory{TotalBytes: nonNegativeInt64(r.OS.Swap.Total), UsedBytes: nonNegativeInt64(r.OS.Swap.Used), FreeBytes: nonNegativeInt64(r.OS.Swap.Free)}
		n.OS.Cgroup.CPU = nodecontext.CgroupCPU{UsageNanos: nonNegativeInt64(r.OS.Cgroup.CPUAcct.Usage), CFSPeriodMicros: nonNegativeInt64(r.OS.Cgroup.CPU.Period), CFSQuotaMicros: r.OS.Cgroup.CPU.Quota, ElapsedPeriods: nonNegativeInt64(r.OS.Cgroup.CPU.Stat.Elapsed), TimesThrottled: nonNegativeInt64(r.OS.Cgroup.CPU.Stat.Throttled), TimeThrottledNanos: nonNegativeInt64(r.OS.Cgroup.CPU.Stat.ThrottledNS)}
		n.OS.Cgroup.Memory.UsageBytes = parseRawUint64(r.OS.Cgroup.Memory.Usage)
		n.OS.Cgroup.Memory.LimitBytes, n.OS.Cgroup.Memory.LimitUnlimited = parseCgroupLimit(r.OS.Cgroup.Memory.Limit)

		n.Process.CPUPercent = nonNegativeInt(r.Process.CPU.Percent)
		n.Process.CPUTotalMillis = nonNegativeInt64(r.Process.CPU.Total)
		n.Process.TotalVirtualMemoryBytes = nonNegativeInt64(r.Process.Mem.TotalVirtual)
		n.Process.OpenFileDescriptors = nonNegativeInt64(r.Process.OpenFD)
		n.Process.MaxFileDescriptors = nonNegativeInt64(r.Process.MaxFD)

		n.Filesystem.TotalBytes = nonNegativeInt64(r.FS.Total.Total)
		n.Filesystem.FreeBytes = nonNegativeInt64(r.FS.Total.Free)
		n.Filesystem.AvailableBytes = nonNegativeInt64(r.FS.Total.Available)
		for _, p := range r.FS.Data {
			n.Filesystem.DataPaths = append(n.Filesystem.DataPaths, nodecontext.DataPath{Path: p.Path, Mount: p.Mount, Type: p.Type, TotalBytes: nonNegativeInt64(p.Total), FreeBytes: nonNegativeInt64(p.Free), AvailableBytes: nonNegativeInt64(p.Available)})
		}
		for _, d := range r.FS.IOStats.Devices {
			n.Filesystem.Devices = append(n.Filesystem.Devices, nodecontext.IODevice{Name: d.Name, Operations: nonNegativeInt64(d.Operations), ReadOperations: nonNegativeInt64(d.ReadOperations), WriteOperations: nonNegativeInt64(d.WriteOperations), ReadKilobytes: nonNegativeInt64(d.ReadKB), WriteKilobytes: nonNegativeInt64(d.WriteKB), IOTimeMillis: nonNegativeInt64(d.IOTime)})
		}

		n.JVM.UptimeMillis = nonNegativeInt64(r.JVM.Uptime)
		n.JVM.HeapUsedBytes = nonNegativeInt64(r.JVM.Mem.HeapUsed)
		n.JVM.HeapMaxBytes = nonNegativeInt64(r.JVM.Mem.HeapMax)
		n.JVM.HeapUsedPct = nonNegativeInt(r.JVM.Mem.HeapUsedPct)
		n.JVM.OldUsedBytes = nonNegativeInt64(r.JVM.Mem.Pools.Old.Used)
		n.JVM.OldMaxBytes = nonNegativeInt64(r.JVM.Mem.Pools.Old.Max)
		for name, gc := range r.JVM.GC.Collectors {
			n.JVM.GCCollectors = append(n.JVM.GCCollectors, nodecontext.GCCollector{Name: name, CollectionCount: nonNegativeInt64(gc.Count), CollectionTimeMillis: nonNegativeInt64(gc.Time)})
		}
		nodes[id] = n
	}
	return snapshot, nodes, nil
}

func mergeNodeResourceInfo(snapshot *nodecontext.Snapshot, nodes map[string]*nodecontext.Node, body []byte) error {
	var raw struct {
		Coverage rawCoverage `json:"_nodes"`
		Nodes    map[string]struct {
			Name  string   `json:"name"`
			IP    string   `json:"ip"`
			Roles []string `json:"roles"`
			OS    struct {
				Name                string `json:"name"`
				PrettyName          string `json:"pretty_name"`
				Version             string `json:"version"`
				Architecture        string `json:"arch"`
				AvailableProcessors *int   `json:"available_processors"`
				AllocatedProcessors *int   `json:"allocated_processors"`
			} `json:"os"`
			Process struct {
				PID      *int64 `json:"id"`
				Mlockall *bool  `json:"mlockall"`
			} `json:"process"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return err
	}
	snapshot.InfoCoverage = coverageOf(raw.Coverage, len(raw.Nodes))
	for id, r := range raw.Nodes {
		n := nodes[id]
		if n == nil {
			n = &nodecontext.Node{ID: id}
			nodes[id] = n
		}
		n.InfoAvailable = true
		if n.Name == "" {
			n.Name = r.Name
		}
		if n.IP == "" {
			n.IP = r.IP
		}
		if len(n.Roles) == 0 {
			n.Roles = append([]string(nil), r.Roles...)
		}
		n.OS.Name, n.OS.PrettyName, n.OS.Version, n.OS.Architecture = r.OS.Name, r.OS.PrettyName, r.OS.Version, r.OS.Architecture
		n.OS.AvailableProcessors = positiveInt(r.OS.AvailableProcessors)
		n.OS.AllocatedProcessors = positiveInt(r.OS.AllocatedProcessors)
		n.Process.PID = nonNegativeInt64(r.Process.PID)
		n.Process.MemoryLocked = r.Process.Mlockall
	}
	return nil
}

func finalizeNodeSnapshot(snapshot *nodecontext.Snapshot, nodes map[string]*nodecontext.Node) {
	snapshot.Nodes = make([]nodecontext.Node, 0, len(nodes))
	for _, n := range nodes {
		sort.Strings(n.Roles)
		sort.Slice(n.Filesystem.DataPaths, func(i, j int) bool { return n.Filesystem.DataPaths[i].Path < n.Filesystem.DataPaths[j].Path })
		sort.Slice(n.Filesystem.Devices, func(i, j int) bool { return n.Filesystem.Devices[i].Name < n.Filesystem.Devices[j].Name })
		sort.Slice(n.JVM.GCCollectors, func(i, j int) bool { return n.JVM.GCCollectors[i].Name < n.JVM.GCCollectors[j].Name })
		snapshot.Nodes = append(snapshot.Nodes, *n)
	}
	sort.Slice(snapshot.Nodes, func(i, j int) bool {
		if snapshot.Nodes[i].Name == snapshot.Nodes[j].Name {
			return snapshot.Nodes[i].ID < snapshot.Nodes[j].ID
		}
		return snapshot.Nodes[i].Name < snapshot.Nodes[j].Name
	})
}

func coverageOf(raw rawCoverage, returned int) nodecontext.Coverage {
	c := nodecontext.Coverage{Returned: returned}
	if raw.Total == nil || raw.Successful == nil || raw.Failed == nil {
		return c
	}
	c.Available = true
	c.Total, c.Successful, c.Failed = *raw.Total, *raw.Successful, *raw.Failed
	return c
}

func appendCoverageIssue(snapshot *nodecontext.Snapshot, name string, c nodecontext.Coverage) {
	if !c.Available {
		snapshot.Issues = append(snapshot.Issues, name+" 缺少 _nodes coverage")
		return
	}
	if !c.Complete() {
		snapshot.Issues = append(snapshot.Issues, fmt.Sprintf("%s 部分回應: total=%d successful=%d failed=%d returned=%d", name, c.Total, c.Successful, c.Failed, c.Returned))
	}
}

func nonNegativeInt(v *int) *int {
	if v == nil || *v < 0 {
		return nil
	}
	return v
}

func positiveInt(v *int) *int {
	if v == nil || *v <= 0 {
		return nil
	}
	return v
}

func nonNegativeInt64(v *int64) *int64 {
	if v == nil || *v < 0 {
		return nil
	}
	return v
}

func parseRawUint64(raw json.RawMessage) *uint64 {
	b := bytes.TrimSpace(raw)
	if len(b) == 0 || bytes.Equal(b, []byte("null")) {
		return nil
	}
	if len(b) >= 2 && b[0] == '"' && b[len(b)-1] == '"' {
		b = b[1 : len(b)-1]
	}
	v, err := strconv.ParseUint(string(b), 10, 64)
	if err != nil {
		return nil
	}
	return &v
}

func parseCgroupLimit(raw json.RawMessage) (*uint64, *bool) {
	b := bytes.TrimSpace(raw)
	if len(b) >= 2 && b[0] == '"' && b[len(b)-1] == '"' {
		b = b[1 : len(b)-1]
	}
	if bytes.EqualFold(b, []byte("max")) {
		unlimited := true
		return nil, &unlimited
	}
	v := parseRawUint64(raw)
	if v == nil {
		return nil, nil
	}
	// Linux cgroup v1 常用 LONG_MAX-4095（9223372036854771712）表示 unlimited，
	// 它仍小於 MaxInt64；以 MaxInt64/2 為保守界線，遠高於現實可配置的記憶體。
	unlimited := *v > uint64(math.MaxInt64/2)
	if unlimited {
		return nil, &unlimited
	}
	return v, &unlimited
}

func firstFloat(values ...*float64) *float64 {
	for _, v := range values {
		if v != nil {
			return v
		}
	}
	return nil
}
