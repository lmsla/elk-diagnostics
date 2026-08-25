---
title: "Kibana Alerting 健康 — 告警執行失敗、連接器異常與限流防護"
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
| 2026/08/23 | 1.0 | 初版：Kibana Alerting & Actions 框架、Connector 執行失敗與告警風暴防護 | 診斷工具架構組 |

# 文件說明

## 報告診斷卡對照

| 屬性 | 內容 |
|---|---|
| 報告分類區塊 | 服務：Kibana（Service: Kibana） |
| 診斷卡中文名稱 | Kibana Alerting 框架健康 |
| 診斷卡 ID | `kibana_alerting` |
| 典型嚴重度 | `WARNING` / `CRITICAL` |
| 觸發關鍵特徵 | Kibana Alerting 規則執行失敗率偏高、第三方通知連接器（Email/Slack/Webhook）認證過期或逾時 |

## 文件目的

本手札用於協助第一線 ELK 維運工程師與顧問在巡檢報告發現 Kibana 告警框架異常時，排查告警規則（Rules）語法錯誤與連接器（Connectors）連線失敗，確保維運監控不漏報。

## 適用範圍

本手札適用於啟用 Alerting 功能之 Kibana 8.x 及 9.x 實例。

# 核心原理與 Alerting 運作機制

## 什麼是 Kibana Alerting & Actions 框架？

Kibana Alerting 是企業級的統一警報引擎，包含兩個核心概念：
1. **Rule（告警規則）**：定義「監控什麼條件」（例如：過去 5 分鐘內 500 錯誤日誌 > 100 筆）。
2. **Action / Connector（動作與連接器）**：定義「滿足條件時通知誰」（例如發送 Webhook 至 Teams / Slack，或發送 Email）。

```text
定時觸發 Rule
       ↓ 執行 Elasticsearch 查詢
比對條件（是否超標？）
       ↓ 是
【觸發 Action Connector】發送 Webhook / Email
       ↓
┌───────────────────────────────────────────────┐
│ 失敗成因：                                    │
│ 1. 查詢目標索引不存在或權限不足（Rule Error）    │
│ 2. Webhook 認證 Token 過期或網路防火牆阻擋     │
│ 3. 告警風暴：短時間內觸發數萬次通知被目標端限流 │
└───────────────────────────────────────────────┘
```

# 報告指標解讀指引

在 `check.html` 報告中，請檢視以下資訊：

| 欄位名稱 | 警戒門檻 | 診斷意涵 |
|---|---|---|
| `failed_rules_count` | > 0 | 執行失敗的規則總數 |
| `connector_errors_count` | > 0 | 第三方通知發送失敗次數 |

# 業務影響與技術說明建議

## 說明範例：告警規則因索引更名而失效

> **技術說明範例**：
> 「維運同仁您好，報告中顯示 Kibana 有 3 條核心告警規則目前處於執行失敗狀態。
> 
> 經檢測，是因為上週日誌索引從 `app-logs-*` 遷移為 `logs-app-*`，但告警規則中的查詢目標未同步更新，導致規則查詢找不到索引而報錯。
> 
> 這會使系統在發生故障時無法正常發送報警。
> 
> 我們建議在 Kibana Management 中將這 3 條規則的目標索引名稱更新，即可立即恢復自動化告警防護。」

# 現場排查與安全處置 SOP

## 階段一：現場唯讀佐證（定位失敗規則）

在 Kibana Management -> Rules and Connectors 介面，篩選狀態為 `Error` 的規則清單。

## 階段二：整改修復方案

1. **修正 Rule 查詢語句**：確認查詢的 Index Pattern 是否存在。
2. **測試 Action Connector**：點擊 Connector 中的 `Test` 按鈕，驗證 Token 與網路連線是否暢通。
3. **配置告警頻率限制（Throttling）**：避免單一問題引發連續重複通知。

## 階段三：變更後驗證

- [ ] 在 Kibana 中手動執行一次 Rule 測試，確認狀態轉為 `Active / OK`
- [ ] 重新執行 `elk-diagnostics check` 確認 `kibana_alerting` 轉為 `PASS`

# 附錄 <!-- appendix -->

## 參考文件

- [Kibana 官方文件：Alerting and Action settings](https://www.elastic.co/docs/reference/kibana/alerting/alerting-getting-started)
- 專案內部規格書：`docs/內部/規格/擴充健檢規格.md`
