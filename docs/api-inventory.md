# elk-diagnostics 0.0.4-mvp — Elasticsearch API 呼叫清單

本工具只送出 HTTP GET，不執行任何寫入操作（見 docs/specs 鐵律 1）。
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
| GET | `/_nodes/stats?filter_path=nodes.*.name,nodes.*.jvm.mem.pools.old` | JVM old pool 記憶體壓力 |
| GET | `/_nodes/stats/breaker?filter_path=nodes.*.name,nodes.*.breakers` | circuit breaker 跳閘累積次數 |
| GET | `/_cat/nodes?format=json&h=name,node.role,cpu,load_1m,allocated_processors,heap.percent,disk.used_percent` | 各節點 CPU／heap／disk 使用率與 allocated_processors |
| GET | `/_cat/allocation?format=json&h=node,shards,shards.undesired,disk.percent` | 各節點 shard 分布與待搬移數 |
| GET | `/_mapping` | 各 index 的 mapping（僅欄位結構，不含文件內容） |
| GET | `/_nodes/stats/ingest?filter_path=nodes.*.ingest.pipelines` | ingest pipeline 處理數與失敗數 |
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
| GET | `/_nodes?filter_path=nodes.*.roles` | 各節點角色（master-eligible 數、data tier 分布） |
| GET | `/_recovery?active_only=true` | 進行中的 snapshot 還原進度 |
| GET | `/_cat/thread_pool/write?format=json&h=node_name,name,size,active,queue,rejected` | write thread pool 大小與積壓（寫入瓶頸因果鏈） |

共 24 個固定端點。

另有 1 個動態端點：

  GET /<index>/_settings?include_defaults=true&flat_settings=true

  僅在 health_report 點名有受影響 index 時才會查詢，最多 20 個 index。
  叢集健康時完全不會呼叫。
