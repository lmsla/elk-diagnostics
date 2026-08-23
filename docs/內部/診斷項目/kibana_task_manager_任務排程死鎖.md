---
title: "ELK 診斷手札：Kibana Task Manager 健康 — 分散式任務輪詢、熱點索引與排程死鎖排查"
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
| 2026/08/23 | 1.0 | 初版：Kibana Task Manager 機制、.kibana_task_manager 索引熱點與任務死鎖 | 診斷工具架構組 |

# 文件說明

## 報告診斷卡對照

| 屬性 | 內容 |
|---|---|
| 報告分類區塊 | 服務：Kibana（Service: Kibana） |
| 診斷卡中文名稱 | Kibana Task Manager 健康 |
| 診斷卡 ID | `kibana_task_manager` |
| 典型嚴重度 | `WARNING` / `CRITICAL` |
| 觸發關鍵特徵 | Kibana `/api/task_manager/_health` 回傳 `UNHEALTHY`，或存在大量逾時、漂移（Drift）與死鎖之排程任務 |

## 文件目的

本手札用於協助第一線 ELK 維運工程師與顧問在巡檢報告發現 Kibana Task Manager 異常時，掌握 Kibana 後台分散式排程引擎機制，排查警報（Alerting）、報告（Reporting）與遙測任務的執行逾時，並指導清理 `.kibana_task_manager` 索引熱點。

## 適用範圍

本手札適用於所有版本之 Kibana 7.x 及 8.x 實例。

# 核心原理與 Kibana Task Manager 機制

## 什麼是 Kibana Task Manager？

Kibana 雖然是 Web 前端，但它在後台運行著一個分散式任務調度引擎（Task Manager），負責非同步執行所有定時與背景工作：
- **Alerting & Rules**：定時執行查詢以觸發告警（例如每分鐘檢查一次錯誤日誌）。
- **Reporting**：非同步產生 PDF / CSV 報表截圖。
- **Fleet & Security**：管理 Agent 狀態與安全策略。

```text
Kibana 背景排程輪詢
       ↓
┌─────────────────────────────────────────────────────────────┐
│ 透過 Elasticsearch 的 .kibana_task_manager 索引              │
│ 進行分散式樂觀鎖（Optimistic Locking）認領任務               │
└─────────────────────────────────────────────────────────────┘
       ↓
【死鎖/逾時成因】：
1. Kibana 節點 CPU 滿載或 Event Loop 卡死，任務無法在指定時間內回報心跳
2. 告警規則過多（數千條規則每秒同時觸發），Task Queue 發生嚴重積壓
3. .kibana_task_manager 索引分片發生讀寫延遲
```

# 報告指標解讀指引

在 `check.html` 報告中，請檢視以下指標：

| 欄位名稱 | 警戒門檻 | 診斷意涵 |
|---|---|---|
| `task_manager_status` | `UNHEALTHY` | Task Manager 引擎異常 |
| `max_drift` | > 30s | 任務排程時間與實際執行時間的延遲偏差過大 |
| `failed_tasks_count` | > 0 | 已發生嚴重失敗的任務數 |

# 客戶溝通話術與情境模擬

## 話術範例：Kibana 告警延遲與任務卡死

> **顧問說明範例**：
> 「維運同仁您好，報告中顯示 Kibana 的 `Task Manager` 處於 Unhealthy 狀態，排程延遲偏差（Drift）達到 45 秒。
> 
> 這會直接導致所有 Kibana 告警規則（Alerting Rules）延遲發送，甚至部分定時報表產出失敗。
> 
> 經分析，是因為目前建立了超過 500 條高頻率（每 10 秒執行一次）的複雜告警規則，超過了單台 Kibana 的任務吞吐上限。
> 
> 我們建議：
> 1. 調整告警規則的檢查頻率（由 10 秒放寬為 1～5 分鐘）；
> 2. 或水平擴充第二台 Kibana 實例，Task Manager 會自動實現分散式負載均衡，立即消除延遲。」

# 現場排查與安全處置 SOP

## 階段一：現場唯讀佐證（查詢 Kibana 任務健康）

向 Kibana 發送健康檢查 API：

```http
GET /api/task_manager/_health
```

## 階段二：整改修復方案

### 方案 A：調整 Kibana Task Manager 併發配置
在 `kibana.yml` 中調大最大並行任務數與批次量：

```yaml
xpack.task_manager.max_workers: 20
xpack.task_manager.poll_interval: 3000
```

### 方案 B：水平擴展 Kibana 節點
啟動多個 Kibana 實例指向同一 Elasticsearch 叢集，Task Manager 會自動在多節點間協同分配任務。

## 階段三：變更後驗證

- [ ] 執行 `GET /api/task_manager/_health` 確認 `status` 轉為 `OK`
- [ ] 重新執行 `elk-diagnostics check` 確認 `kibana_task_manager` 轉為 `PASS`

# 附錄 <!-- appendix -->

## 參考文件

- [Kibana 官方文件：Task Manager settings](https://www.elastic.co/docs/reference/kibana/configuration-reference/task-manager-settings)
- 專案內部規格書：`docs/內部/規格/擴充健檢規格.md`
