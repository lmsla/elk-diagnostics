---
title: "Fielddata 記憶體 — Text 欄位排序聚合與 Heap 洩漏防範"
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
| 2026/08/23 | 1.0 | 初版：Fielddata 原理、Text vs Keyword 記憶體模型與 Eviction 調優 | 診斷工具架構組 |

# 文件說明

## 報告診斷卡對照

| 屬性 | 內容 |
|---|---|
| 報告分類區塊 | 效能（Performance） |
| 診斷卡中文名稱 | Fielddata 記憶體 |
| 診斷卡 ID | `fielddata_memory` |
| 典型嚴重度 | `INFO`（單次快照）/ `WARNING`（Eviction 持續增加且伴隨 GC 壓力） |
| 觸發關鍵特徵 | `_nodes/stats/indices/fielddata` 偵測到 Fielddata Cache 記憶體佔用偏高或發生快取淘汰（Evictions） |

## 文件目的

本手札用於協助第一線 ELK 維運工程師與顧問在巡檢報告發現 Fielddata 記憶體佔用時，釐清 `text`、`keyword`、`doc_values` 與 Fielddata 的底層關係，向客戶解釋為何在高基數 Text 上做排序會引發 JVM 記憶體暴增，並指導安全的 Mapping 重構方案。

## 適用範圍

本手札適用於所有版本之 Elasticsearch 7.x 及 8.x 叢集。

# 核心原理與 Fielddata 機制

## 什麼是 Fielddata？

Elasticsearch 的全文搜尋使用「倒排索引（Inverted Index）」，適合從「詞元（Term）→ 文件 ID」的高效反查。

然而，當使用者對某個欄位進行 **排序（Sort）**、**聚合（Aggregations，如 terms agg）** 或在 Painless Script 中透過 `doc['field']` 取值時，運算引擎需要相反的查詢方向：「**文件 ID → 欄位值**」。

- 對於 `keyword`、`numeric`、`date` 欄位：預設使用硬碟上的正排結構 **`doc_values`**，不消耗 JVM Heap。
- 對於已分詞的 **`text` 欄位**：預設禁用正排結構。若強制對 `text` 欄位開啟排序或聚合，Elasticsearch 必須在查詢時將整份索引的所有分詞載入記憶體，動態建構一組常駐於 JVM Heap 的記憶體內正排結構，這就是 **Fielddata**。

```text
【Doc Values 與 Fielddata 差異對照】
┌─────────────────────────────────────────────────────────────┐
│ 1. Doc Values（預設開啟，針對 keyword/數值/日期）             │
│    儲存在磁碟（Column-oriented on disk），由 OS Page Cache 加速 │
│    ▲ 完全不吃 JVM Heap，極度安全穩定！                        │
├─────────────────────────────────────────────────────────────┤
│ 2. Fielddata（預設關閉，僅針對 text）                        │
│    動態載入並常駐於 JVM Old Gen 記憶體中                      │
│    ▲ 高基數欄位載入時極易吃光 Heap，引發 GC 停頓與 OOM！        │
└─────────────────────────────────────────────────────────────┘
```

## 為什麼高基數 Text 開啟 Fielddata 極度危險？

高基數代表欄位有海量不重複值（例如 URL、`user_id`、`request_id`、日誌原始訊息 `message`）。

若對此類欄位開啟 Fielddata：
1. 查詢發生的第一瞬間，JVM 需要將數千萬個不重複詞元及其文件關聯全部載入 Heap，引發長達數秒的查詢延遲尖峰。
2. Fielddata Cache 迅速填滿（預設上限 40% Heap），引發頻繁的 **Cache Eviction（快取淘汰）** 與頻繁的垃圾回收（GC）。
3. 觸發 `Fielddata Circuit Breaker` 熔斷異常，嚴重時直接引發 `OutOfMemoryError`。

# 報告指標解讀指引

在 `check.html` 報告中，請檢視以下數值：

| 欄位名稱 | 單位 | 診斷意涵 | 處置指引 |
|---|---|---|---|
| `memory_size_in_bytes` | MB / GB | 該節點目前 Fielddata 快取佔用的 JVM Heap | 若超過數 GB 需排查欄位 |
| `evictions` | 次數 | 節點啟動後累積的 Fielddata 快取淘汰次數 | 單次快照標記為 INFO 觀察；持續增加需警惕 |

判讀原則：
- 記憶體有值不代表一定故障，需結合 JVM Heap 與 Circuit Breaker 交叉判斷。
- `evictions` 為累積值；只有在兩次以上採集發現 eviction 持續增長且伴隨 GC 壓力時，才需升級為 Warning 調查。

# 業務影響與技術說明建議

## 說明要點與原則

- **用「書本目錄 vs 書後索引」比喻**：說明 Text 適合搜尋全文，但不適合拿來做排行統計；拿長篇文字做統計好比把整本書逐字背進腦袋，腦袋（Heap）一定會燒掉。
- **提出 Multi-field 替代方案**：說明標準做法是 `text` + `keyword` 雙軌並存，兼顧全文檢索與零記憶體消耗的精準聚合。

## 說明範例：客戶對 Text 欄位開啟 Fielddata 做統計

> **技術說明範例**：
> 「開發團隊您好，我們在檢查中發現節點的 Fielddata 快取佔用了數 GB 的 JVM 記憶體。
> 
> 經分析，是因為前端報表直接對 `message`（長文本欄位）執行了聚合統計，觸發了底層將數百萬個分詞載入記憶體。
> 
> 這會直接蠶食伺服器的核心 Heap 空間，導致垃圾回收器不堪重負。
> 
> 標準的最佳實踐是採用 **Multi-field（多欄位型態）**：讓 `message` 專門負責全文搜尋，並自動衍生一個 `message.keyword` 專門負責排序與統計。`keyword` 使用硬碟正排結構（Doc Values），在執行聚合時完全不吃 JVM 記憶體，既能滿足報表需求，又能徹底免除記憶體洩漏風險。」

# 現場排查與安全處置 SOP

## 階段一：現場唯讀佐證（找出誰在吃 Fielddata）

查詢目前各欄位的 Fielddata 記憶體佔用排行榜：

```http
GET /_cat/fielddata?v&s=size:desc
```

## 階段二：整改修復方案

### 方案 A：在 Mapping 中配置 Multi-field（最佳解法）

```http
PUT /<TARGET_INDEX>/_mapping
{
  "properties": {
    "user_agent": {
      "type": "text",
      "fields": {
        "keyword": {
          "type": "keyword",
          "ignore_above": 256
        }
      }
    }
  }
}
```

業務端查詢時，將聚合欄位由 `user_agent` 改為 `user_agent.keyword`。

### 方案 B：關閉歷史索引的 Fielddata
若非必要，主動關閉該欄位的 fielddata 設定：

```http
PUT /<TARGET_INDEX>/_mapping
{
  "properties": {
    "user_agent": {
      "type": "text",
      "fielddata": false
    }
  }
}
```

## 階段三：變更後驗證

- [ ] 執行 `GET /_cat/fielddata` 確認記憶體佔用已釋放
- [ ] 重新執行 `elk-diagnostics check` 確認 `fielddata_memory` 轉為 `PASS`

# 附錄 <!-- appendix -->

## 參考文件

- [Elasticsearch 官方文件：Text field type & Fielddata](https://www.elastic.co/guide/en/elasticsearch/reference/current/text.html#fielddata-mapping-param)
- [Elasticsearch 官方文件：Doc values](https://www.elastic.co/guide/en/elasticsearch/reference/current/doc-values.html)
- 專案內部規格書：`docs/內部/規格/擴充健檢規格.md`（ES-GAP-16）
