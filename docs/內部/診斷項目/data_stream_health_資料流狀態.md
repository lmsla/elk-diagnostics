---
title: "Data stream 健康 — 資料流狀態、Rollover 機制與 Backing Index 維護"
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
| 2026/08/23 | 1.0 | 初版：Data Stream 結構、Rollover 機制、隱藏索引異常排查與生命週期管理 | 診斷工具架構組 |

# 文件說明

## 報告診斷卡對照

| 屬性 | 內容 |
|---|---|
| 報告分類區塊 | 資料（Data） |
| 診斷卡中文名稱 | Data stream 健康 |
| 診斷卡 ID | `data_stream_health` |
| 典型嚴重度 | `WARNING`（Yellow）/ `CRITICAL`（Red） |
| 觸發關鍵特徵 | `_data_stream` API 回傳某個 Data Stream 的整總體狀態非 `GREEN`，或背後隱藏索引（Backing Index）發生分片未分配 |

## 文件目的

本手札用於協助第一線 ELK 維運工程師與顧問在巡檢報告發現 Data Stream 異常時，掌握 7.9+ / 8.x 資料流架構，排查 Rollover 滾動失敗與隱藏索引（`.ds-*`）的健康問題。

## 適用範圍

本手札適用於所有採用 Data Stream（日誌、指標、Traces）之 Elasticsearch 7.9+ 及 8.x 叢集。

# 核心原理與 Data Stream 機制

## 什麼是 Data Stream？

Data Stream 是 Elasticsearch 專為時間序列資料（Time-series Data，如日誌與 Metrics）設計的抽象層。

客戶端寫入時只需永遠指向單一名稱（例如 `logs-k8s-prod`），後台會自動將資料寫入帶有流水號的隱藏實體索引（Backing Indices）：

```text
客戶端寫入 → logs-k8s-prod (Data Stream 統一入口)
                  ↓
┌─────────────────────────────────────────────────────────────┐
│ 隱藏實體索引（Backing Indices）                              │
│ 1. .ds-logs-k8s-prod-2026.08.01-000001 (唯讀/歷史)          │
│ 2. .ds-logs-k8s-prod-2026.08.15-000002 (唯讀/歷史)          │
│ 3. .ds-logs-k8s-prod-2026.08.23-000003 (🔥 當前寫入中 Write) │
└─────────────────────────────────────────────────────────────┘
```

## Data Stream 的三大核心要件

1. **必須包含 `@timestamp` 欄位**：寫入的每筆資料必須具有合法時間戳。
2. **依賴 Composable Index Template**：模板中必須宣告 `"data_stream": {}`。
3. **自動 Rollover（滾動）**：結合 ILM 或 DSL，當當前 Write Index 達到 50GB 或 30 天時，自動生成下一個流水號索引。

## 常見異常場景

1. **Backing Index 處於 Red/Yellow**：
   - 歷史某個隱藏索引（如 `-000001`）有分片 unassigned，導致整條 Data Stream 的整體健康度被拉降為 Yellow/Red。
2. **Rollover 卡住未觸發**：
   - Write Index 已經達到 200GB 卻遲遲沒有產生新索引，導致單一分片過度肥大。

# 報告指標解讀指引

在 `check.html` 報告中，請檢視以下資訊：

| 欄位名稱 | 範例值 | 診斷意涵 |
|---|---|---|
| `data_stream_name` | `logs-system-default` | 異常的 Data Stream 名稱 |
| `status` | `YELLOW` / `RED` | 資料流整體健康狀態 |
| `backing_indices_count` | `15` | 背後涵蓋的實體索引數量 |

# 業務影響與技術說明建議

## 說明要點與原則

- **用「資料流與子水庫」比喻**：說明 Data Stream 是水流總入口，後端由多個按時間切割的子水庫（Backing Indices）組成。
- **定位受影響的子索引**：向客戶說明當前寫入是否受影響，還是只是某個歷史分區需要修復。

## 說明範例：Data Stream 顯示 Yellow

> **技術說明範例**：
> 「維運同仁您好，報告中顯示 `logs-app-prod` 資料流為 Yellow 狀態。
> 
> 經深入比對，當前負責即時寫入的最新分區（`...-000005`）完全健康，業務寫入不受任何影響。問題出在 2 週前的一個歷史子索引（`...-000002`）在重啟時副本分片未分配。
> 
> 我們只需針對該特定歷史子索引進行分片重試，即可讓整條 Data Stream 恢復為 Green 狀態。」

# 現場排查與安全處置 SOP

## 階段一：現場唯讀佐證（定位具體子索引）

查詢所有 Data Stream 狀態及背後索引清單：

```http
GET /_data_stream/<DATA_STREAM_NAME>
```

檢查各子索引的個別健康度：

```http
GET /_cat/indices/.ds-<DATA_STREAM_NAME>-*?v&h=health,status,index,docs.count,store.size
```

## 階段二：整改修復方案

### 狀況 A：手動觸發一次 Rollover（若滾動卡住）

```http
POST /<DATA_STREAM_NAME>/_rollover
```

### 狀況 B：修復歷史 Backing Index
若某個歷史子索引副本卡住，可針對該子索引調整副本數或執行 reroute：

```http
POST /_cluster/reroute?retry_failed=true
```

## 階段三：變更後驗證

- [ ] 執行 `GET /_data_stream` 確認 `status` 轉為 `GREEN`
- [ ] 重新執行 `elk-diagnostics check` 確認 `data_stream_health` 轉為 `PASS`

# 常見誤區與風險提示

- [ ] **誤區一：直接向 Data Stream 執行 DELETE 指令**：
  執行 `DELETE /logs-app-prod` 會將整條 Data Stream 以及**其背後所有歷史子索引全數永久刪除**！若只需刪除歷史資料，應刪除單一 Backing Index。

# 附錄 <!-- appendix -->

## 參考文件

- [Elasticsearch 官方文件：Data streams](https://www.elastic.co/docs/reference/elasticsearch/data-streams/data-streams)
- 專案內部規格書：`docs/內部/規格/擴充健檢規格.md`（ES-GAP-15）
