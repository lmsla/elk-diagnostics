---
title: "ELK 診斷手札：叢集健康 / 未分配 shard — Red / Yellow 色彩判定與搶修快速指引"
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
| 2026/08/23 | 1.0 | 初版：Health Report 叢集色彩定義、Red/Yellow 應變流程與速查指令 | 診斷工具架構組 |

# 文件說明

## 報告診斷卡對照

| 屬性 | 內容 |
|---|---|
| 報告分類區塊 | 叢集（Cluster） |
| 診斷卡中文名稱 | 叢集健康 / 未分配 shard |
| 診斷卡 ID | `cluster_health` |
| 典型嚴重度 | `WARNING`（Yellow）/ `CRITICAL`（Red） |
| 觸發關鍵特徵 | `_health_report` 或 `_cluster/health` 回傳非 Green 狀態 |

## 文件目的

本手札提供第一線工程師在叢集呈現 Yellow 或 Red 時的標準判定原則、客戶通報話術與 1 分鐘快速定位指令。

## 適用範圍

本手札適用於所有版本之 Elasticsearch 7.x 及 8.x 叢集。

# 核心概念與色彩定義

```text
┌───────────┬─────────────────────────────────────────────────────────┐
│ GREEN     │ 正常。所有 Primary 與 Replica 分片均已分配且健康運行。  │
├───────────┼─────────────────────────────────────────────────────────┤
│ YELLOW    │ 警告。所有 Primary 正常，但有部分 Replica 處於未分配。   │
│           │ ▲ 資料 100% 完整可讀寫，僅容災冗餘降低。                │
├───────────┼─────────────────────────────────────────────────────────┤
│ RED       │ 嚴重！有至少 1 個 Primary 主分片未分配或遺失。           │
│           │ ▲ 資料發生局部無法讀寫，需立即搶修！                   │
└───────────┴─────────────────────────────────────────────────────────┘
```

# 客戶溝通公版話術

## Yellow 狀態話術
> 「主管您好，目前叢集為 Yellow 狀態，經確認所有資料的主分片均正常在線，業務讀寫 100% 正常。這是因為有部分備份副本分片正在排隊同步，我們已在進行背景調度，預計很快會恢復為 Green。」

## Red 狀態話術
> 「主管您好，目前叢集偵測到 Red 狀態，代表有特定索引的主分片暫時無法連線。我們已啟動緊急排查程序，正全力定位離線節點並進行資料復原。」

# 現場 1 分鐘快速佐證與處置 SOP

```http
# 1. 快速查看色彩與未分配分片數
GET /_cluster/health

# 2. 快速找出處於 RED 的索引名單
GET /_cat/indices?v&health=red

# 3. 呼叫官方 Explain 工具診斷第一個未分配分片
POST /_cluster/allocation/explain
```

# 附錄 <!-- appendix -->

- [Elasticsearch 官方文件：Red or yellow cluster status](https://www.elastic.co/docs/troubleshoot/elasticsearch/red-yellow-cluster-status)
