---
title: "Cluster pending tasks — Master 任務佇列阻塞、優先級排程與廣播瓶頸"
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
| 2026/08/23 | 1.0 | 初版：Cluster Pending Tasks 機制、優先級佇列阻塞排查與 Cluster State 瓶頸治理 | 診斷工具架構組 |

# 文件說明

## 報告診斷卡對照

| 屬性 | 內容 |
|---|---|
| 報告分類區塊 | 叢集（Cluster） / 靜態健檢 |
| 診斷卡中文名稱 | Cluster pending tasks 排隊阻塞 |
| 診斷卡 ID | `cluster_pending_tasks` |
| 典型嚴重度 | `WARNING`（排隊時間 > 30s）/ `CRITICAL`（排隊時間 > 5 分鐘或大量堆積） |
| 觸發關鍵特徵 | `_cluster/pending_tasks` 回傳 Master 節點待處理的叢集狀態變更任務隊列持續積壓 |

## 文件目的

本手札用於協助第一線 ELK 維運工程師與顧問在巡檢報告發現 Pending Tasks 阻塞時，分析 Master 節點執行緒隊列的阻塞原因（如大量索引建立/刪除、Mapping 更新、節點加入離開震盪），並給出疏通指引。

## 適用範圍

本手札適用於所有版本之 Elasticsearch 7.x 及 8.x 叢集。

# 核心原理與 Pending Tasks 佇列機制

## 什麼是 Cluster Pending Tasks？

Elasticsearch 的 Master 節點是一個**單執行緒事件循環（Single-threaded Event Loop）**架構。

所有改變「叢集狀態（Cluster State）」的操作（例如創建索引、刪除索引、更新 Mapping、分片重新路由、節點加入/離開）都必須排入 Master 的單一工作佇列，由 Master 逐筆依優先級處理：

```text
各節點發送變更請求
       ↓
【Master 優先級佇列】
┌───────────────────────────────────────────────┐
│ 1. URGENT (緊急)：Master 節點選舉、節點失聯     │
│ 2. HIGH (高)：分片路由變更、節點加入          │
│ 3. NORMAL (一般)：索引創建、索引刪除          │
│ 4. LOW / LANGUID (低)：Mapping 批次更新        │
└───────────────────────────────────────────────┘
       ↓
Master 依序處理並產生新 Cluster State 版本 → 廣播給所有節點
```

## 佇列阻塞的常見成因

1. **瞬時建立/刪除數百個索引**：
   - 批次清理腳本同時下達數百個 `DELETE /index-*` 指令，將 Master 佇列塞爆。
2. **Dynamic Mapping 欄位爆炸**：
   - 數千個客戶端同時向不同索引寫入未知 Key，觸發海量的 `put-mapping` 低優先級任務。
3. **Master 節點發生長 GC 停頓**：
   - Master 節點卡在 JVM 垃圾回收中，單執行緒無法消化排隊事件，導致所有任務等待時間（Time in queue）飆升至數分鐘。

# 報告指標解讀指引

在 `check.html` 報告中，請檢視以下資訊：

| 欄位名稱 | 警戒門檻 | 診斷意涵 |
|---|---|---|
| `pending_tasks_count` | > 0（持續） | 佇列中等待處理的任務總數 |
| `max_time_in_queue` | > 30s（Warning）<br>> 300s（Critical） | 任務在佇列中最長等待時間 |
| `task_priority` | `URGENT` / `HIGH` | 阻塞任務的優先級別 |

# 業務影響與技術說明建議

## 說明範例：Master 任務佇列嚴重阻塞

> **技術說明範例**：
> 「維運主管您好，報告中顯示 Master 節點的任務排隊時間已經突破 2 分鐘（`Cluster Pending Tasks` 阻塞）。
> 
> Master 節點好比整座叢集的單一窗口收發處。經分析，剛才自動化腳本一口氣發送了 500 個歷史索引的刪除請求，同時有大量日誌正在動態註冊新欄位，導致收發窗口被完全塞滿。
> 
> 當 Pending Tasks 排隊過久時，新建索引與分片分配會全面停滯。
> 
> 我們建議優化維運排程：**避免瞬間併發大量 DDL 操作，採用帶有間隔的批次刪除**，並收斂動態 Mapping，讓 Master 執行緒隨時保持輕快流暢。」

# 現場排查與安全處置 SOP

## 階段一：現場唯讀佐證（查看排隊中的任務清單）

```http
GET /_cluster/pending_tasks
```

觀察回傳清單中的 `source`（如 `delete-index [...]`、`put-mapping [...]`）與 `time_in_queue`。

## 階段二：整改修復方案

1. **暫停批次維運腳本**：若有自動化批次建立/刪除索引腳本，先暫停發送新請求。
2. **檢查 Master 節點負載**：
   ```http
   GET /_nodes/stats/jvm,process?filter_path=nodes.*.name,nodes.*.jvm.mem,nodes.*.process.cpu
   ```
   確認 Master 節點未發生高 CPU 或 Full GC 假死。

## 階段三：變更後驗證

- [ ] 執行 `GET /_cluster/pending_tasks` 確認回傳 `tasks: []` 清空為 0
- [ ] 重新執行 `elk-diagnostics check` 確認 `cluster_pending_tasks` 轉為 `PASS`

# 附錄 <!-- appendix -->

## 參考文件

- [Elasticsearch 官方文件：Cluster pending tasks API](https://www.elastic.co/docs/reference/elasticsearch/rest-apis/cluster-pending-tasks)
- 專案內部規格書：`docs/內部/規格/靜態健檢規格.md`（ES-GAP-01）
