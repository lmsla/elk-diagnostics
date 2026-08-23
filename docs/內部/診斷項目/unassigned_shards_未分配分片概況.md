---
title: "ELK 診斷手札：未分配 shard 概況 — 分片未指派清單、Primary vs Replica 與叢集色彩關係"
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
| 2026/08/23 | 1.0 | 初版：Unassigned 分片概況盤點、Primary/Replica 影響與快速對帳 | 診斷工具架構組 |

# 文件說明

## 報告診斷卡對照

| 屬性 | 內容 |
|---|---|
| 報告分類區塊 | 叢集（Cluster） |
| 診斷卡中文名稱 | 未分配 shard 概況 |
| 診斷卡 ID | `unassigned_shards` |
| 典型嚴重度 | `WARNING`（Replica 未分配）/ `CRITICAL`（Primary 未分配） |
| 觸發關鍵特徵 | `_cat/shards` 存在 `state: UNASSIGNED` 之分片 |

## 文件目的

本手札用於協助第一線 ELK 維運工程師與顧問在巡檢報告發現未分配分片時，快速取得未分配分片的總量、索引分佈與主副本性質，並建立排查基準線。

## 適用範圍

本手札適用於所有版本之 Elasticsearch 7.x 及 8.x 叢集。

# 核心原理與分片狀態

## 分片狀態與叢集色彩

```text
分片狀態判定：
┌─────────────────────────────────────────────────────────────┐
│ 1. GREEN：所有 Primary 分片與 Replica 分片皆正常分配在線      │
├─────────────────────────────────────────────────────────────┤
│ 2. YELLOW：所有 Primary 分片在線（資料完整可讀寫），但有副本   │
│    （Replica）處於 UNASSIGNED 未分配                         │
├─────────────────────────────────────────────────────────────┤
│ 3. RED：有至少 1 個 Primary 主分片處於 UNASSIGNED 未分配     │
│    ▲ 資料發生局部遺失或無法讀寫，需最優先搶修！                │
└─────────────────────────────────────────────────────────────┘
```

# 報告指標解讀指引

在 `check.html` 報告中，請檢視以下資訊：

| 欄位名稱 | 範例值 | 診斷意涵 | 處置優先級 |
|---|---|---|---|
| `unassigned_shards_count` | `5` | 未分配分片總數 | 高 |
| `unassigned_primaries` | `1` | 主分片未分配數 | `> 0` 為最高危急 |
| `unassigned_replicas` | `4` | 副本未分配數 | 中 |

# 客戶溝通話術與情境模擬

## 話術範例：解釋 Yellow 狀態的未分配分片

> **顧問說明範例**：
> 「主管您好，報告中顯示叢集有 4 個副本分片處於未分配狀態（Yellow 狀態）。
> 
> 我們確認過所有 Primary 主分片均 100% 正常在線，業務當前的查詢與寫入完全不受影響。
> 
> 這通常發生在節點剛重啟或叢集容量調整之際，系統正在重新安排備份副本。我們只需配合 `allocation_guidance` 診斷卡查看具體阻礙原因並予以排除，即可迅速回歸 Green 狀態。」

# 現場排查與安全處置 SOP

## 階段一：現場唯讀佐證（列出所有未分配分片）

```http
GET /_cat/shards?v&h=index,shard,prirep,state,unassigned.reason
```

篩選出 `state` 為 `UNASSIGNED` 的清單，確認 `unassigned.reason`（如 `NODE_LEFT`、`INDEX_CREATED`、`ALLOCATION_FAILED`）。

## 階段二：整改修復方案

- 若原因為單節點無法放副本，將副本數調整為 0 或擴容節點。
- 若原因為暫時失敗，執行 `POST /_cluster/reroute?retry_failed=true`。

## 階段三：變更後驗證

- [ ] 執行 `GET /_cat/shards?v` 確認所有分片狀態均為 `STARTED`
- [ ] 重新執行 `elk-diagnostics check` 確認 `unassigned_shards` 轉為 `PASS`

# 附錄 <!-- appendix -->

## 參考文件

- [Elasticsearch 官方文件：Diagnose unassigned shards](https://www.elastic.co/docs/troubleshoot/elasticsearch/diagnose-unassigned-shards)
- 專案內部規格書：`docs/內部/規格/健康報告規格.md`（#18）
