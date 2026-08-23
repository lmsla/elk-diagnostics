---
title: "ELK 診斷手札：Restore 快照還原進度 — 進行中還原任務追蹤與進度瓶頸排查"
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
| 2026/08/23 | 1.0 | 初版：Recovery API 快照還原進度監控、分片下載卡住與頻寬限制排查 | 診斷工具架構組 |

# 文件說明

## 報告診斷卡對照

| 屬性 | 內容 |
|---|---|
| 報告分類區塊 | 資料（Data） / 快照（Snapshots） |
| 診斷卡中文名稱 | Restore 快照還原進度 |
| 診斷卡 ID | `restore_status` |
| 典型嚴重度 | `INFO`（正常還原中）/ `WARNING`（還原停滯或超時） |
| 觸發關鍵特徵 | 叢集中存在正在從 Snapshot 執行還原（Recovery from snapshot）之索引與分片 |

## 文件目的

本手札提供第一線工程師在進行資料災難復原或環境遷移時，監控快照下載百分比、排查還原停滯原因與調整下載頻寬。

## 適用範圍

本手札適用於所有版本之 Elasticsearch 7.x 及 8.x 叢集。

# 核心概念與還原流程

```text
發起快照還原 POST _snapshot/<repo>/<snap>/_restore
       ↓
【Recovery 階段】：各 Data 節點從 Repository 平行下載 Segment 檔案
       ↓
┌─────────────────────────────────────────────────────────────┐
│ 1. INIT / INDEX：正在拉取檔案（顯示 bytes_percent: 45%）    │
│ 2. TRANSLOG：重放交易日誌                                   │
│ 3. DONE：分片還原完畢，轉為 STARTED 正常可讀寫              │
└─────────────────────────────────────────────────────────────┘
```

# 業務影響與技術說明建議

> **技術說明範例**：
> 「維運主管您好，報告顯示目前正在執行資料快照還原作業，整體進度已完成 78%，資料正在自雲端備份庫平滑下載中。
> 
> 為了加快還原速度，我們可以臨時調高節點的恢復頻寬限制（`max_bytes_per_sec`），在不影響現有業務的情況下縮短 50% 以上的還原時間。」

# 現場 1 分鐘快速佐證與處置 SOP

```http
# 1. 快速查看所有分片的還原百分比
GET /_cat/recovery?v&active_only=true&h=index,shard,stage,files_percent,bytes_percent,time

# 2. 臨時調大還原下載速度限制（預設 40mb/s，可調至 200mb/s）
PUT /_cluster/settings
{
  "transient": {
    "indices.recovery.max_bytes_per_sec": "200mb"
  }
}
```

# 附錄 <!-- appendix -->

- [Elasticsearch 官方文件：Cat recovery API](https://www.elastic.co/docs/reference/elasticsearch/rest-apis/cat-recovery)
