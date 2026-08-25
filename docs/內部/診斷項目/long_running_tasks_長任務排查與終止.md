---
title: "長時間 task 監控 — 慢查詢、Reindex 卡住與 Task Cancel 安全終止"
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
| 2026/08/23 | 1.0 | 初版：Task Management 架構、長任務（Reindex/Scroll）卡住排查與安全取消 SOP | 診斷工具架構組 |

# 文件說明

## 報告診斷卡對照

| 屬性 | 內容 |
|---|---|
| 報告分類區塊 | 效能（Performance） / 靜態健檢 |
| 診斷卡中文名稱 | 長時間運行 task 監控 |
| 診斷卡 ID | `long_running_tasks` |
| 典型嚴重度 | `INFO` / `WARNING`（運行時間 > 300 秒） |
| 觸發關鍵特徵 | `_tasks` API 偵測到有查詢（Search）、資料重建（Reindex）或 Scroll 任務運行超過 5 分鐘以上 |

## 文件目的

本手札用於協助第一線 ELK 維運工程師與顧問在巡檢報告發現長任務警告時，區分「正常的大型批次作業」與「孤兒查詢（Orphaned Query）/ 死鎖任務」，並指導使用 Task Cancel API 進行精準安全終止。

## 適用範圍

本手札適用於所有版本之 Elasticsearch 8.x 及 9.x 叢集。

# 核心原理與 Task Management 機制

## 什麼是 Tasks API？

Elasticsearch 內部所有耗時較長的操作（例如跨叢集查詢、`_reindex`、`_update_by_query`、`_delete_by_query`、Rollup）都會被註冊為一個具有唯一 Task ID（格式為 `node_id:task_number`）的 **Task**。

```text
客戶端發起非同步任務 POST _reindex
       ↓
【Task Management 註冊】分配 Task ID: es-node-01:18294
       ↓
┌───────────────────────────────────────────────┐
│ 正常任務：穩定推進文件批次，定期回報進度        │
│ 異常任務：客戶端已斷線但後端仍在空轉（孤兒任務）│
│          或被底層鎖死/大聚合卡死達數小時       │
└───────────────────────────────────────────────┘
       ↓
可透過 POST _tasks/<TASK_ID>/_cancel 安全終止！
```

## 常見長任務類型

1. **未設逾時的 Scroll 查詢**：
   - 外部爬蟲或報表程式開啟了 Scroll 查詢，但在取回部分資料後程式當機斷線，未呼叫 `DELETE _search/scroll`，導致後台 Context 常駐數小時並佔用 Heap。
2. **海量資料 Reindex 未加節流（Throttling）**：
   - 重建 1 億筆資料時，單一任務持續滿載運行數小時，拖慢其他正常業務查詢。
3. **無過濾條件的 `_update_by_query`**：
   - 對全量索引進行欄位更新，引發長時間版本衝突（Version Conflict）與反覆重試。

# 報告指標解讀指引

在 `check.html` 報告中，請檢視以下資訊：

| 欄位名稱 | 範例值 | 診斷意涵 | 處置建議 |
|---|---|---|---|
| `task_id` | `es-node-01:18294` | 具體任務唯一識別碼 | 用於精準 Cancel |
| `action` | `indices:data/read/search` | 正在執行的操作類型 | 判斷是否為查詢 |
| `running_time` | `45m` | 該任務已持續運行的時間 | 超過 5m 需評估 |
| `cancellable` | `true` | 該任務是否支援線上取消 | 若為 true 可安全取消 |

# 業務影響與技術說明建議

## 說明要點與原則

- **確認業務背景**：先向客戶確認是否當下正在執行手動資料重構（Reindex）。
- **說明清理孤兒任務效益**：若為客戶端斷線遺留的慢查詢，說明終止它能立刻釋放被佔用的執行緒與記憶體。

## 說明範例：排查常駐孤兒查詢

> **技術說明範例**：
> 「開發團隊您好，報告中偵測到後台有 2 個搜尋任務（Search Tasks）已經在伺服器上持續運行了超過 45 分鐘。
> 
> 經檢視，這兩個任務是某個內部報表系統發起的深度 Scroll 查詢，且客戶端連接似乎早已關閉，但後台仍持續在記憶體中掃描資料。
> 
> 這些長跑任務會霸佔 Search Thread Pool 的 Worker 資源。若您確認這並非正在進行的重要資料導出，我們可以透過 `_cancel` API 安全終止這兩個任務，立即釋放被佔用的運算資源。」

# 現場排查與安全處置 SOP

## 階段一：現場唯讀佐證（列出運行中的長任務）

```http
GET /_tasks?detailed=true&group_by=none
```

檢視 `running_time_in_nanos` 超過 300 秒的任務，查看 `description` 欄位確認其具體執行的 DSL 內容。

## 階段二：整改修復方案（安全取消任務）

### 方案 A：取消單一特定任務
使用該任務的具體 Task ID 執行取消：

```http
POST /_tasks/<TASK_ID>/_cancel
```

### 方案 B：批量取消特定類型的慢查詢（如所有 Scroll 查詢）
```http
POST /_tasks/_cancel?actions=*search*
```

## 階段三：變更後驗證

- [ ] 執行 `GET /_tasks` 確認該卡住任務已成功消失
- [ ] 重新執行 `elk-diagnostics check` 確認 `long_running_tasks` 轉為 `PASS`

# 附錄 <!-- appendix -->

## 參考文件

- [Elasticsearch 官方文件：Task management API](https://www.elastic.co/docs/reference/elasticsearch/rest-apis/task-management)
- 專案內部規格書：`docs/內部/規格/靜態健檢規格.md`（ES-GAP-01）
