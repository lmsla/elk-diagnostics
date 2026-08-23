---
title: "ELK 診斷手札：Planned shutdown 登記狀態 — 節點優雅下線與平滑分片搬移"
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
| 2026/08/23 | 1.0 | 初版：8.x Planned Shutdown API 機制、下線停滯（STALLED）排查與安全維護 SOP | 診斷工具架構組 |

# 文件說明

## 報告診斷卡對照

| 屬性 | 內容 |
|---|---|
| 報告分類區塊 | 叢集（Cluster） |
| 診斷卡中文名稱 | Planned shutdown 登記狀態 |
| 診斷卡 ID | `planned_shutdown` |
| 典型嚴重度 | `WARNING` / `CRITICAL` |
| 觸發關鍵特徵 | 存在處於 `STALLED`（停滯）或長期未完成的節點計畫面下線登記（Node Planned Shutdown） |

## 文件目的

本手札用於協助第一線 ELK 維運工程師與顧問在巡檢報告發現 `planned_shutdown` 異常時，掌握 Elasticsearch 8.x 優雅下線機制的運作狀態，排查分片轉移停滯原因，並確保伺服器維護流程的安全性。

## 適用範圍

本手札適用於 Elasticsearch 8.x 版本（7.x 未提供此原生 API）。

# 核心原理與 Planned Shutdown 機制

## 什麼是 Planned Shutdown API？

在過去（ES 7.x 以前），要重啟或下線某個節點，維運人員必須手動設定 Allocation Exclusion，手動觀察分片清空後才能關機。

在 Elasticsearch 8.x 中，官方引入了標準的 **Node Planned Shutdown API**。在進行節點維護前，可以顯式宣告該節點即將下線（分為暫時重啟 `restart` 或永久下線 `remove`）：

```http
PUT /_nodes/<NODE_ID>/shutdown
{
  "type": "restart",
  "reason": "OS kernel patching"
}
```

## 下線狀態機流轉

```text
下線請求登記 (PUT _nodes/shutdown)
       ↓
【MIGRATING】正在將該節點的分片平滑複製/轉移至其他節點
       ↓
┌───────────────────────────────┐
│ 正常情況：分片清空完畢          │ ──→ 轉為【COMPLETE】，維運人員可安全關機
│ 異常情況：目標節點空間不足/拒絕 │ ──→ 轉為【STALLED】（停滯告警！），分片搬不動
└───────────────────────────────┘
```

## 為什麼會發生 `STALLED` 停滯？

1. **其他節點磁碟空間不足**：叢集其餘節點已達 High Watermark（90%），無處可塞要搬移的分片。
2. **單點副本限制**：沒有其他符合路由條件的 Data 節點可以接收分片。
3. **分片搬移併發過高**：同時有多個節點登記下線，引發網路與磁碟搬遷壅塞。

# 報告指標解讀指引

在 `check.html` 報告中，請檢視以下資訊：

| 欄位名稱 | 範例值 | 診斷意涵 | 處置建議 |
|---|---|---|---|
| `shutdown_status` | `STALLED` | 停滯告警 | 需立刻排查其他節點磁碟與 Deciders |
| `shutdown_status` | `COMPLETE` | 搬遷完成 | 可安全關閉該節點 |
| `shutdown_type` | `restart` / `remove` | 下線模式 | 確認與預期維護場景相符 |

# 業務影響與技術說明建議

## 說明範例：下線流程卡在 `STALLED`

> **技術說明範例**：
> 「維運同仁您好，報告顯示 `es-data-03` 登記了維護下線，但狀態目前停留在 `STALLED`（停滯）。
> 
> 這代表系統正試圖將該節點上的資料安全疏散至其他伺服器，但其餘節點的磁碟空間不足（或觸發了分片限制），導致分片搬遷被卡住。
> 
> 在此狀態下**千萬不要強制關閉伺服器**，否則會造成部分索引直接轉為 Red 或資料遺失。我們需要先清理其餘節點的磁碟空間，讓分片疏散順利轉為 `COMPLETE` 後再行關機。」

# 現場排查與安全處置 SOP

## 階段一：現場唯讀佐證

查詢各節點的 Planned Shutdown 詳細進度：

```http
GET /_nodes/shutdown
```

## 階段二：整改修復方案

### 狀況 A：排查並疏通停滯原因
配合 `POST /_cluster/allocation/explain` 確認目標節點為何拒絕接收搬移過來的分片，釋放目標節點磁碟空間。

### 狀況 B：若維護取消，安全撤銷下線登記

```http
DELETE /_nodes/<NODE_ID>/shutdown
```

## 階段三：變更後驗證

- [ ] 執行 `GET /_nodes/shutdown` 確認狀態為 `COMPLETE`（可關機）或已清除（恢復生產）
- [ ] 重新執行 `elk-diagnostics check` 確認 `planned_shutdown` 轉為 `PASS`

# 常見誤區與風險提示

- [ ] **誤區一：看到 `MIGRATING` 就立刻關機**：
  在狀態尚未變為 `COMPLETE` 之前關機，等同於非預期斷電，會導致副本尚未同步完成的分片直接掉線。

# 附錄 <!-- appendix -->

## 參考文件

- [Elasticsearch 官方文件：Node shutdown API](https://www.elastic.co/docs/reference/elasticsearch/rest-apis/node-shutdown-apis)
- 專案內部規格書：`docs/內部/規格/擴充健檢規格.md`（ES-GAP-12）
