---
title: "Shard 容量上限 — 叢集分片總數限制與生命週期收斂"
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
| 2026/08/23 | 1.0 | 初版：`cluster.max_shards_per_node` 門檻、叢集分片容量上限與擴展指引 | 診斷工具架構組 |

# 文件說明

## 報告診斷卡對照

| 屬性 | 內容 |
|---|---|
| 報告分類區塊 | 容量（Capacity） |
| 診斷卡中文名稱 | Shard 容量上限 |
| 診斷卡 ID | `shards_capacity` |
| 典型嚴重度 | `WARNING` / `CRITICAL` |
| 觸發關鍵特徵 | 叢集總分片數接近或超過 `cluster.max_shards_per_node * data_nodes` 上限（預設單節點 1,000 Shards） |

## 文件目的

本手札提供第一線工程師在分片總數逼近全域上限時的判定原則、技術說明指引與擴展/收斂指令。

## 適用範圍

本手札適用於所有版本之 Elasticsearch 8.x 及 9.x 叢集。

# 核心概念與上限機制

```text
叢集最大允許分片總數 = cluster.max_shards_per_node (預設 1,000) * 在線 Data 節點數
▲ 當總分片數超過此上限時，Elasticsearch 將拒絕創建任何新索引！
```

# 業務影響與技術說明建議

> **技術說明範例**：
> 「維運主管您好，報告顯示叢集分片總數已達到安全上限的 90%。
> 
> 這是 Elasticsearch 防止過多分片拖垮 Master 節點記憶體的保護機制。一旦達到上限，系統將無法建立新的日誌分區。
> 
> 我們建議執行兩項措施：立即清理超過保存期限的舊索引，並採用 ILM Rollover 將小分片合併，即可迅速降低分片總量。」

# 現場 1 分鐘快速佐證與處置 SOP

```http
# 1. 快速查看叢集當前分片總數
GET /_cluster/health?filter_path=active_shards,active_primary_shards

# 2. 緊急調大單節點分片上限（臨時應變）
PUT /_cluster/settings
{
  "persistent": {
    "cluster.max_shards_per_node": 1500
  }
}
```

# 附錄 <!-- appendix -->

- [Elasticsearch 官方文件：Troubleshooting shards capacity](https://www.elastic.co/docs/troubleshoot/elasticsearch/troubleshooting-shards-capacity-issues)
