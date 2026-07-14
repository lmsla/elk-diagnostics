# spec-performance — 效能與分布（C 類缺口）

**實作位置**：`performance.go`。本檔項目 `_health_report` **無對應 indicator**，一律自己打 raw API。

> **共通限制（累積計數器）**：thread pool `rejected`、circuit breaker、`indexing_pressure`、`completed` 等皆自節點啟動起累加。單次快照無法區分「歷史」與「當下持續」。預設模式：非零標 ⚠️ 並註「累積值，需間隔取樣比對差值」；雙取樣模式（`--interval`，v0.2）差值為正才升 ❌。

---

### #6 Rejected requests（thread pool）

- **目標**：偵測 thread pool 請求拒絕（HTTP 429 `es_rejected_execution_exception`）。
- **採集**：`GET /_cat/thread_pool?v=true&h=id,name,queue,active,rejected,completed`（或 `GET /_nodes/stats/thread_pool`）。
- **判定**：`search`/`write` pool `rejected>0`→⚠️（疑似，累積值）；雙取樣差值>0→❌。同時算 `rejected/(rejected+completed)` 供參考。
- **建議**：降 bulk/搜尋批次、紓解 CPU/JVM、清 task backlog。**勿與磁碟 flood-stage 的 429（`cluster_block_exception`）混淆**。
- **官方文件**：https://www.elastic.co/docs/troubleshoot/elasticsearch/rejected-requests
- `tested_versions`: []

### #7 High JVM memory pressure

- **目標**：偵測各節點 heap 使用率過高。
- **採集**：`GET /_nodes/stats/jvm`，取 `jvm.mem.heap_used_percent`。
- **判定**：`heap_used_percent > 85`（閾值入 rules）→⚠️；持續貼頂或伴隨 GC 時間暴增→❌。
- **建議**：擴 heap、減 field data / cache、查重查詢與聚合。連動 #8。
- **官方文件**：https://www.elastic.co/docs/troubleshoot/elasticsearch/high-jvm-memory-pressure
- `tested_versions`: []

### #8 Circuit breaker errors

- **目標**：偵測 circuit breaker 跳閘（保護 heap）。
- **採集**：`GET /_nodes/stats/breaker`，取各 breaker `tripped`。
- **判定**：`tripped>0`→⚠️（累積值，需差值佐證）。
- **建議**：降單次請求記憶體用量；連動 #7 heap 壓力。
- **官方文件**：https://www.elastic.co/docs/troubleshoot/elasticsearch/circuit-breaker-errors
- `tested_versions`: []

### #9 High CPU usage + hot threads

- **目標**：偵測高 CPU 並定位熱點執行緒。
- **採集**：`GET /_cat/nodes?v=true&h=name,cpu,load_1m`；高 CPU 節點再取 `GET /_nodes/<node>/hot_threads`。
- **判定**：`cpu > 85`（閾值入 rules）→⚠️。hot_threads 為**文字輸出**，工具原樣附上供人工判讀，不解析判定。
- **建議**：依 hot_threads 定位（搜尋/索引/merge/GC）；連動 #6、#16。
- **官方文件**：https://www.elastic.co/docs/troubleshoot/elasticsearch/high-cpu-usage
- `tested_versions`: []

### #12 Task queue backlog

- **目標**：偵測管理任務積壓。
- **採集**：`GET /_tasks?detailed`（必要時 `actions`/`group_by` 過濾）。
- **判定**：長時間執行任務數或特定 action 積壓越閾值→⚠️。
- **建議**：定位長任務來源；連動 thread pool（#6）與 hot threads（#9）。
- **官方文件**：https://www.elastic.co/docs/troubleshoot/elasticsearch/task-queue-backlog
- `tested_versions`: []

### #17 Hot spotting

- **目標**：偵測負載/資源集中於少數節點。
- **採集**：`GET /_cat/nodes`（cpu/load/disk）＋ `GET /_nodes/stats/indices`（indexing/search 速率）。
- **判定**：單節點指標顯著偏離叢集中位數（偏離比例入 rules）→⚠️。
- **建議**：檢查 shard 分布、寫入路由、硬體規格不一致。
- **官方文件**：https://www.elastic.co/docs/troubleshoot/elasticsearch/hotspotting
- `tested_versions`: []

### #18 Unbalanced cluster

- **目標**：偵測 shard 在節點間分布不均。
- **採集**：`GET /_cat/shards`（逐節點 shard 數/大小）＋ `GET /_nodes/stats`。
- **判定**：節點間 shard 數或磁碟用量離散度越閾值→⚠️。
- **建議**：檢查 allocation awareness/filtering；必要時人工 reroute（須確認）。
- **官方文件**：https://www.elastic.co/docs/troubleshoot/elasticsearch/troubleshooting-unbalanced-cluster
- `tested_versions`: []

### #31 Search slow log 分析

- **目標**：協助定位慢查詢。
- **採集**：**需事先開啟 search slow log**；工具無法回溯歷史查詢。
- **判定**：偵測 slow log 是否已開啟；未開啟→輸出「需額外條件」+ 開啟方式，**不臆測**。
- **建議**：輸出 `index.search.slowlog.threshold.*` 設定範例供人工開啟後再查。
- **官方文件**：https://www.elastic.co/docs/troubleshoot/elasticsearch/troubleshooting-searches
- `tested_versions`: []
