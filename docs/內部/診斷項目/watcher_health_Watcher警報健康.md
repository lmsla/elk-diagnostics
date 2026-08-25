---
title: "Watcher 引擎健康 — 警報執行失敗、執行緒積壓與狀態重啟"
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
| 2026/08/23 | 1.0 | 初版：Watcher 服務狀態檢測、Watch 執行失敗統計與 Watcher 引擎重啟 | 診斷工具架構組 |

# 文件說明

## 報告診斷卡對照

| 屬性 | 內容 |
|---|---|
| 報告分類區塊 | 管理（Management） |
| 診斷卡中文名稱 | Watcher 警報引擎健康 |
| 診斷卡 ID | `watcher_health` |
| 典型嚴重度 | `WARNING` / `CRITICAL` |
| 觸發關鍵特徵 | `_watcher/stats` 顯示 Watcher 狀態為 `STOPPED`，或大量 Watch 處於 execution failure |

## 文件目的

本手札提供第一線工程師在 Watcher 警報服務異常時的判定原則、技術說明指引與重啟/排查指令。

## 適用範圍

本手札適用於啟用 Watcher 告警之 Elasticsearch 8.x 及 9.x 叢集。

# 核心概念與 Watcher 機制

```text
Watcher 引擎狀態：
1. STARTED：正常輪詢中。
2. STOPPING / STOPPED：已停止，所有 Watch 告警將不再觸發！
```

# 業務影響與技術說明建議

> **技術說明範例**：
> 「維運同仁您好，報告中顯示 Elasticsearch 的 `Watcher` 警報引擎處於停止狀態（STOPPED）。
> 
> 這會使所有設定的日誌告警與郵件通知停擺。
> 
> 我們只需發送一行 API 指令重新啟動 Watcher 引擎，所有監控規則即可自動恢復即時監聽。」

# 現場 1 分鐘快速佐證與處置 SOP

```http
# 1. 查看 Watcher 即時運行狀態
GET /_watcher/stats

# 2. 重新啟動 Watcher 服務
POST /_watcher/_start
```

# 附錄 <!-- appendix -->

- [Elasticsearch 官方文件：Watcher APIs](https://www.elastic.co/docs/reference/elasticsearch/rest-apis/watcher-apis)
