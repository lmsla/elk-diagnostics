---
title: "ELK 診斷手札：Circuit breaker 斷路器 — Parent 熔斷跳閘與短時高壓排查"
project: elk-diagnostics
document_type: 診斷手札
version: 1.0
date: 2026-08-23
owner: ELK 維運架構團隊
audience: 內部維運工程師 / 交付顧問 / SRE
status: approved
numbering: engineering
---

# 修訂記錄 <!-- no-number -->

| 修訂日期 | 版號 | 修訂內容 | 修訂者 |
|---|---|---|---|
| 2026/08/23 | 1.0 | 初版：Circuit Breaker 熔斷家族、Parent Breaker 計算原理與防 OOM 調優 | 診斷工具架構組 |

# 文件說明

## 報告診斷卡對照

| 屬性 | 內容 |
|---|---|
| 報告分類區塊 | 效能（Performance） |
| 診斷卡中文名稱 | Circuit breaker 斷路器 |
| 診斷卡 ID | `circuit_breaker` |
| 典型嚴重度 | `WARNING`（曾發生跳閘）/ `CRITICAL`（頻繁跳閘） |
| 觸發關鍵特徵 | `_nodes/stats/breaker` 偵測到 `tripped > 0`，客戶端拋出 `CircuitBreakingException` |

## 文件目的

本手札用於協助第一線 ELK 維運工程師與顧問在巡檢報告發現斷路器跳閘時，向客戶解釋熔斷機制的保護意義，定位短時間內暴增的記憶體消耗源頭（如大聚合、昂貴查詢或 In-flight 請求），並給出架構防護建議。

## 適用範圍

本手札適用於所有版本之 Elasticsearch 7.x 及 8.x 叢集。

# 核心原理與熔斷器家族體系

## 為什麼 Heap 沒滿卻會拋 CircuitBreakingException？

客戶維運常問：*「我的 JVM Heap 才 70%，為什麼查詢會拋 `CircuitBreakingException: [parent] Data too large` 被拒絕？」*

這是因為 **Circuit Breaker（斷路器）是主動的「事前預估（Pre-flight Check）機制」**，而不是等 JVM 真的發生 OOM 當機才介入。

在執行任何大型查詢、聚合（Aggregations）或接收大批次寫入前，ES 會先預估這筆操作需要消耗多少記憶體：

```text
預估後總記憶體 = 當前已用 Heap + 本次操作所需記憶體
     ↓
┌───────────────────────────────────────────────┐
│ 判定點：是否超過 Parent Circuit Breaker 閾值？  │
│ （預設為 JVM Heap 的 95% 或 70%）               │
└───────────────────────────────────────────────┘
     ↓
【超過上限】──→ 立刻攔截並拒絕該請求，拋出 CircuitBreakingException！
                 ▲ 犧牲單一請求，拯救整座 JVM 避免 OOM 崩潰！
```

## Elasticsearch 斷路器家族

| 斷路器名稱 | 監控範圍與職責 | 預設閾值 |
|---|---|---|
| `parent` | 全域總熔斷器（包含所有子熔斷器與整體 Heap 預估） | 95% Heap（7.x/8.x） |
| `fielddata` | 用於 `text` 欄位排序/聚合的 Fielddata cache 記憶體 | 40% Heap |
| `request` | 查詢中單一請求聚合（如 bucket aggregation）結構所需記憶體 | 60% Heap |
| `in_flight_requests`| 網路傳輸層（Transport/HTTP）正在傳遞中的請求封包記憶體 | 100% Heap |
| `accounting` | Lucene 索引段記憶體、Segment FST、RAM 結構 | 100% Heap |

# 報告指標解讀指引

在 `check.html` 報告中，請檢視各斷路器的跳閘次數與預估使用量：

| 欄位名稱 | 範例值 | 診斷意涵 |
|---|---|---|
| `parent.tripped` | `15` | Parent 總熔斷器歷史累積跳閘次數 |
| `parent.estimated_size` | `28.5 GB` | 熔斷器預估的即時記憶體佔用 |
| `parent.limit_size` | `29.4 GB` | 觸發熔斷的硬性上限 |
| `fielddata.tripped` | `0` | Fielddata 熔斷器跳閘次數 |

# 客戶溝通話術與情境模擬

## 溝通策略原則

- **用「保險絲（無熔絲開關）」比喻**：說明跳閘是保護主機沒燒掉的英雄，而不是 Bug。
- **糾正盲目調大閾值的錯誤想法**：解釋調大閾值就像把保險絲換成銅線，只會直接導致 JVM OOM 崩潰當機。

## 話術範例：對開發主管與架構師

> **顧問說明範例**：
> 「開發主管您好，報告中顯示叢集曾多次觸發 `Parent Circuit Breaker` 熔斷。
> 
> 這好比家裡的無熔絲開關跳電。當業務端發送了超大範圍的時間聚合查詢（例如一次撈 3 年資料做 Cardinality），Elasticsearch 在動手前計算出這筆查詢會吃掉 10GB 記憶體，直接衝破 95% 的安全紅線。
> 
> 系統為了防止整座伺服器因為記憶體耗盡（OOM）直接死機，選擇主動拒絕這筆查詢來保全整座叢集。
> 
> 我們不應該去調高熔斷門檻（那會引發崩潰），而是應該優化前端查詢語句（如限制查詢時間範圍、加上過濾條件、使用 `composite aggregation` 分頁），從源頭消除大查詢的衝擊。」

# 現場排查與安全處置 SOP

## 階段一：現場唯讀佐證（定位哪一個 Breaker 跳閘）

查詢各節點所有 Breaker 的即時統計：

```http
GET /_nodes/stats/breaker?filter_path=nodes.*.name,nodes.*.breakers
```

## 階段二：抓出引發熔斷的元兇查詢

1. 開啟或調低 Search Slow Log，捕捉高耗時與大資料量查詢：

```http
PUT /<TARGET_INDEX>/_settings
{
  "index.search.slowlog.threshold.query.warn": "2s",
  "index.search.slowlog.threshold.fetch.warn": "1s"
}
```

2. 檢查是否有高基數聚合未限制大小（例如 `terms` aggregation 未設 `size` 或設成 100000）。

## 階段三：整改優化方案

### 方案 A：優化業務查詢（根治方案）
- 將 `terms` 聚合改為帶有精確過濾條件的 query。
- 巨量資料匯出改用 `Scroll`（或 8.x 的 `search_after`）搭配 `Point-in-Time (PIT)`。

### 方案 B：若為 Fielddata 引發，關閉 text 欄位的 fielddata
將 `text` 欄位改為使用 `keyword` 的 `doc_values` 進行聚合。

## 階段四：變更後驗證

- [ ] 觀察業務端 `CircuitBreakingException` 錯誤是否歸零
- [ ] 執行 `GET /_nodes/stats/breaker` 確認 `tripped` 計數不再持續增加
- [ ] 重新執行 `elk-diagnostics check` 產出最新報告

# 常見誤區與風險提示

- [ ] **誤區一：手動調高 `indices.breaker.total.use_real_memory` 或設為 99%**：
  這是極度危險的行為，一旦記憶體突增，JVM 無法及時觸發 GC，會直接被作業系統 OOM-Killer 強制殺死行程！

# 附錄 <!-- appendix -->

## 參考文件

- [Elasticsearch 官方文件：Circuit breaker settings](https://www.elastic.co/docs/reference/elasticsearch/configuration-reference/circuit-breaker-settings)
- 專案內部規格書：`docs/內部/規格/效能規格.md`（#8）
