package main

import (
	"encoding/json"
	"math"
	"sort"
	"strconv"
	"strings"

	"elk-diagnostics/internal/collector"
	"elk-diagnostics/internal/diagnostic"
	"elk-diagnostics/internal/nodecontext"
)

// currentStateInput 集中整理 check 已經取得的快照；摘要不另發 API，也不參與
// diagnostic.Result 的計數與 overall status 收斂。
type currentStateInput struct {
	HealthReport        *collector.HealthReport
	ClusterHealth       collector.ClusterHealth
	ExpectedNodes       []string
	NodeSnapshot        *nodecontext.Snapshot
	MasterEligible      int
	MasterEligibleKnown bool
	ILMStatus           string
	ILMKnown            bool
	License             collector.LicenseInfo
	LicenseKnown        bool
	ShardLimits         collector.ClusterShardLimits
	ShardLimitsKnown    bool
	CPUs                []collector.NodeCPU
	Results             []diagnostic.Result
	KibanaRequested     bool
	LogstashRequested   bool
	KibanaEvidence      []collector.KibanaEvidence
	LogstashEvidence    []collector.LogstashEvidence
}

func buildCurrentState(in currentStateInput) *diagnostic.CurrentState {
	state := &diagnostic.CurrentState{
		SnapshotNote: "本區塊為單次採集快照，不代表長期監控結論",
		Health:       currentHealth(in.ClusterHealth, in.HealthReport),
		Nodes:        currentNodes(in.ExpectedNodes, in.NodeSnapshot, in.ClusterHealth),
		Shards:       currentShardsWithReport(in.ClusterHealth, in.ShardLimits, in.ShardLimitsKnown, in.HealthReport, in.NodeSnapshot),
		Disk:         currentDiskWithSnapshot(in.ExpectedNodes, in.NodeSnapshot, in.CPUs),
		Master:       currentMaster(in.MasterEligible, in.MasterEligibleKnown, in.NodeSnapshot),
		ILM:          currentService(in.ILMStatus, in.ILMKnown),
		License:      currentLicense(in.License, in.LicenseKnown),
	}
	if in.KibanaRequested {
		state.Kibana = currentServiceInstances(in.Results, "kibana", serviceVersions(in.KibanaEvidence, nil))
	}
	if in.LogstashRequested {
		state.Logstash = currentServiceInstances(in.Results, "logstash", serviceVersions(nil, in.LogstashEvidence))
	}
	return state
}

func currentHealth(cluster collector.ClusterHealth, report *collector.HealthReport) diagnostic.CurrentHealth {
	status := knownClusterStatus(cluster.Status)
	source := ""
	if status != "unknown" {
		source = "_cluster/health"
	}
	if status == "unknown" && report != nil {
		status = knownClusterStatus(report.Status)
		if status != "unknown" {
			source = "_health_report"
		}
	}
	if source == "" {
		source = "—"
	}
	return diagnostic.CurrentHealth{Known: status != "unknown", Status: status, Source: source}
}

func knownClusterStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "green", "yellow", "red":
		return strings.ToLower(strings.TrimSpace(status))
	default:
		return "unknown"
	}
}

func currentNodes(expected []string, snapshot *nodecontext.Snapshot, health collector.ClusterHealth) diagnostic.CurrentNodes {
	state := diagnostic.CurrentNodes{}
	if len(expected) > 0 {
		value := len(expected)
		state.Expected = &value
	}
	if snapshot != nil {
		value := len(snapshot.Nodes)
		if snapshot.StatsCoverage.Available {
			value = snapshot.StatsCoverage.Returned
		}
		state.Responding = &value
	} else if health.NumberOfNodes != nil {
		value := *health.NumberOfNodes
		state.Responding = &value
	}
	if len(expected) > 0 && snapshot != nil && snapshot.StatsCoverage.Complete() {
		missing := append([]string(nil), snapshot.MissingNodes...)
		sort.Strings(missing)
		value := len(missing)
		state.Missing = &value
		state.MissingKnown = true
		state.MissingNames = missing
	}
	return state
}

func currentShards(health collector.ClusterHealth, limits collector.ClusterShardLimits, limitsKnown bool, snapshots ...*nodecontext.Snapshot) diagnostic.CurrentShards {
	return currentShardsWithReport(health, limits, limitsKnown, nil, snapshots...)
}

func currentShardsWithReport(health collector.ClusterHealth, limits collector.ClusterShardLimits, limitsKnown bool, report *collector.HealthReport, snapshots ...*nodecontext.Snapshot) diagnostic.CurrentShards {
	var snapshot *nodecontext.Snapshot
	if len(snapshots) > 0 {
		snapshot = snapshots[0]
	}
	state := diagnostic.CurrentShards{
		Active:            health.ActiveShards,
		ActivePrimary:     health.ActivePrimaryShards,
		Unassigned:        health.UnassignedShards,
		UnassignedPrimary: health.UnassignedPrimaryShards,
		Relocating:        health.RelocatingShards,
		Initializing:      health.InitializingShards,
		ActivePercent:     health.ActiveShardsPercent,
	}
	// active_shards 已包含正在 relocation 的現有副本；穩定快照下以 active +
	// unassigned 呈現總量。搬移／初始化期間不宣稱精確總量，避免重複計數。
	if state.Active != nil && state.Unassigned != nil && shardTransitionsSettled(state) {
		total := *state.Active + *state.Unassigned
		state.Total = &total
	}
	if limitsKnown {
		state.MaxPerNode = limits.MaxShardsPerNode
	}
	if frozenCount := frozenDataNodeCount(snapshot); frozenCount != nil && *frozenCount > 0 {
		state.FrozenNodeCount = frozenCount
		if limitsKnown {
			state.MaxPerFrozenNode = limits.MaxShardsPerNodeFrozen
		}
	}

	// _health_report/shards_capacity is the authoritative cluster-level value.
	// The settings fallback below exists for older ES versions or incomplete bundles.
	dataCapacity, frozenCapacity, frozenUsed := healthReportShardCapacity(report)
	if nodeCount := dataNodeCount(snapshot); nodeCount != nil && *nodeCount > 0 {
		state.CapacityNodeCount = nodeCount
	}
	if state.FrozenNodeCount != nil && *state.FrozenNodeCount > 0 {
		if frozenCapacity != nil {
			state.FrozenCapacity = frozenCapacity
		} else if limitsKnown && limits.MaxShardsPerNodeFrozen != nil && *limits.MaxShardsPerNodeFrozen > 0 {
			capacity := *limits.MaxShardsPerNodeFrozen * *state.FrozenNodeCount
			state.FrozenCapacity = &capacity
		}
		if state.FrozenCapacity != nil && frozenUsed != nil {
			state.FrozenUsed = frozenUsed
		}
	}
	if dataCapacity != nil {
		state.Capacity = dataCapacity
	} else if limitsKnown && limits.MaxShardsPerNode != nil {
		if nodeCount := dataNodeCount(snapshot); nodeCount != nil && *nodeCount > 0 {
			capacity := *limits.MaxShardsPerNode * *nodeCount
			state.CapacityNodeCount = nodeCount
			state.Capacity = &capacity
		}
	}
	if state.Capacity != nil {
		// MaxTotal is retained as a compatibility alias for older consumers.
		state.MaxTotal = state.Capacity
		if state.Total != nil && *state.Capacity > 0 {
			remaining := *state.Capacity - *state.Total
			if remaining < 0 {
				remaining = 0
			}
			state.Remaining = &remaining
			usedPercent := float64(*state.Total) * 100 / float64(*state.Capacity)
			state.CapacityUsedPercent = &usedPercent
		}
	}
	return state
}

func dataNodeCount(snapshot *nodecontext.Snapshot) *int {
	if snapshot == nil || !snapshot.StatsCoverage.Complete() {
		return nil
	}
	count := 0
	for _, node := range snapshot.Nodes {
		if hasGeneralDataRole(node.Roles) {
			count++
		}
	}
	return &count
}

func frozenDataNodeCount(snapshot *nodecontext.Snapshot) *int {
	if snapshot == nil || !snapshot.StatsCoverage.Complete() {
		return nil
	}
	count := 0
	for _, node := range snapshot.Nodes {
		for _, role := range node.Roles {
			if role == "data_frozen" {
				count++
				break
			}
		}
	}
	return &count
}

func hasGeneralDataRole(roles []string) bool {
	for _, role := range roles {
		switch role {
		case "data", "data_content", "data_hot", "data_warm", "data_cold":
			return true
		}
	}
	return false
}

func healthReportShardCapacity(report *collector.HealthReport) (data, frozen, frozenUsed *int) {
	if report == nil {
		return nil, nil, nil
	}
	indicator, ok := report.Indicators["shards_capacity"]
	if !ok || indicator.Details == nil {
		return nil, nil, nil
	}
	var details struct {
		Data struct {
			MaxShardsInCluster *int `json:"max_shards_in_cluster"`
		} `json:"data"`
		Frozen struct {
			MaxShardsInCluster *int `json:"max_shards_in_cluster"`
			CurrentUsedShards  *int `json:"current_used_shards"`
		} `json:"frozen"`
	}
	if err := json.Unmarshal(indicator.Details, &details); err != nil {
		return nil, nil, nil
	}
	return positivePtr(details.Data.MaxShardsInCluster), positivePtr(details.Frozen.MaxShardsInCluster), nonNegativePtr(details.Frozen.CurrentUsedShards)
}

func positivePtr(value *int) *int {
	if value == nil || *value <= 0 {
		return nil
	}
	return value
}

func nonNegativePtr(value *int) *int {
	if value == nil || *value < 0 {
		return nil
	}
	return value
}

func shardTransitionsSettled(state diagnostic.CurrentShards) bool {
	return (state.Relocating == nil || *state.Relocating == 0) &&
		(state.Initializing == nil || *state.Initializing == 0)
}

func currentDisk(cpus []collector.NodeCPU) diagnostic.CurrentDisk {
	return currentDiskWithSnapshot(nil, nil, cpus)
}

func currentDiskWithSnapshot(expected []string, snapshot *nodecontext.Snapshot, cpus []collector.NodeCPU) diagnostic.CurrentDisk {
	cpuIndex := newNodeCPUIndex(cpus)
	nodeByKey := make(map[string]nodecontext.Node)
	nodeKeysByName := make(map[string][]string)
	if snapshot != nil {
		for index, node := range snapshot.Nodes {
			key := nodecontext.NodeIdentity(node)
			if key == "" {
				key = "node-" + strconv.Itoa(index)
			}
			base := key
			for suffix := 2; ; suffix++ {
				if _, exists := nodeByKey[key]; !exists {
					break
				}
				key = base + "#" + strconv.Itoa(suffix)
			}
			nodeByKey[key] = node
			if node.Name != "" {
				nodeKeysByName[node.Name] = append(nodeKeysByName[node.Name], key)
			}
		}
	}

	type diskEntry struct {
		key      string
		label    string
		expected bool
	}
	entries := make(map[string]diskEntry, len(expected)+len(nodeByKey)+len(cpus))
	add := func(key, label string, isExpected bool) {
		if key == "" {
			return
		}
		entry, exists := entries[key]
		if !exists || (isExpected && !entry.expected) {
			entries[key] = diskEntry{key: key, label: label, expected: isExpected}
		}
	}
	for _, raw := range expected {
		expectedNode := nodecontext.ParseExpectedNode(raw)
		if expectedNode.IP != "" {
			add(expectedNode.IP, nodecontext.ExpectedNodeLabel(raw), true)
			continue
		}
		keys := nodeKeysByName[expectedNode.Name]
		if len(keys) == 0 {
			add("expected:"+expectedNode.Name, nodecontext.ExpectedNodeLabel(raw), true)
			continue
		}
		for _, key := range keys {
			add(key, diskNodeLabel(nodeByKey[key], len(keys) > 1), true)
		}
	}
	for key, node := range nodeByKey {
		add(key, diskNodeLabel(node, len(nodeKeysByName[node.Name]) > 1), false)
	}
	for name, values := range cpuIndex.byName {
		if len(values) == 1 && len(nodeKeysByName[name]) == 0 {
			add("cpu:"+name, name, false)
		}
	}
	orderedKeys := make([]string, 0, len(entries))
	for key := range entries {
		orderedKeys = append(orderedKeys, key)
	}
	sort.Strings(orderedKeys)
	state := diagnostic.CurrentDisk{}
	rows := make([]diagnostic.CurrentDiskNode, 0, len(orderedKeys))
	observedCount := 0
	allBytesKnown := len(orderedKeys) > 0
	var totalBytes, availableBytes, usedBytes int64
	var maxPercent int
	var maxNode string
	maxKnown := false
	for _, key := range orderedKeys {
		entry := entries[key]
		node, nodeOK := nodeByKey[key]
		var cpu collector.NodeCPU
		cpuOK := false
		if nodeOK {
			cpu, cpuOK = cpuIndex.lookup(node)
		} else if strings.HasPrefix(key, "cpu:") && len(cpuIndex.byName[entry.label]) == 1 {
			cpu, cpuOK = cpuIndex.byName[entry.label][0], true
		}
		row := diagnostic.CurrentDiskNode{Name: entry.label}
		if entry.expected && snapshot != nil && snapshot.StatsCoverage.Complete() && !nodeOK {
			row.Missing = true
		}
		if nodeOK || cpuOK {
			observedCount++
		}
		if nodeOK {
			row.TotalBytes = node.Filesystem.TotalBytes
			row.AvailableBytes = node.Filesystem.AvailableBytes
			row.UsedBytes = filesystemUsedBytes(row.TotalBytes, row.AvailableBytes)
		}
		if cpuOK && cpu.DiskKnown {
			value := cpu.DiskPercent
			row.UsedPercent = &value
		}
		if row.UsedPercent == nil {
			row.UsedPercent = bytesPercent(row.UsedBytes, row.TotalBytes)
		}
		if row.UsedPercent != nil && (!maxKnown || *row.UsedPercent > maxPercent) {
			maxPercent = *row.UsedPercent
			maxNode = entry.label
			maxKnown = true
		}
		if row.TotalBytes == nil || row.AvailableBytes == nil || row.UsedBytes == nil || row.Missing {
			allBytesKnown = false
		} else {
			totalBytes += *row.TotalBytes
			availableBytes += *row.AvailableBytes
			usedBytes += *row.UsedBytes
		}
		rows = append(rows, row)
	}

	state.NodeCount = observedCount
	state.Nodes = rows
	state.Known = len(rows) > 0 && (maxKnown || allBytesKnown)
	if maxKnown {
		state.MaxUsedPercent = &maxPercent
		state.MaxNode = maxNode
	}
	if allBytesKnown && observedCount > 0 {
		state.TotalBytes = &totalBytes
		state.AvailableBytes = &availableBytes
		state.UsedBytes = &usedBytes
		state.UsedPercent = bytesPercent(&usedBytes, &totalBytes)
	}
	return state
}

func diskNodeLabel(node nodecontext.Node, duplicateName bool) string {
	name := node.Name
	if name == "" {
		name = node.ID
	}
	if duplicateName && node.IP != "" {
		return name + " (" + node.IP + ")"
	}
	return name
}

func filesystemUsedBytes(total, available *int64) *int64 {
	if total == nil || available == nil || *total < 0 || *available < 0 || *available > *total {
		return nil
	}
	used := *total - *available
	return &used
}

func bytesPercent(used, total *int64) *int {
	if used == nil || total == nil || *used < 0 || *total <= 0 || *used > *total {
		return nil
	}
	percent := int(math.Round(float64(*used) * 100 / float64(*total)))
	return &percent
}

func attachNodeDiskUsage(snapshot *nodecontext.Snapshot, cpus []collector.NodeCPU) {
	if snapshot == nil {
		return
	}
	cpuIndex := newNodeCPUIndex(cpus)
	for i := range snapshot.Nodes {
		cpu, ok := cpuIndex.lookup(snapshot.Nodes[i])
		if ok && cpu.DiskKnown {
			value := cpu.DiskPercent
			snapshot.Nodes[i].DiskUsedPercent = &value
			continue
		}
		// 舊版 bundle 的 _cat/nodes 可能只有 node.name，重複名稱無法安全配對。
		// Nodes Stats 同時帶有每個節點自己的 filesystem 數值，可作為不猜測
		// 節點歸屬的相容 fallback。
		used := filesystemUsedBytes(snapshot.Nodes[i].Filesystem.TotalBytes, snapshot.Nodes[i].Filesystem.AvailableBytes)
		if value := bytesPercent(used, snapshot.Nodes[i].Filesystem.TotalBytes); value != nil {
			snapshot.Nodes[i].DiskUsedPercent = value
		}
	}
}

type nodeCPUIndex struct {
	byID   map[string][]collector.NodeCPU
	byIP   map[string][]collector.NodeCPU
	byName map[string][]collector.NodeCPU
}

func newNodeCPUIndex(cpus []collector.NodeCPU) nodeCPUIndex {
	index := nodeCPUIndex{
		byID:   make(map[string][]collector.NodeCPU, len(cpus)),
		byIP:   make(map[string][]collector.NodeCPU, len(cpus)),
		byName: make(map[string][]collector.NodeCPU, len(cpus)),
	}
	for _, cpu := range cpus {
		if id := strings.TrimSpace(cpu.ID); id != "" {
			index.byID[id] = append(index.byID[id], cpu)
		}
		if ip := nodecontext.NormalizeIP(cpu.IP); ip != "" {
			index.byIP[ip] = append(index.byIP[ip], cpu)
		}
		if name := strings.TrimSpace(cpu.Name); name != "" {
			index.byName[name] = append(index.byName[name], cpu)
		}
	}
	return index
}

func (index nodeCPUIndex) lookup(node nodecontext.Node) (collector.NodeCPU, bool) {
	if id := strings.TrimSpace(node.ID); id != "" {
		if values := index.byID[id]; len(values) == 1 {
			return values[0], true
		}
	}
	if ip := nodecontext.NormalizeIP(node.IP); ip != "" {
		if values := index.byIP[ip]; len(values) == 1 {
			return values[0], true
		}
	}
	if name := strings.TrimSpace(node.Name); name != "" {
		if values := index.byName[name]; len(values) == 1 {
			return values[0], true
		}
	}
	return collector.NodeCPU{}, false
}

func currentMaster(eligible int, known bool, snapshot *nodecontext.Snapshot) diagnostic.CurrentMaster {
	state := diagnostic.CurrentMaster{}
	if known {
		state.EligibleCount = &eligible
	}
	if snapshot != nil {
		seen := map[string]bool{}
		for _, node := range snapshot.Nodes {
			for _, role := range node.Roles {
				if role == "master" && node.Name != "" && !seen[node.Name] {
					state.EligibleNames = append(state.EligibleNames, node.Name)
					seen[node.Name] = true
					break
				}
			}
		}
		sort.Strings(state.EligibleNames)
	}
	return state
}

func currentService(status string, known bool) diagnostic.CurrentService {
	status = strings.ToLower(strings.TrimSpace(status))
	return diagnostic.CurrentService{Known: known && status != "", Status: status}
}

func currentLicense(license collector.LicenseInfo, known bool) diagnostic.CurrentLicense {
	status := strings.ToLower(strings.TrimSpace(license.Status))
	return diagnostic.CurrentLicense{Known: known && status != "", Status: status, Type: strings.TrimSpace(license.Type)}
}

func currentServiceInstances(results []diagnostic.Result, service string, versions []string) *diagnostic.CurrentServiceInstances {
	prefix := service + ".instance."
	state := &diagnostic.CurrentServiceInstances{}
	state.Total, _ = measurementCount(results, prefix+"count")
	state.Available, _ = measurementCount(results, prefix+"available.count")
	state.Degraded, _ = measurementCount(results, prefix+"degraded.count")
	state.Unavailable, _ = measurementCount(results, prefix+"unavailable.count")
	state.Unknown, _ = measurementCount(results, prefix+"unknown.count")
	if service == "logstash" {
		state.NotApplicable, _ = measurementCount(results, "logstash.health_report.skipped.count")
	}
	state.Versions = versions
	state.Known = state.Total != nil
	return state
}

func serviceVersions(kibana []collector.KibanaEvidence, logstash []collector.LogstashEvidence) []string {
	seen := map[string]bool{}
	versions := make([]string, 0, len(kibana)+len(logstash))
	add := func(body []byte) {
		if version := versionFromServiceJSON(body); version != "" && !seen[version] {
			seen[version] = true
			versions = append(versions, version)
		}
	}
	for _, ev := range kibana {
		add(ev.StatusBody)
	}
	for _, ev := range logstash {
		add(ev.RootBody)
	}
	sort.Strings(versions)
	return versions
}

func versionFromServiceJSON(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	var doc struct {
		Version json.RawMessage `json:"version"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return ""
	}
	var version string
	if json.Unmarshal(doc.Version, &version) == nil {
		return strings.TrimSpace(version)
	}
	var nested struct {
		Number string `json:"number"`
	}
	if json.Unmarshal(doc.Version, &nested) == nil {
		return strings.TrimSpace(nested.Number)
	}
	return ""
}

func measurementCount(results []diagnostic.Result, metric string) (*int, bool) {
	for _, result := range results {
		for _, measurement := range result.Measurements {
			if measurement.Metric != metric || measurement.Value < 0 || measurement.Value > float64(int(^uint(0)>>1)) || math.Trunc(measurement.Value) != measurement.Value {
				continue
			}
			value := int(measurement.Value)
			return &value, true
		}
	}
	return nil, false
}
