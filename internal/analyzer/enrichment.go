package analyzer

import (
	"fmt"
	"strings"

	"elk-diagnostics/internal/collector"
	"elk-diagnostics/internal/diagnostic"
)

const (
	docILMPolicies          = "https://www.elastic.co/docs/api/doc/elasticsearch/operation/operation-ilm-get-lifecycle"
	docSnapshotRepositories = "https://www.elastic.co/docs/api/doc/elasticsearch/operation/operation-snapshot-get-repository"
	docDataStreams          = "https://www.elastic.co/docs/api/doc/elasticsearch/operation/operation-indices-get-data-stream"
	docFielddataStats       = "https://www.elastic.co/docs/api/doc/elasticsearch/operation/operation-nodes-stats"
	docFielddataGuide       = "https://www.elastic.co/guide/en/elasticsearch/reference/current/modules-fielddata.html"
)

func ILMPolicyInventory(policies []collector.ILMPolicyDefinition) diagnostic.Result {
	res := diagnostic.Result{ID: "ilm_policy_inventory", Title: "ILM Policy 設定概況", Category: "management", Source: "raw_api", Docs: []string{docILMPolicies}}
	res.Measurements = append(res.Measurements, gauge("elasticsearch.ilm.policy.count", float64(len(policies)), "count", "", "", "", ""))
	if len(policies) == 0 {
		res.Measurements = append(res.Measurements, gauge("elasticsearch.ilm.policy.in_use.count", 0, "count", "", "", "", ""))
		res.Status, res.Conclusion = diagnostic.StatusSkipped, diagnostic.ConclusionNormal
		res.Summary = "未設定 ILM policy"
		return res
	}
	used := 0
	for _, policy := range policies {
		if policy.UsedIndices+policy.UsedDataStreams+policy.UsedIndexTemplates > 0 {
			used++
		}
		var phases []string
		for _, phase := range policy.Phases {
			label := phase.Name
			if phase.MinAge != "" {
				label += "@" + phase.MinAge
			}
			if len(phase.Actions) > 0 {
				label += "(" + strings.Join(phase.Actions, ",") + ")"
			}
			phases = append(phases, label)
		}
		if len(res.Findings) < 20 {
			res.Findings = append(res.Findings, fmt.Sprintf("%s：version=%d phases=[%s] in_use(indices=%d data_streams=%d templates=%d)",
				policy.Name, policy.Version, strings.Join(phases, ", "), policy.UsedIndices, policy.UsedDataStreams, policy.UsedIndexTemplates))
		}
	}
	res.Measurements = append(res.Measurements, gauge("elasticsearch.ilm.policy.in_use.count", float64(used), "count", "", "", "", ""))
	res = pass(res, fmt.Sprintf("已取得 %d 個 ILM policy，其中 %d 個目前被引用", len(policies), used))
	if len(policies) > len(res.Findings) {
		res.Findings = append(res.Findings, fmt.Sprintf("另有 %d 個 policy 未展開顯示", len(policies)-len(res.Findings)))
	}
	return res
}

func SnapshotRepositoryReferences(policies []collector.SLMPolicy, repositories []collector.SnapshotRepository) diagnostic.Result {
	res := diagnostic.Result{ID: "snapshot_repository_references", Title: "Snapshot repository 關聯", Category: "snapshot", Source: "raw_api", Docs: []string{docSnapshotRepositories}}
	res.Measurements = append(res.Measurements,
		gauge("elasticsearch.slm.policy.count", float64(len(policies)), "count", "", "", "", ""),
		gauge("elasticsearch.snapshot.repository.count", float64(len(repositories)), "count", "", "", "", ""),
	)
	if len(policies) == 0 && len(repositories) == 0 {
		res.Measurements = append(res.Measurements, gauge("elasticsearch.slm.missing_repository.count", 0, "count", "", "", "", ""))
		res.Status, res.Conclusion = diagnostic.StatusSkipped, diagnostic.ConclusionNormal
		res.Summary = "未設定 SLM policy 或 snapshot repository"
		return res
	}
	repositoryByName := make(map[string]string, len(repositories))
	for _, repository := range repositories {
		repositoryByName[repository.Name] = repository.Type
	}
	var missing, incomplete []string
	for _, policy := range policies {
		if policy.Repository == "" {
			incomplete = append(incomplete, policy.Name+"：bundle 未包含 policy.repository")
			continue
		}
		if _, ok := repositoryByName[policy.Repository]; !ok {
			missing = append(missing, fmt.Sprintf("SLM policy %s 引用不存在的 repository %s", policy.Name, policy.Repository))
		}
	}
	res.Measurements = append(res.Measurements, gauge("elasticsearch.slm.missing_repository.count", float64(len(missing)), "count", "", "", "", ""))
	if len(missing) > 0 {
		res.Status, res.Conclusion = diagnostic.StatusCritical, diagnostic.ConclusionConfirmed
		res.Summary = fmt.Sprintf("%d 個 SLM policy 引用不存在的 snapshot repository", len(missing))
		res.Findings = missing
		res.Recommendations = []diagnostic.Recommendation{{Desc: "確認 repository 名稱與 SLM policy 設定；修正前 snapshot 排程可能持續失敗"}}
	} else if len(incomplete) > 0 {
		return unknownStatic(res, "SLM policy 缺少 repository 關聯資料，無法完整判定", incomplete)
	} else {
		res = pass(res, fmt.Sprintf("%d 個 snapshot repository；所有 SLM policy 引用均存在", len(repositories)))
		for _, repository := range repositories {
			res.Findings = append(res.Findings, fmt.Sprintf("%s：type=%s", repository.Name, repository.Type))
		}
	}
	res.RequiresExtra = true
	res.ExtraReason = "本項只確認 repository 設定與引用存在，不執行會寫入暫存資料的 repository verify"
	return res
}

func DataStreamHealth(streams []collector.DataStream) diagnostic.Result {
	res := diagnostic.Result{ID: "data_stream_health", Title: "Data stream 健康", Category: "data", Source: "raw_api", Docs: []string{docDataStreams}}
	res.Measurements = append(res.Measurements, gauge("elasticsearch.data_stream.count", float64(len(streams)), "count", "", "", "", ""))
	if len(streams) == 0 {
		res.Status, res.Conclusion = diagnostic.StatusSkipped, diagnostic.ConclusionNormal
		res.Summary = "未設定 data stream"
		return res
	}
	var critical, warning, unknown []string
	statusCounts := map[string]int{}
	for _, stream := range streams {
		status := strings.ToLower(stream.Status)
		res.Measurements = append(res.Measurements, gauge("elasticsearch.data_stream.backing_index.count", float64(stream.BackingIndices), "count", "data_stream", stream.Name, stream.Name, ""))
		finding := fmt.Sprintf("%s：status=%s backing_indices=%d template=%s managed_by=%s ilm_policy=%s",
			stream.Name, stream.Status, stream.BackingIndices, stream.Template, stream.ManagedBy, stream.ILMPolicy)
		switch status {
		case "green":
			statusCounts[status]++
		case "red", "unavailable":
			statusCounts[status]++
			critical = append(critical, finding)
		case "yellow":
			statusCounts[status]++
			warning = append(warning, finding)
		default:
			statusCounts["unknown"]++
			unknown = append(unknown, finding)
		}
	}
	for _, status := range []string{"green", "yellow", "red", "unavailable", "unknown"} {
		res.Measurements = append(res.Measurements, gauge("elasticsearch.data_stream.status.count", float64(statusCounts[status]), "count", "", "", "", status))
	}
	res.Findings = append(res.Findings, critical...)
	res.Findings = append(res.Findings, warning...)
	res.Findings = append(res.Findings, unknown...)
	switch {
	case len(critical) > 0:
		res.Status, res.Conclusion = diagnostic.StatusCritical, diagnostic.ConclusionConfirmed
		res.Summary = fmt.Sprintf("%d 個 data stream 為 red／unavailable", len(critical))
	case len(warning) > 0:
		res.Status, res.Conclusion = diagnostic.StatusWarning, diagnostic.ConclusionSuspected
		res.Summary = fmt.Sprintf("%d 個 data stream 為 yellow", len(warning))
	case len(unknown) > 0:
		return unknownStatic(res, "部分 data stream 未回傳可判讀的健康狀態", res.Findings)
	default:
		return pass(res, fmt.Sprintf("%d 個 data stream 的 backing shard 均為 green", len(streams)))
	}
	res.Recommendations = []diagnostic.Recommendation{{Desc: "依 data stream 對應的 backing index 檢查未分配 shard 與 allocation decider"}}
	return res
}

func FielddataMemory(snapshot *collector.FielddataSnapshot) diagnostic.Result {
	res := diagnostic.Result{
		ID: "fielddata_memory", Title: "Fielddata 記憶體", Category: "performance", Source: "raw_api",
		Docs: []string{docFielddataGuide, docFielddataStats},
		JudgmentGuide: []diagnostic.JudgmentGuide{
			{Condition: "單次 evictions > 0", Interpretation: "很可能只是正常 cache 管理，不能單獨判定故障"},
			{Condition: "一段時間內 evictions 持續增加", Interpretation: "可能有 cache churn，需觀察"},
			{Condition: "evictions 增加 + JVM Heap 高、GC 或查詢延遲上升", Interpretation: "可能是記憶體壓力"},
			{Condition: "evictions 增加 + circuit breaker 拒絕查詢或節點不穩", Interpretation: "才接近實際故障"},
		},
	}
	if snapshot == nil || len(snapshot.Nodes) == 0 {
		return unknownStatic(res, "Fielddata Nodes Stats 不可用", nil)
	}
	res.Measurements = append(res.Measurements,
		gauge("elasticsearch.nodes.fielddata.total", float64(snapshot.Coverage.Total), "count", "", "", "", ""),
		gauge("elasticsearch.nodes.fielddata.successful", float64(snapshot.Coverage.Successful), "count", "", "", "", ""),
		gauge("elasticsearch.nodes.fielddata.failed", float64(snapshot.Coverage.Failed), "count", "", "", "", ""),
		gauge("elasticsearch.nodes.fielddata.returned", float64(snapshot.Coverage.Returned), "count", "", "", "", ""),
	)
	coverage := fmt.Sprintf("Nodes Stats: successful=%d/%d failed=%d returned=%d", snapshot.Coverage.Successful, snapshot.Coverage.Total, snapshot.Coverage.Failed, snapshot.Coverage.Returned)
	if !snapshot.Coverage.Complete() {
		return unknownStatic(res, "Fielddata Nodes Stats 回應不完整，無法判定所有節點", []string{coverage})
	}
	var totalMemory int64
	var evicted []string
	res.Findings = append(res.Findings, coverage)
	for _, node := range snapshot.Nodes {
		name := node.Name
		if name == "" {
			name = node.ID
		}
		if node.MemoryBytes < 0 || node.Evictions < 0 {
			return unknownStatic(res, "Fielddata Nodes Stats 含無效負值，無法判定", []string{name})
		}
		res.Measurements = append(res.Measurements,
			gauge("elasticsearch.node.fielddata.memory", float64(node.MemoryBytes), "bytes", "node", node.ID, name, ""),
			counter("elasticsearch.node.fielddata.evictions", float64(node.Evictions), "count", "node", node.ID, name, ""),
		)
		totalMemory += node.MemoryBytes
		if node.MemoryBytes > 0 || node.Evictions > 0 {
			finding := fmt.Sprintf("%s：memory=%s evictions=%d", name, formatBytes(node.MemoryBytes), node.Evictions)
			res.Findings = append(res.Findings, finding)
			if node.Evictions > 0 {
				evicted = append(evicted, finding)
			}
		}
	}
	res.Measurements = append(res.Measurements, gauge("elasticsearch.fielddata.memory", float64(totalMemory), "bytes", "", "", "", ""))
	if len(evicted) == 0 {
		return pass(res, fmt.Sprintf("Fielddata cache 目前使用 %s，未記錄 eviction", formatBytes(totalMemory)))
	}
	res.Status, res.Conclusion = diagnostic.StatusInfo, diagnostic.ConclusionNormal
	res.Summary = fmt.Sprintf("%d 個節點曾發生 fielddata cache eviction，需觀察", len(evicted))
	res.RequiresExtra = true
	res.ExtraReason = "evictions 是節點啟動以來的累積值；需以前後兩次採集差值確認是否持續增加"
	res.Recommendations = []diagnostic.Recommendation{{Desc: "對照 JVM 壓力與查詢時間序列，檢查高基數聚合、text fielddata 與不必要的欄位載入"}}
	return res
}
