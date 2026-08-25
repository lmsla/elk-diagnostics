---
title: "Kibana 實例負載 — Node.js Event Loop 延遲、Heap 佔用與連線數監控"
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
| 2026/08/23 | 1.0 | 初版：Kibana `/api/stats` 負載指標解讀、Node.js 記憶體與 Event Loop 延遲 | 診斷工具架構組 |

# 文件說明

## 報告診斷卡對照

| 屬性 | 內容 |
|---|---|
| 報告分類區塊 | 服務：Kibana（Service: Kibana） |
| 診斷卡中文名稱 | Kibana 實例負載統計 |
| 診斷卡 ID | `kibana_stats` |
| 典型嚴重度 | `WARNING`（Heap > 85% 或 Event Loop Delay > 100ms） |
| 觸發關鍵特徵 | Kibana 本機 Node.js 記憶體偏高或事件循環發生延遲卡頓 |

## 文件目的

本手札提供第一線工程師在 Kibana 介面反應遲緩時，評估其 Node.js 執行期負載、客戶端連線數與資源擴展指引。

## 適用範圍

本手札適用於所有版本之 Kibana 7.x 及 8.x 實例。

# 核心概念與負載指標

```text
Kibana 關鍵負載指標：
1. process.memory.heap.total_in_bytes / used_in_bytes：Node.js V8 引擎記憶體使用量。
2. os.load / event_loop_delay：Node.js 單執行緒事件循環延遲（>100ms 代表前端反應變慢）。
3. concurrent_connections：當前在線瀏覽的使用者連線數。
```

# 業務影響與技術說明建議

> **技術說明範例**：
> 「維運同仁您好，報告中顯示 Kibana 的事件循環延遲（Event Loop Delay）達到 150ms，且 Node.js 記憶體佔用偏高。
> 
> 這會使儀表板載入與搜尋點擊出現明顯的頓挫感。
> 
> 我們建議：可適度加大 Kibana 的 `max-old-space-size` 記憶體上限，或在前端掛載負載均衡器水平擴展第二台 Kibana，即可讓操作恢復流暢。」

# 現場 1 分鐘快速佐證與處置 SOP

```http
# 1. 快速查看 Kibana 實例負載統計
GET http://<KIBANA_HOST>:5601/api/stats?extended=true
```

# 附錄 <!-- appendix -->

- [Kibana 官方文件：Kibana stats API](https://www.elastic.co/docs/reference/kibana/rest-apis/kibana-stats-api)
