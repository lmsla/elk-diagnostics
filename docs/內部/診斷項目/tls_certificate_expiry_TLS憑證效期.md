---
title: "ELK 診斷手札：TLS 憑證過期時間 — 傳輸層/HTTP 憑證效期檢測與線上熱重載 SOP"
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
| 2026/08/23 | 1.0 | 初版：TLS 憑證到期預警（30d/7d 門檻）、Transport 斷線風險與熱重載 API | 診斷工具架構組 |

# 文件說明

## 報告診斷卡對照

| 屬性 | 內容 |
|---|---|
| 報告分類區塊 | 安全（Security） / 靜態健檢 |
| 診斷卡中文名稱 | TLS 憑證過期時間 |
| 診斷卡 ID | `tls_certificate_expiry` |
| 典型嚴重度 | `WARNING`（剩餘 < 30 天）/ `CRITICAL`（剩餘 < 7 天或已過期） |
| 觸發關鍵特徵 | `_ssl/certificates` 顯示 HTTP 或 Transport 通訊憑證即將過期 |

## 文件目的

本手札提供第一線工程師在 TLS 憑證即將過期時的預警判定、技術說明指引與零停機線上更新憑證（SSL Reload）指令。

## 適用範圍

本手札適用於啟用 TLS/SSL 加密之 Elasticsearch 7.x 及 8.x 叢集。

# 核心概念與過期危害

```text
憑證剩餘天數判定：
┌─────────────────────────────────────────────────────────────┐
│ 1. > 30 天：PASS。安全區間。                                │
│ 2. 7 ～ 30 天：WARNING。需排定更新窗口更換憑證。            │
│ 3. < 7 天或已過期：CRITICAL！                                │
│    ▲ Transport 憑證過期將導致節點間 TCP 通訊瞬間中斷全叢集癱瘓！│
│    ▲ HTTP 憑證過期將導致 Kibana 與客戶端無法連線！             │
└─────────────────────────────────────────────────────────────┘
```

# 業務影響與技術說明建議

> **技術說明範例**：
> 「資安與維運團隊您好，報告中顯示節點的 TLS 通訊憑證將於 15 天後到期（觸發預警）。
> 
> 若憑證過期，節點之間的內部通訊與客戶端連線將會被安全機制直接拒絕，引發服務全面中斷。
> 
> Elasticsearch 支援**線上零停機熱重載（SSL Reload）**。我們只需將新憑證檔案覆蓋至原路徑，並發送一行重載指令，即可在不重啟任何伺服器的情況下完成憑證無縫換發。」

# 現場 1 分鐘快速佐證與處置 SOP

```http
# 1. 快速查看所有節點生效中憑證的到期日與格式
GET /_ssl/certificates

# 2. 替換檔案後，執行零停機 SSL 熱重載（無需重啟節點！）
POST /_nodes/reload_ssl_certificates
```

# 附錄 <!-- appendix -->

- [Elasticsearch 官方文件：SSL certificate reload API](https://www.elastic.co/docs/reference/elasticsearch/rest-apis/reload-ssl-certificates-api)
