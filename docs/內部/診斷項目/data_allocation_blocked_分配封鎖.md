---
title: "叢集層級 shard 分配封鎖 — 路由分配開關與維護策略"
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
| 2026/08/23 | 1.0 | 初版：`cluster.routing.allocation.enable` 設定解析與滾動升級防禦 | 診斷工具架構組 |

# 文件說明

## 報告診斷卡對照

| 屬性 | 內容 |
|---|---|
| 報告分類區塊 | 叢集（Cluster） |
| 診斷卡中文名稱 | 叢集層級 shard 分配封鎖 |
| 診斷卡 ID | `data_allocation_blocked` |
| 典型嚴重度 | `WARNING` / `CRITICAL` |
| 觸發關鍵特徵 | `cluster.routing.allocation.enable` 被設定為 `none`、`primaries` 或 `new_primaries`，全域禁止副本或現有分片分配 |

## 文件目的

本手札用於協助第一線 ELK 維運工程師與顧問在巡檢報告發現全域分配封鎖時，快速判斷是否為滾動維護（Rolling Restart）遺留之開關未復原，並指導安全開啟。

## 適用範圍

本手札適用於所有 Elasticsearch 7.x 及 8.x 叢集。

# 核心原理與分配開關機制

## 什麼是 cluster.routing.allocation.enable？

在進行叢集滾動維護或伺服器重啟時，當某個 Data 節點剛重啟離線，Elasticsearch 預設會在 1 分鐘後（`unassigned.node_left.delayed_timeout`）自動在其他節點上複製副本分片，引發劇烈的跨節點 I/O 與網路傳輸風暴（I/O Storm）。

為了避免這種無謂的分片搬移，標準滾動重啟 SOP 要求維運人員在關機前暫時關閉分片分配：

```http
PUT /_cluster/settings
{
  "persistent": {
    "cluster.routing.allocation.enable": "primaries"
  }
}
```

## 開關可設定的值與影響

| 設定值 | 允許分配的項目 | 禁止分配的項目 | 適用時機 |
|---|---|---|---|
| `all`（預設） | 所有 Primary 與 Replica | 無（完全開放） | 正常生產營運 |
| `primaries` | 僅允許 Primary 分配 | 禁止所有 Replica 分配 | 滾動重啟維護期間 |
| `new_primaries` | 僅允許新建立索引的 Primary | 禁止現有分片與 Replica | 重大異常緊急限流 |
| `none` | 完全禁止任何分片分配 | 所有分片均無法分配 | 叢集緊急止血維護 |

## 忘記復原的後果

維護結束所有節點都開機上線後，若維運人員**忘記將此開關改回 `all`**：
- 新建立的索引無法生成副本；
- 離線期間落後的分片無法同步；
- 叢集會永久卡在 **Yellow** 狀態，看似有節點卻不修復。

# 報告指標解讀指引

在 `check.html` 報告中，請檢視以下資訊：

| 欄位名稱 | 範例值 | 診斷意涵 |
|---|---|---|
| `allocation_enable` | `primaries` 或 `none` | 當前生效的全域分配設定 |
| `setting_source` | `persistent` / `transient` | 設定被寫在永久層還是臨時層 |

# 業務影響與技術說明建議

## 說明範例：維護後開關未復原

> **技術說明範例**：
> 「維運主管您好，我們在報告中發現叢集的分片分配開關目前被鎖定為 `primaries`（或 `none`）。
> 
> 這通常是上次進行伺服器重啟或版本升級時，工程師為了防止節點重啟引發網路流量風暴而主動關閉的保護措施。但在所有節點都順利開機後，漏掉了將開關恢復為 `all` 的最後一步。
> 
> 只要我們執行一行設定指令將開關復原，叢集就會立刻開始同步副本，並在幾分鐘內自動恢復為最健康的 Green 狀態。」

# 現場排查與安全處置 SOP

## 階段一：現場唯讀佐證

檢查目前生效的叢集分配設定：

```http
GET /_cluster/settings?flat_settings=true&include_defaults=false
```

確認 `cluster.routing.allocation.enable` 是否存在於 `persistent` 或 `transient` 中。

## 階段二：整改修復方案（安全恢復）

在確認所有節點皆已正常在線且網路連線無誤後，將設定恢復為 `all`（或重設為 null 以沿用預設值）：

```http
PUT /_cluster/settings
{
  "persistent": {
    "cluster.routing.allocation.enable": null
  },
  "transient": {
    "cluster.routing.allocation.enable": null
  }
}
```

## 階段三：變更後驗證

- [ ] 執行 `GET /_cluster/settings` 確認 `allocation.enable` 已清除或為 `all`
- [ ] 執行 `GET /_cluster/health` 觀察分片同步進度，直到狀態轉為 `green`
- [ ] 重新執行 `elk-diagnostics check` 確認 `data_allocation_blocked` 轉為 `PASS`

# 常見誤區與風險提示

- [ ] **誤區一：在大量節點依然離線時貿然開啟**：
  若叢集還有多台實體機正在重開機或硬體維修中，請等候所有節點開機完成再開啟，避免引發不必要的跨網路重建。

# 附錄 <!-- appendix -->

## 參考文件

- [Elasticsearch 官方文件：Cluster-level shard allocation settings](https://www.elastic.co/docs/reference/elasticsearch/configuration-reference/cluster-routing-allocation-settings)
- 專案內部規格書：`docs/內部/規格/健康報告規格.md`（#19）
