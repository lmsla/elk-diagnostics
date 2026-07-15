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
// filter_path bug 已經示範過「以為在查 X、其實查了空氣」的代價（見 VERIFICATION.md §1）。

const (
	EpRoot                  = "/"
	EpHealthReport          = "/_health_report"
	EpClusterHealth         = "/_cluster/health"
	EpClusterSettings       = "/_cluster/settings?include_defaults=true&flat_settings=true"
	EpAllocationExplain     = "/_cluster/allocation/explain"
	EpNodesRoles            = "/_nodes?filter_path=nodes.*.roles"
	EpNodesJVMOldPool       = "/_nodes/stats?filter_path=nodes.*.name,nodes.*.jvm.mem.pools.old"
	EpNodesBreakers         = "/_nodes/stats/breaker?filter_path=nodes.*.name,nodes.*.breakers"
	EpNodesIngest           = "/_nodes/stats/ingest?filter_path=nodes.*.ingest.pipelines"
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
)

// Endpoint 是 check 會呼叫的單一唯讀端點。Purpose 供 API 清單與採集腳本註解使用，
// 寫給客戶的資安/導入審查人員看，故用途要寫得具體、可自我說明（見 spec-config §唯讀）。
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
	{EpNodesJVMOldPool, "nodes_stats_jvm.json", "JVM old pool 記憶體壓力"},
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
