---
title: "ELK 診斷手札：磁碟容量 / watermark — 三級磁碟水位線防護與容量清理速查"
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
| 2026/08/23 | 1.0 | 初版：Low/High/Flood-stage 三級水位線速查與磁碟清理指令 | 診斷工具架構組 |

# 文件說明

## 報告診斷卡對照

| 屬性 | 內容 |
|---|---|
| 報告分類區塊 | 容量（Capacity） |
| 診斷卡中文名稱 | 磁碟容量 / watermark |
| 診斷卡 ID | `disk` |
| 典型嚴重度 | `WARNING`（> 85% Low / 90% High）/ `CRITICAL`（> 95% Flood-stage） |
| 觸發關鍵特徵 | 節點磁碟使用率觸發 85%/90%/95% 水位線防護 |

## 文件目的

本手札提供第一線工程師在磁碟觸發水位線時的快速判斷、客戶說明話術與緊急空間清理指令。

## 適用範圍

本手札適用於所有版本之 Elasticsearch 7.x 及 8.x 叢集。

# 核心概念與三級水位線

```text
┌─────────────────────────┬────────┬────────────────────────────────────────────────────────┐
│ 水位線名稱               │ 預設門檻│ 系統自動行為                                           │
├─────────────────────────┼────────┼────────────────────────────────────────────────────────┤
│ 1. Low Watermark        │ 85%    │ 停止向該節點指派新分片。                               │
│ 2. High Watermark       │ 90%    │ 開始主動將該節點的現有分片搬移至其他較空節點。          │
│ 3. Flood-stage Watermark│ 95%    │ 強制將該節點所有索引設為 read_only_allow_delete（唯讀） │
└─────────────────────────┴────────┴────────────────────────────────────────────────────────┘
```

# 客戶溝通公版話術

> **顧問說明範例**：
> 「維運主管您好，報告顯示部分資料節點硬碟使用率已突破 85%（或 90%）保護水位線。
> 
> 這是 Elasticsearch 內建的防護機制，正在防止單一伺服器因硬碟寫滿而崩潰。
> 
> 建議我們立即清理 30 天前的歷史舊日誌，或為資料磁碟擴充容量；只要容量降至 85% 以下，系統即會自動恢復正常分片調度。」

# 現場 1 分鐘快速佐證與處置 SOP

```http
# 1. 查看各節點磁碟剩餘空間排行榜
GET /_cat/allocation?v&s=disk.percent:desc&h=node,shards,disk.percent,disk.avail,disk.used

# 2. 找出佔用容量最大的前 5 大歷史索引
GET /_cat/indices?v&s=store.size:desc&h=index,docs.count,store.size

# 3. 緊急刪除過期索引以釋放空間
DELETE /logs-history-2026.06.*
```

# 附錄 <!-- appendix -->

- [Elasticsearch 官方文件：Fix watermark errors](https://www.elastic.co/docs/troubleshoot/elasticsearch/fix-watermark-errors)
