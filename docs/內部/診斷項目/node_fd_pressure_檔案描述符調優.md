---
title: "File Descriptors 壓力 — Linux 檔案描述符限制、Lucene 段檔案與 ulimit 調優"
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
| 2026/08/23 | 1.0 | 初版：檔案描述符壓力判定、Lucene 檔案數量與系統 ulimit 調優 SOP | 診斷工具架構組 |

# 文件說明

## 報告診斷卡對照

| 屬性 | 內容 |
|---|---|
| 報告分類區塊 | 節點環境（Node Context） |
| 診斷卡中文名稱 | File Descriptors 壓力 |
| 診斷卡 ID | `node_fd_pressure` |
| 典型嚴重度 | `WARNING`（使用率 > 80%）/ `CRITICAL`（使用率 > 90% 或上限 < 65535） |
| 觸發關鍵特徵 | `_nodes/stats/process` 顯示 `open_file_descriptors` 接近 `max_file_descriptors` |

## 文件目的

本手札用於協助第一線 ELK 維運工程師與顧問在巡檢報告發現檔案描述符（File Descriptors）壓力過高時，向客戶解釋 Lucene 底層大量 Segment 檔案與網路 Socket 的消耗特性，並指導調優 Linux `nofile` 限制。

## 適用範圍

本手札適用於所有運行在 Linux 或容器環境中之 Elasticsearch 8.x 及 9.x 節點。

# 核心原理與 File Descriptors 機制

## 為什麼 Elasticsearch 需要海量 File Descriptors？

在 Linux 系統中，「一切皆檔案」。對 Elasticsearch 而言，以下操作均會佔用 File Descriptor（FD）：
1. **Lucene Segment 檔案**：每個 Shard 包含數十到數百個 Segment，每個 Segment 都由多個底層檔案（`.tim`, `.tip`, `.doc`, `.dvd`, `.cfs`）組成，且 Lucene 會保持檔案開啟狀態以追求讀取效能。
2. **網路 Socket 連線**：每個客戶端 HTTP 連線、Logstash 連線、Kibana 連線、以及節點之間的 Transport 通訊連線（預設每個節點間維護 13 個 TCP 連線）。

```text
單一節點開啟檔案總數 = (分片數 * 每個分片的 Segment 檔案數) + (HTTP 客戶端連線數) + (節點間 Transport 連線數)
```

## 達到上限的後果（Too many open files）

一旦達到上限（`max_file_descriptors`）：
- 節點無法開啟新的 Lucene 檔案，寫入直接崩潰並中斷；
- 節點無法接受新的 TCP 請求，客戶端連線全部 Timeout；
- 嚴重時引發節點假死並脫離叢集。

# 報告指標解讀指引

在 `check.html` 報告中，請檢視以下數值：

| 欄位名稱 | 警戒門檻 | 診斷意涵 | 處置建議 |
|---|---|---|---|
| `max_file_descriptors` | < 65,535 | 系統上限設得太低 | 必須立即調大至 65535+ |
| `open_file_descriptors_pct` | > 80%（Warning）<br>> 90%（Critical） | 檔案描述符即將耗盡 | 需合併分片或調大上限 |

# 業務影響與技術說明建議

## 說明範例：系統預設上限過低或分片過多

> **技術說明範例**：
> 「維運同仁您好，報告中顯示 `es-data-01` 的檔案描述符（File Descriptors）使用率已達到 88%，即將觸發系統硬上限。
> 
> Elasticsearch 的搜尋引擎特性需要同時開啟大量索引檔案與網路連線。一旦檔案描述符耗盡，系統會拋出 `java.io.IOException: Too many open files`，導致所有新查詢與寫入被瞬間拒絕。
> 
> 官方的硬性標準要求：**Linux 的 `nofile` 限制必須至少設為 65,535（建議 131,072）**。我們只需調整 systemd 或 security limits 配置，即可徹底消除此風險。」

# 現場排查與安全處置 SOP

## 階段一：現場唯讀佐證

查詢各節點目前開啟的 FD 數量與上限：

```http
GET /_nodes/stats/process?filter_path=nodes.*.name,nodes.*.process.open_file_descriptors,nodes.*.process.max_file_descriptors
```

## 階段二：整改修復方案

### 步驟 1：修改 systemd 服務設定（永久生效）
編輯 `/etc/systemd/system/elasticsearch.service.d/override.conf`：

```ini
[Service]
LimitNOFILE=131072
```

執行 `sudo systemctl daemon-reload` 並重啟服務。

### 步驟 2：修改 Linux 全域 limits（針對非 systemd 啟動）
編輯 `/etc/security/limits.conf`：

```text
elasticsearch soft nofile 131072
elasticsearch hard nofile 131072
```

## 階段三：變更後驗證

- [ ] 執行 `GET /_nodes/stats/process` 確認 `max_file_descriptors` 顯示為 `131072`
- [ ] 重新執行 `elk-diagnostics check` 確認 `node_fd_pressure` 轉為 `PASS`

# 附錄 <!-- appendix -->

## 參考文件

- [Elasticsearch 官方文件：File Descriptors configuration](https://www.elastic.co/docs/reference/elasticsearch/configuration-reference/advanced-configuration#file-descriptors)
- 專案內部規格書：`docs/內部/規格/節點環境規格.md`
