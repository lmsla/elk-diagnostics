---
title: "ELK 診斷手札：Data tier 節點可用性 — Hot / Warm / Cold / Frozen 階層完整度評估"
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
| 2026/08/23 | 1.0 | 初版：Data Tier 階層角色覆蓋度、缺少 Warm/Cold 節點排查與配置 | 診斷工具架構組 |

# 文件說明

## 報告診斷卡對照

| 屬性 | 內容 |
|---|---|
| 報告分類區塊 | 資料（Data） |
| 診斷卡中文名稱 | Data tier 節點可用性 |
| 診斷卡 ID | `data_tier_availability` |
| 典型嚴重度 | `WARNING` |
| 觸發關鍵特徵 | ILM 策略中定義了 Warm / Cold / Frozen 階段，但叢集中不存在對應角色的節點 |

## 文件目的

本手札提供第一線工程師在 Data Tier 角色缺失時的判定原則、技術說明指引與節點角色配置指令。

## 適用範圍

本手札適用於採用 7.10+ / 8.x Data Tier 架構之 Elasticsearch 叢集。

# 核心概念與階層角色

```text
┌───────────────┬─────────────────────────────────────────────────────────────┐
│ data_hot      │ 必備。負責接收最新即時寫入與高頻搜尋。                      │
├───────────────┼─────────────────────────────────────────────────────────────┤
│ data_warm     │ 選配。存放唯讀歷史資料，硬體規格可略低。                    │
├───────────────┼─────────────────────────────────────────────────────────────┤
│ data_cold     │ 選配。存放極低頻查詢資料，可搭配 Searchable Snapshots。      │
├───────────────┼─────────────────────────────────────────────────────────────┤
│ data_frozen   │ 選配。完全依賴雲端物件儲存快取。                            │
└───────────────┴─────────────────────────────────────────────────────────────┘
▲ 若 ILM 定義了 Warm 階段但全叢集無 data_warm 節點，ILM 遷移將卡住！
```

# 業務影響與技術說明建議

> **技術說明範例**：
> 「架構主管您好，報告顯示 ILM 策略中啟用了 Warm 冷資料遷移，但目前叢集中尚未配置標記為 `data_warm` 角色的伺服器。
> 
> 這會使歷史資料無法依照生命週期降級至冷儲存。
> 
> 若目前暫無規劃獨立 Warm 伺服器，建議可將現有資料節點的角色調整為同時支援 `[data_hot, data_warm, data_content]`，即可讓 ILM 策略平滑運行。」

# 現場 1 分鐘快速佐證與處置 SOP

```http
# 1. 快速查看各節點目前分配的角色清單
GET /_cat/nodes?v&h=name,node.role

# 2. 檢查各 Tier 的在線節點數
GET /_cluster/state?filter_path=nodes.*.roles
```

# 附錄 <!-- appendix -->

- [Elasticsearch 官方文件：Data tiers](https://www.elastic.co/docs/reference/elasticsearch/configuration-reference/data-tiers)
