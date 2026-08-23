---
title: "ELK 診斷手札：Indexing pressure 當下使用量 — 寫入背壓機制與階層節流排查"
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
| 2026/08/23 | 1.0 | 初版：Indexing Pressure 三階段記憶體模型、節流（Throttling）與寫入拒絕調優 | 診斷工具架構組 |

# 文件說明

## 報告診斷卡對照

| 屬性 | 內容 |
|---|---|
| 報告分類區塊 | 效能（Performance） |
| 診斷卡中文名稱 | Indexing pressure 當下使用量 |
| 診斷卡 ID | `indexing_pressure` |
| 典型嚴重度 | `WARNING`（使用量 > 80%）/ `CRITICAL`（使用量 > 95% 或發生拒絕） |
| 觸發關鍵特徵 | `_nodes/stats/indexing_pressure` 顯示 Coordinating、Primary 或 Replica 寫入記憶體衝破保護閾值 |

## 文件目的

本手札用於協助第一線 ELK 維運工程師與顧問在巡檢報告發現寫入背壓（Indexing Pressure）高壓時，精準拆解寫入資料流在哪一個階段發生阻塞，指導客戶排查寫入瓶頸並避免 429 拒絕。

## 適用範圍

本手札適用於 Elasticsearch 7.10+ 及 8.x 全系列叢集。

# 核心原理與 Indexing Pressure 機制

## 為什麼需要 Indexing Pressure？

在早期版本的 Elasticsearch 中，當海量寫入請求灌入時，節點只能依靠 Thread Pool 的 Queue 進行排隊。當佇列滿了之後，未進入佇列的龐大 HTTP 封包依然停留在記憶體中，極易引發 JVM OOM。

在 7.10+ 之後，Elasticsearch 引入了 **Indexing Pressure（索引壓力管理）**，精確追蹤寫入過程在 **記憶體中佔用的位元組數（Bytes）**：

```text
客戶端 Bulk 寫入請求
       ↓
┌───────────────────────────────────────────────┐
│ 1. Coordinating 階段（協調節點解析與路由分發） │ ── 追蹤協調記憶體
│ 2. Primary 階段（主分片寫入與 Lucene 索引）    │ ── 追蹤主分片記憶體
│ 3. Replica 階段（副本並行同步寫入）             │ ── 追蹤副本記憶體
└───────────────────────────────────────────────┘
       ↓
【自動保護機制】：
- 達 70% 限制：開始對 Coordinating 階段實施微延遲（Soft Throttle），減緩接收速度
- 達 100% 限制：直接拒絕新的 Coordinating 寫入請求（拋出 429），強制客戶端退避！
```

## 三個階段的高壓代表什麼？

1. **Coordinating 壓力高**：
   - 請求封包體積過大（例如單次 Bulk 超過 50MB～100MB），或同一協調節點接收了過多的併發寫入。
2. **Primary 壓力高**：
   - 主分片寫入速度跟不上，常見於 Ingest Pipeline 處理複雜（如肥大的 Grok）、Mapping 欄位過多、或磁碟 I/O 寫入延遲過高。
3. **Replica 壓力高**：
   - 副本節點硬體規格劣於主分片節點，或跨節點網路頻寬受限，導致主分片已寫完但副本同步塞車。

# 報告指標解讀指引

在 `check.html` 報告中，請重點檢視以下數值：

| 欄位名稱 | 警戒門檻 | 診斷意涵 |
|---|---|---|
| `coordinating_in_bytes` | > 80% limit | 協調接收層記憶體吃緊 |
| `primary_in_bytes` | > 80% limit | 主分片寫入速度受限（I/O 或 Pipeline） |
| `replica_in_bytes` | > 80% limit | 副本同步延遲（網路或節點效能差異） |
| `coordinating_rejections` | > 0 | 已發生寫入拒絕，客戶端收到 429 |

# 業務影響與技術說明建議

## 說明要點與原則

- **用「物流分揀中心」比喻**：Coordinating 是收發櫃檯，Primary 是主倉庫分揀員，Replica 是分倉庫備份員。
- **定位堵塞瓶頸**：指出是「櫃檯一次接太多件（Batch 太大）」還是「分揀員打單太慢（I/O 或 Pipeline 慢）」。

## 說明範例：Primary 階段高壓導致 429

> **技術說明範例**：
> 「系統工程師您好，報告中顯示叢集的 `Indexing Pressure（寫入背壓）` 已達到 85% 的警戒線，且主要堵塞在 Primary（主分片）處理階段。
> 
> 這代表收發窗口（Coordinating）收單很快，但後端主分片在進行資料解析與落盤時發生了堵塞。
> 
> 經深入排查，是因為 Logstash 送入的每一筆日誌都經過了 10 道複雜的 Grok 正規表達式處理，且寫入的單個 Bulk 封包達到了 80MB。
> 
> 我們建議將客戶端 Bulk 批次大小收斂到 5MB～15MB，並優化 Ingest Pipeline 的解析規則，寫入吞吐量即可大幅改善，徹底解除寫入背壓。」

# 現場排查與安全處置 SOP

## 階段一：現場唯讀佐證

查詢各節點 Indexing Pressure 詳細統計：

```http
GET /_nodes/stats/indexing_pressure?filter_path=nodes.*.name,nodes.*.indexing_pressure
```

## 階段二：整改修復方案

### 方案 A：優化客戶端 Bulk 批次尺寸（最佳實踐）
- 將客戶端（Logstash / Fluentd / 自研程式）的 Bulk Batch Size 調整為 **5MB ～ 15MB**（或單批 1,000 ～ 5,000 筆）。
- 切勿發送超過 50MB 的超大 Bulk。

### 方案 B：增加 Refresh Interval 降低寫入段生成頻率
對於高吞吐日誌型索引，將 `refresh_interval` 從預設 1 秒放寬為 30 秒：

```http
PUT /<TARGET_INDEX>/_settings
{
  "index.refresh_interval": "30s"
}
```

## 階段三：變更後驗證

- [ ] 執行 `GET /_nodes/stats/indexing_pressure` 確認各階段使用量降至 50% 以下
- [ ] 重新執行 `elk-diagnostics check` 確認 `indexing_pressure` 轉為 `PASS`

# 常見誤區與風險提示

- [ ] **誤區一：手動關閉 Indexing Pressure**：
  Indexing Pressure 是防止 OOM 的核心保護盾，切勿嘗試在配置中關閉此機制。

# 附錄 <!-- appendix -->

## 參考文件

- [Elasticsearch 官方文件：Indexing pressure settings](https://www.elastic.co/docs/reference/elasticsearch/configuration-reference/indexing-pressure-settings)
- 專案內部規格書：`docs/內部/規格/擴充健檢規格.md`（ES-GAP-07）
