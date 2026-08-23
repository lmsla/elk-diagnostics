---
title: "ELK 診斷手札：CPU / Hot threads — CPU 高負載、熱執行緒呼叫堆疊解讀與調優"
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
| 2026/08/23 | 1.0 | 初版：Hot Threads 呼叫堆疊解讀、Lucene 搜尋/合併/正則高 CPU 排查 | 診斷工具架構組 |

# 文件說明

## 報告診斷卡對照

| 屬性 | 內容 |
|---|---|
| 報告分類區塊 | 效能（Performance） |
| 診斷卡中文名稱 | CPU / Hot threads |
| 診斷卡 ID | `high_cpu` |
| 典型嚴重度 | `WARNING`（CPU > 85%）/ `CRITICAL`（CPU > 95% 持續高壓） |
| 觸發關鍵特徵 | `_cat/nodes` 顯示 CPU 使用率飆高，且 `_nodes/hot_threads` 呈現特定執行緒 100% 佔用 |

## 文件目的

本手札用於協助第一線 ELK 維運工程師與顧問在巡檢報告發現節點 CPU 飆高時，透過 `hot_threads` 呼叫堆疊（Stack Trace）精準判定是查詢（Search）、聚合（Aggs）、段合併（Segment Merge）還是 Ingest Grok 耗盡 CPU，並給出精準解法。

## 適用範圍

本手札適用於所有版本之 Elasticsearch 7.x 及 8.x 叢集。

# 核心原理與 Hot Threads 解讀心法

## 什麼是 Hot Threads API？

`GET /_nodes/hot_threads` 是 Elasticsearch 最強大的原生診斷武器之一。它會在一段時間內多次取樣節點的 JVM 執行緒堆疊，直接揪出**「當下是哪一行 Java 代碼正在瘋狂燃燒 CPU」**。

## 常見 Hot Threads 呼叫堆疊四大型態

```text
1. 查詢與正則高 CPU（Search / Script / Wildcard）
   org.apache.lucene.search.RegexpQuery...
   org.elasticsearch.painless...
   ▲ 診斷：業務端正在執行開頭帶星號的萬用字元查詢（*abc*）或未編譯的 Painless Script。

2. 高基數聚合高 CPU（Aggregations）
   org.elasticsearch.search.aggregations.bucket.terms.StringTermsAggregator...
   ▲ 診斷：大範圍高基數字串聚合正在消耗算力。

3. Ingest Pipeline 正則回溯（Grok Processor）
   org.joni.Regex.match... / Joni (Java Oniguruma)
   ▲ 診斷：日誌包含非預期格式，導致 Grok 出現嚴重的 Catastrophic Backtracking（災難性回溯）。

4. 頻繁段合併高 CPU（Lucene Segment Merging）
   org.apache.lucene.index.ConcurrentMergeScheduler...
   ▲ 診斷：大批量連續寫入時，Lucene 正在後台將數百個小檔案壓縮合併為大檔案。
```

# 報告指標解讀指引

在 `check.html` 報告中，請檢視以下資訊：

| 欄位名稱 | 警戒門檻 | 診斷意涵 |
|---|---|---|
| `cpu_percent` | > 85%（Warning）<br>> 95%（Critical） | 節點即時 CPU 使用率 |
| `hot_threads_sample` | 特定 stack trace | 當下消耗 CPU 的代碼堆疊 |

# 業務影響與技術說明建議

## 說明要點與原則

- **不要只講 CPU 滿了，要講出「哪種操作」在吃 CPU**：明確告訴客戶是搜尋、日誌解析還是後台資料庫整理。
- **針對性提出優化點**：例如指出是某一條報表 SQL/DSL 語句拖垮了整座叢集。

## 說明範例：萬用字元查詢與 Grok 回溯引發 CPU 100%

> **技術說明範例**：
> 「開發團隊您好，報告顯示 `es-node-02` 的 CPU 長期處於 95% 滿載狀態。
> 
> 我們分析了系統即時抓取的熱執行緒（Hot Threads）呼叫堆疊，發現並非資料量過大，而是有以下兩個高耗能元兇：
> 1. 前端查詢頻繁使用了**開頭萬用字元（`*keyword*`）**搜尋，導致 Lucene 無法走倒排索引加速，必須對數千萬筆文件做全量逐字掃描；
> 2. Logstash 送入的未格式化錯誤日誌，觸發了 Grok 正則表達式的反覆回溯計算。
> 
> 我們建議前端查詢改用 `match_phrase_prefix` 或 `wildcard` 欄位類型，並在 Grok 中加上防呆長度限制，CPU 即可迅速回落至 30% 的健康水準。」

# 現場排查與安全處置 SOP

## 階段一：現場唯讀佐證（抓取當下 Hot Threads）

```http
GET /_nodes/hot_threads?threads=5&interval=500ms&type=cpu
```

觀察前 3 個熱執行緒的 Java 套件名稱（Package Name）。

## 階段二：整改修復方案

### 狀況 A：若是 Grok 正則吃 CPU
- 在 Logstash 或 Ingest Pipeline 中避免使用貪婪匹配 `.*`，改用精確 pattern（如 `\S+` 或 `\d+`）。

### 狀況 B：若是萬用字元查詢吃 CPU
- 將需要模糊搜尋的欄位在 Mapping 中宣告為 `wildcard` 欄位類型（7.9+ 專利加速結構）。

### 狀況 C：若是高併發段合併（Merge）吃 CPU
- 將寫入並行數調優，或在巨量歷史資料匯入時暫時將 `index.number_of_replicas` 調為 0，匯入完畢後再恢復。

## 階段三：變更後驗證

- [ ] 執行 `GET /_cat/nodes?v&h=name,cpu` 確認 CPU 使用率回落至 80% 以下
- [ ] 重新執行 `elk-diagnostics check` 產出最新報告

# 附錄 <!-- appendix -->

## 參考文件

- [Elasticsearch 官方文件：Nodes hot threads API](https://www.elastic.co/docs/reference/elasticsearch/rest-apis/nodes-hot-threads)
- 專案內部規格書：`docs/內部/規格/效能規格.md`（#9）
