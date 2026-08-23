---
title: "ELK 診斷手札：升級棄用警告 — 跨大版本升級不相容項清查與平滑升級評估"
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
| 2026/08/23 | 1.0 | 初版：Deprecations API 解讀、跨版本（7.x → 8.x → 9.x）不相容項清查與整改 | 診斷工具架構組 |

# 文件說明

## 報告診斷卡對照

| 屬性 | 內容 |
|---|---|
| 報告分類區塊 | 管理（Management） |
| 診斷卡中文名稱 | 升級棄用警告 |
| 診斷卡 ID | `upgrade_deprecations` |
| 典型嚴重度 | `WARNING`（存在即將廢棄項目）/ `CRITICAL`（存在阻礙升級之關鍵項目） |
| 觸發關鍵特徵 | `_migration/deprecations` 偵測到有索引 Mapping、設定檔參數或 API 在未來大版本中已被廢棄 |

## 文件目的

本手札用於協助第一線 ELK 維運工程師與顧問在巡檢報告發現升級棄用警告時，協助客戶盤點現有環境在升級至下一個重大版本（如 7.x 升 8.x，或 8.x 升 9.x）前必須修復的不相容項目，並給出平滑升級建議。

## 適用範圍

本手札適用於所有計劃進行版本升級評估之 Elasticsearch 7.x 及 8.x 叢集。

# 核心原理與跨版本升級不相容項

## 什麼是 Deprecations API？

Elasticsearch 提供了 `GET /_migration/deprecations` API，主動掃描整座叢集的以下層級：
1. **Cluster Settings**：廢棄的全域設定參數。
2. **Node Settings**：節點本機 `elasticsearch.yml` 中的過時參數。
3. **Index Mappings & Settings**：索引模板或現存索引中不支援的欄位型態或語法。

```text
Deprecations 掃描
       ↓
┌───────────────────────────────────────────────┐
│ 1. CRITICAL (阻礙升級)：升級前必須修正，否則節點無法啟動 │
│ 2. WARNING (即將廢棄)：未來下個大版本將移除       │
│ 3. INFO (提示資訊)：新特性推薦使用               │
└───────────────────────────────────────────────┘
```

## 常見重大版本升級痛點（7.x → 8.x 範例）

1. **Mapping `_type` 徹底移除**：
   - 舊客戶端若在 URL 依然帶有 `/{index}/{type}/{id}`，在 8.x 會被嚴格拒絕。
2. **舊版安全與 TLS 參數廢棄**：
   - 8.x 預設強制開啟安全與 HTTPS，舊版 `xpack.security.enabled: false` 相關設定被重新規範。
3. **Template 語法轉型**：
   - 舊版 Legacy Template（`_template`）必須遷移為現代 Composable Index Template（`_index_template`）。

# 報告指標解讀指引

在 `check.html` 報告中，請檢視以下資訊：

| 欄位名稱 | 範例值 | 診斷意涵 | 處置優先級 |
|---|---|---|---|
| `critical_deprecations` | `2` | 阻礙升級的致命項目 | 必須先修復才能升級 |
| `warning_deprecations` | `5` | 建議修復的廢棄項目 | 升級前規劃整改 |

# 客戶溝通話術與情境模擬

## 話術範例：向主管報告升級前置準備

> **顧問說明範例**：
> 「主管您好，報告中顯示叢集有 2 項 `CRITICAL` 級別的升級棄用警告。
> 
> 這代表我們如果直接升級至下一個重大版本（8.x / 9.x），這 2 個舊版設定會導致 Elasticsearch 啟動失敗。
> 
> 經盤點，主要是 3 個歷史日誌模板仍在使用舊版的 Legacy Template 語法。
> 
> 我們建議在排定版本升級窗口前，先花 1 週時間將這 3 個模板轉移為標準的 Composable Template，掃除所有升級阻礙，確保未來版本升級能實現 100% 零停機平滑過渡。」

# 現場排查與安全處置 SOP

## 階段一：現場唯讀佐證（導出完整棄用清單）

```http
GET /_migration/deprecations
```

檢視回傳結果中的 `level: critical` 項目與 `details` 說明。

## 階段二：整改修復方案

依照官方提供的 `url` 連結逐項修正 Template 或 Mapping：
- 將舊版語法轉換為新版標準語法。
- 清除 `elasticsearch.yml` 中過時的配置鍵名。

## 階段三：變更後驗證

- [ ] 執行 `GET /_migration/deprecations` 確認 `critical` 項目為 `0`
- [ ] 重新執行 `elk-diagnostics check` 確認 `upgrade_deprecations` 轉為 `PASS`

# 附錄 <!-- appendix -->

## 參考文件

- [Elasticsearch 官方文件：Migration deprecations API](https://www.elastic.co/docs/reference/elasticsearch/rest-apis/migration-apis)
- 專案內部規格書：`docs/內部/規格/管理功能規格.md`（#34）
