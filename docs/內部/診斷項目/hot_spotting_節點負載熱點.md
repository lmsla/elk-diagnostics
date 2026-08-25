---
title: "Hot spotting（資源分布不均）— 單點過熱、Routing Key 偏斜與分片熱點"
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
| 2026/08/23 | 1.0 | 初版：節點資源偏斜判定、單一大型分片過熱、自訂 Routing 偏斜排查 | 診斷工具架構組 |

# 文件說明

## 報告診斷卡對照

| 屬性 | 內容 |
|---|---|
| 報告分類區塊 | 效能（Performance） |
| 診斷卡中文名稱 | Hot spotting（資源分布不均） |
| 診斷卡 ID | `hot_spotting` |
| 典型嚴重度 | `WARNING` / `CRITICAL` |
| 觸發關鍵特徵 | 單一或少數節點之 CPU 或記憶體指標超出同群組中位數（Median）30% 以上（負載嚴重不均） |

## 文件目的

本手札用於協助第一線 ELK 維運工程師與顧問在巡檢報告發現「一節點有難，全叢集圍觀（單點發燙）」時，排查是單一大索引分片過少、自訂 Routing Key 分佈不均、還是 Coordinating 流量傾斜所致。

## 適用範圍

本手札適用於 3 台節點以上之 Elasticsearch 8.x 及 9.x 叢集。

# 核心原理與熱點產生機制

## 什麼是 Hot Spotting（熱點現象）？

在理想的叢集中，所有同角色節點的 CPU 與 I/O 應該大致平均（例如都在 40%～50%）。

但實務上常出現 **「某 1 台節點 CPU 99% 且 I/O 爆表，其他 9 台節點 CPU 卻只有 10%」** 的怪異現象。這代表算力沒有被分散，整座叢集的效能被這台「最熱節點」卡死。

```text
【Hot Spotting 熱點現象】
┌──────────────┐  ┌──────────────┐  ┌──────────────┐
│ Data Node 01 │  │ Data Node 02 │  │ Data Node 03 │
│  CPU: 98% 🔥 │  │  CPU: 12% ❄️ │  │  CPU: 10% ❄️ │
└──────────────┘  └──────────────┘  └──────────────┘
  ▲ 承載了最火熱的單一 Primary Shard，或被特定客戶端專門攻擊！
```

## 三大熱點成因

1. **單一超大活躍索引只有 1 個 Primary Shard**：
   - 某核心業務索引每秒寫入 5 萬筆，但 `number_of_shards` 只設了 1。所有的寫入全部只能由該分片所在的單一節點承載，其餘節點無法分擔。
2. **自訂 Routing Key 嚴重資料偏斜（Data Skew）**：
   - 使用 `routing: tenant_id` 寫入，但某個大客戶（如 VIP 租戶）貢獻了 90% 的資料，導致所有資料全部 hash 到同一個分片與節點。
3. **客戶端連線未做負載均衡（Load Balancing）**：
   - 所有 Logstash / Beats 或應用程式端，設定檔都寫死了 `http://node-01:9200`，使 Node 01 成為唯一承受所有 HTTP/JSON 解析的協調節點。

# 報告指標解讀指引

在 `check.html` 報告中，請檢視節點指標離散度表格：

| 欄位名稱 | 警戒門檻 | 診斷意涵 |
|---|---|---|
| `hotspot_spread_pct` | > 30% | 節點數值與中位數差距過大 |
| `hottest_node` | `es-data-01` | 當前負載最高的發熱節點 |
| `coldest_node` | `es-data-08` | 當前負載最低的閒置節點 |

# 業務影響與技術說明建議

## 說明要點與原則

- **點出「木桶效應」**：說明硬體買得再好，只要有熱點，整個叢集的響應時間就等於那台最慢節點的時間。
- **強調分流效益**：不需要擴容，只要把分片打散或在前方加掛 Load Balancer 就能讓整體吞吐翻倍。

## 說明範例：單一索引主分片過少引發單點過熱

> **技術說明範例**：
> 「架構師您好，我們在報告中發現 `es-data-01` 節點 CPU 達到 95%，但其餘資料節點平均只有 15%。
> 
> 這代表叢集存在嚴重的『單點熱點』問題。經分析，目前最繁忙的 `orders-v2` 索引每秒承載了 80% 的全站寫入量，但該索引只配置了 1 個 Primary 分片。
> 
> 這好比全公司有 10 個員工，但所有的訂單全部只塞給 1 號員工處理，其餘 9 位員工無事可做。
> 
> 我們建議將該核心索引的分片數由 1 調整為 3～5（或在新模板中調整），將寫入壓力均攤到所有伺服器上，熱點即可立刻消退，整體系統延遲能降低 70%。」

# 現場排查與安全處置 SOP

## 階段一：現場唯讀佐證（定位發熱節點上的分片）

1. 找出發熱節點上的主要索引分片清單：

```http
GET /_cat/shards?v&s=node,store:desc&h=node,index,shard,prirep,store,docs
```

2. 檢查各節點的寫入/查詢吞吐量：

```http
GET /_nodes/stats/indices/indexing,search?filter_path=nodes.*.name,nodes.*.indices
```

## 階段二：整改修復方案

### 方案 A：調整活躍索引的分片數（針對寫入熱點）
在 Index Template 中將未來新日期的索引分片數由 1 提高為節點數的約數（例如 3 節點配 3 或 6 Shards）。

### 方案 B：客戶端加掛負載均衡器（針對 Coordinating 熱點）
在 Elasticsearch 前方架設 HAProxy / Nginx，或在客戶端 Client 配置中填入所有節點的 IP，開啟 Round-Robin 輪詢。

## 階段三：變更後驗證

- [ ] 執行 `GET /_cat/nodes?v&h=name,cpu,heap.percent` 確認所有節點 CPU 差距收斂在 15% 以內
- [ ] 重新執行 `elk-diagnostics check` 確認 `hot_spotting` 轉為 `PASS`

# 附錄 <!-- appendix -->

## 參考文件

- [Elasticsearch 官方文件：Troubleshooting hot spotting](https://www.elastic.co/docs/troubleshoot/elasticsearch/troubleshooting-hot-spotting)
- 專案內部規格書：`docs/內部/規格/效能規格.md`（#17）
