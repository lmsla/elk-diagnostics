---
title: "ELK 診斷手札：Stack Monitoring 採集狀態 — 原生監控開關、生產集與監控集架構"
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
| 2026/08/23 | 1.0 | 初版：`xpack.monitoring.collection.enabled` 狀態檢測、獨立監控叢集最佳實踐 | 診斷工具架構組 |

# 文件說明

## 報告診斷卡對照

| 屬性 | 內容 |
|---|---|
| 報告分類區塊 | 管理（Management） |
| 診斷卡中文名稱 | Stack Monitoring 採集狀態 |
| 診斷卡 ID | `monitoring_collection` |
| 典型嚴重度 | `INFO` / `WARNING` |
| 觸發關鍵特徵 | `xpack.monitoring.collection.enabled` 為 `false`（監控數據未採集，Kibana Stack Monitoring 呈現空白） |

## 文件目的

本手札提供第一線工程師在 Stack Monitoring 監控功能未啟用時的判定原則、技術說明指引與動態啟用指令。

## 適用範圍

本手札適用於所有版本之 Elasticsearch 7.x 及 8.x 叢集。

# 核心概念與 Monitoring 架構

```text
Stack Monitoring 數據流：
xpack.monitoring.collection.enabled: true
       ↓
各節點定時將 CPU、Heap、Indexing 指標寫入 .monitoring-es-* 索引
       ↓
供 Kibana Stack Monitoring 介面繪製時間序列折線圖！
```

# 業務影響與技術說明建議

> **技術說明範例**：
> 「維運主管您好，報告中顯示叢集的 `Stack Monitoring` 歷史指標採集功能目前未開啟。
> 
> 這會使 Kibana 上的叢集歷史效能監控圖表無法顯示。
> 
> 我們只需執行一行線上動態設定指令將採集開關打開，Kibana 即可立即呈現整座叢集的 CPU、記憶體與分片歷史趨勢圖。」

# 現場 1 分鐘快速佐證與處置 SOP

```http
# 1. 快速查看監控採集開關設定
GET /_cluster/settings?include_defaults=true&filter_path=*.*monitoring*

# 2. 線上動態啟用監控採集（秒級生效）
PUT /_cluster/settings
{
  "persistent": {
    "xpack.monitoring.collection.enabled": true
  }
}
```

# 附錄 <!-- appendix -->

- [Elasticsearch 官方文件：Stack Monitoring settings](https://www.elastic.co/docs/reference/elasticsearch/configuration-reference/monitoring-settings)
