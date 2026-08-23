---
title: "ELK 診斷手札：Thread pool 拒絕請求 — 執行緒池飽和、429 錯誤與重試風暴治理"
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
| 2026/08/23 | 1.0 | 初版：Thread Pool 佇列機制、Write/Search 拒絕成因與客戶端退避設計 | 診斷工具架構組 |

# 文件說明

## 報告診斷卡對照

| 屬性 | 內容 |
|---|---|
| 報告分類區塊 | 效能（Performance） |
| 診斷卡中文名稱 | Thread pool 拒絕請求 |
| 診斷卡 ID | `rejected_requests` |
| 典型嚴重度 | `WARNING`（偶發拒絕）/ `CRITICAL`（持續大量拒絕） |
| 觸發關鍵特徵 | `_nodes/stats/thread_pool` 或 `_cat/thread_pool` 顯示 `write` 或 `search` 執行緒池的 `rejected > 0` |

## 文件目的

本手札用於協助第一線 ELK 維運工程師與顧問在巡檢報告發現執行緒池拒絕時，快速區分是「寫入型拒絕（Write Rejections）」還是「查詢型拒絕（Search Rejections）」，並指導客戶端配置指數退避（Exponential Backoff）重試機制。

## 適用範圍

本手札適用於所有版本之 Elasticsearch 7.x 及 8.x 叢集。

# 核心原理與 Thread Pool 機制

## 什麼是 Thread Pool Rejections？

Elasticsearch 內部為不同的操作維護獨立的執行緒池（Thread Pool），其中最核心的兩個是：

1. **`write` 執行緒池**：
   - 固定尺寸（Fixed Pool），大小通常為 `allocated_processors + 1`。
   - 附帶一個固定長度的 Queue（預設佇列長度為 10,000）。
2. **`search` 執行緒池**：
   - 尺寸通常為 `(allocated_processors * 3) / 2 + 1`。
   - 附帶一個長度為 1,000 的 Queue。

```text
客戶端請求湧入
     ↓
【Worker 執行中】（Active = Max Size）
     ↓ 滿載時
【Queue 排隊】（Queue 正在累積 1... 1000... 10000）
     ↓ 佇列完全填滿時
【REJECTED 拒絕！】──→ 立即回傳 HTTP 429 (Too Many Requests) / es_rejected_execution_exception
```

## 拒絕發生的兩大主因

### 1. 寫入拒絕（Write Rejection）
- **瞬時突發流量（Traffic Spike）**：日誌量短時間內激增數倍。
- **慢寫入拖垮 Worker**：磁碟 I/O 延遲高、Refresh 間隔太短（1s 頻繁生成小段）、Ingest Pipeline 正規表達式效能差。

### 2. 查詢拒絕（Search Rejection）
- **大量併發慢查詢**：報表系統或儀表板（Kibana）在同一秒發送數十個掃描數億文件的聚合查詢，佔滿所有 Search Worker，後續查詢立刻被 Queue 擠出並拒絕。

# 報告指標解讀指引

在 `check.html` 報告中，請檢視以下數值：

| 欄位名稱 | 範例值 | 診斷意涵 |
|---|---|---|
| `write.rejected` | `1,250` | 寫入被拒絕的累積次數 |
| `write.queue` | `10,000` | 寫入佇列當前排隊數量（已滿） |
| `search.rejected` | `45` | 查詢被拒絕的累積次數 |
| `search.queue` | `1,000` | 查詢佇列當前排隊數量（已滿） |

# 業務影響與技術說明建議

## 說明要點與原則

- **說明拒絕是保護機制**：拒絕請求是為了防止佇列無限制膨脹導致記憶體耗盡當機。
- **推動客戶端重試規範**：強調客戶端「遇到 429 盲目立即重試」只會雪上加霜，必須實施「指數退避（Backoff）」。

## 說明範例：日誌採集端遭遇 429 拒絕

> **技術說明範例**：
> 「維運同仁您好，報告中顯示叢集的 `write` 執行緒池發生了多次請求拒絕（429 錯誤）。
> 
> 這代表在業務流量高峰時，寫入請求的灌入速度超過了後端硬碟與 CPU 的消化能力，導致 10,000 個排隊名額全部被佔滿。
> 
> 很多同仁第一反應是想把 Queue 調大到 50,000，但這只會把排隊的資料全部壓在記憶體裡，隨時可能引發全伺服器 OOM 崩潰。
> 
> 真正的解決方案有兩步：
> 1. **在客戶端（如 Logstash/SDK）啟用帶隨機抖動的指數退避重試（Exponential Backoff with Jitter）**，避免所有客戶端同時重試引發重試風暴；
> 2. **優化 Elasticsearch 寫入效能**（如調大 Refresh Interval、批次合併），從根本提升每秒消化能力。」

# 現場排查與安全處置 SOP

## 階段一：現場唯讀佐證（定位哪個節點在拒絕）

```http
GET /_cat/thread_pool/write,search?v&h=node_name,name,active,queue,rejected,completed
```

## 階段二：整改修復方案

### 方案 A：客戶端實施指數退避重試（最關鍵）
客戶端在捕獲 HTTP 429 時，依序等待 `100ms -> 200ms -> 400ms -> 800ms` 並加上隨機值後重試，切勿立刻死循環重送。

### 方案 B：調大日誌索引的 Refresh Interval
```http
PUT /<LOG_INDEX>/_settings
{
  "index.refresh_interval": "30s"
}
```

## 階段三：變更後驗證

- [ ] 觀察客戶端是否仍有 429 錯誤日誌
- [ ] 執行 `GET /_cat/thread_pool` 確認 `queue` 數字降至 0 或極低水位
- [ ] 重新執行 `elk-diagnostics check` 產出最新報告

# 常見誤區與風險提示

- [ ] **誤區一：手動把 `thread_pool.write.queue_size` 調成十幾萬**：
  官方極度不建議放大 queue_size。超大 Queue 不會增加吞吐量，只會增加記憶體開銷與請求超時（Timeout）機率。

# 附錄 <!-- appendix -->

## 參考文件

- [Elasticsearch 官方文件：Thread pool settings](https://www.elastic.co/docs/reference/elasticsearch/configuration-reference/thread-pool-settings)
- 專案內部規格書：`docs/內部/規格/效能規格.md`（#6）
