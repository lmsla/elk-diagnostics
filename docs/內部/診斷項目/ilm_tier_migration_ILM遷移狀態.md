---
title: "ILM Tier 遷移狀態 — 節點角色與舊版路由屬性衝突排解"
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
| 2026/08/23 | 1.0 | 初版：ILM Migrate Action、7.10+ Node Roles 與舊版 box_type 路由衝突解鎖 | 診斷工具架構組 |

# 文件說明

## 報告診斷卡對照

| 屬性 | 內容 |
|---|---|
| 報告分類區塊 | 管理（Management） |
| 診斷卡中文名稱 | ILM Tier 遷移狀態 |
| 診斷卡 ID | `ilm_tier_migration` |
| 典型嚴重度 | `WARNING` / `CRITICAL` |
| 觸發關鍵特徵 | `_ilm/explain` 顯示索引在 `migrate` 階段或階段流轉時停滯，分片無法順利搬遷至 Warm/Cold 節點 |

## 文件目的

本手札用於協助第一線 ELK 維運工程師與顧問在巡檢報告發現 ILM 遷移卡住時，精準診斷 7.10+ / 8.x 原生 Data Tier（`data_hot`, `data_warm`, `data_cold`）與舊版自訂屬性（如 `box_type: hot`）的混用衝突，並指導手動解鎖與遷移至新標準。

## 適用範圍

本手札適用於Elasticsearch 8.x 及 9.x 叢集。

# 核心原理與架構演進衝突

## 兩代冷熱架構演進

1. **舊版架構（ES 7.9 以前）**：
   - 依靠自訂節點屬性：`node.attr.box_type: hot` 與 `node.attr.box_type: warm`。
   - 索引路由透過屬性過濾：`index.routing.allocation.require.box_type: hot`。
2. **現代架構（ES 7.10+ / 8.x）**：
   - 官方原生 Data Tier 節點角色：`node.roles: [ data_hot ]`、`node.roles: [ data_warm ]`。
   - 索引路由透過 Tier 偏好：`index.routing.allocation.include._tier_preference: "data_hot,data_warm"`。

```text
【最常見的升級卡死陷阱】
索引身上同時殘留了兩套衝突設定：
1. 舊設定：index.routing.allocation.require.box_type: "hot" (強制只能在 hot 節點)
2. ILM 嘗試遷移：試圖將 _tier_preference 改為 "data_warm"
     ↓
【衝突爆發】：分片想搬去 warm 節點，但被 require.box_type 狠狠拉住！
              ▲ ILM 在 migrate action 陷入永久等待死鎖！
```

# 報告指標解讀指引

在 `check.html` 報告中，請檢視以下資訊：

| 欄位名稱 | 範例值 | 診斷意涵 |
|---|---|---|
| `migrating_indices_count` | `8` | 當前卡在遷移狀態的索引數量 |
| `stuck_indices` | `["logs-2026.07-000001"]` | 具體卡住的索引名單 |

# 業務影響與技術說明建議

## 說明要點與原則

- **肯定升級歷程**：說明這通常是從舊版本升級上來留下的過渡設定。
- **推動標準化對齊**：建議全面廢棄舊的 `box_type` 標籤，擁抱官方原生 Data Tier 體系。

## 說明範例：ILM 冷熱搬遷卡住

> **技術說明範例**：
> 「維運同仁您好，報告中顯示有多個歷史日誌索引在執行 ILM 遷移（搬往 Warm 節點）時卡住了。
> 
> 經排查，這是因為叢集早期升級時，索引身上同時殘留了舊版的 `box_type: hot` 鎖定規則。當 ILM 試圖把資料搬往 Warm 節點時，被舊規則直接攔截，導致分片進退兩難。
> 
> 我們只需執行一行清除舊版屬性的指令，ILM 就會立刻解除鎖定，自動在背景將數百 GB 的冷資料平滑搬移至 Warm 節點，釋放出寶貴的 Hot SSD 儲存空間。」

# 現場排查與安全處置 SOP

## 階段一：現場唯讀佐證（查看 ILM Explain）

```http
GET /<STUCK_INDEX>/_ilm/explain
```

查看 `action` 是否為 `migrate` 或 `step_info.type` 是否包含 allocation 失敗訊息。

## 階段二：整改修復方案（清除舊路由限制）

清除舊版 `box_type` 屬性過濾，讓 ILM 專利接管：

```http
PUT /<STUCK_INDEX>/_settings
{
  "index.routing.allocation.require.box_type": null,
  "index.routing.allocation.include.box_type": null
}
```

手動重試該索引的 ILM 步驟：

```http
POST /<STUCK_INDEX>/_ilm/retry
```

## 階段三：變更後驗證

- [ ] 執行 `GET /<STUCK_INDEX>/_ilm/explain` 確認 `phase` 順利進入 `warm` 或 `cold`
- [ ] 重新執行 `elk-diagnostics check` 確認 `ilm_tier_migration` 轉為 `PASS`

# 附錄 <!-- appendix -->

## 參考文件

- [Elasticsearch 官方文件：Data tiers](https://www.elastic.co/docs/reference/elasticsearch/configuration-reference/data-tiers)
- 專案內部規格書：`docs/內部/規格/健康報告規格.md`（#25）
