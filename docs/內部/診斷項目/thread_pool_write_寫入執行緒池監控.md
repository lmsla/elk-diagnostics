---
title: "Write Thread Pool 狀態 — 寫入並行度、Queue 積壓監控與 Bulk 批次調優"
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
| 2026/08/23 | 1.0 | 初版：Write Thread Pool 固定尺寸架構、佇列回堵排查與客戶端批次調優 | 診斷工具架構組 |

# 文件說明

## 報告診斷卡對照

| 屬性 | 內容 |
|---|---|
| 報告分類區塊 | 效能（Performance） |
| 診斷卡中文名稱 | Write Thread Pool 狀態 |
| 診斷卡 ID | `thread_pool_write` |
| 典型嚴重度 | `WARNING`（Queue > 1,000）/ `CRITICAL`（Queue 達到 10,000 滿載） |
| 觸發關鍵特徵 | `_cat/thread_pool/write` 顯示 `write` 執行緒池的 `queue` 數字偏高或頻繁發生排隊積壓 |

## 文件目的

本手札用於協助第一線 ELK 維運工程師與顧問在巡檢報告發現 Write 執行緒池積壓時，評估底層 I/O 與 CPU 寫入吞吐量，並指導客戶端優化 Bulk 寫入批次。

## 適用範圍

本手札適用於所有版本之 Elasticsearch 8.x 及 9.x 叢集。

# 核心原理與 Write Thread Pool 機制

## 什麼是 Write Thread Pool？

Write Thread Pool 專門負責執行索引新增、更新、刪除與 Bulk 批次寫入：
- **尺寸計算公式**：
  $$\text{Write Pool Size} = \text{allocated\_processors} + 1$$
  （例如 8 核 CPU 節點，Write Worker 固定為 9 個）。
- **佇列大小（Queue Size）**：預設為 **10,000**。

```text
客戶端 Bulk 寫入
     ↓
【9 個 Write Worker】（處理 Lucene 段寫入與 Refresh）
     ↓ 寫入速度受限時
【Queue 排隊】（1 ... 2000 ... 10000）
     ↓ 佇列滿載時
拋出 429 Bulk Rejection
```

# 報告指標解讀指引

在 `check.html` 報告中，請檢視以下數值：

| 欄位名稱 | 警戒門檻 | 診斷意涵 |
|---|---|---|
| `write.active` | 等於 pool size | 寫入 Worker 100% 滿載 |
| `write.queue` | > 1,000 | 寫入正在回堵，延遲上升 |
| `write.rejected` | > 0 | 已發生寫入拒絕 |

# 業務影響與技術說明建議

## 說明範例：寫入隊列回堵排查

> **技術說明範例**：
> 「維運同仁您好，報告中顯示節點的 `Write Thread Pool` 隊列已經累積了數千筆等待寫入的請求。
> 
> 這代表寫入請求的灌入速率暫時高於後端硬碟與 CPU 的落盤能力。
> 
> 我們建議檢查客戶端發送批次：**避免單筆單筆頻繁發送，改用每批 5MB～10MB 的 Bulk 寫入**，並將活躍日誌索引的 `refresh_interval` 放寬至 30 秒，寫入消化速度即可獲得顯著提升。」

# 現場排查與安全處置 SOP

## 階段一：現場唯讀佐證

```http
GET /_cat/thread_pool/write?v&h=node_name,name,active,queue,rejected,completed
```

## 階段二：整改修復方案

1. 將日誌索引 `refresh_interval` 設為 `30s`。
2. 檢查磁碟 I/O 延遲，確認是否為硬碟寫入瓶頸。

## 階段三：變更後驗證

- [ ] 執行 `GET /_cat/thread_pool/write` 確認 `queue` 數字迅速回落至 0
- [ ] 重新執行 `elk-diagnostics check` 確認 `thread_pool_write` 轉為 `PASS`

# 附錄 <!-- appendix -->

## 參考文件

- [Elasticsearch 官方文件：Thread pool settings](https://www.elastic.co/docs/reference/elasticsearch/configuration-reference/thread-pool-settings)
- 專案內部規格書：`docs/內部/規格/效能規格.md`（#6）
