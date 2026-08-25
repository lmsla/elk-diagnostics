---
title: "Node API 回應完整度 — 部分節點回應防禦、網路分區與假死排查"
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
| 2026/08/23 | 1.0 | 初版：Nodes API Partial Response 防禦、網路分區、GC 假死與 Unknown 狀態判讀 | 診斷工具架構組 |

# 文件說明

## 報告診斷卡對照

| 屬性 | 內容 |
|---|---|
| 報告分類區塊 | 節點環境（Node Context） |
| 診斷卡中文名稱 | Node API 回應完整度 |
| 診斷卡 ID | `node_api_coverage` |
| 典型嚴重度 | `UNKNOWN` / `WARNING` |
| 觸發關鍵特徵 | `_nodes/stats` 或 `_nodes` API 回應中，`_nodes.successful < _nodes.total`（部分節點逾時或未回應） |

## 文件目的

本手札用於協助第一線 ELK 維運工程師與顧問在巡檢報告發現 Node API 回應不完整時，理解工具的防禦性設計（嚴禁用局部正常推論全體正常），並排查未回應節點背後的網路分區（Network Partition）、長時間 GC 假死或行程 Crash。

## 適用範圍

本手札適用於所有多節點 Elasticsearch 8.x 及 9.x 叢集。

# 核心原理與防禦性診斷哲學

## 什麼是 Partial Nodes API Response？

在 Elasticsearch 中，許多叢集監控 API（如 `/_nodes/stats`）採用「散彈式廣播（Scatter-Gather）」模式：

```text
客戶端請求 GET /_nodes/stats
       ↓
協調節點向所有 10 台節點發送資料收集請求
       ↓
┌─────────────────────────────────────────────────────────────┐
│ 9 台節點正常回傳資料 (Successful: 9)                         │
│ 1 台節點逾時無回應 (Failed: 1)                              │
└─────────────────────────────────────────────────────────────┘
       ↓
【ES 預設行為】：不會拋錯，而是返回 HTTP 200，並在 JSON 標記：
  "_nodes": { "total": 10, "successful": 9, "failed": 1 }
```

## 為什麼不能以偏概全（The Partial Response Danger）？

若自動化監控工具只看回傳的 9 台正常節點，很容易得出「各節點 CPU 30%、Heap 50%，叢集非常健康（PASS）」的荒謬結論！

**然而，那台唯一沒回應的節點，往往正是當下發生 100% CPU 暴衝、發生 60 秒 Full GC 假死、甚至已經被 OOM-Killer 殺死的罪魁禍首！**

因此，`elk-diagnostics` 貫徹嚴格的防禦性原則：**只要 `failed > 0`，所有依賴該 API 的衍生檢查一律標記為 `UNKNOWN`**，絕不掩蓋災情。

# 報告指標解讀指引

在 `check.html` 報告中，請檢視以下資訊：

| 欄位名稱 | 範例值 | 診斷意涵 |
|---|---|---|
| `nodes_total` | `10` | 叢集預期節點總數 |
| `nodes_successful` | `9` | 成功回傳資料的節點數 |
| `nodes_failed` | `1` | 逾時或失敗的節點數（異常！） |
| `unresponsive_nodes` | `["es-data-07"]` | 未能及時回應的節點名單 |

# 業務影響與技術說明建議

## 說明要點與原則

- **強調「報表嚴謹度」**：向客戶說明我們不會因為有局部數據就隨便給 Pass，確保安全無死角。
- **直擊失聯節點**：引導維運團隊立即登入該台失聯主機排查。

## 說明範例：部分節點未回應導致報告出現 UNKNOWN

> **技術說明範例**：
> 「維運主管您好，報告中有多個節點檢查呈現 `UNKNOWN`（未知）狀態，並非工具故障，而是觸發了嚴格的『防以偏概全』保護機制。
> 
> 經檢測，叢集共有 10 台伺服器，其中 9 台回應正常，但 `es-data-07` 在 API 查詢時超時未回應。
> 
> 很多監控工具會直接忽略這台機器並顯示全部正常，但實務上這台沒回應的節點往往正面臨嚴重的 GC 假死或網路中斷。
> 
> 我們建議立即排查 `es-data-07` 的系統日誌（`elasticsearch.log`）與主機負載，恢復連線後整份報告即可完整判定。」

# 現場排查與安全處置 SOP

## 階段一：現場唯讀佐證（定位誰沒回應）

手動執行 API 並查看 `_nodes.failed`：

```http
GET /_nodes/stats?timeout=5s&filter_path=_nodes,nodes.*.name
```

## 階段二：排查失聯節點本機狀態

登入未回應的伺服器：

1. 檢查行程是否存活：
   ```bash
   ps aux | grep elasticsearch
   ```
2. 檢查 JVM 垃圾回收停頓日誌：
   ```bash
   grep -i "gc" /var/log/elasticsearch/<cluster-name>.log | tail -n 20
   ```
3. 檢查節點間網路連線（Ping / Telnet 9300 埠）。

## 階段三：變更後驗證

- [ ] 執行 `GET /_nodes/stats` 確認 `_nodes.failed` 數值為 `0`
- [ ] 重新執行 `elk-diagnostics check` 確認 `node_api_coverage` 轉為 `PASS`

# 附錄 <!-- appendix -->

## 參考文件

- [Elasticsearch 官方文件：Cluster Nodes Stats API](https://www.elastic.co/docs/reference/elasticsearch/rest-apis/cluster-nodes-stats)
- 專案內部規格書：`docs/內部/規格/韌性規格.md`
