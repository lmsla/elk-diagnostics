---
title: "Kibana 服務狀態 — 狀態碼判定、ES 連線檢驗與服務恢復"
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
| 2026/08/23 | 1.0 | 初版：Kibana `/api/status` 狀態解讀、與 ES 連線障礙排查與恢復指令 | 診斷工具架構組 |

# 文件說明

## 報告診斷卡對照

| 屬性 | 內容 |
|---|---|
| 報告分類區塊 | 服務：Kibana（Service: Kibana） |
| 診斷卡中文名稱 | Kibana 服務狀態 |
| 診斷卡 ID | `kibana_status` |
| 典型嚴重度 | `WARNING`（Degraded）/ `CRITICAL`（Unavailable） |
| 觸發關鍵特徵 | Kibana 核心狀態非 `available`（如 `degraded` 或 `unavailable`） |

## 文件目的

本手札提供第一線工程師在 Kibana 服務異常時的判定原則、技術說明指引與快速排查指令。

## 適用範圍

本手札適用於所有版本之 Kibana 7.x 及 8.x 實例。

# 核心概念與 Kibana 狀態

```text
Kibana 服務狀態碼：
1. available（綠色）：所有核心外掛與 ES 連線正常。
2. degraded（黃色）：部分非核心外掛異常，但基礎 UI 仍可存取。
3. unavailable（紅色）：無法連線至 Elasticsearch，UI 完全無法開啟！
```

# 業務影響與技術說明建議

> **技術說明範例**：
> 「維運主管您好，報告中顯示 Kibana 服務目前為 Unavailable 狀態。
> 
> 這通常是因為 Kibana 與 Elasticsearch 之間的帳號密碼過期、TLS 憑證不信任、或網路中斷。
> 
> 我們正立即檢查 Kibana 的連線設定檔，預計幾分鐘內即可恢復視覺化介面存取。」

# 現場 1 分鐘快速佐證與處置 SOP

```http
# 1. 快速查看 Kibana 整體健康與各外掛狀態
GET http://<KIBANA_HOST>:5601/api/status
```

檢查日誌：
```bash
journalctl -u kibana -n 50 --no-pager
```

# 附錄 <!-- appendix -->

- [Kibana 官方文件：Kibana status API](https://www.elastic.co/docs/reference/kibana/rest-apis/kibana-status-api)
