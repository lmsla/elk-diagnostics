# Elasticsearch 健檢覆蓋缺口

本清單是 Elasticsearch 後續擴充的單一追蹤來源。`PROGRESS.md` 只連結本檔，
不複製狀態。狀態定義：`planned`、`specified`、`implemented`、`verified`、`deferred`。

範圍原則：

- 只做 Elasticsearch 唯讀 API 可取得的資料；不導入 SSH、agent 或主機命令。
- Live 與 Bundle 使用同一份 Collector → Analyzer → Reporter 契約。
- 單次快照不足以下結論的 counter/rate 留給 Stack Monitoring，不納入本輪。
- 「可取得」不等於「可宣稱正常」；部分回應、權限不足與版本不支援一律回 `unknown` 或 `skipped`。

## 第一批：單次快照高價值項目

| ID | 優先級 | 狀態 | 項目 | 主要 API | 判定與邊界 | 權限 | 驗收 |
|---|---|---|---|---|---|---|---|
| ES-GAP-01 | P0 | implemented | Cluster pending tasks／長時間 task | `GET /_cluster/pending_tasks`、`GET /_tasks?detailed=true&group_by=none` | 依 queue/running age 判定；合法的長任務只標疑似並列出 action | `monitor` | 單元／Bundle 路徑完成；403 與真機待驗 |
| ES-GAP-02 | P0 | implemented | Shard 大小與小 shard 增生 | `GET /_cat/shards?...store,docs` | 排除系統 index；大 shard 與大量小 primary shard 使用可覆寫 heuristic，不宣稱官方硬限制 | cluster/index `monitor` | parser、門檻、data stream 測試完成；ES8／ES9 真機待驗 |
| ES-GAP-03 | P0 | implemented | Snapshot 新鮮度／RPO | `GET /_slm/policy` | 依每個 policy 的 last success age、last failure 與尚未成功執行判定；無 SLM 不等於無備份，標 `skipped` | `read_slm` | 無 policy／時間門檻／失敗測試完成；403 與真機待驗 |
| ES-GAP-04 | P0 | implemented | Node 版本／JDK／plugin／heap 漂移 | `GET /_nodes/jvm,plugins` | 比對所有成功回應節點；Nodes API 不完整時不得回 pass | `monitor` | 2-node 一致／漂移／partial response／排序測試完成；真機待驗 |
| ES-GAP-05 | P0 | implemented | TLS 憑證與 License 到期 | `GET /_ssl/certificates`、`GET /_license` | License 為 cluster 視角；SSL API 只代表回應節點，未逐節點採集前不得宣稱全叢集憑證正常 | `monitor` | 到期門檻／永久 license／單節點限制測試完成；403 與真機待驗 |
| ES-GAP-06 | P0 | implemented | 高可用結構（第一階段） | `GET /_settings`、`GET /_cluster/settings`、Nodes Info | 檢查非系統 index replica=0、allocation awareness 設定與節點屬性；partial response 不得 pass | cluster/index `monitor` | replica／awareness／partial response 完成；實際 shard 跨 zone placement 留第二階段 |

本批新增報告 ID：`cluster_pending_tasks`、`long_running_tasks`、`shard_sizing`、
`snapshot_freshness`、`node_runtime_consistency`、`tls_certificate_expiry`、
`license_expiry`、`replica_resilience`、`allocation_awareness`。`implemented` 僅代表程式與自動化測試完成；
升為 `verified` 前仍需 ES8／ES9、多節點與權限不足的真機驗證。

## 第二批：單次快照次要或功能相依項目

| ID | 優先級 | 狀態 | 項目 | 主要 API | 邊界 |
|---|---|---|---|---|---|
| ES-GAP-07 | P1 | planned | Indexing pressure 當下使用量 | `GET /_nodes/stats/indexing_pressure` | 只判目前 bytes/limit；累積 rejection 不下持續性結論 |
| ES-GAP-08 | P1 | planned | Index read/write block | 既有 `GET /_settings?flat_settings=true` | 區分人為維護、watermark 自動 block；不提供自動解除 |
| ES-GAP-09 | P1 | planned | 最近重啟與 memory lock | 既有 Nodes Stats／Info | uptime 只提示最近重啟；`mlockall=false` 必須與 swap 設定交叉判讀 |
| ES-GAP-10 | P1 | planned | CCR follower／auto-follow 健康 | `GET /_ccr/stats` | 未使用 CCR 時 `skipped`；絕對 lag 可呈現，趨勢留給 Monitoring |
| ES-GAP-11 | P2 | planned | ML job／datafeed 狀態 | ML stats APIs | 未授權或未使用時 `skipped`，不讓 optional feature 污染整體健康 |
| ES-GAP-12 | P2 | planned | Planned shutdown／voting exclusion 殘留 | Shutdown／cluster state APIs | 僅檢查明確殘留與失敗狀態，避免把正常維護判成故障 |

## 明確排除：需時間序列或外部證據

以下項目不會因 backlog 後續實作而被遺忘，但不納入單次 API 快照規則：

- Search／index latency、throughput、merge、refresh、flush、GC、I/O、CPU throttling rate。
- Rejection、breaker、ingest failure 等累積 counter 的當下增量。
- Recovery／relocation 是否真正停滯。
- OOM、實際 corruption、選舉中斷、網路分區與硬體／kernel 錯誤。
- 實際查詢正確性、資料新鮮度及業務 SLO。

上述資料應由 Stack Monitoring、產品日誌或主動探測負責；本工具只在報告中提示證據缺口。
