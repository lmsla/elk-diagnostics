package analyzer

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"elk-diagnostics/internal/collector"
	"elk-diagnostics/internal/diagnostic"
	"elk-diagnostics/rules"
)

const (
	docPendingTasks = "https://www.elastic.co/guide/en/elasticsearch/reference/current/cluster-pending.html"
	docTasks        = "https://www.elastic.co/docs/api/doc/elasticsearch/operation/operation-tasks-list"
	docShardSizing  = "https://www.elastic.co/guide/en/elasticsearch/reference/current/size-your-shards.html"
	docSLMPolicy    = "https://www.elastic.co/docs/api/doc/elasticsearch/operation/operation-slm-get-lifecycle"
	docRuntimeInfo  = "https://www.elastic.co/guide/en/elasticsearch/reference/current/cluster-nodes-info.html"
	docSSLCerts     = "https://www.elastic.co/guide/en/elasticsearch/reference/current/security-api-ssl.html"
	docLicense      = "https://www.elastic.co/guide/en/elasticsearch/reference/current/get-license.html"
	docAwareness    = "https://www.elastic.co/docs/deploy-manage/distributed-architecture/shard-allocation-relocation-recovery/shard-allocation-awareness"
)

func PendingClusterTasks(tasks []collector.PendingClusterTask, t rules.Thresholds) diagnostic.Result {
	warnMillis := int64(t.StaticHealth.PendingTaskWarnSeconds) * 1000
	critMillis := int64(t.StaticHealth.PendingTaskCritSeconds) * 1000
	res := diagnostic.Result{ID: "cluster_pending_tasks", Title: "Cluster pending tasks", Category: "cluster", Source: "raw_api", Docs: []string{docPendingTasks}}
	var critical, warning []string
	var maxQueueMillis int64
	for _, task := range tasks {
		if task.QueueTimeMillis > maxQueueMillis {
			maxQueueMillis = task.QueueTimeMillis
		}
		finding := fmt.Sprintf("priority=%s queue=%s executing=%t source=%s", task.Priority, formatDurationMillis(task.QueueTimeMillis), task.CurrentlyExecuting, task.Source)
		switch {
		case task.QueueTimeMillis >= critMillis:
			critical = append(critical, finding)
		case task.QueueTimeMillis >= warnMillis:
			warning = append(warning, finding)
		}
	}
	res.Measurements = append(res.Measurements,
		gauge("elasticsearch.cluster.pending_task.count", float64(len(tasks)), "count", "", "", "", ""),
		gauge("elasticsearch.cluster.pending_task.max_queue_time", float64(maxQueueMillis), "milliseconds", "", "", "", ""),
	)
	res.Findings = append(critical, warning...)
	switch {
	case len(critical) > 0:
		res.Status, res.Conclusion = diagnostic.StatusCritical, diagnostic.ConclusionConfirmed
		res.Summary = fmt.Sprintf("%d 個 cluster task 排隊至少 %d 秒", len(critical), t.StaticHealth.PendingTaskCritSeconds)
	case len(warning) > 0:
		res.Status, res.Conclusion = diagnostic.StatusWarning, diagnostic.ConclusionSuspected
		res.Summary = fmt.Sprintf("%d 個 cluster task 排隊至少 %d 秒", len(warning), t.StaticHealth.PendingTaskWarnSeconds)
	default:
		return pass(res, fmt.Sprintf("無 cluster task 排隊超過 %d 秒", t.StaticHealth.PendingTaskWarnSeconds))
	}
	res.Recommendations = []diagnostic.Recommendation{{Cmd: "GET /_cluster/pending_tasks", Desc: "依 priority/source 定位 mapping、ILM、shard-started 等 cluster state 壅塞來源"}}
	return res
}

func LongRunningTasks(tasks []collector.RunningTask, t rules.Thresholds) diagnostic.Result {
	warnNanos := int64(t.StaticHealth.LongTaskWarnSeconds) * int64(time.Second)
	res := diagnostic.Result{ID: "long_running_tasks", Title: "長時間執行 task", Category: "performance", Source: "raw_api", Docs: []string{docTasks}}
	var hits []string
	var maxRunningNanos int64
	for _, task := range tasks {
		if strings.HasPrefix(task.Action, "cluster:monitor/tasks/lists") || task.RunningNanos < warnNanos {
			continue
		}
		if task.RunningNanos > maxRunningNanos {
			maxRunningNanos = task.RunningNanos
		}
		description := task.Description
		if len(description) > 160 {
			description = description[:160] + "…"
		}
		finding := fmt.Sprintf("%s node=%s action=%s running=%s cancellable=%t", task.ID, task.Node, task.Action, formatDurationMillis(task.RunningNanos/int64(time.Millisecond)), task.Cancellable)
		if description != "" {
			finding += " description=" + description
		}
		hits = append(hits, finding)
	}
	res.Measurements = append(res.Measurements,
		gauge("elasticsearch.task.long_running.count", float64(len(hits)), "count", "", "", "", ""),
		gauge("elasticsearch.task.long_running.max_duration", float64(maxRunningNanos), "nanoseconds", "", "", "", ""),
	)
	if len(hits) == 0 {
		return pass(res, fmt.Sprintf("無 task 執行超過 %d 秒", t.StaticHealth.LongTaskWarnSeconds))
	}
	res.Status, res.Conclusion = diagnostic.StatusWarning, diagnostic.ConclusionSuspected
	res.Summary = fmt.Sprintf("%d 個 task 已執行至少 %d 秒", len(hits), t.StaticHealth.LongTaskWarnSeconds)
	res.Findings = hits
	res.RequiresExtra = true
	res.ExtraReason = "長時間 reindex、snapshot 或查詢可能是合法操作；需對照進度、發起人與維護窗口"
	res.Recommendations = []diagnostic.Recommendation{{Desc: "確認 task 是否仍有進度；只有 cancellable 且確認不再需要時才人工取消"}}
	return res
}

func ShardSizing(shards []collector.ShardSize, t rules.Thresholds) diagnostic.Result {
	largeBytes := int64(t.StaticHealth.ShardLargeWarnGB) * 1024 * 1024 * 1024
	smallBytes := int64(t.StaticHealth.ShardSmallMaxMB) * 1024 * 1024
	res := diagnostic.Result{ID: "shard_sizing", Title: "Shard 大小規劃", Category: "capacity", Source: "raw_api", Docs: []string{docShardSizing}}
	var large, small []string
	primaryCount := 0
	var maxStoreBytes int64
	for _, shard := range shards {
		if !shard.Primary {
			continue
		}
		primaryCount++
		if shard.StoreBytes > maxStoreBytes {
			maxStoreBytes = shard.StoreBytes
		}
		switch {
		case shard.StoreBytes >= largeBytes:
			large = append(large, fmt.Sprintf("%s shard=%d store=%s docs=%d node=%s", shard.Index, shard.Shard, formatBytes(shard.StoreBytes), shard.Docs, shard.Node))
		case shard.StoreBytes <= smallBytes:
			small = append(small, fmt.Sprintf("%s shard=%d store=%s docs=%d", shard.Index, shard.Shard, formatBytes(shard.StoreBytes), shard.Docs))
		}
	}
	res.Measurements = append(res.Measurements,
		gauge("elasticsearch.shard.primary.count", float64(primaryCount), "count", "", "", "", ""),
		gauge("elasticsearch.shard.primary.max_store", float64(maxStoreBytes), "bytes", "", "", "", ""),
		gauge("elasticsearch.shard.primary.large.count", float64(len(large)), "count", "", "", "", ""),
		gauge("elasticsearch.shard.primary.small.count", float64(len(small)), "count", "", "", "", ""),
	)
	if len(large) == 0 && len(small) < t.StaticHealth.ShardSmallCountWarn {
		return pass(res, fmt.Sprintf("無 primary shard ≥%d GiB，且小 shard 數量低於 %d", t.StaticHealth.ShardLargeWarnGB, t.StaticHealth.ShardSmallCountWarn))
	}
	res.Status, res.Conclusion = diagnostic.StatusWarning, diagnostic.ConclusionSuspected
	res.Findings = append(res.Findings, large...)
	if len(small) >= t.StaticHealth.ShardSmallCountWarn {
		limit := len(small)
		if limit > 20 {
			limit = 20
		}
		res.Findings = append(res.Findings, small[:limit]...)
	}
	parts := []string{}
	if len(large) > 0 {
		parts = append(parts, fmt.Sprintf("%d 個大型 primary shard", len(large)))
	}
	if len(small) >= t.StaticHealth.ShardSmallCountWarn {
		parts = append(parts, fmt.Sprintf("%d 個 ≤%d MiB 的小 primary shard", len(small), t.StaticHealth.ShardSmallMaxMB))
	}
	res.Summary = strings.Join(parts, "；")
	res.RequiresExtra = true
	res.ExtraReason = "shard size 是 sizing heuristic，不是硬限制；需依資料量、硬體與 recovery 目標校正"
	res.Recommendations = []diagnostic.Recommendation{{Desc: "以 rollover、shrink 或 reindex 調整 shard 大小；先在代表性 workload 驗證"}}
	return res
}

func SnapshotFreshness(policies []collector.SLMPolicy, t rules.Thresholds, now time.Time) diagnostic.Result {
	res := diagnostic.Result{ID: "snapshot_freshness", Title: "Snapshot 新鮮度 / RPO", Category: "snapshot", Source: "raw_api", Docs: []string{docSLMPolicy}}
	res.Measurements = append(res.Measurements, gauge("elasticsearch.slm.freshness.evaluated_policy.count", float64(len(policies)), "count", "", "", "", ""))
	if len(policies) == 0 {
		res.Status, res.Conclusion = diagnostic.StatusSkipped, diagnostic.ConclusionNormal
		res.Summary = "未設定 SLM policy；無法由 SLM API 判斷外部或手動備份"
		return res
	}
	nowMillis := now.UnixMilli()
	warnAge := int64(t.StaticHealth.SnapshotWarnHours) * int64(time.Hour/time.Millisecond)
	critAge := int64(t.StaticHealth.SnapshotCritHours) * int64(time.Hour/time.Millisecond)
	var critical, warning []string
	for _, policy := range policies {
		res.Measurements = append(res.Measurements,
			counter("elasticsearch.slm.snapshot.taken", float64(policy.SnapshotsTaken), "count", "slm_policy", policy.Name, policy.Name, ""),
			counter("elasticsearch.slm.snapshot.failed", float64(policy.SnapshotsFailed), "count", "slm_policy", policy.Name, policy.Name, ""),
		)
		if policy.LastSuccessMillis > 0 {
			res.Measurements = append(res.Measurements, gauge("elasticsearch.slm.snapshot.last_success_age", float64(nowMillis-policy.LastSuccessMillis)/float64(time.Hour/time.Millisecond), "hours", "slm_policy", policy.Name, policy.Name, ""))
		}
		switch {
		case policy.LastSuccessMillis == 0:
			finding := fmt.Sprintf("%s：尚無成功 snapshot（taken=%d failed=%d next=%s）", policy.Name, policy.SnapshotsTaken, policy.SnapshotsFailed, formatEpochMillis(policy.NextExecutionMillis))
			if policy.SnapshotsFailed > 0 || (policy.NextExecutionMillis > 0 && policy.NextExecutionMillis < nowMillis) {
				critical = append(critical, finding)
			} else {
				warning = append(warning, finding)
			}
		case policy.LastFailureMillis > policy.LastSuccessMillis:
			critical = append(critical, fmt.Sprintf("%s：最後失敗 %s 晚於最後成功 %s", policy.Name, formatEpochMillis(policy.LastFailureMillis), formatEpochMillis(policy.LastSuccessMillis)))
		default:
			age := nowMillis - policy.LastSuccessMillis
			finding := fmt.Sprintf("%s：最後成功 %s（%s前）snapshot=%s", policy.Name, formatEpochMillis(policy.LastSuccessMillis), formatDurationMillis(age), policy.LastSuccessSnapshot)
			switch {
			case age >= critAge:
				critical = append(critical, finding)
			case age >= warnAge:
				warning = append(warning, finding)
			}
		}
	}
	res.Findings = append(critical, warning...)
	switch {
	case len(critical) > 0:
		res.Status, res.Conclusion = diagnostic.StatusCritical, diagnostic.ConclusionConfirmed
		res.Summary = fmt.Sprintf("%d 個 SLM policy 未達預設 RPO 或最後執行失敗", len(critical))
	case len(warning) > 0:
		res.Status, res.Conclusion = diagnostic.StatusWarning, diagnostic.ConclusionSuspected
		res.Summary = fmt.Sprintf("%d 個 SLM policy 需要確認首次執行或 snapshot 新鮮度", len(warning))
	default:
		return pass(res, fmt.Sprintf("各 SLM policy 最近成功時間均在 %d 小時內", t.StaticHealth.SnapshotWarnHours))
	}
	res.RequiresExtra = true
	res.ExtraReason = "預設 RPO 是工具 heuristic；應依使用者的正式備份政策覆寫門檻"
	res.Recommendations = []diagnostic.Recommendation{{Desc: "確認 repository 可用性、policy schedule、最近失敗原因與必要 index/feature state 是否納入"}}
	return res
}

func NodeRuntimeConsistency(snapshot *collector.NodeRuntimeSnapshot) diagnostic.Result {
	res := diagnostic.Result{ID: "node_runtime_consistency", Title: "Node runtime 一致性", Category: "node", Source: "raw_api", Docs: []string{docRuntimeInfo}}
	if snapshot == nil || len(snapshot.Nodes) == 0 {
		return unknownStatic(res, "Nodes runtime 資料不可用", nil)
	}
	coverageFinding := fmt.Sprintf("Nodes Info: successful=%d/%d failed=%d returned=%d", snapshot.Coverage.Successful, snapshot.Coverage.Total, snapshot.Coverage.Failed, snapshot.Coverage.Returned)
	res.Measurements = append(res.Measurements,
		gauge("elasticsearch.nodes.runtime.total", float64(snapshot.Coverage.Total), "count", "", "", "", ""),
		gauge("elasticsearch.nodes.runtime.successful", float64(snapshot.Coverage.Successful), "count", "", "", "", ""),
		gauge("elasticsearch.nodes.runtime.failed", float64(snapshot.Coverage.Failed), "count", "", "", "", ""),
		gauge("elasticsearch.nodes.runtime.returned", float64(snapshot.Coverage.Returned), "count", "", "", "", ""),
	)
	if !snapshot.Coverage.Complete() {
		return unknownStatic(res, "Nodes runtime API 回應不完整，無法判斷全叢集一致性", []string{coverageFinding})
	}
	var findings []string
	base := snapshot.Nodes[0]
	for _, node := range snapshot.Nodes {
		res.Measurements = append(res.Measurements,
			gauge("elasticsearch.node.runtime.heap_init", float64(node.HeapInitBytes), "bytes", "node", node.ID, node.Name, ""),
			gauge("elasticsearch.node.runtime.heap_max", float64(node.HeapMaxBytes), "bytes", "node", node.ID, node.Name, ""),
			gauge("elasticsearch.node.runtime.plugin.count", float64(len(node.Plugins)), "count", "node", node.ID, node.Name, ""),
		)
	}
	for _, node := range snapshot.Nodes[1:] {
		if node.ESVersion != base.ESVersion || node.BuildHash != base.BuildHash {
			findings = append(findings, fmt.Sprintf("版本漂移：%s=%s/%s，%s=%s/%s", base.Name, base.ESVersion, shortHash(base.BuildHash), node.Name, node.ESVersion, shortHash(node.BuildHash)))
		}
	}
	groups := map[string][]collector.NodeRuntime{}
	for _, node := range snapshot.Nodes {
		groups[strings.Join(node.Roles, ",")] = append(groups[strings.Join(node.Roles, ",")], node)
	}
	for roleSet, nodes := range groups {
		if len(nodes) < 2 {
			continue
		}
		groupBase := nodes[0]
		for _, node := range nodes[1:] {
			if node.JVMVersion != groupBase.JVMVersion || node.VMVersion != groupBase.VMVersion {
				findings = append(findings, fmt.Sprintf("JDK 漂移（roles=%s）：%s=%s，%s=%s", roleSet, groupBase.Name, groupBase.JVMVersion, node.Name, node.JVMVersion))
			}
			if node.HeapInitBytes != groupBase.HeapInitBytes || node.HeapMaxBytes != groupBase.HeapMaxBytes {
				findings = append(findings, fmt.Sprintf("heap 漂移（roles=%s）：%s init/max=%s/%s，%s=%s/%s", roleSet, groupBase.Name, formatBytes(groupBase.HeapInitBytes), formatBytes(groupBase.HeapMaxBytes), node.Name, formatBytes(node.HeapInitBytes), formatBytes(node.HeapMaxBytes)))
			}
			if pluginSignature(node.Plugins) != pluginSignature(groupBase.Plugins) {
				findings = append(findings, fmt.Sprintf("plugin 漂移（roles=%s）：%s=[%s]，%s=[%s]", roleSet, groupBase.Name, pluginSignature(groupBase.Plugins), node.Name, pluginSignature(node.Plugins)))
			}
		}
	}
	if len(findings) == 0 {
		res = pass(res, fmt.Sprintf("%d 個節點版本一致；相同角色節點的 JDK/heap/plugin 無漂移", len(snapshot.Nodes)))
		res.Findings = []string{coverageFinding}
		return res
	}
	res.Status, res.Conclusion = diagnostic.StatusWarning, diagnostic.ConclusionSuspected
	res.Summary = fmt.Sprintf("偵測到 %d 項 node runtime 漂移", len(findings))
	res.Findings = append([]string{coverageFinding}, findings...)
	res.RequiresExtra = true
	res.ExtraReason = "rolling upgrade 或異質硬體可能造成短暫／刻意差異；需對照部署基準線"
	res.Recommendations = []diagnostic.Recommendation{{Desc: "確認 rolling upgrade 是否完成，並比對相同角色節點的映像、JDK、heap 與 plugin 基準"}}
	return res
}

func TLSCertificateExpiry(certs []collector.TLSCertificate, t rules.Thresholds, now time.Time) diagnostic.Result {
	res := diagnostic.Result{ID: "tls_certificate_expiry", Title: "TLS 憑證到期", Category: "security", Source: "raw_api", Docs: []string{docSSLCerts}}
	res.Measurements = append(res.Measurements, gauge("elasticsearch.tls.certificate.count", float64(len(certs)), "count", "", "", "", ""))
	if len(certs) == 0 {
		res.Status, res.Conclusion = diagnostic.StatusSkipped, diagnostic.ConclusionNormal
		res.Summary = "回應節點未回傳 Elasticsearch TLS certificate context"
		return res
	}
	warnBefore := now.Add(time.Duration(t.StaticHealth.ExpiryWarnDays) * 24 * time.Hour)
	var critical, warning, parseFailures []string
	for _, cert := range certs {
		expiry, err := time.Parse(time.RFC3339Nano, cert.Expiry)
		if err != nil {
			parseFailures = append(parseFailures, fmt.Sprintf("subject=%s expiry=%q 無法解析", cert.Subject, cert.Expiry))
			continue
		}
		name := cert.Alias
		if name == "" {
			name = cert.Subject
		}
		if name == "" {
			name = cert.Path
		}
		res.Measurements = append(res.Measurements, gauge("elasticsearch.tls.certificate.days_remaining", expiry.Sub(now).Hours()/24, "days", "certificate", name, name, ""))
		finding := fmt.Sprintf("subject=%s issuer=%s expiry=%s private_key=%t path=%s", cert.Subject, cert.Issuer, expiry.UTC().Format(time.RFC3339), cert.HasPrivateKey, cert.Path)
		switch {
		case !expiry.After(now) && cert.HasPrivateKey:
			critical = append(critical, finding)
		case !expiry.After(now):
			warning = append(warning, "已過期 trust certificate："+finding)
		case !expiry.After(warnBefore):
			warning = append(warning, finding)
		}
	}
	res.Findings = append(append(critical, warning...), parseFailures...)
	switch {
	case len(critical) > 0:
		res.Status, res.Conclusion = diagnostic.StatusCritical, diagnostic.ConclusionConfirmed
		res.Summary = fmt.Sprintf("%d 張 identity certificate 已過期", len(critical))
	case len(warning) > 0:
		res.Status, res.Conclusion = diagnostic.StatusWarning, diagnostic.ConclusionSuspected
		res.Summary = fmt.Sprintf("%d 張 certificate 已過期或將於 %d 天內到期", len(warning), t.StaticHealth.ExpiryWarnDays)
	case len(parseFailures) > 0:
		return unknownStatic(res, "部分 certificate 到期日無法解析", parseFailures)
	default:
		res = pass(res, fmt.Sprintf("本次 API 回傳的憑證皆距到期超過 %d 天", t.StaticHealth.ExpiryWarnDays))
	}
	res.RequiresExtra = true
	res.ExtraReason = "/_ssl/certificates 只回報收到請求的 Elasticsearch 節點；完整叢集需逐節點採集"
	res.Recommendations = []diagnostic.Recommendation{{Desc: "在到期前更新 HTTP/transport/realm 憑證，並確認所有節點已載入新憑證"}}
	return res
}

func LicenseHealth(info collector.LicenseInfo, t rules.Thresholds, now time.Time) diagnostic.Result {
	res := diagnostic.Result{ID: "license_expiry", Title: "License 狀態與到期", Category: "management", Source: "raw_api", Docs: []string{docLicense}}
	if info.Status == "" {
		return unknownStatic(res, "License API 未提供狀態", nil)
	}
	if info.ExpiryMillis > 0 {
		expiry := time.UnixMilli(info.ExpiryMillis)
		res.Measurements = append(res.Measurements, gauge("elasticsearch.license.days_remaining", expiry.Sub(now).Hours()/24, "days", "", "", "", info.Type))
	}
	if info.Status == "expired" || info.Status == "invalid" {
		res.Status, res.Conclusion = diagnostic.StatusCritical, diagnostic.ConclusionConfirmed
		res.Summary = fmt.Sprintf("License status=%s type=%s", info.Status, info.Type)
		return res
	}
	if info.ExpiryMillis <= 0 {
		return pass(res, fmt.Sprintf("License status=%s type=%s，無到期日", info.Status, info.Type))
	}
	expiry := time.UnixMilli(info.ExpiryMillis)
	finding := fmt.Sprintf("type=%s status=%s issued_to=%s expiry=%s", info.Type, info.Status, info.IssuedTo, expiry.UTC().Format(time.RFC3339))
	res.Findings = []string{finding}
	if !expiry.After(now) {
		res.Status, res.Conclusion = diagnostic.StatusCritical, diagnostic.ConclusionConfirmed
		res.Summary = "License 已到期"
	} else if !expiry.After(now.Add(time.Duration(t.StaticHealth.ExpiryWarnDays) * 24 * time.Hour)) {
		res.Status, res.Conclusion = diagnostic.StatusWarning, diagnostic.ConclusionConfirmed
		res.Summary = fmt.Sprintf("License 將於 %d 天內到期", t.StaticHealth.ExpiryWarnDays)
	} else {
		return pass(res, fmt.Sprintf("License status=%s，超過 %d 天後到期", info.Status, t.StaticHealth.ExpiryWarnDays))
	}
	res.Recommendations = []diagnostic.Recommendation{{Desc: "確認續約或降級後仍需使用的功能，避免到期時服務能力改變"}}
	return res
}

func ReplicaCoverage(indices []collector.IndexReplica) diagnostic.Result {
	res := diagnostic.Result{ID: "replica_resilience", Title: "Index replica 容錯", Category: "cluster", Source: "raw_api", Docs: []string{docRedYellow}}
	res.Measurements = append(res.Measurements, gauge("elasticsearch.index.replica.evaluated.count", float64(len(indices)), "count", "", "", "", ""))
	if len(indices) == 0 {
		return pass(res, "未找到非系統 index")
	}
	var noReplica, unsafeAutoExpand []string
	minReplicas := 0
	if len(indices) > 0 {
		minReplicas = indices[0].Replicas
	}
	for _, index := range indices {
		if index.Replicas < minReplicas {
			minReplicas = index.Replicas
		}
		if index.Replicas == 0 {
			noReplica = append(noReplica, fmt.Sprintf("%s：number_of_replicas=0 auto_expand=%s", index.Index, valueOr(index.AutoExpand, "未設定")))
		}
		if strings.HasSuffix(strings.ToLower(index.AutoExpand), "-all") {
			unsafeAutoExpand = append(unsafeAutoExpand, fmt.Sprintf("%s：auto_expand_replicas=%s（上限 all 會忽略 allocation awareness）", index.Index, index.AutoExpand))
		}
	}
	res.Measurements = append(res.Measurements,
		gauge("elasticsearch.index.replica.minimum", float64(minReplicas), "count", "", "", "", ""),
		gauge("elasticsearch.index.replica.zero.count", float64(len(noReplica)), "count", "", "", "", ""),
		gauge("elasticsearch.index.replica.auto_expand_all.count", float64(len(unsafeAutoExpand)), "count", "", "", "", ""),
	)
	if len(noReplica) == 0 && len(unsafeAutoExpand) == 0 {
		return pass(res, "所有非系統 index 目前至少有 1 個 replica")
	}
	res.Status, res.Conclusion = diagnostic.StatusWarning, diagnostic.ConclusionSuspected
	parts := []string{}
	if len(noReplica) > 0 {
		parts = append(parts, fmt.Sprintf("%d 個非系統 index 沒有 replica", len(noReplica)))
	}
	if len(unsafeAutoExpand) > 0 {
		parts = append(parts, fmt.Sprintf("%d 個 index 的 auto-expand 上限為 all", len(unsafeAutoExpand)))
	}
	res.Summary = strings.Join(parts, "；")
	res.Findings = append(noReplica, unsafeAutoExpand...)
	res.RequiresExtra = true
	res.ExtraReason = "單節點、可重建資料或成本考量可能刻意無 replica；auto-expand 上限 all 另會忽略 allocation awareness，需依 RPO 與 failure domain 基準判斷"
	res.Recommendations = []diagnostic.Recommendation{{Desc: "確認資料可重建性與節點數；需要高可用時設定 replica 並確保可配置到其他 failure domain"}}
	return res
}

func AllocationAwareness(attributes []string, snapshot *collector.NodeTopologySnapshot) diagnostic.Result {
	res := diagnostic.Result{ID: "allocation_awareness", Title: "Shard allocation awareness", Category: "cluster", Source: "raw_api", Docs: []string{docAwareness}}
	res.Measurements = append(res.Measurements, gauge("elasticsearch.allocation.awareness.attribute.count", float64(len(attributes)), "count", "", "", "", ""))
	if len(attributes) == 0 {
		res.Status, res.Conclusion = diagnostic.StatusSkipped, diagnostic.ConclusionNormal
		res.Summary = "未配置 allocation awareness；工具不知道此叢集是否要求跨 failure domain"
		return res
	}
	if snapshot == nil || !snapshot.Coverage.Complete() {
		var findings []string
		if snapshot != nil {
			findings = []string{fmt.Sprintf("Nodes Info: successful=%d/%d failed=%d returned=%d", snapshot.Coverage.Successful, snapshot.Coverage.Total, snapshot.Coverage.Failed, snapshot.Coverage.Returned)}
		}
		return unknownStatic(res, "Nodes topology 回應不完整，無法驗證全叢集 allocation awareness", findings)
	}
	var dataNodes []collector.TopologyNode
	for _, node := range snapshot.Nodes {
		if hasDataRole(node.Roles) {
			dataNodes = append(dataNodes, node)
		}
	}
	if len(dataNodes) == 0 {
		return unknownStatic(res, "找不到 data node，無法驗證 allocation awareness", nil)
	}
	res.Measurements = append(res.Measurements, gauge("elasticsearch.allocation.awareness.data_node.count", float64(len(dataNodes)), "count", "", "", "", ""))
	var findings []string
	for _, attr := range attributes {
		values := map[string]bool{}
		missingCount := 0
		for _, node := range dataNodes {
			value := strings.TrimSpace(node.Attributes[attr])
			if value == "" {
				missingCount++
				findings = append(findings, fmt.Sprintf("%s 缺少 node.attr.%s", node.Name, attr))
				continue
			}
			values[value] = true
		}
		res.Measurements = append(res.Measurements,
			gauge("elasticsearch.allocation.awareness.value.count", float64(len(values)), "count", "", "", "", attr),
			gauge("elasticsearch.allocation.awareness.missing_node.count", float64(missingCount), "count", "", "", "", attr),
		)
		if len(values) < 2 {
			findings = append(findings, fmt.Sprintf("awareness attribute %s 只有 %d 個值，無法形成跨 failure domain 冗餘", attr, len(values)))
		}
	}
	if len(findings) == 0 {
		res = pass(res, fmt.Sprintf("%d 個 data node 均具備 awareness attributes %v，且各 attribute 至少有兩個值", len(dataNodes), attributes))
		res.RequiresExtra = true
		res.ExtraReason = "設定與節點屬性正常不等於每個 shard copy 已跨 zone；placement 驗證將於 ES-GAP-06 第二階段補齊"
		return res
	}
	res.Status, res.Conclusion = diagnostic.StatusWarning, diagnostic.ConclusionConfirmed
	res.Summary = fmt.Sprintf("allocation awareness 配置不完整（%d 項）", len(findings))
	res.Findings = findings
	res.Recommendations = []diagnostic.Recommendation{{Desc: "補齊所有 data node 的 awareness attribute，並確認各 failure domain 容量足以配置 primary/replica"}}
	return res
}

func unknownStatic(res diagnostic.Result, summary string, findings []string) diagnostic.Result {
	res.Status, res.Conclusion, res.Summary = diagnostic.StatusUnknown, diagnostic.ConclusionNormal, summary
	res.Findings = findings
	return res
}

func formatDurationMillis(ms int64) string {
	if ms < 0 {
		ms = 0
	}
	return (time.Duration(ms) * time.Millisecond).Round(time.Second).String()
}

func formatEpochMillis(ms int64) string {
	if ms <= 0 {
		return "不可得"
	}
	return time.UnixMilli(ms).UTC().Format(time.RFC3339)
}

func shortHash(hash string) string {
	if len(hash) > 8 {
		return hash[:8]
	}
	return hash
}

func pluginSignature(plugins []collector.NodePlugin) string {
	items := make([]string, 0, len(plugins))
	for _, plugin := range plugins {
		items = append(items, plugin.Name+"@"+plugin.Version)
	}
	sort.Strings(items)
	return strings.Join(items, ",")
}

func valueOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func hasDataRole(roles []string) bool {
	for _, role := range roles {
		if role == "data" || strings.HasPrefix(role, "data_") {
			return true
		}
	}
	return false
}
