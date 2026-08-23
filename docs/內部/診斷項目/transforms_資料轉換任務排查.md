---
title: "ELK 診斷手札：Transforms 轉換任務 — 連續聚合轉換、失敗原因定位與重置 SOP"
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
| 2026/08/23 | 1.0 | 初版：Transforms 運作機制、Checkpoint 推進、Failed 失敗原因提取與重置 | 診斷工具架構組 |

# 文件說明

## 報告診斷卡對照

| 屬性 | 內容 |
|---|---|
| 報告分類區塊 | 管理（Management） |
| 診斷卡中文名稱 | Transforms 轉換任務 |
| 診斷卡 ID | `transforms` |
| 典型嚴重度 | `WARNING` / `CRITICAL` |
| 觸發關鍵特徵 | `_transform/_stats` 顯示特定 Transform 任務處於 `failed` 狀態或頻繁報錯 |

## 文件目的

本手札用於協助第一線 ELK 維運工程師與顧問在巡檢報告發現 Transform 任務失敗時，快速提取底層 `reason` 錯誤原因（如來源 Mapping 變更、目標索引權限不足、聚合超時），並指導執行安全重啟或 Checkpoint 重置。

## 適用範圍

本手札適用於使用 Transform（透視表/實體轉換）之 Elasticsearch 7.x 及 8.x 叢集。

# 核心原理與 Transforms 機制

## 什麼是 Transform？

Elasticsearch Transform 是將現有索引資料進行 **即時透視（Pivot）或實體化彙總（Entity-centric Indexing）** 的後台功能（例如將每秒數萬筆的原始 Web Access Log，即時彙總為以 `user_id` 為主鍵的活躍使用者統計表）。

```text
原始時間序列日誌（海量 raw logs）
       ↓
【Continuous Transform】後台定期檢查 Checkpoint
       ↓ 執行聚合（terms + sum/avg）
目標匯總實體索引（輕量 pivot index）
```

## 任務失敗（`state: failed`）三大主因

1. **來源欄位型態變更（Mapping Conflict）**：
   - 來源索引新增了非預期型態的資料，導致 Transform 的聚合 Script 拋出 ClassCastException。
2. **目標索引權限或唯讀鎖定**：
   - 目標匯總索引被磁碟水位線鎖定為唯讀，或 Transform 執行的 API Key 缺乏寫入權限。
3. **聚合查詢超時（Search Timeout）**：
   - 單個 Checkpoint 涵蓋的資料量過大，聚合計算超過 30 秒被底層中斷。

# 報告指標解讀指引

在 `check.html` 報告中，請檢視以下資訊：

| 欄位名稱 | 範例值 | 診斷意涵 | 處置建議 |
|---|---|---|---|
| `transform_id` | `user-daily-metrics` | 具體失敗的 Transform ID | 用於重啟與修復 |
| `state` | `failed` | 當前狀態（已停擺） | 需立即介入 |
| `reason` | `Search timeout after 30s...` | 具體底層報錯訊息 | 依據原因修復 |

# 業務影響與技術說明建議

## 說明範例：Transform 任務失敗停滯

> **技術說明範例**：
> 「資料工程團隊您好，報告中顯示 `user-daily-metrics` 的 Transform 資料轉換任務目前處於 `failed` 停止狀態。
> 
> 經分析底層錯誤日誌，是因為昨天來源索引中有部分日誌的 `response_time` 欄位送入了字串格式，導致 Transform 在進行平均值計算時發生型態錯誤而中斷。
> 
> Transform 停擺會導致後續的報表儀表板數據停留在昨天的狀態。
> 
> 我們建議修復來源資料格式，並執行一次安全重啟（`_start` API），Transform 會自動從上次中斷的 Checkpoint 繼續補算，確保數據完整銜接。」

# 現場排查與安全處置 SOP

## 階段一：現場唯讀佐證（提取失敗 Reason）

```http
GET /_transform/<TRANSFORM_ID>/_stats
```

查看回傳 JSON 中的 `reason` 欄位，精準掌握具體 Java 異常堆疊。

## 階段二：整改修復方案

### 步驟 1：若需要重置 Checkpoint（從頭或從特定時間重算）
先停止任務：
```http
POST /_transform/<TRANSFORM_ID>/_stop?force=true
```

若需重置目標索引：
```http
POST /_transform/<TRANSFORM_ID>/_reset
```

### 步驟 2：重新啟動 Transform
```http
POST /_transform/<TRANSFORM_ID>/_start
```

## 階段三：變更後驗證

- [ ] 執行 `GET /_transform/<TRANSFORM_ID>/_stats` 確認 `state` 恢復為 `started` 或 `indexing`
- [ ] 重新執行 `elk-diagnostics check` 確認 `transforms` 轉為 `PASS`

# 附錄 <!-- appendix -->

## 參考文件

- [Elasticsearch 官方文件：Transform APIs](https://www.elastic.co/docs/reference/elasticsearch/rest-apis/transform-apis)
- 專案內部規格書：`docs/內部/規格/管理功能規格.md`（#28）
