# Node Context：ES API 可見的節點環境資訊

## 1. 目的與邊界

本功能只透過 Elasticsearch 唯讀 API，取得叢集中**所有有回應節點**的 OS、Elasticsearch process、filesystem 與 JVM 快照，並將原始上下文與診斷結論分開呈現。

它不是完整主機健檢。MVP 明確不做：

- SSH、agent、主機端 shell 指令或讀取 `/proc`。
- kernel log、OOM log、NTP、網路封包／延遲、非 Elasticsearch process。
- 以單次累積計數器判定 I/O latency、GC rate 或 CPU throttling rate。
- 自動修復或修改叢集／主機設定。

## 2. 唯讀 API

固定採集兩個端點；Live 與 Bundle 共用相同端點及解析器：

1. `GET /_nodes/stats/os,process,fs,jvm?...`
   - OS：CPU、load average、memory、swap、Linux cgroup。
   - process：CPU、virtual memory、open/max file descriptors。
   - filesystem：總容量、data path、裝置累積 I/O counter。
   - JVM：heap、old pool、uptime、GC 累積 counter。
2. `GET /_nodes/os,process?...`
   - OS 名稱／版本／架構、available/allocated processors。
   - Elasticsearch PID 與 `mlockall`。

權限沿用 Elasticsearch Nodes APIs 的要求：cluster privilege `monitor` 或 `manage`。官方資料來源：

- [Nodes stats API](https://www.elastic.co/guide/en/elasticsearch/reference/current/cluster-nodes-stats.html)
- [Nodes info API](https://www.elastic.co/guide/en/elasticsearch/reference/current/cluster-nodes-info.html)

## 3. 資料契約

報告新增 `node_context`，包含：

- `stats_coverage`、`info_coverage`：`total`、`successful`、`failed`、實際回傳 node 數。
- `nodes`：以 node name、node ID 穩定排序的節點陣列。
- `issues`：端點缺檔、部分回應或輔助資料不可用的原因。

每個 node 保留：

- identity：node ID、name、roles。
- OS：名稱、版本、架構、processors、CPU/load、memory/swap、cgroup memory/CPU counter。
- process：PID、`mlockall`、CPU、virtual memory、file descriptors。
- filesystem：總容量、data path/mount/type、裝置 I/O 累積 counter。
- JVM：uptime、heap、old pool、各 collector 的 GC 累積 count/time。

欄位不存在、平台不支援或 ES 回 `-1` 時必須保留為「不可得」，不得轉成 `0`。Linux-only cgroup 欄位在其他平台缺少是正常現象。

## 4. 節點完整性

所有 Nodes API 都必須讀取 `_nodes.total/successful/failed`：

- 兩個端點皆 `failed=0`、`successful=total`，且實際 node 數相符：`pass`。
- 任一端點缺少 coverage、部分失敗、回傳 node 數不符或 Bundle 缺檔：`unknown`。
- partial response 不得因已回傳節點看似正常而產生整體 `pass`。

完整性是獨立診斷 `node_api_coverage`；已成功取得的 node context 仍保留，不因另一端點失敗而全部丟棄。

## 5. MVP 診斷

只對單次快照足以支持的狀態下結論：

| ID | 判定 | 預設 |
|---|---|---|
| `node_swap_usage` | 任一 node `swap.used_in_bytes > 0` 為 warning | Elastic 明確建議避免 swapping；缺欄位且無任何異常時為 unknown |
| `node_file_descriptor_pressure` | `open / max` 達門檻 | warning 80%、critical 90%；屬工具 heuristic，可由 rules 覆寫 |
| `node_cgroup_memory_pressure` | 有限 cgroup limit 下 `usage / limit` 達門檻 | warning 90%；因 usage 含 file cache，單次快照不升 critical，並要求時間序列／OOM 事件佐證 |

官方依據：

- [Disable swapping](https://www.elastic.co/docs/deploy-manage/deploy/self-managed/setup-configuration-memory)
- [Increase the file descriptor limit](https://www.elastic.co/guide/en/elasticsearch/reference/current/file-descriptors.html)

OS memory 使用率、`mlockall`、filesystem I/O、cgroup throttling 與 JVM GC 在 MVP 只做 context，不單獨告警。理由是它們可能是 page cache、部署選擇或自啟動以來的累積值；單次取樣不足以證明持續性問題。

## 6. 相容性與安全

- 擴充既有 `nodes_stats_jvm.json` 的採集內容並保留檔名；舊 Bundle 仍能提供既有 JVM old-pool 診斷。
- 舊 Bundle 缺少 Nodes Info 或新增欄位時，新診斷回 `unknown`，既有診斷不得受影響。
- Bundle 會包含 node name、OS 版本、data path、mount 與 device name；這些可能識別客戶環境。交付前必須由客戶檢視，不得上傳公開管道。

## 7. 驗證基準

- parser：完整欄位、缺欄位、`-1`、cgroup string number／unlimited、亂序 node map。
- analyzer：門檻邊界、部分欄位缺失、partial node response。
- Bundle：舊 Bundle 相容、新兩端點缺檔、Live/Bundle 同資料同結論。
- 真機：ES 8、ES 9；另以至少 2-node 叢集驗證所有節點均被保留。
