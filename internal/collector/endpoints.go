package collector

// endpoints.go：`check` 會呼叫的所有唯讀端點的單一事實來源。
//
// 這張表同時服務多個用途，故不能與 collector 實際呼叫的字串有任何落差：
//  1. `--from-bundle` 離線分析：path → bundle 目錄中的檔名
//  2. golden test 的 fixture 回放（原本各自維護一份，已改為共用本表）
//  3. 後續：產生採集腳本（`collect.sh`）與客戶導入審查用的 API 清單
//
// **為了讓「表」與「實際呼叫」不可能漂移，collector 各方法一律使用本檔的常數，
// 不再各自寫字面字串。** 否則這張表會變成新一輪靜默錯誤的來源——2026-07-15 的
// filter_path bug 已經示範過「以為在查 X、其實查了空氣」的代價（見 驗證狀態.md §1）。

const (
	EpRoot               = "/"
	EpHealthReport       = "/_health_report"
	EpClusterHealth      = "/_cluster/health"
	EpClusterSettings    = "/_cluster/settings?include_defaults=true&flat_settings=true"
	EpAllocationExplain  = "/_cluster/allocation/explain"
	EpNodesRoles         = "/_nodes?timeout=5s&filter_path=nodes.*.roles"
	EpNodesResourceStats = "/_nodes/stats/os,process,fs,jvm?timeout=5s&filter_path=_nodes,nodes.*.name,nodes.*.roles,nodes.*.os.cpu,nodes.*.os.load_average,nodes.*.os.mem,nodes.*.os.swap,nodes.*.os.cgroup,nodes.*.process.cpu,nodes.*.process.mem,nodes.*.process.open_file_descriptors,nodes.*.process.max_file_descriptors,nodes.*.fs.total,nodes.*.fs.data,nodes.*.fs.io_stats,nodes.*.jvm.uptime_in_millis,nodes.*.jvm.mem,nodes.*.jvm.gc"
	// EpNodesJVMOldPool 保留舊名稱，既有 analyzer 與舊 bundle 的 JVM 診斷不必分岔；
	// 實際端點已擴充成完整 node resource stats，檔名仍維持 nodes_stats_jvm.json。
	EpNodesJVMOldPool       = EpNodesResourceStats
	EpNodesResourceInfo     = "/_nodes/os,process?timeout=5s&filter_path=_nodes,nodes.*.name,nodes.*.roles,nodes.*.os.name,nodes.*.os.pretty_name,nodes.*.os.arch,nodes.*.os.version,nodes.*.os.available_processors,nodes.*.os.allocated_processors,nodes.*.process.id,nodes.*.process.mlockall"
	EpNodesBreakers         = "/_nodes/stats/breaker?timeout=5s&filter_path=nodes.*.name,nodes.*.breakers"
	EpNodesIngest           = "/_nodes/stats/ingest?timeout=5s&filter_path=nodes.*.ingest.pipelines"
	EpCatNodes              = "/_cat/nodes?format=json&h=name,node.role,cpu,load_1m,allocated_processors,heap.percent,disk.used_percent"
	EpCatAllocation         = "/_cat/allocation?format=json&h=node,shards,shards.undesired,disk.percent"
	EpCatIndices            = "/_cat/indices?format=json&h=index,health,status"
	EpCatThreadPool         = "/_cat/thread_pool?format=json&h=node_name,name,active,queue,rejected,completed"
	EpCatThreadPoolWrite    = "/_cat/thread_pool/write?format=json&h=node_name,name,size,active,queue,rejected"
	EpMapping               = "/_mapping"
	EpAllSettings           = "/_settings?flat_settings=true"
	EpIlmStatus             = "/_ilm/status"
	EpIlmExplainErrors      = "/_all/_ilm/explain?only_errors=true&only_managed=true"
	EpIlmExplainManaged     = "/_all/_ilm/explain?only_managed=true"
	EpWatcherStats          = "/_watcher/stats"
	EpTransformStats        = "/_transform/_stats"
	EpRemoteInfo            = "/_remote/info"
	EpMigrationDeprecations = "/_migration/deprecations"
	EpRecovery              = "/_recovery?active_only=true"
	EpPendingTasks          = "/_cluster/pending_tasks"
	EpRunningTasks          = "/_tasks?timeout=5s&detailed=true&group_by=none&filter_path=tasks.*.node,tasks.*.type,tasks.*.action,tasks.*.description,tasks.*.running_time_in_nanos,tasks.*.cancellable"
	EpCatShardsSizing       = "/_cat/shards?format=json&bytes=b&h=index,shard,prirep,state,node,store,docs"
	EpSLMPolicies           = "/_slm/policy?filter_path=*.modified_date_millis,*.next_execution_millis,*.last_success.snapshot_name,*.last_success.time,*.last_failure.snapshot_name,*.last_failure.time,*.stats.snapshots_taken,*.stats.snapshots_failed"
	EpNodesRuntime          = "/_nodes/jvm,plugins?timeout=5s&filter_path=_nodes,nodes.*.name,nodes.*.roles,nodes.*.version,nodes.*.build_hash,nodes.*.jvm.version,nodes.*.jvm.vm_version,nodes.*.jvm.mem.heap_init_in_bytes,nodes.*.jvm.mem.heap_max_in_bytes,nodes.*.plugins.name,nodes.*.plugins.version"
	EpSSLCertificates       = "/_ssl/certificates"
	EpLicense               = "/_license?filter_path=license.status,license.type,license.issued_to,license.expiry_date_in_millis"
	EpNodesTopology         = "/_nodes?timeout=5s&filter_path=_nodes,nodes.*.name,nodes.*.roles,nodes.*.attributes"
	EpNodesIndexingPressure = "/_nodes/stats/indexing_pressure?timeout=5s&filter_path=_nodes,nodes.*.name,nodes.*.indexing_pressure.memory.current.combined_coordinating_and_primary_in_bytes,nodes.*.indexing_pressure.memory.current.replica_in_bytes,nodes.*.indexing_pressure.memory.current.all_in_bytes,nodes.*.indexing_pressure.memory.limit_in_bytes"
	EpCCRStats              = "/_ccr/stats?filter_path=auto_follow_stats.number_of_failed_follow_indices,auto_follow_stats.number_of_failed_remote_cluster_state_requests,auto_follow_stats.recent_auto_follow_errors,follow_stats.indices.index,follow_stats.indices.total_global_checkpoint_lag,follow_stats.indices.shards.shard_id,follow_stats.indices.shards.leader_global_checkpoint,follow_stats.indices.shards.follower_global_checkpoint,follow_stats.indices.shards.fatal_exception,follow_stats.indices.shards.read_exceptions"
	EpMLJobStats            = "/_ml/anomaly_detectors/_stats?allow_no_match=true&filter_path=count,jobs.job_id,jobs.state,jobs.assignment_explanation"
	EpMLDatafeedStats       = "/_ml/datafeeds/_stats?allow_no_match=true&filter_path=count,datafeeds.datafeed_id,datafeeds.state,datafeeds.assignment_explanation,datafeeds.timing_stats.job_id"
	EpPlannedShutdown       = "/_nodes/shutdown"
	EpVotingExclusions      = "/_cluster/state/metadata?filter_path=metadata.cluster_coordination.voting_config_exclusions"
)

// Endpoint 是 check 會呼叫的單一唯讀端點。Purpose 供 API 清單與採集腳本註解使用，
// 寫給客戶的資安/導入審查人員看，故用途要寫得具體、可自我說明（見 設定規格 §唯讀）。
type Endpoint struct {
	Path    string
	File    string
	Purpose string
}

// Endpoints 依 check 的實際呼叫順序排列，方便對照報告與採集腳本。
var Endpoints = []Endpoint{
	{EpRoot, "version.json", "版本偵測與 cluster_name（決定走 health_report 或 fallback）"},
	{EpHealthReport, "health_report.json", "叢集健康總表，A/B 類診斷的地基"},
	{EpIlmStatus, "ilm_status.json", "ILM 服務狀態（RUNNING/STOPPING/STOPPED）"},
	{EpIlmExplainErrors, "ilm_explain_errors.json", "卡在 ERROR step 的 index（health_report 的 ilm indicator 會延遲，須直接問）"},
	{EpCatThreadPool, "cat_thread_pool.json", "thread pool 佇列與拒絕數"},
	{EpNodesResourceStats, "nodes_stats_jvm.json", "各節點 OS／process／filesystem／JVM 快照與 JVM old pool 記憶體壓力"},
	{EpNodesResourceInfo, "nodes_info_os_process.json", "各節點 OS 版本／架構／processors、PID 與 memory lock 狀態"},
	{EpNodesBreakers, "nodes_stats_breaker.json", "circuit breaker 跳閘累積次數"},
	{EpCatNodes, "cat_nodes.json", "各節點 CPU／heap／disk 使用率與 allocated_processors"},
	{EpCatAllocation, "cat_allocation.json", "各節點 shard 分布與待搬移數"},
	{EpMapping, "mapping.json", "各 index 的 mapping（僅欄位結構，不含文件內容）"},
	{EpNodesIngest, "nodes_stats_ingest.json", "ingest pipeline 處理數與失敗數"},
	{EpCatIndices, "cat_indices.json", "各 index 健康與開關狀態"},
	{EpWatcherStats, "watcher_stats.json", "Watcher 服務是否被手動停止"},
	{EpTransformStats, "transform_stats.json", "transform 執行狀態"},
	{EpRemoteInfo, "remote_info.json", "remote cluster 連線狀態"},
	{EpMigrationDeprecations, "migration_deprecations.json", "升版 deprecation 警告"},
	{EpClusterSettings, "cluster_settings.json", "叢集層級設定（allocation.enable、monitoring collection 等生效值）"},
	{EpAllSettings, "all_settings.json", "各 index 設定（search slow log 門檻）"},
	{EpAllocationExplain, "allocation_explain.json", "未分配 shard 的 decider 級根因"},
	{EpIlmExplainManaged, "ilm_explain_managed.json", "受管理 index 的 ILM 階段（tier 遷移候選）"},
	{EpClusterHealth, "cluster_health.json", "叢集節點數（master 穩定性佐證）"},
	{EpNodesRoles, "nodes_roles.json", "各節點角色（master-eligible 數、data tier 分布）"},
	{EpRecovery, "recovery.json", "進行中的 snapshot 還原進度"},
	{EpCatThreadPoolWrite, "cat_thread_pool_write.json", "write thread pool 大小與積壓（寫入瓶頸因果鏈）"},
	{EpPendingTasks, "cluster_pending_tasks.json", "尚未套用的 cluster state task 與排隊時間"},
	{EpRunningTasks, "running_tasks.json", "目前執行中的 task 與執行時間（不採集 request body/header）"},
	{EpCatShardsSizing, "cat_shards_sizing.json", "各 shard 大小、文件數與配置節點（shard sizing）"},
	{EpSLMPolicies, "slm_policies.json", "SLM policy 最近成功／失敗時間與下次執行時間"},
	{EpNodesRuntime, "nodes_runtime.json", "各節點 ES/JDK/heap/plugin 一致性與 Nodes API coverage"},
	{EpSSLCertificates, "ssl_certificates.json", "回應節點載入的 TLS 憑證與到期日（單節點視角）"},
	{EpLicense, "license.json", "叢集 License 狀態、類型與到期日"},
	{EpNodesTopology, "nodes_topology.json", "節點角色與 allocation awareness attributes"},
	{EpNodesIndexingPressure, "nodes_indexing_pressure.json", "各節點當下 indexing pressure 記憶體使用量與限制（不使用累積 rejection 下趨勢結論）"},
	{EpCCRStats, "ccr_stats.json", "CCR follower checkpoint lag、fatal/read exception 與 auto-follow 失敗"},
	{EpMLJobStats, "ml_job_stats.json", "Machine Learning anomaly detection job 執行狀態"},
	{EpMLDatafeedStats, "ml_datafeed_stats.json", "Machine Learning datafeed 執行與 assignment 狀態"},
	{EpPlannedShutdown, "planned_shutdown.json", "已登記的 planned shutdown 狀態（選配高權限檢查）"},
	{EpVotingExclusions, "voting_exclusions.json", "cluster state 中尚未清除的 voting configuration exclusions"},
}

// EpIndexSettings 組出單一 index 的 settings 端點。
//
// 這是唯一的**動態**端點——要哪些 index 得先看 health_report 點名哪些受影響，
// 因此不在 Endpoints 表中，`--from-bundle` 也無法涵蓋（採集當下才知道要抓誰）。
// bundle 模式下 #20 會因此判定為 unknown 而非 pass，這是刻意的：查不到就說查不到，
// 不能因為「沒查到封鎖」就宣稱正常。
func EpIndexSettings(index string) string {
	return "/" + index + "/_settings?include_defaults=true&flat_settings=true"
}

var endpointFiles = func() map[string]string {
	m := make(map[string]string, len(Endpoints))
	for _, e := range Endpoints {
		m[e.Path] = e.File
	}
	return m
}()

// FileForEndpoint 回傳該端點在 bundle 中對應的檔名；動態或未知端點回 false。
func FileForEndpoint(path string) (string, bool) {
	f, ok := endpointFiles[path]
	return f, ok
}
