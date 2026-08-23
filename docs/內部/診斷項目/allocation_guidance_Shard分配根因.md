---
title: "ELK 診斷手札：Shard 分配根因（decider 級）— Unassigned 分片決策鏈解讀"
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
| 2026/08/23 | 1.0 | 初版：Deciders 決策鏈架構、常見分配阻塞排查與客戶說明話術 | 診斷工具架構組 |

# 文件說明

## 報告診斷卡對照

| 屬性 | 內容 |
|---|---|
| 報告分類區塊 | 叢集（Cluster） |
| 診斷卡中文名稱 | Shard 分配根因（decider 級） |
| 診斷卡 ID | `allocation_guidance` |
| 典型嚴重度 | `WARNING`（Yellow 叢集）/ `CRITICAL`（Red 叢集） |
| 觸發關鍵特徵 | `_cluster/allocation/explain` 偵測到有未分配分片，且回傳具體阻止分配之 Decider 名稱 |

## 文件目的

本手札用於協助第一線 ELK 維運工程師與顧問在巡檢報告出現 `allocation_guidance` 異常時，精準解讀 Elasticsearch 分片分配器（Allocation Deciders）的決策原因，並指導客戶進行安全修復。

## 適用範圍

本手札適用於所有自建（On-Premise）、容器化（ECK/K8s）與雲端部署之 Elasticsearch 7.x 及 8.x 叢集。

# 核心原理與 Deciders 機制深拆

## 什麼是 Allocation Decider？

Elasticsearch 在決定「某個 Shard 是否能分配到某個 Node」時，不會只看磁碟空間，而是由十多個內建的 **Allocation Deciders** 進行一票否決制（Any NO = Reject）的嚴格審查。

常見的關鍵 Deciders 分類如下：

```text
分片分配請求
     ↓
┌────────────────────────────────────────┐
│ 1. SameShardAllocationDecider          │ ── 同一分片的主副本絕不能在同一節點
│ 2. DiskThresholdDecider                │ ── 磁碟超過 85%/90% 水位線即拒絕
│ 3. FilterAllocationDecider             │ ── 索引或節點層級的 routing 規則過濾
│ 4. AwarenessAllocationDecider          │ ── 機架/AZ 容災隔離限制
│ 5. ShardsLimitAllocationDecider        │ ── 超過單節點最大分片數上限
│ 6. NodeVersionAllocationDecider        │ ── 副本不能分配到版本低於主的節點
│ 7. ThrottlingAllocationDecider         │ ── 同時恢復（Recovery）數量達到上限
└────────────────────────────────────────┘
     ↓
所有 Deciders 皆為 YES → 執行分片分配
任一 Decider 回傳 NO → 分片停留在 Unassigned（Yellow/Red）
```

## 常見 Decider 拒絕原因與真實場景

### 1. `same_shard`（SameShardAllocationDecider）
- **現象**：單節點或節點數少於 `number_of_replicas + 1`。
- **原因**：為了防止單點故障（SPOF），ES 嚴禁將主分片（Primary）與副本（Replica）放在同一台主機。如果叢集只有 1 台 Data 節點卻要求 1 個副本，副本必然卡在 Unassigned。

### 2. `disk_threshold`（DiskThresholdDecider）
- **現象**：磁碟使用率達到 Low Watermark（預設 85%）。
- **原因**：目標節點剩餘空間不足以容納新分片，ES 自動停止向該節點指派新分片。

### 3. `filter`（FilterAllocationDecider）
- **現象**：設定了 `index.routing.allocation.include/exclude/require` 或 `_tier_preference`。
- **原因**：索引指定了特定節點屬性（例如 `tag: ssd` 或 `_tier: data_warm`），但具備該屬性的節點不存在或已離線。

### 4. `awareness`（AwarenessAllocationDecider）
- **現象**：設定了機架感知（`cluster.routing.allocation.awareness.attributes: rack_id`）。
- **原因**：若某一機架故障或容量用盡，ES 為了保證跨機架容災，拒絕將多餘的副本塞入同一機架。

### 5. `shards_limit`（ShardsLimitAllocationDecider）
- **現象**：觸發了 `total_shards_per_node`（索引或叢集層級）。
- **原因**：該節點承載的此索引分片數量已達人工設定之上限。

# 報告指標解讀指引

在 `check.html` 報告中，若本項異常，請展開查看以下觀測欄位：

| 欄位名稱 | 範例值 | 診斷意涵 | 處置優先級 |
|---|---|---|---|
| `unassigned_index` | `logs-app-2026.08` | 受影響的具體索引名稱 | 高 |
| `shard_id` | `0` | 卡住的分片編號 | 中 |
| `primary` | `false` | `true` 表示主分片遺失（Red）；`false` 表示副本未分配（Yellow） | `true` 為極高 |
| `decider` | `same_shard` | 阻止分配的核心 Decider 名稱 | 關鍵線索 |
| `explanation` | `the shard cannot be allocated...` | ES 官方給出的具體拒絕細節 | 依細節行動 |

# 客戶溝通話術與情境模擬

## 溝通策略原則

- **Yellow 狀態安心化**：向客戶說明 Yellow 代表資料完整（Primary 都在），只是缺少冗餘保護，無需過度恐慌。
- **Red 狀態嚴肅化**：Red 代表有部分資料當下無法讀寫，需立即排查是節點離線還是磁碟損毀。

## 話術範例一：單節點 POC/開發環境 Yellow（`same_shard`）

> **顧問說明範例**：
> 「客戶窗口您好，目前報告顯示叢集為 Yellow 狀態，原因在於這是一座單節點的 Elasticsearch 叢集，但預設索引配置了 1 個副本（Replica）。
> 
> Elasticsearch 的高可用機制要求主分片與副本必須放在不同的實體節點上，以達到備份效果。在只有單台伺服器的情況下，系統安全機制會刻意拒絕在同一台機器上放置副本。
> 
> 這屬於單機環境下的正常預期行為，資料完全正常。在正式環境擴充至 2 台以上節點後即可自動轉為 Green；若在單機環境希望消除警告，可將副本數調整為 0。」

## 話術範例二：磁碟水位線觸發拒絕（`disk_threshold`）

> **顧問說明範例**：
> 「主管您好，我們發現分片無法分配是因為 `data-node-03` 的磁碟使用率已突破 85% 的保護水位線。
> 
> 這是 Elasticsearch 內建的保護機制，防止單一節點因硬碟寫滿而直接崩潰。建議我們立即清理過期日誌索引，或為資料磁碟擴充容量，一旦磁碟降至 85% 以下，系統便會自動恢復分片搬移與分配。」

# 現場排查與安全處置 SOP

## 階段一：現場唯讀佐證（精準定位根因）

使用 `allocation/explain` API 直接向 Elasticsearch 索取診斷報告：

```http
POST /_cluster/allocation/explain
{
  "index": "<UNASSIGNED_INDEX_NAME>",
  "shard": 0,
  "primary": false
}
```

檢視回傳 JSON 中的 `can_allocate` 與 `allocate_explanation`，確認哪一個節點的哪一個 Decider 回傳 `NO`。

## 階段二：依據 Decider 實施整改

### 狀況 A：單節點或副本過多（`same_shard`）
將不具備實體副本條件之索引的 `number_of_replicas` 調為 0 或符合節點數：

```http
PUT /<UNASSIGNED_INDEX_NAME>/_settings
{
  "index": {
    "number_of_replicas": 0
  }
}
```

### 狀況 B：磁碟空間已釋放但分片未自動重試（`disk_threshold` / `max_retry`）
若分片因連續失敗超過 5 次而停滯，手動觸發一次重試：

```http
POST /_cluster/reroute?retry_failed=true
```

### 狀況 C：錯誤的 Routing 限制（`filter`）
檢查並清除索引上不正確的分配過濾規則：

```http
PUT /<UNASSIGNED_INDEX_NAME>/_settings
{
  "index.routing.allocation.include._tier_preference": null
}
```

## 階段三：變更後驗證

- [ ] 執行 `GET /_cluster/health` 確認狀態轉為 `green`
- [ ] 執行 `GET /_cat/shards?v&h=index,shard,prirep,state,unassigned.reason` 確認無未分配分片
- [ ] 重新執行 `elk-diagnostics check` 確認 `allocation_guidance` 轉為 `PASS`

# 常見誤區與風險提示

- [ ] **誤區一：隨意執行 `allocate_empty_primary`**：
  在 Red 狀態下，若主分片卡住，千萬不可盲目下達 `allocate_empty_primary` 指令，這會分配一個全新的空分片並徹底覆蓋原有資料，造成資料永久遺失！
- [ ] **誤區二：為了轉 Green 盲目把正式環境 Replica 設為 0**：
  正式環境關閉副本會失去容災能力，任何一台主機重啟都將直接導致叢集噴 Red 與業務中斷。

# 附錄 <!-- appendix -->

## 參考文件

- [Elasticsearch 官方文件：Diagnose unassigned shards](https://www.elastic.co/docs/troubleshoot/elasticsearch/diagnose-unassigned-shards)
- [Elasticsearch 官方文件：Cluster Allocation Explain API](https://www.elastic.co/docs/reference/elasticsearch/rest-apis/cluster-allocation-explain)
- 專案內部規格書：`docs/內部/規格/健康報告規格.md`（#37）
