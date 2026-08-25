---
title: "Mapping 欄位膨脹 — Dynamic Mapping 治理與 Master 記憶體保護"
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
| 2026/08/23 | 1.0 | 初版：Mapping Explosion 原理、Cluster State 廣播開銷與欄位收斂方案 | 診斷工具架構組 |

# 文件說明

## 報告診斷卡對照

| 屬性 | 內容 |
|---|---|
| 報告分類區塊 | 資料（Data） |
| 診斷卡中文名稱 | Mapping 欄位膨脹 |
| 診斷卡 ID | `mapping_explosion` |
| 典型嚴重度 | `WARNING`（欄位數 > 800）/ `CRITICAL`（突破 1,000 上限） |
| 觸發關鍵特徵 | 單一索引欄位總數超過 `index.mapping.total_fields.limit`（預設 1,000），引發 `illegal_argument_exception` 寫入拒絕 |

## 文件目的

本手札用於協助第一線 ELK 維運工程師與顧問在巡檢報告發現 Mapping 欄位過多時，向客戶說明為什麼欄位過多會拖垮 Master 節點與全叢集效能，並指導實施 Index Template 動態 Mapping 治理與 Flattened 欄位收斂。

## 適用範圍

本手札適用於所有版本之 Elasticsearch 8.x 及 9.x 叢集。

# 核心原理與 Mapping 膨脹危害

## 什麼是 Mapping Explosion（欄位爆炸）？

Elasticsearch 預設支援「動態映射（Dynamic Mapping）」，當客戶端寫入包含新 JSON Key 的資料時，ES 會自動識別並將該欄位加入索引的 Mapping 定義中。

當日誌中包含動態產生的隨機 Key（例如包含 UUID、IP、User ID、或未過濾的 Query Parameter 作為 JSON 鍵名）時：

```json
{
  "metric_user_1001": 25,
  "metric_user_1002": 30,
  "metric_user_1003": 45,
  "...": "數千個動態 key..."
}
```

索引的欄位數量會從幾十個迅速暴增至幾千甚至上萬個！

## 欄位爆炸對叢集的毀滅性打擊

```text
新欄位寫入 → Master 更新 Cluster State 結構
     ↓
┌─────────────────────────────────────────────────────────────┐
│ 1. Master 節點 JVM Heap 耗盡                                 │
│    Cluster State 體積從幾 MB 膨脹到數百 MB，Master 頻繁 GC 崩潰 │
├─────────────────────────────────────────────────────────────┤
│ 2. 叢集狀態廣播網路風暴（Cluster State Serialization）         │
│    每次新增欄位，Master 必須將巨大狀態廣播給所有 Node，全網塞車 │
├─────────────────────────────────────────────────────────────┤
│ 3. 寫入拒絕（Limit Exceeded）                                │
│    突破 1000 上限後，ES 自動拒絕寫入，日誌大量遺失！           │
└─────────────────────────────────────────────────────────────┘
```

# 報告指標解讀指引

在 `check.html` 報告中，請檢視各索引欄位數量統計：

| 欄位名稱 | 警戒門檻 | 診斷意涵 | 處置建議 |
|---|---|---|---|
| `field_count` | > 800 | 接近 1000 臨界點 | 需預先治理 Template |
| `field_count` | >= 1,000 | 已發生寫入拒絕 | 需立即調大限制或收斂欄位 |

# 業務影響與技術說明建議

## 說明要點與原則

- **用「表格欄位無限制增加」比喻**：說明這好比資料庫表從 50 欄暴增到 5,000 欄，資料庫目錄本身會比資料還肥大。
- **糾正隨意調大 limit 的想法**：說明把上限調到 5,000 只是飲鴆止渴，最後會直接把 Master 節點炸掉。

## 說明範例：日誌未清洗導致欄位破千

> **技術說明範例**：
> 「開發團隊您好，報告中顯示 `app-events` 索引的欄位數已經達到 980 個，即將衝破 1,000 個的安全上限。
> 
> 經分析，是因為程式端將隨機的使用者 ID 直接當作 JSON 鍵名（如 `"user_12345": "click"`）傳入，觸發了 Elasticsearch 的自動欄位生成。
> 
> 這會使整座叢集的大腦（Master 節點）每次都要同步這上千個欄位定義，導致記憶體消耗激增，一旦達到 1,000 個，後續資料將被直接拒絕寫入。
> 
> 正確的做法是將資料格式正規化（如改為 `{"key": "user_12345", "value": "click"}`），或者對動態 Payload 使用 **`flattened` 欄位型態**，既能保留搜尋能力，又能將欄位數限制在 1 個，徹底根絕崩潰隱患。」

# 現場排查與安全處置 SOP

## 階段一：現場唯讀佐證（找出欄位暴增的索引）

查詢各索引欄位總數排行榜：

```http
GET /_cluster/state?filter_path=metadata.indices.*.mappings
```

## 階段二：整改修復方案

### 方案 A：緊急應變（暫時調大上限防止寫入中斷）
若線上正在噴 429/500 拒絕，先暫時調大該索引上限以爭取整改時間：

```http
PUT /<TARGET_INDEX>/_settings
{
  "index.mapping.total_fields.limit": 2000
}
```

### 方案 B：使用 `flattened` 欄位型態（治本最佳解法）
在 Index Template 中，將存放不可預期動態 Key 的 JSON 宣告為 `flattened`：

```json
PUT /_index_template/app_template
{
  "index_patterns": ["app-events-*"],
  "template": {
    "mappings": {
      "properties": {
        "dynamic_payload": {
          "type": "flattened"
        }
      }
    }
  }
}
```
`flattened` 會把裡面幾百個 Key 統一當作 1 個欄位處理，支援子欄位搜尋，但 Mapping 欄位計數永遠只算 1！

### 方案 C：停用全域動態映射（Strict 模式）
在模板中將 `dynamic` 設為 `runtime` 或 `false`，未定義的欄位不自動擴展 Mapping。

## 階段三：變更後驗證

- [ ] 新建的隔日分區索引欄位數收斂至 200 以內
- [ ] 重新執行 `elk-diagnostics check` 確認 `mapping_explosion` 轉為 `PASS`

# 附錄 <!-- appendix -->

## 參考文件

- [Elasticsearch 官方文件：Mapping limit settings](https://www.elastic.co/docs/reference/elasticsearch/configuration-reference/mapping-settings#mapping-limit-settings)
- [Elasticsearch 官方文件：Flattened field type](https://www.elastic.co/docs/reference/elasticsearch/mapping-reference/flattened)
- 專案內部規格書：`docs/內部/規格/資料規格.md`（#11）
