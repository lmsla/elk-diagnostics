# 單次快照覆蓋擴充（ES-GAP-01～06）

## 1. 邊界

本規格只新增單次唯讀 API 快照足以支持的診斷。Search/index latency、GC、I/O、
CPU throttling 與累積 rejection 的 rate/delta 明確留給 Stack Monitoring，不在工具內做短間隔雙取樣。

所有新增端點必須進入 `collector.Endpoints`，因此 Live、`collect.sh`、Bundle、API inventory
與 golden 回放共用同一份資料契約。任何端點缺檔、403、版本不支援或 partial response
不得回 `pass`。

## 2. 診斷項目

### ES-GAP-01：Cluster task 壅塞

- 採集：`GET /_cluster/pending_tasks`、`GET /_tasks?detailed=true&group_by=none`；後者以
  `filter_path` 僅保留 node/type/action/description/running time/cancellable，不保存 task headers 或 status body。
- `cluster_pending_tasks`：queue age 達 warning／critical 門檻即告警；保留 priority/source。
- `long_running_tasks`：running age 達門檻標 warning/suspected。長任務可能是合法 reindex、snapshot
  或查詢，單次快照不得標 confirmed；忽略本次 `cluster:monitor/tasks/lists` 自查 task。
- 預設：pending warning 30 秒、critical 300 秒；long-running warning 300 秒。

### ES-GAP-02：Shard sizing

- 採集：`GET /_cat/shards?format=json&bytes=b&h=index,shard,prirep,state,node,store,docs`。
- 排除系統 index，但保留 `.ds-*` data stream backing index。
- primary shard `store >= 50 GiB` 標 warning；primary shard `store <= 1 GiB` 且數量達 100 個標 warning。
- 這些是可覆寫 heuristic，不是 Elasticsearch 硬限制；報告不得輸出 critical。
- 未分配 shard 沒有可用 store 時忽略，分配問題由既有 `cluster_health` 處理。

### ES-GAP-03：Snapshot 新鮮度

- 採集：`GET /_slm/policy`，以 `filter_path` 只保留 policy 名稱與判定所需時間／統計，不保存 repository、schedule 或 index pattern。
- 每個 policy 使用 `last_success.time`、`last_failure.time`、`modified_date_millis`、
  `next_execution_millis` 與 stats 判讀。
- `last_success.time`／`last_failure.time` 依官方 schema 解析 RFC3339 字串，並相容既有 epoch millis 回應。
- 無 policy：`skipped`，明確說明外部／手動備份不可由 SLM API 判定。
- Bundle 有 `_manifest.json` 時，以 `collected_at` 作為 RPO 與到期判斷基準；不得用數天後的分析時間改寫採集當下狀態。
- 從未成功且已到過首次排程、最後失敗晚於最後成功、最後成功超過門檻：warning/critical。
- 預設最大成功年齡：warning 48 小時、critical 168 小時；客戶 RPO 不同時必須覆寫。
- 權限：`read_slm`。權限不足回 `unknown`，不得要求採集帳號取得寫入型 `manage_slm`。

### ES-GAP-04：Node runtime 一致性

- 採集：`GET /_nodes/jvm,plugins`，保留 `_nodes` coverage、node name、ES version/build、
  JDK/VM version、heap init/max、plugin name/version。
- ES version/build 在全叢集比對；JDK、heap 與 plugin 集合只在相同 role signature 內比對，
  避免把異質角色的合理差異誤判為漂移。
- 任一差異標 warning/suspected；Nodes API partial response 則 `unknown`。

### ES-GAP-05：TLS 與 License 到期

- 採集：`GET /_ssl/certificates`、`GET /_license`；License 以 `filter_path` 只保留 status/type/issued_to/expiry。
- 憑證：已過期為 critical；30 天內到期為 warning。具 private key 的 identity certificate
  優先；trust-only certificate 仍列 finding，但單獨過期只標 warning。
- `/_ssl/certificates` 只回應單一節點的 TLS context，因此即使結果正常也設
  `requires_extra=true`，不得聲稱全叢集正常；完整覆蓋需逐節點採集。
- License：expired/invalid 為 critical；30 天內到期為 warning；無 expiry（例如 basic）為 pass。
- 權限：`monitor`。

### ES-GAP-06：高可用結構

- 使用既有 `GET /_settings?flat_settings=true` 檢查非系統 index 的有效 replica 數；
  `number_of_replicas=0` 標 warning/suspected，不宣稱資料已遺失。
- `index.auto_expand_replicas` 上限為 `all` 時標 warning；Elastic 官方明確指出此設定會忽略 allocation awareness。
- 採集精簡 Nodes topology（`_nodes` coverage、name、roles、attributes），搭配既有 cluster settings。
- 未配置 allocation awareness：`skipped`，因為工具不知道客戶是否有跨 zone 需求。
- 已配置 awareness 時，任一 data node 缺指定 attribute，或 attribute 只有一個值，標 warning。
- Nodes topology partial response 時標 `unknown`，不得用回應子集宣稱設定完整。
- 分配後的 shard copy 跨 zone 驗證留待 ES-GAP-06 第二階段；第一階段不得把「設定存在」
  等同「所有 shard copies 已跨 zone」。

## 3. 規則欄位

```yaml
static_health:
  pending_task_warn_seconds: 30
  pending_task_crit_seconds: 300
  long_task_warn_seconds: 300
  shard_large_warn_gb: 50
  shard_small_max_mb: 1024
  shard_small_count_warn: 100
  snapshot_warn_hours: 48
  snapshot_crit_hours: 168
  expiry_warn_days: 30
```

所有門檻必須大於 0；覆寫為 0 視為未提供，沿用內建值。

## 4. 報告與安全

- Bundle 會新增 task description、plugin 名、node runtime、憑證 subject/issuer/path 與 SLM policy 名；
  這些均屬客戶環境 metadata，沿用現有交付前檢視／redaction 風險提示。
- 不採集文件內容、query body、task headers、憑證私鑰或 secure settings。
- Snapshot policy 的 repository、schedule 與 index pattern 不進入 Bundle；端點已用 `filter_path` 從源頭排除。

## 5. 官方依據

- [Pending cluster tasks API](https://www.elastic.co/guide/en/elasticsearch/reference/current/cluster-pending.html)
- [Task management API](https://www.elastic.co/docs/api/doc/elasticsearch/operation/operation-tasks-list)
- [CAT shards API](https://www.elastic.co/docs/api/doc/elasticsearch/operation/operation-cat-shards)
- [SLM policy API](https://www.elastic.co/docs/api/doc/elasticsearch/operation/operation-slm-get-lifecycle)
- [Nodes info API](https://www.elastic.co/guide/en/elasticsearch/reference/current/cluster-nodes-info.html)
- [SSL certificate API](https://www.elastic.co/guide/en/elasticsearch/reference/current/security-api-ssl.html)
- [License API](https://www.elastic.co/guide/en/elasticsearch/reference/current/get-license.html)
- [General index settings](https://www.elastic.co/docs/reference/elasticsearch/index-settings/index-modules)
