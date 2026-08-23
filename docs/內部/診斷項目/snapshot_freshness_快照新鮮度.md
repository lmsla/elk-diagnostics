---
title: "ELK 診斷手札：Snapshot 新鮮度 — 備份間隔檢測、RPO 達標評估與備份逾期排查"
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
| 2026/08/23 | 1.0 | 初版：快照新鮮度判定（24h/7d 門檻）、RPO 違規分析與即時備份觸發 | 診斷工具架構組 |

# 文件說明

## 報告診斷卡對照

| 屬性 | 內容 |
|---|---|
| 報告分類區塊 | 資料（Data） / 快照（Snapshots） |
| 診斷卡中文名稱 | Snapshot 新鮮度 |
| 診斷卡 ID | `snapshot_freshness` |
| 典型嚴重度 | `WARNING`（超過 24 小時未成功備份）/ `CRITICAL`（超過 7 天無成功快照） |
| 觸發關鍵特徵 | 距離最近一次成功快照的時間差超過預期 RPO 門檻 |

## 文件目的

本手札提供第一線工程師在快照過期（久未備份）時的判定標準、資安通報指引與手動發起備份指令。

## 適用範圍

本手札適用於所有版本之 Elasticsearch 7.x 及 8.x 叢集。

# 核心概念與 RPO 評估

```text
最近一次成功快照時間 vs 當前時間
       ↓
┌─────────────────────────────────────────────────────────────┐
│ 1. < 24 小時：PASS。符合企業標準每日備份 RPO。              │
│ 2. 24 小時 ～ 7 天：WARNING。備份排程可能停擺或未執行。     │
│ 3. > 7 天：CRITICAL。長期無有效備份，面臨極大資料遺失風險！ │
└─────────────────────────────────────────────────────────────┘
```

# 業務影響與技術說明建議

> **技術說明範例**：
> 「資安主管您好，報告中顯示叢集最近一次成功的快照備份時間為 3 天前，已超出 24 小時的標準備份週期（RPO 違規）。
> 
> 這代表一旦今天發生伺服器故障，我們可能丟失過去 3 天的資料。
> 
> 經檢測，是因為自動備份排程在週末被暫停。我們已手動觸發一次即時增量快照，並重新啟用排程，預計 10 分鐘內即可完成最新數據的備份防護。」

# 現場 1 分鐘快速佐證與處置 SOP

```http
# 1. 查看最近 5 次快照的完成時間與狀態
GET /_cat/snapshots/<REPOSITORY_NAME>?v&s=end_epoch:desc&size=5

# 2. 立即發起一次手動增量快照補齊資料
POST /_slm/policy/<POLICY_NAME>/_execute
```

# 附錄 <!-- appendix -->

- [Elasticsearch 官方文件：Snapshot APIs](https://www.elastic.co/docs/reference/elasticsearch/rest-apis/snapshot-restore-apis)
