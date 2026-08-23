---
title: "ELK 診斷手札：Index 層級 shard 分配封鎖 — 單一索引路由設定與衝突解鎖"
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
| 2026/08/23 | 1.0 | 初版：Index 級別 allocation.enable、路由過濾衝突與批量解鎖 SOP | 診斷工具架構組 |

# 文件說明

## 報告診斷卡對照

| 屬性 | 內容 |
|---|---|
| 報告分類區塊 | 叢集（Cluster） |
| 診斷卡中文名稱 | Index 層級 shard 分配封鎖 |
| 診斷卡 ID | `index_allocation_blocked` |
| 典型嚴重度 | `WARNING` / `CRITICAL` |
| 觸發關鍵特徵 | 特定索引的 `index.routing.allocation.enable` 被設為 `none` / `primaries`，或路由過濾屬性衝突 |

## 文件目的

本手札用於協助第一線 ELK 維運工程師與顧問在巡檢報告發現個別索引出現分配封鎖時，快速定位受阻索引名單、分析設定來源並進行安全修復。

## 適用範圍

本手札適用於所有 Elasticsearch 7.x 及 8.x 叢集。

# 核心原理與索引分配機制

## 索引層級覆寫全域設定

Elasticsearch 允許對單一索引單獨設定 `index.routing.allocation.enable`。**索引層級的設定優先度高於全域叢集設定**。

常見的情境包括：
1. **備份與還原作業**：第三方備份或搬遷工具在還原索引時，暫時鎖定該索引的分配。
2. **舊版維運腳本殘留**：自訂腳本在修復特定索引時設定了 `index.routing.allocation.enable: none`，事後未復原。
3. **錯誤的模板（Index Template）繼承**：Index Template 中寫入了分配鎖定設定，導致所有新建索引全部被自動鎖死。

## 路由過濾條件衝突（Routing Filter Conflict）

除了 `allocation.enable` 之外，更常見的隱性封鎖是索引上的路由過濾條件：
```json
{
  "index.routing.allocation.require.box_type": "hot"
}
```
若叢集中所有標記為 `box_type: hot` 的節點均已下線或被重新命名為 `data_hot`，該索引的分片將因無符合節點而永久卡住。

# 報告指標解讀指引

在 `check.html` 報告中，請檢視以下資訊：

| 欄位名稱 | 範例值 | 診斷意涵 |
|---|---|---|
| `blocked_indices_count` | `3` | 被鎖定分配的索引總數 |
| `blocked_indices` | `["app-log-2026.07", ...]` | 具體被鎖定的索引清單 |

# 業務影響與技術說明建議

## 說明範例：個別歷史索引遺留鎖定

> **技術說明範例**：
> 「客戶窗口您好，我們在檢查時發現有 3 個歷史索引（例如 `app-log-2026.07`）被單獨加上了禁止分片分配的標記。
> 
> 這通常是過去在進行特定資料還原或排錯時，維運腳本針對這幾個索引下了鎖定指令，但事後漏掉解除。
> 
> 目前這幾個索引無法建立副本。我們只需對這批索引執行清除設定指令，即可讓分片正常分配並恢復健康。」

# 現場排查與安全處置 SOP

## 階段一：現場唯讀佐證（找出受阻索引）

查詢所有被單獨設定 `routing.allocation` 的索引：

```http
GET /*/_settings?filter_path=*.settings.index.routing.allocation
```

## 階段二：整改修復方案（安全解鎖）

### 方案 A：解除單一索引的分配封鎖

```http
PUT /<TARGET_INDEX_NAME>/_settings
{
  "index.routing.allocation.enable": null
}
```

### 方案 B：批量清除所有非系統索引的錯誤路由限制

```http
PUT /*,-.* /_settings
{
  "index.routing.allocation.enable": null,
  "index.routing.allocation.require._tier_preference": null
}
```

## 階段三：變更後驗證

- [ ] 執行 `GET /*/_settings?filter_path=*.settings.index.routing.allocation.enable` 確認已無非預期鎖定
- [ ] 執行 `GET /_cluster/health` 確認狀態轉為 `green`
- [ ] 重新執行 `elk-diagnostics check` 確認 `index_allocation_blocked` 轉為 `PASS`

# 常見誤區與風險提示

- [ ] **誤區一：忘記檢查 Index Template**：
  如果解除現有索引後，隔天新生成的索引又被鎖定，請立即檢查 `_index_template` 與舊版 `_template`，清除模板內殘留的 `index.routing.allocation.enable: none` 設定。

# 附錄 <!-- appendix -->

## 參考文件

- [Elasticsearch 官方文件：Index-level shard allocation filtering](https://www.elastic.co/docs/reference/elasticsearch/configuration-reference/index-routing-allocation-settings)
- 專案內部規格書：`docs/內部/規格/健康報告規格.md`（#20）
