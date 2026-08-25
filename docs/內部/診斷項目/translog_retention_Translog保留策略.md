---
title: "Translog 保留策略 — 交易日誌磁碟佔用、Peer Recovery 加速與廢棄設定清查"
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
| 2026/08/23 | 1.0 | 初版：Translog 運作原理、Flush 門檻、7.4+ Soft Deletes 取代與殘留配置排查 | 診斷工具架構組 |

# 文件說明

## 報告診斷卡對照

| 屬性 | 內容 |
|---|---|
| 報告分類區塊 | 資料（Data） / 靜態健檢 |
| 診斷卡中文名稱 | Translog 保留策略 |
| 診斷卡 ID | `translog_retention` |
| 典型嚴重度 | `INFO` / `WARNING` |
| 觸發關鍵特徵 | 索引設定中存在已廢棄的 `index.translog.retention.*` 參數，或 Translog Flush 門檻不合理 |

## 文件目的

本手札用於協助第一線 ELK 維運工程師與顧問在巡檢報告發現 Translog 策略相關警告時，向客戶解釋 Translog 與 Soft Deletes（軟刪除）的架構演進，並指導清除廢棄參數以避免升級衝突。

## 適用範圍

本手札適用於 Elasticsearch 7.x 及 8.x 全系列叢集。

# 核心原理與 Translog 機制

## 什麼是 Translog？

Elasticsearch 在寫入文件時，資料會先進入記憶體緩衝區（Memory Buffer），同時追加寫入磁碟上的 **Translog（交易事務日誌）**，確保在節點突發當機時，尚未 Flush 落盤的資料能透過 Translog 完整重放復原（Durability）。

```text
客戶端寫入
   ↓
┌───────────────────────┬───────────────────────┐
│ Memory Index Buffer   │ Translog (硬碟日誌檔)  │
└───────────────────────┴───────────────────────┘
   ↓ Refresh (1s)           ↓ Flush (512MB / 30m)
Lucene In-memory Segment   Lucene Segment 落盤 (fsync)
```

## 7.4+ 版本的架構演進：Soft Deletes 取代 Translog Retention

- **舊版本（ES 6.x / 早期 7.x）**：使用 `index.translog.retention.size` 與 `age` 保留歷史日誌，用於節點斷線重連時的快速同步（Peer Recovery）。
- **現代版本（ES 7.4+ / 8.x）**：官方全面採用了更高效的 **Soft Deletes（軟刪除機制）** 取代了 Translog 保留，舊版的 `index.translog.retention.*` 參數已被正式宣告廢棄。

# 報告指標解讀指引

在 `check.html` 報告中，請檢視以下資訊：

| 欄位名稱 | 檢驗要點 | 診斷意涵 |
|---|---|---|
| `deprecated_retention_settings` | 存在已廢棄 key | 建議清除以避免升級阻礙 |
| `flush_threshold_size` | 預設 512MB | Flush 觸發門檻是否合理 |

# 業務影響與技術說明建議

## 說明範例：清除舊版 Translog 殘留參數

> **技術說明範例**：
> 「維運同仁您好，報告中顯示部分舊索引設定中依然保留了 `index.translog.retention.size` 等早期參數。
> 
> 在 Elasticsearch 7.4 之後，系統已經全面升級為更先進的 Soft Deletes 機制進行節點快速同步，舊版的 Translog 保留設定已經被官方廢棄且不再生效。
> 
> 雖然目前不影響業務運行，但為了避免未來升級至 8.x/9.x 時發生語法衝突，我們建議在下一次模板更新時將這些廢棄參數移除。」

# 現場排查與安全處置 SOP

## 階段一：現場唯讀佐證

```http
GET /*/_settings?filter_path=*.settings.index.translog
```

## 階段二：整改修復方案（清除廢棄參數）

```http
PUT /<TARGET_INDEX>/_settings
{
  "index.translog.retention.size": null,
  "index.translog.retention.age": null
}
```

## 階段三：變更後驗證

- [ ] 執行 `GET /<TARGET_INDEX>/_settings` 確認廢棄 key 已清除
- [ ] 重新執行 `elk-diagnostics check` 確認 `translog_retention` 轉為 `PASS`

# 附錄 <!-- appendix -->

## 參考文件

- [Elasticsearch 官方文件：Translog settings](https://www.elastic.co/docs/reference/elasticsearch/configuration-reference/index-translog-settings)
- 專案內部規格書：`docs/內部/規格/靜態健檢規格.md`（ES-GAP-07）
