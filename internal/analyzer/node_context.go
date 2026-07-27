package analyzer

import (
	"fmt"
	"strings"

	"elk-diagnostics/internal/diagnostic"
	"elk-diagnostics/internal/nodecontext"
	"elk-diagnostics/rules"
)

const (
	docNodeStats  = "https://www.elastic.co/guide/en/elasticsearch/reference/current/cluster-nodes-stats.html"
	docNodeInfo   = "https://www.elastic.co/guide/en/elasticsearch/reference/current/cluster-nodes-info.html"
	docSwap       = "https://www.elastic.co/docs/deploy-manage/deploy/self-managed/setup-configuration-memory"
	docFileLimits = "https://www.elastic.co/guide/en/elasticsearch/reference/current/file-descriptors.html"
)

// NodeContextResults 只產出單次快照足以支持的診斷。I/O、GC、CPU throttling 等累積
// counter 保留在 Snapshot 給 reporter 呈現，但不在這裡硬推 rate 或 latency。
func NodeContextResults(snapshot *nodecontext.Snapshot, t rules.Thresholds) []diagnostic.Result {
	var nodes []nodecontext.Node
	if snapshot != nil {
		nodes = snapshot.Nodes
	}
	return []diagnostic.Result{
		NodeAPICoverage(snapshot),
		NodeSwapUsage(nodes),
		NodeFileDescriptorPressure(nodes, t),
		NodeCgroupMemoryPressure(nodes, t),
		RecentNodeRestart(snapshot, t),
		NodeMemoryLock(snapshot),
	}
}

func RecentNodeRestart(snapshot *nodecontext.Snapshot, t rules.Thresholds) diagnostic.Result {
	res := diagnostic.Result{ID: "recent_node_restart", Title: "節點近期重啟", Category: "node", Source: "raw_api", Docs: []string{docNodeStats}}
	if snapshot == nil || len(snapshot.Nodes) == 0 {
		return unknownNodeContext(res, "Nodes Stats 不可用，無法判定節點 uptime", nil)
	}
	if !snapshot.StatsCoverage.Complete() {
		return unknownNodeContext(res, "Nodes Stats 回應不完整，無法判定所有節點 uptime", snapshot.Issues)
	}
	warnMillis := int64(t.StaticHealth.RecentRestartWarnMinutes) * 60 * 1000
	var hits, missing []string
	for _, node := range snapshot.Nodes {
		if node.JVM.UptimeMillis == nil {
			missing = append(missing, nodeName(node)+"：JVM uptime 不可得")
			continue
		}
		if *node.JVM.UptimeMillis < warnMillis {
			hits = append(hits, fmt.Sprintf("%s：uptime=%s", nodeName(node), formatDurationMillis(*node.JVM.UptimeMillis)))
		}
	}
	if len(hits) > 0 {
		res.Status, res.Conclusion = diagnostic.StatusWarning, diagnostic.ConclusionSuspected
		res.Summary = fmt.Sprintf("%d 個節點在最近 %d 分鐘內啟動", len(hits), t.StaticHealth.RecentRestartWarnMinutes)
		res.Findings = append(hits, missing...)
		res.RequiresExtra = true
		res.ExtraReason = "uptime 只能證明啟動時間，不能區分正常維護、crash 或主機重啟"
		res.Recommendations = []diagnostic.Recommendation{{Desc: "對照變更窗口、Elasticsearch 日誌與主機事件確認重啟原因及是否反覆發生"}}
		return res
	}
	if len(missing) > 0 {
		return unknownNodeContext(res, "部分節點缺少 JVM uptime，無法完整判定", missing)
	}
	return pass(res, fmt.Sprintf("所有節點 uptime 均 ≥%d 分鐘", t.StaticHealth.RecentRestartWarnMinutes))
}

func NodeMemoryLock(snapshot *nodecontext.Snapshot) diagnostic.Result {
	res := diagnostic.Result{ID: "node_memory_lock", Title: "Memory lock / swap 設定", Category: "node", Source: "raw_api", Docs: []string{docNodeInfo, docSwap}}
	if snapshot == nil || len(snapshot.Nodes) == 0 {
		return unknownNodeContext(res, "Nodes Info 不可用，無法判定 memory lock", nil)
	}
	if !snapshot.InfoCoverage.Complete() || !snapshot.StatsCoverage.Complete() {
		return unknownNodeContext(res, "Nodes Info／Stats 回應不完整，無法交叉判讀 memory lock 與 swap", snapshot.Issues)
	}
	var risks, missing []string
	for _, node := range snapshot.Nodes {
		name := nodeName(node)
		if node.Process.MemoryLocked == nil {
			missing = append(missing, name+"：mlockall 不可得")
			continue
		}
		if *node.Process.MemoryLocked {
			continue
		}
		if node.OS.Swap.TotalBytes == nil {
			missing = append(missing, name+"：mlockall=false 且 swap total 不可得")
			continue
		}
		if *node.OS.Swap.TotalBytes > 0 {
			risks = append(risks, fmt.Sprintf("%s：mlockall=false swap_total=%s swap_used=%s", name, formatBytes(*node.OS.Swap.TotalBytes), formatOptionalBytes(node.OS.Swap.UsedBytes)))
		}
	}
	if len(risks) > 0 {
		res.Status, res.Conclusion = diagnostic.StatusWarning, diagnostic.ConclusionSuspected
		res.Summary = fmt.Sprintf("%d 個節點未鎖定記憶體且主機配置了 swap", len(risks))
		res.Findings = append(risks, missing...)
		res.RequiresExtra = true
		res.ExtraReason = "Elastic 支援停用 swap 或啟用 memory lock；mlockall=false 單獨不代表故障"
		res.Recommendations = []diagnostic.Recommendation{{Desc: "依部署方式選擇完全停用 swap，或設定 bootstrap.memory_lock=true 並正確配置系統 memlock 上限"}}
		return res
	}
	if len(missing) > 0 {
		return unknownNodeContext(res, "部分節點缺少 memory lock／swap 欄位，無法完整判定", missing)
	}
	return pass(res, "所有節點已鎖定記憶體，或在 mlockall=false 時確認 swap total=0")
}

func NodeAPICoverage(snapshot *nodecontext.Snapshot) diagnostic.Result {
	res := diagnostic.Result{ID: "node_api_coverage", Title: "節點資料完整性", Category: "node", Source: "raw_api", Docs: []string{docNodeStats, docNodeInfo}}
	if snapshot == nil {
		return unknownNodeContext(res, "Nodes Stats 不可用，無法驗證節點資料完整性", nil)
	}
	stats, info := snapshot.StatsCoverage, snapshot.InfoCoverage
	findings := []string{
		fmt.Sprintf("Nodes Stats: successful=%d/%d failed=%d returned=%d", stats.Successful, stats.Total, stats.Failed, stats.Returned),
		fmt.Sprintf("Nodes Info: successful=%d/%d failed=%d returned=%d", info.Successful, info.Total, info.Failed, info.Returned),
	}
	findings = append(findings, snapshot.Issues...)
	if stats.Complete() && info.Complete() && len(snapshot.Issues) == 0 {
		res = pass(res, fmt.Sprintf("Nodes Stats 與 Nodes Info 均完整回應 %d 個節點", stats.Total))
		res.Findings = findings
		return res
	}
	return unknownNodeContext(res, "Nodes API 回應不完整，無法宣稱已涵蓋所有節點", findings)
}

func NodeSwapUsage(nodes []nodecontext.Node) diagnostic.Result {
	res := diagnostic.Result{ID: "node_swap_usage", Title: "節點 Swap 使用", Category: "node", Source: "raw_api", Docs: []string{docSwap}}
	var hits, missing []string
	for _, n := range nodes {
		name := nodeName(n)
		swap := n.OS.Swap
		switch {
		case swap.UsedBytes != nil && *swap.UsedBytes > 0:
			hits = append(hits, fmt.Sprintf("%s：swap used=%s total=%s", name, formatBytes(*swap.UsedBytes), formatOptionalBytes(swap.TotalBytes)))
		case swap.UsedBytes != nil:
			// 明確為 0。
		case swap.TotalBytes != nil && *swap.TotalBytes == 0:
			// swap 未配置，used 欄位即使平台未回也可確定沒有 swap 容量。
		default:
			missing = append(missing, name+"：swap 欄位不可得")
		}
	}
	if len(hits) > 0 {
		res.Status, res.Conclusion = diagnostic.StatusWarning, diagnostic.ConclusionConfirmed
		res.Summary = fmt.Sprintf("%d 個節點正在使用 swap；Elastic 建議避免 swapping", len(hits))
		res.Findings = append(hits, missing...)
		res.Recommendations = []diagnostic.Recommendation{{Desc: "確認 swap／swappiness 與 memory lock 設定；正式變更前先評估主機記憶體餘裕"}}
		return res
	}
	if len(nodes) == 0 || len(missing) > 0 {
		return unknownNodeContext(res, "部分或全部節點缺少 swap 資料，無法判定", missing)
	}
	return pass(res, "各節點 swap used=0")
}

func NodeFileDescriptorPressure(nodes []nodecontext.Node, t rules.Thresholds) diagnostic.Result {
	warnPct, critPct := t.NodeContext.FDWarnPct, t.NodeContext.FDCritPct
	res := diagnostic.Result{ID: "node_file_descriptor_pressure", Title: "File descriptor 使用率", Category: "node", Source: "raw_api", Docs: []string{docFileLimits}}
	var critical, warning, missing []string
	for _, n := range nodes {
		name := nodeName(n)
		open, max := n.Process.OpenFileDescriptors, n.Process.MaxFileDescriptors
		if open == nil || max == nil || *max <= 0 {
			missing = append(missing, name+"：open/max file descriptors 不可得")
			continue
		}
		pct := int(100 * *open / *max)
		finding := fmt.Sprintf("%s：open=%d max=%d（%d%%）", name, *open, *max, pct)
		switch {
		case pct >= critPct:
			critical = append(critical, finding)
		case pct >= warnPct:
			warning = append(warning, finding)
		}
	}
	res.Findings = append(append(critical, warning...), missing...)
	switch {
	case len(critical) > 0:
		res.Status, res.Conclusion = diagnostic.StatusCritical, diagnostic.ConclusionConfirmed
		res.Summary = fmt.Sprintf("%d 個節點 file descriptor 使用率 ≥%d%%", len(critical), critPct)
	case len(warning) > 0:
		res.Status, res.Conclusion = diagnostic.StatusWarning, diagnostic.ConclusionConfirmed
		res.Summary = fmt.Sprintf("%d 個節點 file descriptor 使用率 ≥%d%%", len(warning), warnPct)
	case len(nodes) == 0 || len(missing) > 0:
		return unknownNodeContext(res, "部分或全部節點缺少 file descriptor 資料，無法判定", missing)
	default:
		return pass(res, fmt.Sprintf("各節點 file descriptor 使用率 <%d%%", warnPct))
	}
	res.Recommendations = []diagnostic.Recommendation{{Desc: "確認 descriptor 是否持續增加，並檢查 Elasticsearch process 的 nofile 上限"}}
	return res
}

func NodeCgroupMemoryPressure(nodes []nodecontext.Node, t rules.Thresholds) diagnostic.Result {
	warnPct := t.NodeContext.CgroupMemoryWarnPct
	res := diagnostic.Result{ID: "node_cgroup_memory_pressure", Title: "Cgroup memory 餘裕", Category: "node", Source: "raw_api", Docs: []string{docNodeStats}}
	var hits, missing []string
	limited := 0
	for _, n := range nodes {
		name := nodeName(n)
		cg := n.OS.Cgroup.Memory
		if cg.LimitUnlimited != nil && *cg.LimitUnlimited {
			continue
		}
		if cg.LimitBytes == nil {
			// cgroup 是 Linux-only；已知非 Linux 時不算採集缺失。
			if n.OS.Name == "" || strings.EqualFold(n.OS.Name, "Linux") {
				missing = append(missing, name+"：有限 cgroup memory limit 不可得")
			}
			continue
		}
		limited++
		if *cg.LimitBytes == 0 || cg.UsageBytes == nil {
			missing = append(missing, name+"：cgroup memory usage/limit 不完整")
			continue
		}
		pct := int(100 * float64(*cg.UsageBytes) / float64(*cg.LimitBytes))
		if pct >= warnPct {
			hits = append(hits, fmt.Sprintf("%s：usage=%s limit=%s（%d%%）", name, formatBytesU(*cg.UsageBytes), formatBytesU(*cg.LimitBytes), pct))
		}
	}
	if len(hits) > 0 {
		res.Status, res.Conclusion = diagnostic.StatusWarning, diagnostic.ConclusionSuspected
		res.Summary = fmt.Sprintf("%d 個節點 cgroup memory usage ≥%d%%", len(hits), warnPct)
		res.Findings = append(hits, missing...)
		res.RequiresExtra = true
		res.ExtraReason = "cgroup usage 包含可回收 file cache；需用時間序列、memory.events／OOM 事件與 workload 佐證"
		res.Recommendations = []diagnostic.Recommendation{{Desc: "確認 container memory limit、JVM heap 與非 heap 記憶體配置是否保留足夠餘裕"}}
		return res
	}
	if len(nodes) == 0 || len(missing) > 0 {
		return unknownNodeContext(res, "部分或全部 Linux 節點缺少 cgroup memory 資料，無法判定", missing)
	}
	if limited == 0 {
		return pass(res, "未偵測到有限 cgroup memory limit，本門檻不適用")
	}
	return pass(res, fmt.Sprintf("有限 cgroup 的 memory usage 均 <%d%%", warnPct))
}

func unknownNodeContext(res diagnostic.Result, summary string, findings []string) diagnostic.Result {
	res.Status, res.Conclusion = diagnostic.StatusUnknown, diagnostic.ConclusionNormal
	res.Summary = summary
	res.Findings = findings
	return res
}

func nodeName(n nodecontext.Node) string {
	if n.Name != "" {
		return n.Name
	}
	return n.ID
}

func formatOptionalBytes(v *int64) string {
	if v == nil {
		return "不可得"
	}
	return formatBytes(*v)
}

func formatBytes(v int64) string { return formatBytesU(uint64(v)) }

func formatBytesU(v uint64) string {
	const (
		kiB = 1024
		miB = 1024 * kiB
		giB = 1024 * miB
		tiB = 1024 * giB
	)
	switch {
	case v >= tiB:
		return fmt.Sprintf("%.1f TiB", float64(v)/tiB)
	case v >= giB:
		return fmt.Sprintf("%.1f GiB", float64(v)/giB)
	case v >= miB:
		return fmt.Sprintf("%.1f MiB", float64(v)/miB)
	case v >= kiB:
		return fmt.Sprintf("%.1f KiB", float64(v)/kiB)
	default:
		return fmt.Sprintf("%d B", v)
	}
}
