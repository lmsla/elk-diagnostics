---
title: "Search concurrency 負載 — 查詢併發限制、分片分散度與協調節點保護"
project: elk-diagnostics
document_type: 診斷手札
version: 1.0
date: 2026-08-23
owner: ELK 維運架構團隊
audience: 內部維運工程師 / 交付顧問 / SRE
numbering: engineering
system: ELK 8.x / 9.x
---

# 修訂記錄 <!-- no-number -->

| 修訂日期 | 版號 | 修訂內容 | 修訂者 |
|---|---|---|---|
| 2026/08/23 | 1.0 | 初版：搜尋併發模型、`max_concurrent_shard_requests` 與大查詢風暴治理 | 診斷工具架構組 |

# 文件說明

## 報告診斷卡對照

| 屬性 | 內容 |
|---|---|
| 報告分類區塊 | 效能（Performance） |
| 診斷卡中文名稱 | Search concurrency 負載 |
| 診斷卡 ID | `search_concurrency` |
| 典型嚴重度 | `WARNING` / `CRITICAL` |
| 觸發關鍵特徵 | 單一查詢涉及過多分片（如跨數百個 Shards 查詢）且未限制併發度，引發 Coordinating 節點記憶體暴增 |

## 文件目的

本手札用於協助第一線 ELK 維運工程師與顧問在巡檢報告發現搜尋併發過高時，分析前端應用程式是否在無意中發送了掃描「數百個分片」的未收斂查詢，並指導配置 `max_concurrent_shard_requests` 與 Kibana 查詢優化。

## 適用範圍

本手札適用於所有版本之 Elasticsearch 7.x 及 8.x 叢集。

# 核心原理與搜尋併發模型

## 什麼是 Search Concurrency 與 Coordinating 衝擊？

當客戶端發送一個跨大時間範圍的查詢（例如 `GET /logs-*/_search`）：
- 該萬用字元可能匹配到 50 個索引，涉及 250 個 Shards。
- 協調節點（Coordinating Node）預設會向所有 250 個 Shard 發起平行查詢請求。

```text
客戶端大查詢 (logs-*)
       ↓
【Coordinating 協調節點】
同時向 250 個 Shard 發起請求並等待回傳
       ↓
┌─────────────────────────────────────────────────────────────┐
│ 1. 記憶體爆炸：每個 Shard 回傳優先佇列，協調節點需在記憶體合併  │
│ 2. 執行緒排隊：所有 Data 節點的 Search Thread Pool 被瞬間佔滿 │
│ 3. 阻塞其他業務：一般小查詢被大查詢擠在 Queue 裡超時          │
└─────────────────────────────────────────────────────────────┘
```

## 保護機制：`max_concurrent_shard_requests`

為了避免單一查詢癱瘓叢集，Elasticsearch 提供了 `max_concurrent_shard_requests` 參數（預設為每個請求最多平行打 5 個 Shards）：
- 它將大查詢的分片請求改為「分批執行（Batched）」；
- 雖然稍微增加了該大查詢的總耗時，但能徹底保護 Coordinating 節點記憶體與全域執行緒池。

# 報告指標解讀指引

在 `check.html` 報告中，請檢視以下資訊：

| 欄位名稱 | 警戒門檻 | 診斷意涵 |
|---|---|---|
| `active_searches` | 接近 search pool 上限 | 當前正在執行的查詢併發數 |
| `avg_shards_per_search` | > 50 shards | 單一查詢掃描分片數過多 |

# 業務影響與技術說明建議

## 說明範例：查詢涵蓋過多分片引發延遲

> **技術說明範例**：
> 「開發團隊您好，報告中顯示叢集的搜尋併發負載（`Search concurrency`）偏高，平均每個查詢掃描了超過 80 個分片。
> 
> 經分析，是因為前端預設使用了 `logs-*` 進行全量搜尋，即使使用者只想看今天最後 10 分鐘的資料，系統依然會把過去半年的所有歷史分片全部喚醒並發起並行查詢。
> 
> 這會使資料節點的搜尋執行緒瞬間飽和，並拖慢其他使用者的操作。
> 
> 我們建議在前端加上時間索引過濾（如 `logs-2026.08.23`）或使用 Kibana 預設的時間收斂過濾，將單次查詢分片數控制在 5～10 個以內，查詢延遲即可從 3 秒縮短至 100 毫秒以內。」

# 現場排查與安全處置 SOP

## 階段一：現場唯讀佐證

查詢當前正在執行的搜尋任務及其分片分佈：

```http
GET /_tasks?actions=*search*&detailed=true
```

## 階段二：整改修復方案

### 方案 A：在前端查詢加入 `max_concurrent_shard_requests` 限制
```http
POST /logs-*/_search?max_concurrent_shard_requests=5
{
  "query": { ... }
}
```

### 方案 B：應用層按日期精確路由
避免全量模糊查詢 `logs-*`，改為動態拼裝時間範圍（如 `logs-2026.08.22,logs-2026.08.23`）。

## 階段三：變更後驗證

- [ ] 執行 `GET /_cat/thread_pool/search` 確認 `active` 保持在健康範圍
- [ ] 重新執行 `elk-diagnostics check` 確認 `search_concurrency` 轉為 `PASS`

# 附錄 <!-- appendix -->

## 參考文件

- [Elasticsearch 官方文件：Search request concurrency](https://www.elastic.co/docs/reference/elasticsearch/rest-apis/search)
- 專案內部規格書：`docs/內部/規格/效能規格.md`
