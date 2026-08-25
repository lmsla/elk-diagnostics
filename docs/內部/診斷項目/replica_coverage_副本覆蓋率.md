---
title: "Index 副本覆蓋度 — 零副本單點風險、資料安全性與範本調優"
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
| 2026/08/23 | 1.0 | 初版：副本覆蓋度評估、零副本單點故障（SPOF）風險與批次補齊副本 | 診斷工具架構組 |

# 文件說明

## 報告診斷卡對照

| 屬性 | 內容 |
|---|---|
| 報告分類區塊 | 資料（Data） / 靜態健檢 |
| 診斷卡中文名稱 | Index 副本覆蓋度 |
| 診斷卡 ID | `replica_coverage` |
| 典型嚴重度 | `WARNING` |
| 觸發關鍵特徵 | 正式環境存在 `number_of_replicas: 0` 的非暫存業務索引，缺乏高可用備份 |

## 文件目的

本手札提供第一線工程師在正式環境發現未配置副本（Replica = 0）時的風險評估、技術說明指引與批次配置指令。

## 適用範圍

本手札適用於所有正式營運之 Elasticsearch 7.x 及 8.x 叢集。

# 核心概念與單點風險

```text
number_of_replicas = 0 的風險：
▲ 只要承載該分片的伺服器突發硬體毀損或重開機，該索引將立即變成 RED 狀態，業務查詢直接中斷！
```

# 業務影響與技術說明建議

> **技術說明範例**：
> 「維運主管您好，報告中顯示有 5 個業務日誌索引的副本數設為 0。
> 
> 這意味著這些資料在叢集中只有單一份拷貝，一旦所在的主機發生重啟或硬體故障，資料將暫時無法存取。
> 
> 官方標準規範要求正式環境**至少配置 1 個副本（Replica = 1）**。我們只需執行一行指令將副本數調為 1，即可獲得完整的硬體容錯能力。」

# 現場 1 分鐘快速佐證與處置 SOP

```http
# 1. 快速列出所有副本數為 0 的索引名單
GET /_cat/indices?v&s=rep:asc&h=index,pri,rep,docs.count,store.size

# 2. 批量將目標索引的副本數補齊為 1
PUT /logs-app-*/_settings
{
  "index": {
    "number_of_replicas": 1
  }
}
```

# 附錄 <!-- appendix -->

- [Elasticsearch 官方文件：Index settings (number_of_replicas)](https://www.elastic.co/docs/reference/elasticsearch/configuration-reference/index-level-settings#dynamic-index-settings)
