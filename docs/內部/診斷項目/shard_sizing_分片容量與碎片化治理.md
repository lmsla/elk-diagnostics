---
title: "ELK 診斷手札：Shard 大小與小 shard 增生 — 分片容量容量規劃、碎片化治理與 Shrink 縮分片"
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
| 2026/08/23 | 1.0 | 初版：分片黃金容量區間（10GB～50GB）、碎片化記憶體浪費與 Shrink/Rollover 治理 | 診斷工具架構組 |

# 文件說明

## 報告診斷卡對照

| 屬性 | 內容 |
|---|---|
| 報告分類區塊 | 管理（Management） |
| 診斷卡中文名稱 | Shard 大小與小 shard 增生 |
| 診斷卡 ID | `shard_sizing` |
| 典型嚴重度 | `WARNING` / `CRITICAL` |
| 觸發關鍵特徵 | 單一 Shard 大小超過 50GB（過大風險），或存在大量小於 1GB 之碎片化 Primary Shard（小分片增生） |

## 文件目的

本手札用於協助第一線 ELK 維運工程師與顧問在巡檢報告發現分片規劃不當時，向客戶說明「分片太大（>50GB）」與「分片太小（<1GB 海量碎片）」對搜尋延遲、記憶體開銷與節點復原速度的實體影響，並給出標準的分片架構整改方案。

## 適用範圍

本手札適用於所有版本之 Elasticsearch 7.x 及 8.x 叢集。

# 核心原理與分片容量黃金標準

## 官方推薦分片容量黃金區間

| 分片容量級別 | 容量區間 | 評估與影響 | 處置建議 |
|---|---|---|---|
| **過小（碎片化）** | < 1 GB | **極度浪費記憶體**。每千個小分片常駐吃掉數 GB JVM Heap，且查詢時需要發送大量網路子請求。 | 依日期合併或使用 Shrink |
| **黃金標準區間** | **10 GB ～ 50 GB** | **最佳效能與平衡**。兼顧寫入並行度、Lucene 段合併效率與故障復原速度。 | 保持現有生命週期 |
| **過大（肥大化）** | > 50 GB | **高危險**。節點重啟時分片跨網路搬移極為緩慢，且段合併（Merge）極易吃滿 I/O。 | 縮減 Rollover 門檻 |

```text
【分片數量與記憶體的代價】
每個分片都是一個獨立的 Lucene 實例，在記憶體中常駐維護：
1. FST 詞典 (Term Inverted Index Memory)
2. 檔案描述符 (File Descriptors)
3. 執行緒與佇列資源
▲ 100 個 100MB 的碎片小分片，消耗的 JVM 記憶體比 1 個 10GB 的標準分片高出 10 倍以上！
```

# 報告指標解讀指引

在 `check.html` 報告中，請檢視以下分片分佈指標：

| 欄位名稱 | 警戒門檻 | 診斷意涵 |
|---|---|---|
| `oversized_shards_count` | > 0 | 容量超過 50GB 的肥大分片數 |
| `undersized_shards_count` | > 100 | 小於 1GB 的碎片小分片總數 |
| `max_shard_size` | > 50 GB | 叢集中最大的單一分片大小 |

# 客戶溝通話術與情境模擬

## 溝通策略原則

- **用「小貨車 vs 貨櫃車」比喻**：說明碎片化好比用 1,000 台小發財車只載 1 箱貨，司機薪水與過路費（記憶體開銷）會拖垮公司；過大分片好比超載砂石車，一翻車路面整天癱瘓。
- **強調效能提升**：說明將小分片合併為標準分片，能直接減少 40% 的搜尋延遲並釋放數 GB Heap。

## 話術範例：日誌按小時分區導致萬級小分片

> **顧問說明範例**：
> 「架構團隊您好，報告中顯示叢集目前累積了超過 3,500 個小分片，且 90% 以上的分片體積小於 50MB。
> 
> 經分析，是因為日誌目前採用了『按小時建立索引（hourly index）』且每個索引配置了 5 個分片。
> 
> 這好比每天派出了上百台大卡車去運送幾張紙，這些零碎分片在記憶體中常駐吃掉了 12GB 的 JVM Heap，導致 Master 節點負擔沉重。
> 
> 官方的黃金標準是『單一分片容量維持在 10GB～50GB 之間』。我們建議改用按天（Daily）或直接改用 **Data Stream + ILM Rollover**，將分片總數縮減至 200 個以內，搜尋速度與記憶體效率將獲得立竿見影的提升。」

# 現場排查與安全處置 SOP

## 階段一：現場唯讀佐證（找出過大與過小的分片）

查詢所有分片的大小排行榜：

```http
GET /_cat/shards?v&s=store:desc&h=index,shard,prirep,store,docs
```

## 階段二：整改修復方案

### 方案 A：對歷史小索引執行 `_shrink` 縮分片
將歷史多分片索引收斂為單一分片：

1. 先將索引設為唯讀並集中至單一節點：
```http
PUT /<HISTORY_INDEX>/_settings
{
  "settings": {
    "index.blocks.write": true,
    "index.routing.allocation.require._name": "es-data-01"
  }
}
```

2. 呼叫 `_shrink` 縮減為 1 個分片：
```http
POST /<HISTORY_INDEX>/_shrink/<HISTORY_INDEX>-shrunk
{
  "settings": {
    "index.number_of_shards": 1,
    "index.number_of_replicas": 1
  }
}
```

### 方案 B：修改未來模板（Template）
- 將 `number_of_shards` 從 5 調整為 1 或與 Data 節點數相稱。
- 採用 ILM `max_primary_shard_size: 50gb` 進行精準滾動。

## 階段三：變更後驗證

- [ ] 執行 `GET /_cat/allocation` 確認總分片數大幅下降
- [ ] 重新執行 `elk-diagnostics check` 確認 `shard_sizing` 轉為 `PASS`

# 附錄 <!-- appendix -->

## 參考文件

- [Elasticsearch 官方文件：Size your shards](https://www.elastic.co/docs/reference/elasticsearch/configuration-reference/advanced-configuration#size-your-shards)
- 專案內部規格書：`docs/內部/規格/靜態健檢規格.md`（ES-GAP-02）
