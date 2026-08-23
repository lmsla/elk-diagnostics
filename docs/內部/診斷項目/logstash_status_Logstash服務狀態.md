---
title: "ELK 診斷手札：Logstash 服務狀態 — 節點在線檢測、執行期版本與 JVM 資源"
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
| 2026/08/23 | 1.0 | 初版：Logstash `_node` API 狀態檢測、JVM 記憶體與執行期版本 | 診斷工具架構組 |

# 文件說明

## 報告診斷卡對照

| 屬性 | 內容 |
|---|---|
| 報告分類區塊 | 服務：Logstash（Service: Logstash） |
| 診斷卡中文名稱 | Logstash 服務狀態 |
| 診斷卡 ID | `logstash_status` |
| 典型嚴重度 | `CRITICAL`（服務未在線或無法連線） |
| 觸發關鍵特徵 | Logstash 監控 API（9600 埠）無法連通或服務未正常啟動 |

## 文件目的

本手札提供第一線工程師在 Logstash 服務失聯時的快速判定原則、客戶說明話術與排查指令。

## 適用範圍

本手札適用於所有版本之 Logstash 7.x 及 8.x 實例。

# 核心概念與 Logstash 狀態

```text
Logstash 服務檢查：
透過 GET http://<LOGSTASH_HOST>:9600/ 取得 Logstash 執行期版本、JVM 狀態與啟動時間。
▲ 若 API 連線拒絕（Connection Refused），代表 Logstash 行程已 Crash 或未啟動！
```

# 客戶溝通公版話術

> **顧問說明範例**：
> 「維運主管您好，報告中顯示 Logstash 採集服務目前處於離線狀態。
> 
> 這會使前端日誌無法正常被解析並寫入 Elasticsearch。
> 
> 我們正立即檢查 Logstash 主機行程日誌，排查是否有語法錯誤或記憶體 OOM，預計很快會重新拉起服務。」

# 現場 1 分鐘快速佐證與處置 SOP

```bash
# 1. 查看 Logstash 行程與服務狀態
systemctl status logstash

# 2. 查看 Logstash 最新日誌輸出
tail -n 50 /var/log/logstash/logstash-plain.log
```

# 附錄 <!-- appendix -->

- [Logstash 官方文件：Monitoring APIs](https://www.elastic.co/docs/reference/logstash/monitoring-apis)
