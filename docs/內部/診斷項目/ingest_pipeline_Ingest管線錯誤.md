---
title: "ELK 診斷手札：Ingest pipeline 失敗 — Ingest 處理器錯誤、正則回溯與容錯設計"
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
| 2026/08/23 | 1.0 | 初版：Ingest Node 處理器架構、Grok/Script 失敗統計與 on_failure 容錯管線 | 診斷工具架構組 |

# 文件說明

## 報告診斷卡對照

| 屬性 | 內容 |
|---|---|
| 報告分類區塊 | 資料（Data） |
| 診斷卡中文名稱 | Ingest pipeline 失敗 |
| 診斷卡 ID | `ingest_pipeline_errors` |
| 典型嚴重度 | `WARNING`（失敗率 > 10%）/ `CRITICAL`（大量寫入因 Pipeline 失敗遭拒） |
| 觸發關鍵特徵 | `_nodes/stats/ingest` 顯示特定 Pipeline 的 `failed_count` 快速增加，或失敗率突破閾值 |

## 文件目的

本手札用於協助第一線 ELK 維運工程師與顧問在巡檢報告發現 Ingest Pipeline 失敗時，快速定位是哪一條 Pipeline、哪一個 Processor（Grok / Date / JSON / Script）解析出錯，並指導配置 `on_failure` 容錯機制以防止日誌丟失。

## 適用範圍

本手札適用於使用 Ingest Node 進行日誌預處理之 Elasticsearch 7.x 及 8.x 叢集。

# 核心原理與 Ingest Pipeline 機制

## 什麼是 Ingest Pipeline？

Ingest Pipeline 是 Elasticsearch 內建的輕量級 ETL 引擎。在資料正式寫入索引前，先透過一連串的 **Processors（處理器）** 對 JSON 進行轉換（例如 Grok 切割、Date 時間解析、GeoIP 地理位置轉換、Drop 過濾等）：

```text
原始日誌 JSON 寫入
       ↓
【Ingest Pipeline】
┌─────────────────────────────────────────────────────────────┐
│ 1. Grok Processor：解析日誌行 message → 各欄位              │
│ 2. Date Processor：將 "2026-08-23" 轉為標準 @timestamp       │
│ 3. User Agent Processor：解析瀏覽器/裝置型號                │
└─────────────────────────────────────────────────────────────┘
       ↓
落盤寫入索引
```

## 失敗（Failed Count）常見原因

1. **日誌格式非預期（Grok Pattern 不符）**：
   - 應用程式偶爾輸出帶有 Exception Stack Trace 的多行日誌，無法被單行的 Grok pattern 匹配。
2. **型態轉換失敗（Type Casting Error）**：
   - 某個欄位預期是整數，但來源資料送入字串（如 `"status": "null"` 或 `"timeout"`），Convert processor 拋出型態轉換異常。
3. **缺少 `on_failure` 容錯設計**：
   - Pipeline 預設在遇到任何單一處理器錯誤時，會直接中斷並拒絕整筆 Document 的寫入（返回 400 Bad Request），導致日誌丟失。

# 報告指標解讀指引

在 `check.html` 報告中，請檢視各 Pipeline 執行指標：

| 欄位名稱 | 範例值 | 診斷意涵 |
|---|---|---|
| `pipeline_name` | `nginx-access-pipeline` | 發生錯誤的具體 Pipeline 名稱 |
| `count` | `1,000,000` | 該 Pipeline 總處理文件數 |
| `failed_count` | `125,000` | 處理失敗的文件總數 |
| `failed_pct` | `12.5%` | 失敗率（> 10% 觸發警告） |

# 客戶溝通話術與情境模擬

## 溝通策略原則

- **強調資料保全**：說明解析失敗不應該直接把日誌丟掉，應該先進死信隊列或標記 `_grokparsefailure` 保存下來。
- **推動容錯管線**：建議在 Pipeline 結尾配置 `on_failure` 降級處理。

## 話術範例：日誌解析失敗導致寫入中斷

> **顧問說明範例**：
> 「開發團隊您好，報告中顯示 `nginx-access-pipeline` 的處理失敗率達到了 12.5%，有十多萬筆日誌在寫入時被拒絕。
> 
> 經排查，是因為部分 Nginx 存取日誌包含了未帶引號的自訂 Header，導致 Grok 處理器在進行正則匹配時發生錯誤。
> 
> 由於目前 Pipeline 尚未配置失敗容錯（`on_failure`），導致這些解析失敗的日誌直接被拋棄。
> 
> 我們建議在 Pipeline 中加上 `on_failure` 機制：即使特定欄位解析失敗，系統仍會先將整筆原始日誌完整寫入，並標記 `error.message`，確保資料 100% 零遺失，後續也便於工程師追蹤除錯。」

# 現場排查與安全處置 SOP

## 階段一：現場唯讀佐證（定位哪個 Processor 失敗）

查詢各節點各 Pipeline 統計：

```http
GET /_nodes/stats/ingest?filter_path=nodes.*.ingest.pipelines
```

## 階段二：使用 Simulate API 重現錯誤

使用 `_simulate` API 傳入真實日誌樣本進行除錯：

```http
POST /_ingest/pipeline/<PIPELINE_NAME>/_simulate
{
  "docs": [
    {
      "_source": {
        "message": "192.168.1.1 - - [23/Aug/2026:17:00:00 +0800] \"INVALID REQUEST\" 400"
      }
    }
  ]
}
```

## 階段三：整改修復方案（配置 on_failure 容錯）

在 Pipeline 定義中為易出錯的處理器加上 `ignore_failure: true`，或在整條 Pipeline 加上 `on_failure`：

```json
PUT /_ingest/pipeline/nginx-access-pipeline
{
  "processors": [
    {
      "grok": {
        "field": "message",
        "patterns": ["%{COMBINEDAPACHELOG}"],
        "ignore_missing": true
      }
    }
  ],
  "on_failure": [
    {
      "set": {
        "field": "error.message",
        "value": "{{ _ingest.on_failure_message }}"
      }
    },
    {
      "set": {
        "field": "tags",
        "value": ["_grokparsefailure"]
      }
    }
  ]
}
```

## 階段四：變更後驗證

- [ ] 執行 `GET /_nodes/stats/ingest` 確認失敗率不再攀升
- [ ] 重新執行 `elk-diagnostics check` 確認 `ingest_pipeline_errors` 轉為 `PASS`

# 附錄 <!-- appendix -->

## 參考文件

- [Elasticsearch 官方文件：Ingest pipelines](https://www.elastic.co/docs/reference/elasticsearch/ingest/ingest-pipelines)
- 專案內部規格書：`docs/內部/規格/資料規格.md`（#13）
