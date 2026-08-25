---
title: "Allocation awareness 高可用分佈 — 機架感知與容災分區架構"
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
| 2026/08/23 | 1.0 | 初版：機架感知原理、強制分區孤島排查與跨機房高可用配置 | 診斷工具架構組 |

# 文件說明

## 報告診斷卡對照

| 屬性 | 內容 |
|---|---|
| 報告分類區塊 | 叢集（Cluster） |
| 診斷卡中文名稱 | Allocation awareness 高可用分佈 |
| 診斷卡 ID | `allocation_awareness` |
| 典型嚴重度 | `WARNING` / `CRITICAL` |
| 觸發關鍵特徵 | 設定了機架感知屬性但節點屬性不完整、分區分佈不對稱、或未配置強制分區導致孤島風險 |

## 文件目的

本手札用於協助第一線 ELK 維運工程師與顧問在巡檢報告中評估叢集跨機架（Rack）、跨可用區（AZ）或跨機房的高可用架構配置，避免因感知屬性錯誤導致分片集中於單一機架或副本拒絕分配。

## 適用範圍

本手札適用於具有實體機房多機架、多機房、或公有雲多可用區（Multi-AZ）之 Elasticsearch 8.x 及 9.x 部署。

# 核心原理與機架感知機制

## 為什麼需要 Allocation Awareness？

在多機架或多可用區環境中，預設的分片調度器只會確保「主副本不在同一台主機」，但**有可能把主分片與副本分配在同一個機架（Rack A）的兩台不同機器上**。

一旦 Rack A 發生整櫃斷電或交換機故障，主分片與副本將同時遺失，引發叢集 Red 災難。

```text
【未設定 Awareness 的危險情境】
  機架 A (Rack A)          機架 B (Rack B)
┌─────────────────┐      ┌─────────────────┐
│ Node 1: Pri [0] │      │ Node 3:         │
│ Node 2: Rep [0] │      │ Node 4:         │
└─────────────────┘      └─────────────────┘
  ▲ 斷電時分片全滅！
```

## Awareness 機制運作方式

透過設定 `node.attr.zone: zone_a` 與叢集設定 `cluster.routing.allocation.awareness.attributes: zone`：

Elasticsearch 會強制將 Shard 0 的 Primary 分配在 `zone_a`，Replica 分配在 `zone_b`，確保任何單一機房/機架全毀時，業務服務依然 100% 正常。

```text
【正確設定 Awareness 的高可用情境】
  機房 A (zone_a)          機房 B (zone_b)
┌─────────────────┐      ┌─────────────────┐
│ Node 1: Pri [0] │      │ Node 3: Rep [0] │
│ Node 2: Pri [1] │      │ Node 4: Rep [1] │
└─────────────────┘      └─────────────────┘
  ▲ 單一機房斷線，另一機房自動接管！
```

## 常見架構陷阱：強制分區（Forced Awareness）

當叢集設定了 `forced_awareness`（例如宣告了 `zone_a, zone_b`）：

1. **若預期機房尚未建置完成（只有 zone_a 節點上線）**：
   - ES 會拒絕在 `zone_a` 內分配任何副本，導致所有索引停留在 Yellow 狀態。
2. **容量失衡（Asymmetric Capacity）**：
   - 若 `zone_a` 有 10 台機器，`zone_b` 只有 2 台機器，分片總數受限於較小機房的承載力，會迅速引發 `zone_b` 的磁碟水位線報警與效能瓶頸。

# 報告指標解讀指引

在 `check.html` 報告中，請重點檢視以下配置項目：

| 欄位名稱 | 範例值 | 診斷意涵 |
|---|---|---|
| `awareness_attributes` | `zone` 或 `rack_id` | 目前生效的感知屬性名稱 |
| `forced_attributes` | `zone_a,zone_b` | 明確宣告的強制感知分區名單 |
| `zone_distribution` | `zone_a: 3, zone_b: 3` | 各分區的實際在線節點數量 |
| `missing_attribute_nodes` | `["es-node-04"]` | 遺漏配置該感知屬性的節點清單 |

# 業務影響與技術說明建議

## 說明要點與原則

- **從容災價值出發**：向客戶說明機架感知是企業級容災的標準配備，能抵禦整個機櫃或 AZ 級別的硬體故障。
- **點出不對稱風險**：若客戶多機房間資源配置不均，向客戶解釋為何副本無法順利擴展。

## 說明範例：多機房感知屬性配置遺漏

> **技術說明範例**：
> 「主管您好，我們在檢查時發現，叢集雖然配置了跨機房的高可用感知（`zone` 屬性），但新上線的 `es-node-04` 與 `es-node-05` 在設定檔中漏掉了機房標籤。
> 
> 這會導致 Elasticsearch 無法識別這兩台新伺服器屬於哪一個機房，進而可能將主副分片集中在同一處，削弱了整體的跨機房容災能力。
> 
> 我們建議在節點的 `elasticsearch.yml` 中補齊機房屬性標籤，確保叢集維持最嚴格的容災隔離水準。」

# 現場排查與安全處置 SOP

## 階段一：現場唯讀佐證

1. 檢查叢集層級的 Awareness 設定：

```http
GET /_cluster/settings?include_defaults=true&filter_path=*.*awareness*
```

2. 檢查各節點的自訂屬性：

```http
GET /_cat/nodeattrs?v&h=node,attr,value
```

## 階段二：整改修復方案

### 方案 A：在 elasticsearch.yml 為節點補齊屬性

```yaml
# 節點設定檔 elasticsearch.yml
node.attr.zone: zone_a
```

### 方案 B：動態啟用叢集機架感知

```http
PUT /_cluster/settings
{
  "persistent": {
    "cluster.routing.allocation.awareness.attributes": "zone"
  }
}
```

### 方案 C：若多機房尚未完全就緒，暫時關閉強制分區

```http
PUT /_cluster/settings
{
  "persistent": {
    "cluster.routing.allocation.awareness.force.zone.values": null
  }
}
```

## 階段三：變更後驗證

- [ ] 執行 `GET /_cat/nodeattrs` 確認所有 Data 節點均有正確的感知屬性
- [ ] 執行 `GET /_cat/shards?v` 抽查主副本分佈，確認 Primary 與 Replica 均座落於不同 zone
- [ ] 重新執行 `elk-diagnostics check` 確認 `allocation_awareness` 轉為 `PASS`

# 常見誤區與風險提示

- [ ] **誤區一：以為設了 Awareness 就不需要副本**：
  機架感知只負責「主副分片的空間隔離」，如果索引本身 `number_of_replicas: 0`，Awareness 完全起不到任何容災保護作用。
- [ ] **誤區二：兩機房節點數與硬體規格嚴重懸殊**：
  若兩個機房間規格不一致，較小的一方會成為整座叢集的容量與寫入瓶頸（木桶效應）。

# 附錄 <!-- appendix -->

## 參考文件

- [Elasticsearch 官方文件：Shard Allocation Awareness](https://www.elastic.co/docs/reference/elasticsearch/configuration-reference/shard-allocation-awareness)
- 專案內部規格書：`docs/內部/規格/靜態健檢規格.md`（ES-GAP-06）
