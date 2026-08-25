---
title: "Search Thread Pool 狀態 — 查詢 Worker 飽和、排隊佇列監控與資源隔離"
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
| 2026/08/23 | 1.0 | 初版：Search Thread Pool 尺寸計算、Active 飽和度排查與 Coordinating 節點分離 | 診斷工具架構組 |

# 文件說明

## 報告診斷卡對照

| 屬性 | 內容 |
|---|---|
| 報告分類區塊 | 效能（Performance） |
| 診斷卡中文名稱 | Search Thread Pool 狀態 |
| 診斷卡 ID | `thread_pool_search` |
| 典型嚴重度 | `WARNING`（Queue > 100）/ `CRITICAL`（Queue 達到 1,000 滿載） |
| 觸發關鍵特徵 | `_cat/thread_pool/search` 顯示 `search` 執行緒池的 `active` 持續滿載或 `queue` 長時間排隊 |

## 文件目的

本手札用於協助第一線 ELK 維運工程師與顧問在巡檢報告發現 Search 執行緒池排隊時，快速評估當前查詢負載是否已超出節點算力承載極限，並指導架構層級的 Coordinating 專用節點隔離。

## 適用範圍

本手札適用於所有版本之 Elasticsearch 8.x 及 9.x 叢集。

# 核心原理與 Search Thread Pool 機制

## 什麼是 Search Thread Pool？

Search Thread Pool 專門負責執行索引查詢、文件取回與聚合計算。

- **尺寸計算公式**：
  $$\text{Search Pool Size} = \frac{\text{allocated\_processors} \times 3}{2} + 1$$
  （例如 8 核 CPU 節點，Search Worker 數量為 13 個）。
- **佇列大小（Queue Size）**：預設為 **1,000**。

```text
客戶端查詢請求
     ↓
【13 個 Search Worker】（全部處於 Active 執行中）
     ↓ 滿載時
【Queue 排隊】（1 ... 500 ... 1000）
     ↓ 超過 1,000 時
拋出 429 rejected_execution_exception
```

## Active 居高不下代表什麼？

1. **有長時間慢查詢霸佔 Worker**：例如無分頁全表掃描或未加過濾的大型聚合。
2. **併發查詢量超過硬體負載**：多個儀表板或微服務每秒發送數百次查詢。

# 報告指標解讀指引

在 `check.html` 報告中，請檢視以下數值：

| 欄位名稱 | 警戒門檻 | 診斷意涵 |
|---|---|---|
| `search.active` | 等於 pool size | 查詢工作執行緒 100% 飽和 |
| `search.queue` | > 100（Warning）<br>> 500（Critical） | 查詢正在排隊，延遲增加 |
| `search.rejected` | > 0 | 已發生查詢丟失/拒絕 |

# 業務影響與技術說明建議

## 說明範例：Search Worker 飽和導致查詢變慢

> **技術說明範例**：
> 「系統維運主管您好，報告中顯示節點的 `Search Thread Pool` 活躍度長期處於 100% 滿載狀態，且隊列有數百筆查詢在排隊。
> 
> 這代表伺服器的查詢消化速度跟不上前端進來的請求量，導致所有使用者的查詢延遲被動拉長。
> 
> 我們建議採取兩項改進措施：
> 1. **透過 Search Slow Log 揪出耗時超過 2 秒的慢查詢語句**，優化其聚合與分頁邏輯；
> 2. **在架構前端規劃 2 台專用的 Coordinating 協調節點**，將查詢接收與資料節點隔離，大幅提升併發響應能力。」

# 現場排查與安全處置 SOP

## 階段一：現場唯讀佐證

```http
GET /_cat/thread_pool/search?v&h=node_name,name,active,queue,rejected,completed
```

## 階段二：整改修復方案

1. 使用 `GET /_tasks?actions=*search*` 找出執行時間最長的查詢並進行優化。
2. 在前端增加快取層（如 Redis 或 Elasticsearch 內建 Request Cache）。

## 階段三：變更後驗證

- [ ] 執行 `GET /_cat/thread_pool/search` 確認 `queue` 歸零且 `active` 降至 50% 以下
- [ ] 重新執行 `elk-diagnostics check` 確認 `thread_pool_search` 轉為 `PASS`

# 附錄 <!-- appendix -->

## 參考文件

- [Elasticsearch 官方文件：Thread pool settings](https://www.elastic.co/docs/reference/elasticsearch/configuration-reference/thread-pool-settings)
- 專案內部規格書：`docs/內部/規格/效能規格.md`（#6）
