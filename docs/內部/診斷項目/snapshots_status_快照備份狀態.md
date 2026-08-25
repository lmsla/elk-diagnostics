---
title: "Snapshot 快照備份狀態 — 備份失敗原因定位、增量快照原理與復原演練"
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
| 2026/08/23 | 1.0 | 初版：Snapshot 增量備份機制、PARTIAL / FAILED 狀態分析與儲存庫健康排查 | 診斷工具架構組 |

# 文件說明

## 報告診斷卡對照

| 屬性 | 內容 |
|---|---|
| 報告分類區塊 | 資料（Data） / 快照（Snapshots） |
| 診斷卡中文名稱 | Snapshot 快照備份狀態 |
| 診斷卡 ID | `snapshots_status` |
| 典型嚴重度 | `WARNING`（PARTIAL 快照）/ `CRITICAL`（FAILED 或長期無成功快照） |
| 觸發關鍵特徵 | `_snapshot/_all` 偵測到最近一次快照狀態非 `SUCCESS`（如 `FAILED`、`PARTIAL`、`INCOMPATIBLE`） |

## 文件目的

本手札用於協助第一線 ELK 維運工程師與顧問在巡檢報告發現快照備份異常時，分析快照失敗或殘缺的根本原因（如特定分片超時、物件儲存權限不足、節點離線），並指導重試與驗證流程。

## 適用範圍

本手札適用於使用 Snapshot and Restore 機制之 Elasticsearch 7.x 及 8.x 叢集。

# 核心原理與快照機制

## 什麼是 Snapshot 增量備份？

Elasticsearch 快照是**分片級別的增量備份（Incremental Snapshot）**：
- **第一次快照**：將所有分片的 Lucene Segment 檔案完整複製到儲存庫（Repository，如 GCS、S3、NFS）。
- **後續快照**：只上傳自上次快照以來新增或變更的 Segment 檔案，舊 Segment 僅建立指標引用，因此速度極快且節省空間。

```text
快照狀態流轉：
【IN_PROGRESS】正在上傳 Segment 至 Repository
       ↓
┌─────────────────────────────────────────────────────────────┐
│ 1. SUCCESS：所有 Primary 分片均 100% 成功上傳（健康狀態）    │
│ 2. PARTIAL：部分分片備份成功，但有分片因節點離線/逾時而跳過 │
│ 3. FAILED：儲存庫無法寫入或 Master 節點在中途崩潰           │
└─────────────────────────────────────────────────────────────┘
```

## `PARTIAL` 狀態的隱患

`PARTIAL` 代表「部分成功」。若直接從該快照還原，會缺少部分分片的資料。

常見原因：
1. 備份進行時，某台資料節點重啟或網路中斷。
2. 某個索引處於 Red 狀態（主分片本身就不存在），快照無法讀取該分片。

# 報告指標解讀指引

在 `check.html` 報告中，請檢視以下資訊：

| 欄位名稱 | 範例值 | 診斷意涵 | 處置建議 |
|---|---|---|---|
| `snapshot_name` | `snapshot-2026.08.23` | 最近一次快照名稱 | 用於狀態追蹤 |
| `state` | `FAILED` / `PARTIAL` | 快照最終結果 | 需排查失敗分片 |
| `failed_shards` | `2` | 備份失敗的分片數 | 需定位具體節點 |

# 業務影響與技術說明建議

## 說明範例：快照出現 PARTIAL 警告

> **技術說明範例**：
> 「維運主管您好，報告中顯示昨日凌晨的自動快照狀態為 `PARTIAL`（部分成功），有 2 個分片未順利備份。
> 
> 經比對，是因為備份期間剛好有一台資料節點進行網路維護暫時離線。
> 
> 雖然大部分資料皆已備份完成，但為了確保災難復原時資料 100% 完整，我們建議在節點恢復後手動觸發一次增量快照補齊缺漏，只需數分鐘即可完成。」

# 現場排查與安全處置 SOP

## 階段一：現場唯讀佐證（定位失敗分片與原因）

```http
GET /_snapshot/<REPOSITORY_NAME>/<SNAPSHOT_NAME>/_status
```

查看回傳 JSON 中的 `failures` 區塊，確認具體是哪個節點的哪個分片報錯。

## 階段二：整改修復方案（重新發起快照）

確認叢集 Green/Yellow 且節點連線正常後，發起一次即時快照：

```http
PUT /_snapshot/<REPOSITORY_NAME>/manual-fix-snapshot?wait_for_completion=true
{
  "indices": "*",
  "ignore_unavailable": true,
  "include_global_state": true
}
```

## 階段三：變更後驗證

- [ ] 執行 `GET /_snapshot/<REPOSITORY_NAME>/manual-fix-snapshot` 確認 `state: SUCCESS`
- [ ] 重新執行 `elk-diagnostics check` 確認 `snapshots_status` 轉為 `PASS`

# 附錄 <!-- appendix -->

## 參考文件

- [Elasticsearch 官方文件：Snapshot and restore](https://www.elastic.co/docs/reference/elasticsearch/snapshot-restore/snapshot-restore)
- 專案內部規格書：`docs/內部/規格/資料規格.md`（#12）
