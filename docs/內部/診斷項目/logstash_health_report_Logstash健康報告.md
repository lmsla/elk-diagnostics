---
title: "Logstash 健康報告 — 綜合健康指標、Queue 狀態與異常匯總"
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
| 2026/08/23 | 1.0 | 初版：Logstash 節點健康報告解讀、整體管線健康與資源瓶頸 | 診斷工具架構組 |

# 文件說明

## 報告診斷卡對照

| 屬性 | 內容 |
|---|---|
| 報告分類區塊 | 服務：Logstash（Service: Logstash） |
| 診斷卡中文名稱 | Logstash 健康報告 |
| 診斷卡 ID | `logstash_health_report` |
| 典型嚴重度 | `WARNING` / `CRITICAL` |
| 觸發關鍵特徵 | Logstash 節點整體健康指標異常、發生持續的事件丟失或 JVM GC 停頓 |

## 文件目的

本手札提供第一線工程師在 Logstash 整體健康出現警告時的判定原則、技術說明指引與排查指令。

## 適用範圍

本手札適用於所有版本之 Logstash 7.x 及 8.x 實例。

# 核心概念與 Logstash 健檢

```text
Logstash 總體健檢核心：
1. jvm.mem.heap_used_percent：JVM 記憶體壓力（>85% 警戒）。
2. jvm.gc.collectors.old.collection_time_in_millis：老年代 GC 累計停頓。
3. process.cpu.percent：CPU 使用率。
```

# 業務影響與技術說明建議

> **技術說明範例**：
> 「維運同仁您好，報告中顯示 Logstash 採集節點的 JVM 記憶體使用率已達到 88%，且 GC 停頓次數增多。
> 
> 這通常是因為管線的 batch size 設得過大，或在 Filter 中快取了過多事件物件。
> 
> 我們建議在 `jvm.options` 中將 Logstash Heap 調大至 4GB～8GB，即可徹底消除 GC 停頓，維持管線平穩吞吐。」

# 現場 1 分鐘快速佐證與處置 SOP

```http
# 1. 快速查看 Logstash JVM 與行程統計
GET http://<LOGSTASH_HOST>:9600/_node/stats/jvm,process
```

# 附錄 <!-- appendix -->

- [Logstash 官方文件：Node stats API](https://www.elastic.co/docs/reference/logstash/node-stats-api)
