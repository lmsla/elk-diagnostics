# elk-diagnostics 0.0.4-mvp — Elasticsearch API 呼叫清單

本工具只送出 HTTP GET，不執行任何寫入操作。
以下為 check 會呼叫的全部端點，皆為叢集／節點層級的中繼資料。

資料範圍：
  - 工具不讀取任何文件（document）內容
  - /_mapping 會回傳各 index 的「欄位名稱」（不含欄位值）
  - /_cat/nodes、/_nodes 會回傳節點名稱、角色與資源使用率
  - 其餘為叢集狀態與統計

| 方法 | 端點 | 用途 |
|---|---|---|
| GET | `/` | 版本偵測與 cluster_name（決定走 health_report 或 fallback） |
| GET | `/_health_report` | 叢集健康總表，A/B 類診斷的地基 |
| GET | `/_ilm/status` | ILM 服務狀態（RUNNING/STOPPING/STOPPED） |
| GET | `/_all/_ilm/explain?only_errors=true&only_managed=true` | 卡在 ERROR step 的 index（health_report 的 ilm indicator 會延遲，須直接問） |
| GET | `/_cat/thread_pool?format=json&h=node_name,name,active,queue,rejected,completed` | thread pool 佇列與拒絕數 |
| GET | `/_nodes/stats/os,process,fs,jvm?timeout=5s&filter_path=_nodes,nodes.*.name,nodes.*.roles,nodes.*.os.cpu,nodes.*.os.load_average,nodes.*.os.mem,nodes.*.os.swap,nodes.*.os.cgroup,nodes.*.process.cpu,nodes.*.process.mem,nodes.*.process.open_file_descriptors,nodes.*.process.max_file_descriptors,nodes.*.fs.total,nodes.*.fs.data,nodes.*.fs.io_stats,nodes.*.jvm.uptime_in_millis,nodes.*.jvm.mem,nodes.*.jvm.gc` | 各節點 OS／process／filesystem／JVM 快照與 JVM old pool 記憶體壓力 |
| GET | `/_nodes/os,process?timeout=5s&filter_path=_nodes,nodes.*.name,nodes.*.roles,nodes.*.os.name,nodes.*.os.pretty_name,nodes.*.os.arch,nodes.*.os.version,nodes.*.os.available_processors,nodes.*.os.allocated_processors,nodes.*.process.id,nodes.*.process.mlockall` | 各節點 OS 版本／架構／processors、PID 與 memory lock 狀態 |
| GET | `/_nodes/stats/breaker?timeout=5s&filter_path=nodes.*.name,nodes.*.breakers` | circuit breaker 跳閘累積次數 |
| GET | `/_cat/nodes?format=json&h=name,node.role,cpu,load_1m,allocated_processors,heap.percent,disk.used_percent` | 各節點 CPU／heap／disk 使用率與 allocated_processors |
| GET | `/_cat/allocation?format=json&h=node,shards,shards.undesired,disk.percent` | 各節點 shard 分布與待搬移數 |
| GET | `/_mapping` | 各 index 的 mapping（僅欄位結構，不含文件內容） |
| GET | `/_nodes/stats/ingest?timeout=5s&filter_path=nodes.*.ingest.pipelines` | ingest pipeline 處理數與失敗數 |
| GET | `/_cat/indices?format=json&h=index,health,status` | 各 index 健康與開關狀態 |
| GET | `/_watcher/stats` | Watcher 服務是否被手動停止 |
| GET | `/_transform/_stats` | transform 執行狀態 |
| GET | `/_remote/info` | remote cluster 連線狀態 |
| GET | `/_migration/deprecations` | 升版 deprecation 警告 |
| GET | `/_cluster/settings?include_defaults=true&flat_settings=true` | 叢集層級設定（allocation.enable、monitoring collection 等生效值） |
| GET | `/_settings?flat_settings=true` | 各 index 設定（search slow log 門檻） |
| GET | `/_cluster/allocation/explain` | 未分配 shard 的 decider 級根因 |
| GET | `/_all/_ilm/explain?only_managed=true` | 受管理 index 的 ILM 階段（tier 遷移候選） |
| GET | `/_cluster/health` | 叢集節點數（master 穩定性佐證） |
| GET | `/_nodes?timeout=5s&filter_path=nodes.*.roles` | 各節點角色（master-eligible 數、data tier 分布） |
| GET | `/_recovery?active_only=true` | 進行中的 snapshot 還原進度 |
| GET | `/_cat/thread_pool/write?format=json&h=node_name,name,size,active,queue,rejected` | write thread pool 大小與積壓（寫入瓶頸因果鏈） |
| GET | `/_cluster/pending_tasks` | 尚未套用的 cluster state task 與排隊時間 |
| GET | `/_tasks?timeout=5s&detailed=true&group_by=none&filter_path=tasks.*.node,tasks.*.type,tasks.*.action,tasks.*.description,tasks.*.running_time_in_nanos,tasks.*.cancellable` | 目前執行中的 task 與執行時間（不採集 request body/header） |
| GET | `/_cat/shards?format=json&bytes=b&h=index,shard,prirep,state,node,store,docs` | 各 shard 大小、文件數與配置節點（shard sizing） |
| GET | `/_slm/policy?filter_path=*.modified_date_millis,*.next_execution_millis,*.last_success.snapshot_name,*.last_success.time,*.last_failure.snapshot_name,*.last_failure.time,*.stats.snapshots_taken,*.stats.snapshots_failed` | SLM policy 最近成功／失敗時間與下次執行時間 |
| GET | `/_nodes/jvm,plugins?timeout=5s&filter_path=_nodes,nodes.*.name,nodes.*.roles,nodes.*.version,nodes.*.build_hash,nodes.*.jvm.version,nodes.*.jvm.vm_version,nodes.*.jvm.mem.heap_init_in_bytes,nodes.*.jvm.mem.heap_max_in_bytes,nodes.*.plugins.name,nodes.*.plugins.version` | 各節點 ES/JDK/heap/plugin 一致性與 Nodes API coverage |
| GET | `/_ssl/certificates` | 回應節點載入的 TLS 憑證與到期日（單節點視角） |
| GET | `/_license?filter_path=license.status,license.type,license.issued_to,license.expiry_date_in_millis` | 叢集 License 狀態、類型與到期日 |
| GET | `/_nodes?timeout=5s&filter_path=_nodes,nodes.*.name,nodes.*.roles,nodes.*.attributes` | 節點角色與 allocation awareness attributes |
| GET | `/_nodes/stats/indexing_pressure?timeout=5s&filter_path=_nodes,nodes.*.name,nodes.*.indexing_pressure.memory.current.combined_coordinating_and_primary_in_bytes,nodes.*.indexing_pressure.memory.current.replica_in_bytes,nodes.*.indexing_pressure.memory.current.all_in_bytes,nodes.*.indexing_pressure.memory.limit_in_bytes` | 各節點當下 indexing pressure 記憶體使用量與限制（不使用累積 rejection 下趨勢結論） |
| GET | `/_ccr/stats?filter_path=auto_follow_stats.number_of_failed_follow_indices,auto_follow_stats.number_of_failed_remote_cluster_state_requests,auto_follow_stats.recent_auto_follow_errors,follow_stats.indices.index,follow_stats.indices.total_global_checkpoint_lag,follow_stats.indices.shards.shard_id,follow_stats.indices.shards.leader_global_checkpoint,follow_stats.indices.shards.follower_global_checkpoint,follow_stats.indices.shards.fatal_exception,follow_stats.indices.shards.read_exceptions` | CCR follower checkpoint lag、fatal/read exception 與 auto-follow 失敗 |
| GET | `/_ml/anomaly_detectors/_stats?allow_no_match=true&filter_path=count,jobs.job_id,jobs.state,jobs.assignment_explanation` | Machine Learning anomaly detection job 執行狀態 |
| GET | `/_ml/datafeeds/_stats?allow_no_match=true&filter_path=count,datafeeds.datafeed_id,datafeeds.state,datafeeds.assignment_explanation,datafeeds.timing_stats.job_id` | Machine Learning datafeed 執行與 assignment 狀態 |
| GET | `/_nodes/shutdown` | 已登記的 planned shutdown 狀態（選配高權限檢查） |
| GET | `/_cluster/state/metadata?filter_path=metadata.cluster_coordination.voting_config_exclusions` | cluster state 中尚未清除的 voting configuration exclusions |

共 39 個固定端點。

另有 1 個動態端點：

  GET /<index>/_settings?include_defaults=true&flat_settings=true

  僅在 health_report 點名有受影響 index 時才會查詢，最多 20 個 index。
  叢集健康時完全不會呼叫。
