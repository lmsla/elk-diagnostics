# elk-diagnostics 診斷規格（specs）

**這是實作的唯一依據。打開本目錄、讀完即可動工。**

## 這是什麼

elk-diagnostics：連上 ES、跑一輪唯讀診斷、輸出一份中文健康報告的單一二進位 CLI。
兩種用法：`elk-diagnostics check`（全面巡檢）、`elk-diagnostics diagnose --symptom <x>`（症狀排查）。

## 架構演進（一句話）

- **原規劃**：37 條診斷各自打 raw API、各自手刻 analyzer。
- **現結論**：ES 8.4+ 內建的 `_health_report` 已涵蓋約 19 條叢集健康判定，故**採集優先吃 `_health_report`，它沒做的才自己補**。建構量從「37 條手刻」收斂為「1 個 health_report 解析 + 4 類補洞模組 + 報告層」。

## 架構（白話三步）

1. **拿資料**：先跟 ES 要 `_health_report`；它沒有的（thread pool、JVM、CPU、mapping…）再自己打對應 API；ES < 8.4 才全部自己打。
2. **下判斷**：同一套規則把數值判成 ✅ 正常 / ⚠️ 注意 / ❌ 出事，附中文建議。**判斷邏輯與資料來源無關。**
3. **出報告**：合併成一份 JSON / 離線 HTML。判不準的（需 slow log、需時間序列、累積計數）誠實標「需額外條件」，不臆測。

## 鐵律（實作不可違反）

1. **只做唯讀**（GET/HEAD）。修復指令只輸出文字供人工執行，工具絕不送出任何寫入。
2. **實作每條前先讀該條官方文件**，不憑記憶推斷 response 結構。
3. `rules/default.yaml` 以 `//go:embed` 內嵌，**零設定即可跑**；外部 `--rules` 僅作覆寫。
4. 唯一必填輸入是**連線資訊**（host／認證），config.yaml 或 flag/env 擇一。
5. 每條標 `tested_versions`，未測版本主動警告（支援下限 **8.4**）。
6. 全程**繁體中文**輸出。

## 開工關卡（MVP 前必做一次）

取目標版本的**真實 `_health_report` 輸出**，逐 indicator 確認其 `diagnosis` 顆粒度足以支撐 A 類各條；不足者，該條改走 raw API（降為自己判斷）。**此驗證完成前，A 類為設計假設。**

## 每條規格怎麼讀（統一格式）

> **目標** ｜ **採集**（primary indicator 或 API）｜ **判定**（✅/⚠️/❌ + 閾值）｜ **建議**（唯讀引導）｜ **限制** ｜ **官方文件** ｜ `tested_versions`

嚴重度：`info`→✅ ｜ `warning`→⚠️ ｜ `critical`→❌。
三態結論：**已確認異常**（API 直證）／**疑似異常**（越閾值需佐證）／**正常**。
免責（報告固定附）：本工具提供診斷引導，非根因確認，結論基於單次唯讀快照與預設閾值，請結合現場日誌與時間序列綜合判斷。

## 37 條建構矩陣

- **A**＝直接讀 `_health_report` indicator（採集層解析，低維護）
- **B**＝先讀 indicator，需更細才補打 raw API
- **C**＝無 indicator，自己打 API 手刻（**差異化核心**）
- **基座**＝`_health_report` 本身

| # | 項目 | 類 | 來源（primary → fallback） | 規格檔 | analyzer |
|---|---|---|---|---|---|
| 1 | Red/Yellow cluster health | A | `shards_availability` | spec-health-report | cluster |
| 2 | Unassigned shards 根因 | A | `shards_availability` → `allocation/explain` | spec-health-report | cluster |
| 3 | Watermark errors | A | `disk` → `_nodes/stats/fs` | spec-health-report | capacity |
| 4 | Data nodes out of disk | A | `disk` → `_nodes/stats/fs` | spec-health-report | capacity |
| 5 | ILM stopped / errors | A | `ilm`+`slm` → `_ilm/explain` | spec-health-report | management |
| 6 | Rejected requests（thread pool） | **C** | `_nodes/stats/thread_pool`,`_cat/thread_pool` | spec-performance | performance |
| 7 | High JVM memory pressure | **C** | `_nodes/stats/jvm` | spec-performance | performance |
| 8 | Circuit breaker errors | **C** | `_nodes/stats/breaker` | spec-performance | performance |
| 9 | High CPU + hot threads | **C** | `_cat/nodes`,`_nodes/hot_threads` | spec-performance | performance |
| 10 | Shard capacity issues | A | `shards_capacity` | spec-health-report | capacity |
| 11 | Mapping explosion | **C** | `_mapping` | spec-data | data |
| 12 | Task queue backlog | **C** | `_tasks` | spec-performance | performance |
| 13 | Ingest pipeline errors | **C** | `_nodes/stats/ingest` | spec-data | data |
| 14 | Master/Other nodes out of disk | A | `disk` → `_nodes/stats/fs` | spec-health-report | capacity |
| 15 | Snapshot policy failures (SLM) | A | `slm` → `_slm/policy` | spec-health-report | snapshot |
| 16 | Write bottleneck（因果鏈） | **C** | 多源（見規格） | spec-write-bottleneck | write_bottleneck |
| 17 | Hot spotting | **C** | `_cat/nodes`,`_nodes/stats/indices` | spec-performance | performance |
| 18 | Unbalanced cluster | **C** | `_cat/shards`,`_nodes/stats` | spec-performance | performance |
| 19 | Data allocation blocked | B | `shards_availability` → `_cluster/settings` | spec-health-report | cluster |
| 20 | Index allocation blocked | B | `shards_availability` → `_settings` | spec-health-report | cluster |
| 21 | Not enough nodes for replica | A | `shards_availability` | spec-health-report | cluster |
| 22 | Shards per index exceeded | A | `shards_capacity` → `_settings` | spec-health-report | capacity |
| 23 | Shards per node exceeded | A | `shards_capacity` → `_cluster/settings` | spec-health-report | capacity |
| 24 | Preferred data tier missing | B | `data_stream_lifecycle` → `_cat/nodes` | spec-health-report | management |
| 25 | Incomplete migration to tiers | B | `ilm` → `_ilm/explain` | spec-health-report | management |
| 26 | Broken snapshot repositories | A | `repository_integrity` → `_snapshot/_status` | spec-health-report | snapshot |
| 27 | Watcher troubleshooting | **C** | `_watcher/stats` | spec-management | management |
| 28 | Transforms troubleshooting | **C** | `_transform/_stats` | spec-management | management |
| 29 | `_health_report` 整合 | **基座** | `_health_report` | spec-health-report | collector |
| 30 | Unstable cluster | B | `master_is_stable` → `_cluster/health` | spec-health-report | cluster |
| 31 | Search slow log 分析 | **C** | 需事先開啟 slow log | spec-performance | performance |
| 32 | Data corruption 偵測 | **C** | `_cat/indices?h=health,status` | spec-data | data |
| 33 | Monitoring troubleshooting | **C** | `_cluster/settings` | spec-management | management |
| 34 | Upgrade deprecation warnings | **C** | `_migration/deprecations` | spec-management | management |
| 35 | Remote clusters 狀態 | **C** | `_remote/info` | spec-management | management |
| 36 | Restore from snapshot 狀態 | B | `repository_integrity` → `_snapshot` | spec-health-report | snapshot |
| 37 | Cluster allocation 引導 | B | `shards_availability` → `allocation/explain` | spec-health-report | cluster |

加總：A=12、B=7、C=17、基座=1，合計 **37**。

## 規格檔索引與實作順序

| 規格檔 | 涵蓋項目 | analyzer / collector |
|---|---|---|
| [spec-health-report.md](./spec-health-report.md) | A+B 共 19 條（#1-5,10,14,15,19-26,30,36,37） | collector/health_report.go + cluster/capacity/management/snapshot |
| [spec-performance.md](./spec-performance.md) | #6,7,8,9,12,17,18,31 | performance.go |
| [spec-data.md](./spec-data.md) | #11,13,32 | data.go |
| [spec-management.md](./spec-management.md) | #27,28,33,34,35 | management.go |
| [spec-write-bottleneck.md](./spec-write-bottleneck.md) | #16 | write_bottleneck.go |
| [spec-static-health.md](./spec-static-health.md) | ES-GAP-01～06 單次快照覆蓋擴充 | static_health.go |
| [spec-extended-health.md](./spec-extended-health.md) | ES-GAP-07～12 單次快照與選配功能覆蓋擴充 | extended_health.go + node_context.go |
| [spec-report.md](./spec-report.md) | 最終報告（結果契約 / JSON / 離線 HTML / 收斂規則 / check vs diagnose） | reporter/json.go, html.go + internal/diagnostic |

> **所有 analyzer 一律產出 spec-report.md §1 的 `DiagnosticResult`，不自行輸出文字。** 報告的組裝、收斂、渲染全在 reporter，與診斷邏輯解耦。

### 平台規格（連線 / 規則 / CLI / 症狀）

| 規格檔 | 內容 | 實作位置 |
|---|---|---|
| [spec-config.md](./spec-config.md) | 連線與設定（host/認證/TLS/逾時、來源優先序、安全） | config.yaml + collector/client.go |
| [spec-rules.md](./spec-rules.md) | 規則引擎（default.yaml schema、條件 DSL、變數命名空間、覆寫合併） | rules/default.yaml + internal/rules |
| [spec-cli.md](./spec-cli.md) | 指令、flag、**結束碼**、版本偵測與 fallback | cmd/ |
| [spec-diagnose-symptoms.md](./spec-diagnose-symptoms.md) | 症狀診斷樹（red-cluster / write-bottleneck / high-heap / ingest-lag / ilm-stuck）、反向觸發 | cmd/diagnose.go |
| [spec-resilience.md](./spec-resilience.md) | 錯誤與韌性：逾時/重試、部分不可達→unknown、host 故障轉移範圍 | collector/client.go + cmd/check.go, diagnose.go |
| [spec-bundle.md](./spec-bundle.md) | 採集與判斷分離：離線 bundle 模式、端點表單一事實來源 | collector/endpoints.go, client.go + cmd/check.go |
| [spec-node-context.md](./spec-node-context.md) | ES API 可見的多節點 OS/process/filesystem/JVM context、完整性與快照診斷 | collector/node_context.go + analyzer/node_context.go |

| 階段 | 內容 |
|---|---|
| **MVP** | `_health_report` 解析（基座）+ A 類解讀 + 缺口 #6 + JSON 報告（spec-report §1,2,4） |
| **v0.2** | 缺口主力 #7,8,9,12 + #11,13 + 離線 HTML 報告 |
| **v0.3** | #16 write-bottleneck + #17,18,32 + B 類加深 |
| **v0.4** | 長尾 #24,25,27,28,30,31,33,34,35,36,37 |

> 排除（結構性不可行）：Security 類（認證失敗則工具連不上）、Discovery（process 未起則 API 不通）、File-based recovery（需操作節點檔案系統）、Upgrade Assistant（破壞性一次性操作）。
