package analyzer

import (
	"fmt"
	"sort"
	"strings"

	"elk-diagnostics/internal/collector"
	"elk-diagnostics/internal/diagnostic"
	"elk-diagnostics/rules"
)

const (
	docIndexingPressure = "https://www.elastic.co/guide/en/elasticsearch/reference/current/index-modules-indexing-pressure.html"
	docIndexBlocks      = "https://www.elastic.co/guide/en/elasticsearch/reference/current/index-modules-blocks.html"
	docCCRStats         = "https://www.elastic.co/docs/api/doc/elasticsearch/operation/operation-ccr-stats"
	docMLJobStats       = "https://www.elastic.co/docs/api/doc/elasticsearch/operation/operation-ml-get-job-stats"
	docMLDatafeedStats  = "https://www.elastic.co/docs/api/doc/elasticsearch/operation/operation-ml-get-datafeed-stats"
	docPlannedShutdown  = "https://www.elastic.co/docs/api/doc/elasticsearch/operation/operation-shutdown-get-node"
	docVotingExclusions = "https://www.elastic.co/docs/api/doc/elasticsearch/operation/operation-cluster-state"
)

func IndexingPressure(snapshot *collector.IndexingPressureSnapshot, t rules.Thresholds) diagnostic.Result {
	res := diagnostic.Result{ID: "indexing_pressure", Title: "Indexing pressure 當下使用量", Category: "performance", Source: "raw_api", Docs: []string{docIndexingPressure}}
	if snapshot == nil || len(snapshot.Nodes) == 0 {
		return unknownStatic(res, "Indexing pressure 資料不可用", nil)
	}
	coverage := fmt.Sprintf("Nodes Stats: successful=%d/%d failed=%d returned=%d", snapshot.Coverage.Successful, snapshot.Coverage.Total, snapshot.Coverage.Failed, snapshot.Coverage.Returned)
	if !snapshot.Coverage.Complete() {
		return unknownStatic(res, "Indexing pressure Nodes API 回應不完整，無法判定所有節點", []string{coverage})
	}
	var critical, warning, missing []string
	for _, node := range snapshot.Nodes {
		name := node.Name
		if name == "" {
			name = node.ID
		}
		if node.LimitBytes == nil || *node.LimitBytes <= 0 || node.CombinedCoordinatingPrimary == nil || node.ReplicaBytes == nil {
			missing = append(missing, name+"：combined/replica/limit 欄位不完整")
			continue
		}
		combinedPct := int(100 * float64(*node.CombinedCoordinatingPrimary) / float64(*node.LimitBytes))
		// Elastic 的 replica rejection 上限是 coordinating+primary limit 的 1.5 倍。
		replicaPct := int(100 * float64(*node.ReplicaBytes) / (1.5 * float64(*node.LimitBytes)))
		pct := combinedPct
		if replicaPct > pct {
			pct = replicaPct
		}
		finding := fmt.Sprintf("%s：combined=%s/%s（%d%%），replica=%s/%s（%d%%）", name,
			formatBytes(*node.CombinedCoordinatingPrimary), formatBytes(*node.LimitBytes), combinedPct,
			formatBytes(*node.ReplicaBytes), formatBytes(int64(1.5*float64(*node.LimitBytes))), replicaPct)
		switch {
		case pct >= t.StaticHealth.IndexingPressureCritPct:
			critical = append(critical, finding)
		case pct >= t.StaticHealth.IndexingPressureWarnPct:
			warning = append(warning, finding)
		}
	}
	res.Findings = append([]string{coverage}, append(append(critical, warning...), missing...)...)
	switch {
	case len(critical) > 0:
		res.Status, res.Conclusion = diagnostic.StatusCritical, diagnostic.ConclusionSuspected
		res.Summary = fmt.Sprintf("%d 個節點 indexing pressure 已達拒絕上限的 %d%%", len(critical), t.StaticHealth.IndexingPressureCritPct)
	case len(warning) > 0:
		res.Status, res.Conclusion = diagnostic.StatusWarning, diagnostic.ConclusionSuspected
		res.Summary = fmt.Sprintf("%d 個節點 indexing pressure 已達拒絕上限的 %d%%", len(warning), t.StaticHealth.IndexingPressureWarnPct)
	case len(missing) > 0:
		return unknownStatic(res, "部分節點缺少 indexing pressure 欄位，無法完整判定", res.Findings)
	default:
		return pass(res, fmt.Sprintf("各節點當下 indexing pressure 均 <%d%%", t.StaticHealth.IndexingPressureWarnPct))
	}
	res.RequiresExtra = true
	res.ExtraReason = "單次快照只能證明採集當下壓力；是否持續需由 Stack Monitoring 時間序列確認"
	res.Recommendations = []diagnostic.Recommendation{{Desc: "確認 bulk 併發／批次大小、ingest 處理速度與 data node heap；避免只提高保護上限"}}
	return res
}

func IndexReadWriteBlocks(blocks []collector.IndexBlock) diagnostic.Result {
	res := diagnostic.Result{ID: "index_read_write_blocks", Title: "Index read/write blocks", Category: "data", Source: "raw_api", Docs: []string{docIndexBlocks}}
	if len(blocks) == 0 {
		return pass(res, "非系統 index 未設定 read/write/metadata block")
	}
	for _, block := range blocks {
		var kinds []string
		if block.ReadOnly {
			kinds = append(kinds, "read_only")
		}
		if block.ReadOnlyAllowDelete {
			kinds = append(kinds, "read_only_allow_delete（常見於 flood-stage watermark）")
		}
		if block.Read {
			kinds = append(kinds, "read")
		}
		if block.Write {
			kinds = append(kinds, "write")
		}
		if block.Metadata {
			kinds = append(kinds, "metadata")
		}
		res.Findings = append(res.Findings, fmt.Sprintf("%s：%s", block.Index, strings.Join(kinds, ", ")))
	}
	res.Status, res.Conclusion = diagnostic.StatusCritical, diagnostic.ConclusionConfirmed
	res.Summary = fmt.Sprintf("%d 個非系統 index 存在讀寫或 metadata block", len(blocks))
	res.RequiresExtra = true
	res.ExtraReason = "block 可能是維護窗口刻意設定；read_only_allow_delete 也可能由 flood-stage watermark 自動加上"
	res.Recommendations = []diagnostic.Recommendation{{Desc: "先確認磁碟 watermark 與維護脈絡；read_only_allow_delete 應由 Elasticsearch 在磁碟下降後自動解除，不要盲目手動清除"}}
	return res
}

func CCRHealth(stats collector.CCRStats, t rules.Thresholds) diagnostic.Result {
	res := diagnostic.Result{ID: "ccr_health", Title: "CCR follower / auto-follow 健康", Category: "replication", Source: "raw_api", Docs: []string{docCCRStats}}
	if len(stats.Followers) == 0 && stats.FailedFollowIndices == 0 && stats.FailedRemoteStateRequests == 0 && len(stats.RecentAutoFollowErrors) == 0 {
		res.Status, res.Conclusion = diagnostic.StatusSkipped, diagnostic.ConclusionNormal
		res.Summary = "未偵測到 CCR follower 或 auto-follow 活動"
		return res
	}
	var critical, warning []string
	for _, follower := range stats.Followers {
		if len(follower.FatalErrors) > 0 || len(follower.ReadErrors) > 0 {
			critical = append(critical, fmt.Sprintf("%s：fatal=%v read=%v", follower.Index, follower.FatalErrors, follower.ReadErrors))
		}
		if follower.GlobalCheckpointLag >= int64(t.StaticHealth.CCRLagWarnOps) {
			warning = append(warning, fmt.Sprintf("%s：global checkpoint lag=%d", follower.Index, follower.GlobalCheckpointLag))
		}
	}
	if stats.FailedFollowIndices > 0 || stats.FailedRemoteStateRequests > 0 {
		warning = append(warning, fmt.Sprintf("auto-follow 累積失敗：follow_indices=%d remote_cluster_state=%d", stats.FailedFollowIndices, stats.FailedRemoteStateRequests))
	}
	for _, recent := range stats.RecentAutoFollowErrors {
		warning = append(warning, "recent auto-follow error: "+recent)
	}
	res.Findings = append(critical, warning...)
	switch {
	case len(critical) > 0:
		res.Status, res.Conclusion = diagnostic.StatusCritical, diagnostic.ConclusionConfirmed
		res.Summary = fmt.Sprintf("%d 個 CCR follower 出現 fatal/read exception", len(critical))
	case len(warning) > 0:
		res.Status, res.Conclusion = diagnostic.StatusWarning, diagnostic.ConclusionSuspected
		res.Summary = fmt.Sprintf("CCR 有 %d 項 lag 或 auto-follow 異常跡象", len(warning))
	default:
		return pass(res, fmt.Sprintf("%d 個 CCR follower 未見 exception，checkpoint lag <%d", len(stats.Followers), t.StaticHealth.CCRLagWarnOps))
	}
	res.RequiresExtra = true
	res.ExtraReason = "auto-follow failed counter 是累積值，checkpoint lag 是單次絕對值；是否持續惡化需用時間序列佐證"
	res.Recommendations = []diagnostic.Recommendation{{Desc: "檢查 remote cluster 連線、leader index、follower shard exception 與 CCR 權限／license"}}
	return res
}

func CCRFeatureUnavailable() diagnostic.Result {
	res := diagnostic.Result{ID: "ccr_health", Title: "CCR follower / auto-follow 健康", Category: "replication", Source: "raw_api", Docs: []string{docCCRStats}}
	res.Status, res.Conclusion = diagnostic.StatusSkipped, diagnostic.ConclusionNormal
	res.Summary = "目前 license 未啟用 CCR，本項不適用"
	return res
}

func MLHealth(jobs []collector.MLJob, feeds []collector.MLDatafeed) diagnostic.Result {
	res := diagnostic.Result{ID: "ml_jobs_datafeeds", Title: "ML jobs / datafeeds 狀態", Category: "machine_learning", Source: "raw_api", Docs: []string{docMLJobStats, docMLDatafeedStats}}
	if len(jobs) == 0 && len(feeds) == 0 {
		res.Status, res.Conclusion = diagnostic.StatusSkipped, diagnostic.ConclusionNormal
		res.Summary = "未設定 anomaly detection job 或 datafeed"
		return res
	}
	var critical, warning []string
	states := map[string]int{}
	for _, job := range jobs {
		state := strings.ToLower(job.State)
		states["job:"+state]++
		finding := fmt.Sprintf("job %s state=%s", job.ID, job.State)
		if job.AssignmentExplanation != "" {
			finding += " assignment=" + job.AssignmentExplanation
		}
		if state == "failed" {
			critical = append(critical, finding)
		} else if job.AssignmentExplanation != "" {
			warning = append(warning, finding)
		}
	}
	for _, feed := range feeds {
		state := strings.ToLower(feed.State)
		states["datafeed:"+state]++
		finding := fmt.Sprintf("datafeed %s state=%s", feed.ID, feed.State)
		if feed.JobID != "" {
			finding += " job=" + feed.JobID
		}
		if feed.AssignmentExplanation != "" {
			warning = append(warning, finding+" assignment="+feed.AssignmentExplanation)
		}
	}
	res.Findings = append(critical, warning...)
	switch {
	case len(critical) > 0:
		res.Status, res.Conclusion = diagnostic.StatusCritical, diagnostic.ConclusionConfirmed
		res.Summary = fmt.Sprintf("%d 個 ML job 處於 failed", len(critical))
	case len(warning) > 0:
		res.Status, res.Conclusion = diagnostic.StatusWarning, diagnostic.ConclusionSuspected
		res.Summary = fmt.Sprintf("%d 個 ML job/datafeed 有 assignment 說明需確認", len(warning))
	default:
		var summary []string
		for state, count := range states {
			summary = append(summary, fmt.Sprintf("%s=%d", state, count))
		}
		sort.Strings(summary)
		return pass(res, "ML 狀態無 failed／assignment 問題（"+strings.Join(summary, ", ")+"）")
	}
	res.Recommendations = []diagnostic.Recommendation{{Desc: "依 assignment explanation 檢查 ML node 容量、job 設定與 datafeed 查詢；closed/stopped 本身不是故障"}}
	return res
}

func MLFeatureUnavailable() diagnostic.Result {
	res := diagnostic.Result{ID: "ml_jobs_datafeeds", Title: "ML jobs / datafeeds 狀態", Category: "machine_learning", Source: "raw_api", Docs: []string{docMLJobStats, docMLDatafeedStats}}
	res.Status, res.Conclusion = diagnostic.StatusSkipped, diagnostic.ConclusionNormal
	res.Summary = "目前 license 未啟用 Machine Learning，本項不適用"
	return res
}

func PlannedShutdownHealth(nodes []collector.PlannedShutdown) diagnostic.Result {
	res := diagnostic.Result{ID: "planned_shutdown", Title: "Planned shutdown 登記狀態", Category: "cluster", Source: "raw_api", Docs: []string{docPlannedShutdown}}
	if len(nodes) == 0 {
		return pass(res, "沒有已登記的 planned shutdown")
	}
	var critical, warning []string
	for _, node := range nodes {
		finding := fmt.Sprintf("node=%s type=%s status=%s shard_migration=%s remaining=%d persistent_tasks=%s plugins=%s reason=%s",
			node.NodeID, node.Type, node.Status, node.ShardMigrationStatus, node.ShardMigrationsRemaining, node.PersistentTasksStatus, node.PluginsStatus, node.Reason)
		if strings.EqualFold(node.Status, "stalled") || strings.EqualFold(node.ShardMigrationStatus, "stalled") || strings.EqualFold(node.PersistentTasksStatus, "stalled") {
			critical = append(critical, finding)
		} else {
			warning = append(warning, finding)
		}
	}
	res.Findings = append(critical, warning...)
	if len(critical) > 0 {
		res.Status, res.Conclusion = diagnostic.StatusCritical, diagnostic.ConclusionConfirmed
		res.Summary = fmt.Sprintf("%d 個 planned shutdown 登記處於 stalled", len(critical))
	} else {
		res.Status, res.Conclusion = diagnostic.StatusWarning, diagnostic.ConclusionSuspected
		res.Summary = fmt.Sprintf("目前有 %d 個 planned shutdown 登記，需確認是否仍在維護窗口", len(nodes))
	}
	res.RequiresExtra = true
	res.ExtraReason = "in_progress／complete 登記可能是正常維護流程；只有 stalled 能直接確認異常"
	res.Recommendations = []diagnostic.Recommendation{{Desc: "對照維護窗口確認登記是否仍需要；不得在未確認節點狀態前直接刪除 shutdown metadata"}}
	return res
}

func PlannedShutdownUnavailable(status int) diagnostic.Result {
	res := diagnostic.Result{ID: "planned_shutdown", Title: "Planned shutdown 登記狀態", Category: "cluster", Source: "raw_api", Docs: []string{docPlannedShutdown}}
	res.Status, res.Conclusion = diagnostic.StatusSkipped, diagnostic.ConclusionNormal
	res.Summary = fmt.Sprintf("Planned shutdown API 不可用（HTTP %d）；此選配檢查需要 manage／operator 權限", status)
	return res
}

func VotingExclusionsHealth(exclusions []collector.VotingExclusion) diagnostic.Result {
	res := diagnostic.Result{ID: "voting_config_exclusions", Title: "Voting configuration exclusions", Category: "cluster", Source: "raw_api", Docs: []string{docVotingExclusions}}
	if len(exclusions) == 0 {
		return pass(res, "沒有尚未清除的 voting configuration exclusion")
	}
	for _, exclusion := range exclusions {
		res.Findings = append(res.Findings, fmt.Sprintf("node_id=%s node_name=%s", exclusion.NodeID, exclusion.NodeName))
	}
	res.Status, res.Conclusion = diagnostic.StatusWarning, diagnostic.ConclusionSuspected
	res.Summary = fmt.Sprintf("有 %d 個 voting configuration exclusion 尚未清除", len(exclusions))
	res.RequiresExtra = true
	res.ExtraReason = "voting exclusion 在 master 節點下線流程中可能是正常暫態；正常運作完成後應清除"
	res.Recommendations = []diagnostic.Recommendation{{Desc: "先確認被排除節點是否已停止且維護流程完成，再依官方程序清除 exclusions"}}
	return res
}
