---
title: "ELK 診斷手札：CCR follower / auto-follow 健康 — 跨叢集複製延遲、Global Checkpoint 落後與同步修復"
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
| 2026/08/23 | 1.0 | 初版：Cross-Cluster Replication 讀寫同步、Checkpoint 落後判定與同步中斷重連 | 診斷工具架構組 |

# 文件說明

## 報告診斷卡對照

| 屬性 | 內容 |
|---|---|
| 報告分類區塊 | 管理（Management） / 擴充健檢 |
| 診斷卡中文名稱 | CCR follower / auto-follow 健康 |
| 診斷卡 ID | `ccr_health` |
| 典型嚴重度 | `WARNING`（同步落後 > 10,000 ops）/ `CRITICAL`（同步例外中斷） |
| 觸發關鍵特徵 | `_ccr/stats` 顯示 Follower 索引與 Leader 索引的 Global Checkpoint 落後過大，或出現 `fatal_exception` |

## 文件目的

本手札用於協助第一線 ELK 維運工程師與顧問在巡檢報告發現跨叢集複製（CCR）異常時，評估異地災備資料同步的即時性，排查同步中斷或 Checkpoint 嚴重落後的原因，並指導執行安全重連。

## 適用範圍

本手札適用於啟用 Platinum / Enterprise License 之 Elasticsearch 7.x 及 8.x 跨叢集複製環境。

# 核心原理與 CCR 機制

## 什麼是 Cross-Cluster Replication（CCR）？

CCR 允許將主資料中心（Leader Cluster）的特定索引，主動、即時、單向地複製到異地災備中心（Follower Cluster）。

```text
主叢集 (Leader Cluster)
  Leader Shard 寫入 (Global Checkpoint: 1,000,000)
       ↓ Transport 跨網拉取 (Pull-based)
從叢集 (Follower Cluster)
  Follower Shard 同步 (Global Checkpoint: 999,950)
       ▲ 落後量 (Lag) = 50 操作
```

## 同步異常兩大主因

1. **跨機房頻寬飽和或延遲過高**：
   - 寫入吞吐量過大，跨機房網路速度跟不上，導致 Operations Lag 持續擴大至數十萬筆。
2. **Leader 索引執行了 Force Merge 或 Mapping 變更**：
   - 某些不相容的 DDL 變更可能導致 Follower 索引無法自動對齊，拋出 `fatal_exception` 並暫停同步。

# 報告指標解讀指引

在 `check.html` 報告中，請檢視以下數值：

| 欄位名稱 | 警戒門檻 | 診斷意涵 |
|---|---|---|
| `operations_behind_global_checkpoint` | > 10,000 ops | 同步落後量過大 |
| `fatal_exception` | 存在異常 stack | 同步已徹底中斷 |

# 客戶溝通話術與情境模擬

## 話術範例：跨機房異地備份同步落後

> **顧問說明範例**：
> 「災備維運主管您好，報告中顯示異地備份叢集的跨叢集複製（CCR）目前落後了約 5 萬筆操作（Lag 偏高）。
> 
> 這代表災備中心的資料存在約數十秒至數分鐘的延遲。經排查，是因為主機房昨晚執行了大規模歷史資料匯入，瞬時流量超過了跨機房專線頻寬。
> 
> 目前連線依然正常推進中。若專線頻寬受限，我們建議在匯入高峰期為 CCR 配置並行讀取線程數優化，確保災備 RPO 永遠維持在 5 秒以內。」

# 現場排查與安全處置 SOP

## 階段一：現場唯讀佐證

查詢 CCR 同步詳細統計：

```http
GET /_ccr/stats
```

## 階段二：整改修復方案（重啟或暫停/恢復同步）

若某個 Follower 索引發生暫時性中斷，可手動暫停後重新恢復：

```http
# 暫停同步
POST /<FOLLOWER_INDEX>/_ccr/pause_follow

# 恢復同步
POST /<FOLLOWER_INDEX>/_ccr/resume_follow
{
  "max_read_request_operation_count": 5120,
  "max_outstanding_read_requests": 12
}
```

## 階段三：變更後驗證

- [ ] 執行 `GET /_ccr/stats` 確認 `operations_behind_global_checkpoint` 收斂至 100 以內
- [ ] 重新執行 `elk-diagnostics check` 確認 `ccr_health` 轉為 `PASS`

# 附錄 <!-- appendix -->

## 參考文件

- [Elasticsearch 官方文件：Cross-cluster replication](https://www.elastic.co/docs/reference/elasticsearch/cluster-management/cross-cluster-replication)
- 專案內部規格書：`docs/內部/規格/擴充健檢規格.md`（ES-GAP-10）
