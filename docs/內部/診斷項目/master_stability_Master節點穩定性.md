---
title: "Master 穩定性結構檢查 — 角色分離、選主風暴與 GC 心跳逾時"
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
| 2026/08/23 | 1.0 | 初版：專用 Master 架構原則、選主風暴成因與心跳超時調優 | 診斷工具架構組 |

# 文件說明

## 報告診斷卡對照

| 屬性 | 內容 |
|---|---|
| 報告分類區塊 | 叢集（Cluster） |
| 診斷卡中文名稱 | Master 穩定性結構檢查 |
| 診斷卡 ID | `master_stability_context` |
| 典型嚴重度 | `WARNING` / `CRITICAL` |
| 觸發關鍵特徵 | Master-eligible 節點數量為偶數（如 2 或 4）、未實施 Dedicated Master、或頻繁發生 Master 節點切換 |

## 文件目的

本手札用於協助第一線 ELK 維運工程師與顧問在巡檢報告中評估叢集大腦（Master 節點群）的架構健康度，向客戶說明 Dedicated Master 角色分離原則，並排查頻繁選主風暴。

## 適用範圍

本手札適用於中大型或生產環境之 Elasticsearch 7.x 及 8.x 叢集。

# 核心原理與 Master 架構規範

## 什麼是 Dedicated Master（專用主節點）？

Elasticsearch 的 Master 節點負責管理整座叢集的核心中樞（Cluster State、分片調度、索引創建與刪除、節點加入/離開廣播）。

在中大型叢集中，最常見的架構嚴重失誤是 **「讓 Master 同時兼任 Data 節點（Mixed Node）」**：

```text
【混合節點架構的致命隱患】
┌───────────────────────────────────────────────┐
│ Master + Data 混合節點                        │
│ ┌───────────────────┐ ┌─────────────────────┐ │
│ │ 核心大腦管理      │ │ 繁重查詢 / 聚合寫入 │ │
│ └───────────────────┘ └─────────────────────┘ │
└───────────────────────────────────────────────┘
  ▲ 當客戶端送入大查詢或海量日誌寫入時，JVM 發生長達 10 秒的 Full GC
  ▲ Master 節點無法回應心跳 ping → 叢集誤判 Master 掛掉 → 觸發全體重新選主！
  ▲ 新 Master 接手後又被查詢打垮 → 形成選主死循環（Master Flapping）！
```

## Master 節點配置黃金法則

1. **三台原則（Odd Number Quorum）**：
   - Master-eligible 節點數**必須為奇數（通常為 3 台，極大型叢集為 5 台）**。
   - 偶數台（如 2 台或 4 台）在容錯能力上完全沒有提升，甚至更容易發生 Quorum 不足。
2. **專用分離原則（Dedicated Role）**：
   - 當 Data 節點數超過 5～10 台時，**必須獨立部署 3 台 Dedicated Master 節點**（僅配置 `master` 角色，不承載 `data`、`ingest` 或 `transform`）。
   - 專用 Master 節點只需分配較小記憶體（如 4GB～8GB Heap），但能提供 100% 絕對穩定的控制層。

# 報告指標解讀指引

在 `check.html` 報告中，請檢視以下架構數據：

| 欄位名稱 | 範例值 | 診斷意涵 | 處置建議 |
|---|---|---|---|
| `total_nodes` | `12` | 叢集總節點數 | 評估是否需 Dedicated Master |
| `master_eligible_count` | `2` | 具備 Master 投票資格的節點數 | 若為 2 需立即告警增加至 3 |
| `is_dedicated_master` | `false` | 是否採用專用 Master 節點 | 規模 >10 節點強烈建議分離 |

# 業務影響與技術說明建議

## 說明要點與原則

- **用「大腦與四肢」比喻**：說明 Master 就像公司總指揮，Data 節點就像前線搬運工；總指揮不能下場搬磚，否則累倒時整間公司就陷入混亂。
- **強調穩定性投資回報**：3 台規格較小的 Dedicated Master 成本很低，但能杜絕 90% 的全叢集失聯災難。

## 說明範例：大型叢集未實施 Dedicated Master

> **技術說明範例**：
> 「主管您好，我們在架構檢查中發現，目前叢集已有 10 餘台資料節點，但依然採用混合角色架構（Master 同時負責儲存與查詢）。
> 
> 這好比讓總指揮官親自下場做高強度的體力搬運。一旦業務端有複雜報表查詢或突發大流量寫入，導致伺服器 JVM 發生暫時停頓時，整座叢集就會誤判總指揮失聯，從而引發連鎖的重新選主震盪與服務中斷。
> 
> 我們強烈建議規劃 3 台專用的 Dedicated Master 伺服器（只需 4～8G 記憶體即可）。將大腦與搬運工作徹底分開，叢集將獲得最高水準的抗壓與穩定性。」

# 現場排查與安全處置 SOP

## 階段一：現場唯讀佐證

1. 檢查目前當選的 Master 節點與各節點角色分佈：

```http
GET /_cat/nodes?v&h=ip,name,master,node.role,heap.percent,cpu
```

2. 檢查 Master 節點是否有長時間排隊任務（Pending Tasks）：

```http
GET /_cluster/pending_tasks
```

## 階段二：整改修復方案（規劃 Dedicated Master）

新增 3 台專用節點，並在 `elasticsearch.yml` 中僅保留 `master` 角色：

```yaml
# 3 台 Dedicated Master 節點配置
node.name: es-master-01
node.roles: [ master ]
```

現有的 Data 節點移除 `master` 角色，專注於資料儲存：

```yaml
# 現有 Data 節點配置
node.roles: [ data, ingest ]
```

## 階段三：變更後驗證

- [ ] 執行 `GET /_cat/nodes` 確認 3 台專用 Master 正常加入，且 `master` 欄位標記唯一的主節點 `*`
- [ ] 重新執行 `elk-diagnostics check` 確認 `master_stability_context` 轉為 `PASS`

# 常見誤區與風險提示

- [ ] **誤區一：以為設 2 台 Master 比 1 台好**：
  2 台 Master 節點的 Quorum 是 2，只要掛掉 1 台整座叢集即失去法定人數而停擺，容錯度與 1 台完全相同，且可能引發選主僵局。
- [ ] **誤區二：專用 Master 分配過大 Heap**：
  專用 Master 不儲存資料也不跑 Lucene 查詢，Heap 過大（如 32GB）反而會使 GC 停頓變長，通常 4GB～8GB 即綽綽有餘。

# 附錄 <!-- appendix -->

## 參考文件

- [Elasticsearch 官方文件：Node roles (master)](https://www.elastic.co/docs/reference/elasticsearch/configuration-reference/node-roles#master-node)
- [Elasticsearch 官方文件：Troubleshooting unstable cluster](https://www.elastic.co/docs/troubleshoot/elasticsearch/troubleshooting-unstable-cluster)
- 專案內部規格書：`docs/內部/規格/健康報告規格.md`（#30）
