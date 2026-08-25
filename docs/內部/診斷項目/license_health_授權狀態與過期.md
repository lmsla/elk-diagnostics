---
title: "License 授權狀態 — 商業版授權過期預警、降級行為與更新授權"
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
| 2026/08/23 | 1.0 | 初版：License 類型判定（Basic/Platinum/Enterprise）、過期降級影響與更新 | 診斷工具架構組 |

# 文件說明

## 報告診斷卡對照

| 屬性 | 內容 |
|---|---|
| 報告分類區塊 | 授權（License） / 靜態健檢 |
| 診斷卡中文名稱 | License 授權狀態 |
| 診斷卡 ID | `license_health` |
| 典型嚴重度 | `WARNING`（剩餘 < 30 天）/ `CRITICAL`（剩餘 < 7 天或已過期） |
| 觸發關鍵特徵 | `_license` 顯示商業授權（Platinum/Enterprise）即將到期或狀態非 `active` |

## 文件目的

本手札提供第一線工程師在 License 即將過期時的預警判定、技術說明指引與線上更新授權指令。

## 適用範圍

本手札適用於所有版本之 Elasticsearch 7.x 及 8.x 叢集。

# 核心概念與過期降級機制

```text
授權類型：
1. Basic（永久免費）：無到期日，支援核心搜尋、ILM、Data Stream 等。
2. Platinum / Enterprise（付費商業版）：具備到期日，支援 CCR、ML、進階安全等。

▲ 過期後的降級行為：
- 基本讀寫搜尋「不會中斷」；
- 但進階功能（如 CCR 跨叢集複製、ML 異常檢測、報表生成）將被自動禁用！
```

# 業務影響與技術說明建議

> **技術說明範例**：
> 「採購與 IT 主管您好，報告中顯示目前叢集的 Enterprise 商業授權將於 20 天後到期。
> 
> 雖然授權過期後基礎資料讀寫依然正常，但異地複製（CCR）與機器學習告警等進階功能將會被系統暫停。
> 
> 建議儘速向原廠申請續約 License JSON 檔案，我們只需透過 API 匯入新憑證，無需重啟任何伺服器即可完成授權展延。」

# 現場 1 分鐘快速佐證與處置 SOP

```http
# 1. 快速查看當前授權類型、狀態與到期日
GET /_license

# 2. 線上匯入新授權 JSON 檔案
PUT /_license
{
  "license": { ...新授權內容... }
}
```

# 附錄 <!-- appendix -->

- [Elasticsearch 官方文件：License management API](https://www.elastic.co/docs/reference/elasticsearch/rest-apis/licensing-apis)
