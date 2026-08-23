---
title: "ELK 診斷手札：Machine Learning 任務健康 — Anomaly Detection、Datafeed 狀態與重啟"
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
| 2026/08/23 | 1.0 | 初版：ML Job 與 Datafeed 狀態檢測、Failed 原因排查與安全重啟 | 診斷工具架構組 |

# 文件說明

## 報告診斷卡對照

| 屬性 | 內容 |
|---|---|
| 報告分類區塊 | 管理（Management） / 擴充健檢 |
| 診斷卡中文名稱 | Machine Learning 任務健康 |
| 診斷卡 ID | `ml_health` |
| 典型嚴重度 | `WARNING` / `CRITICAL` |
| 觸發關鍵特徵 | `_ml/anomaly_detectors/_stats` 顯示特定 ML Job 處於 `failed` 狀態 |

## 文件目的

本手札提供第一線工程師在機器學習異常檢測任務失敗時的判定原則、技術說明指引與重啟指令。

## 適用範圍

本手札適用於啟用 Platinum/Enterprise ML 功能之 Elasticsearch 7.x 及 8.x 叢集。

# 核心概念與 ML 機制

```text
ML 工作流：
Datafeed (拉取索引資料) → ML Anomaly Job (演算法計算)
▲ 若 Datafeed 停止或 Job 記憶體超出 ml.max_model_memory_limit，Job 將轉為 FAILED！
```

# 業務影響與技術說明建議

> **技術說明範例**：
> 「維運主管您好，報告中顯示 `response-time-anomaly` 機器學習任務目前處於 `failed` 停止狀態。
> 
> 經檢視，是因為來源資料量短時間內激增，模型記憶體達到了上限。
> 
> 我們建議適度調大模型的記憶體限制，並重新啟動該任務，異常檢測演算法即可繼續自動分析業務日誌。」

# 現場 1 分鐘快速佐證與處置 SOP

```http
# 1. 查看所有 ML Jobs 狀態與失敗原因
GET /_ml/anomaly_detectors/_stats

# 2. 重新啟動失敗的 ML Job
POST /_ml/anomaly_detectors/<JOB_ID>/_open
POST /_ml/datafeeds/datafeed-<JOB_ID>/_start
```

# 附錄 <!-- appendix -->

- [Elasticsearch 官方文件：Machine learning APIs](https://www.elastic.co/docs/reference/elasticsearch/rest-apis/machine-learning-apis)
