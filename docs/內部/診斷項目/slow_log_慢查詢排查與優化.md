---
title: "Search slow log 分析 — 慢查詢分期耗時、Profile API 與索引調優"
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
| 2026/08/23 | 1.0 | 初版：Query vs Fetch 兩階段慢日誌分析、Profile API 剖析與 DSL 調優 | 診斷工具架構組 |

# 文件說明

## 報告診斷卡對照

| 屬性 | 內容 |
|---|---|
| 報告分類區塊 | 效能（Performance） |
| 診斷卡中文名稱 | Search slow log 分析 |
| 診斷卡 ID | `slow_log` |
| 典型嚴重度 | `INFO` / `WARNING` |
| 觸發關鍵特徵 | 索引未啟用 Search Slowlog（無法捕捉慢查詢），或 Slowlog 記錄到大量超過閾值之慢搜尋 |

## 文件目的

本手札用於協助第一線 ELK 維運工程師與顧問在巡檢報告中評估慢日誌機制是否就緒，並在捕捉到慢查詢日誌時，能精準拆解 Query Phase 與 Fetch Phase 的耗時，指導開發端進行 DSL 語句優化。

## 適用範圍

本手札適用於所有版本之 Elasticsearch 7.x 及 8.x 叢集。

# 核心原理與慢查詢兩階段剖析

## 什麼是 Search Slow Log？

Elasticsearch 的搜尋執行分為兩個主要階段：

1. **Query Phase（查詢階段）**：
   - 協調節點將查詢廣播至所有相關 Shard。
   - 每個 Shard 透過 Lucene 倒排索引找出符合條件的文件，計算分數，並在記憶體中建立排序好的 Top N 優先佇列（Priority Queue），僅將 **Document ID 與分數** 回傳給協調節點。
2. **Fetch Phase（取回階段）**：
   - 協調節點彙整所有 Shard 的結果，篩選出全域最終的 Top N（例如第 1～10 筆）。
   - 協調節點僅向這 10 筆文件所在的分片發送取回請求，讀取硬碟中的 `_source` 完整內容並拼裝回傳給客戶端。

```text
客戶端 Search Request
       ↓
【Query Phase】各分片平行比對條件 → 回傳 (DocID + Score)
       ↓ 協調節點全域排序
【Fetch Phase】向目標分片讀取 _source 完整資料 → 回傳結果
```

## 慢在 Query 還是 Fetch？

- **Query 階段慢**：
  - 條件過多、包含複雜 Script、全文檢索未走索引、開頭萬用字元（`*abc`）、未加過濾（Filter Context 未快取）。
- **Fetch 階段慢**：
  - 單筆 Document 體積巨大（例如單筆 JSON 達 10MB）、深度分頁（`from: 10000, size: 50` 導致各 Shard 必須取回海量資料）、或啟用了高亮（Highlighting）且沒有合適的 Fast Vector Highlighter。

# 報告指標解讀指引

在 `check.html` 報告中，請檢視以下資訊：

| 欄位名稱 | 範例值 | 診斷意涵 |
|---|---|---|
| `slowlog_enabled_indices_count` | `0` | 尚未開啟慢日誌的索引數（盲飛狀態） |
| `slowlog_enabled` | `false` | 建議開啟 Slowlog 以利常態化監控 |

# 業務影響與技術說明建議

## 說明要點與原則

- **用「黑盒子 vs 行車記錄器」比喻**：說明沒開 Slowlog 好比沒裝行車記錄器，查詢變慢時無從追查是哪支 API 造成的。
- **強調開 Slowlog 的低開銷性**：說明只要合理設定 1s～2s 的閾值，平時正常查詢零日誌，對效能影響微乎其微。

## 說明範例：建議客戶啟用 Slowlog 並調優慢查詢

> **技術說明範例**：
> 「開發團隊您好，報告中顯示目前主要業務索引尚未配置 Search Slow Log（慢搜尋日誌）。
> 
> 這意味著當使用者反映系統偶發變慢時，我們無法回溯究竟是哪一條 DSL 查詢或哪一個儀表板在大吃資源。
> 
> 我們建議在核心索引上開啟門檻為 2 秒的慢日誌。這不會對生產效能產生負擔，但能像行車記錄器一樣，在出現慢查詢的第一時間捕捉到完整的查詢語句、耗時階段與參數，讓工程師能立刻針對性優化。」

# 現場排查與安全處置 SOP

## 階段一：動態開啟 Search Slow Log

為目標索引配置分級閾值（無需重啟節點，線上秒級生效）：

```http
PUT /<TARGET_INDEX>/_settings
{
  "index.search.slowlog.threshold.query.warn": "2s",
  "index.search.slowlog.threshold.query.info": "1s",
  "index.search.slowlog.threshold.fetch.warn": "1s",
  "index.search.slowlog.threshold.fetch.info": "500ms"
}
```

## 階段二：使用 Profile API 深度剖析慢查詢

在慢查詢 DSL 中加上 `"profile": true`，分析 Lucene 內部各個 Filter 與 Scorer 的納秒級耗時：

```http
POST /<TARGET_INDEX>/_search
{
  "profile": true,
  "query": {
    "bool": {
      "filter": [ { "term": { "status": "active" } } ],
      "must": [ { "match": { "content": "error" } } ]
    }
  }
}
```

## 階段三：常見 DSL 優化方案

1. **精確比對改用 `filter` 而非 `must`**：
   - `filter` 區塊不計算相關度分數（Score），且會自動被 Elasticsearch 放入記憶體快取（Node Query Cache）。
2. **深度分頁改用 `search_after`**：
   - 嚴禁使用 `from + size > 10000`。
3. **按需取回欄位（`_source` 過濾）**：
   - 只取回前端需要的欄位：`"_source": ["id", "title", "created_at"]`。

## 階段四：變更後驗證

- [ ] 查看 `<cluster-name>_index_search_slowlog.json` 日誌確認慢查詢已被成功記錄或已優化消除
- [ ] 重新執行 `elk-diagnostics check` 確認 `slow_log` 轉為 `PASS`

# 附錄 <!-- appendix -->

## 參考文件

- [Elasticsearch 官方文件：Search Slow Log](https://www.elastic.co/docs/reference/elasticsearch/configuration-reference/logging#search-slow-log)
- [Elasticsearch 官方文件：Profile API](https://www.elastic.co/docs/reference/elasticsearch/rest-apis/profile-api)
- 專案內部規格書：`docs/內部/規格/效能規格.md`（#31）
