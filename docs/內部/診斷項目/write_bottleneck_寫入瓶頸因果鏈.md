---
title: "寫入瓶頸（因果鏈）— 容器 CPU 配額誤判排查"
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
| 2026/08/23 | 1.0 | 初版：寫入瓶頸因果鏈、容器 Cgroup 機制與技術溝通建議 | 診斷工具架構組 |

# 文件說明

## 報告診斷卡對照

| 屬性 | 內容 |
|---|---|
| 報告分類區塊 | 專屬症狀排查（`diagnose --symptom write-bottleneck`） |
| 診斷卡中文名稱 | 寫入瓶頸（因果鏈） |
| 診斷卡 ID | `write_bottleneck` |
| 典型嚴重度 | `WARNING`（疑似積壓）/ `CRITICAL`（三條件確認命中） |
| 觸發關鍵特徵 | CPU < 50% 且 write queue >= 1 且 allocated_processors <= 2 |

## 文件目的

本手札用於協助第一線 ELK 維運工程師與顧問在執行 `elk-diagnostics diagnose --symptom write-bottleneck` 症狀排查時，深入理解並處置寫入瓶頸因果鏈，排查客戶端寫入拋出 `429 Too Many Requests` 但主機 CPU 偏低（如 20%～40%）之反常現象。

## 適用範圍

本手札適用於容器化（Kubernetes、OpenShift、Docker）與虛擬化平台之 Elasticsearch 8.x 與 9.x 叢集。

# 核心原理與因果鏈解析

## 為什麼 CPU 很低卻會發生寫入阻塞？

在傳統認知中，寫入佇列積壓通常伴隨 CPU 100% 滿載。然而在容器化或微服務環境中，最常見的「隱形殺手」是 **Elasticsearch 執行緒池（Thread Pool）尺寸與容器配額（Cgroup Quota）的落差**。

Elasticsearch 的寫入執行緒池大小（`write thread pool size`）預設由以下公式決定：

```text
write thread pool size = allocated_processors + 1
```

在啟動時，Elasticsearch 會透過 JVM 偵測環境的可用處理器核心數（`allocated_processors`）。

## 因果鏈三要素判定

本工具的 `write_bottleneck` 診斷卡採用嚴格的「因果鏈三條件」共同判定：

```text
〔條件 1：CPU 偏低〕
節點 CPU 使用率 < 50%
       ↓
〔條件 2：Write Queue 積壓〕
write thread pool queue >= 1
       ↓
〔條件 3：Allocated Processors 偏低〕
allocated_processors <= 2 核心
       ↓
【判定結論】：已確認（Confirmed）容器 CPU 配額誤判引發之寫入瓶頸
```

1. **若三者皆成立**：標記為 `CRITICAL / Confirmed`，直接命中容器配額問題。
2. **若僅有 Queue 積壓但 CPU 偏高**：標記為 `WARNING / Suspected`，通常為真正的算力不足、昂貴寫入（如過多 Pipeline 處理）或 Shard 熱點（Hot spotting）。
3. **無 Queue 積壓**：標記為 `PASS`，此因果鏈無法解釋寫入問題。

## 容器環境常見配置陷阱

在 Kubernetes 部署 Elasticsearch 時，常見以下配置失誤：

1. **未設定 `resources.limits.cpu` 或設定過低**：
   - 若僅配置 `requests.cpu: 1000m` 而未適當設定 limits，或限制為 1～2 核，ES 會將 `allocated_processors` 鎖定為 1 或 2，導致 write 執行緒池只有 2～3 個 worker。
2. **節點實際上運行在高規格實體機（如 64 核）**：
   - 儘管實體伺服器擁有強大運算能力，但容器內的 ES 每次只能由 2 個執行緒處理寫入，其餘 62 核完全無法被利用。

# 報告指標解讀指引

在診斷報告中，請重點觀察以下觀測值：

| 指標名稱 | 單位 | 正常期望值 | 警戒臨界值 | 診斷意涵 |
|---|---|---|---|---|
| `write_bottleneck.cpu` | % | 負載相稱 | < 50%（搭配積壓） | 算力未被充分調用 |
| `write_bottleneck.queue` | count | 0 | >= 1 | 寫入車道發生回堵 |
| `write_bottleneck.allocated_processors` | count | 與硬體相符 | <= 2 | 核心數偵測過低 |
| `write_bottleneck.pool_size` | count | `processors + 1` | <= 3 | 寫入併發通道過窄 |

# 業務影響與技術說明建議

## 說明要點與原則

- **避免使用過度晦澀的底層代碼術語**：不要對非技術主管講「CFS Bandwidth quota 導致 runtime availableProcessors 被截斷」。
- **採用交通車道比喻**：將 write thread pool 比喻為「收費站的收費車道」，將 CPU 比喻為「工作人員的速度」。

## 說明範例：對客戶 IT 主管 / 維運窗口

> **技術說明範例**：
> 「主管您好，我們在健檢報告中發現，叢集雖然硬體規格充足，但寫入經常卡住且出現 429 報警。
> 
> 這好比我們的主機明明有 8 線道的馬路空間，但容器配置將 Elasticsearch 限制在只有 2 個收費收費亭（執行緒）工作。當瞬間車流湧入時，收費亭立刻塞滿排隊（Queue 積壓），但整座主機的 CPU 大部分都還閒置著。
> 
> 我們不需要花預算加買伺服器，只需要調整 Kubernetes 的容器 CPU 配額（limits）或在設定檔中明確宣告處理器數量，將 8 個車道完全打開，寫入效能即可立即獲得數倍提升。」

# 現場排查與安全處置 SOP

## 階段一：現場唯讀佐證

在動手變更設定前，請依序執行以下唯讀指令確認現況：

1. 檢查各節點目前偵測到的處理器核心數：

```http
GET /_nodes/os?filter_path=nodes.*.name,nodes.*.os.allocated_processors,nodes.*.os.available_processors
```

2. 檢查各節點 write 執行緒池狀態：

```http
GET /_cat/thread_pool/write?v&h=node_name,name,active,queue,rejected,completed,size
```

3. 檢查 Hot Threads 確認寫入執行緒是否 100% 飽和：

```http
GET /_nodes/hot_threads?threads=5&type=cpu
```

## 階段二：整改修復方案

依據客戶架構選擇以下修復路徑之一：

### 方案 A：修正 Kubernetes 容器資源配額（推薦）

檢查 StatefulSet 或 ECK Elasticsearch CRD 中的 `resources` 區塊：

```yaml
resources:
  requests:
    cpu: "4"
    memory: "8Gi"
  limits:
    cpu: "8"      # 確保 limits 充足，讓 ES 偵測到對應核心數
    memory: "8Gi"
```

### 方案 B：透過 elasticsearch.yml 顯式宣告核心數

若因環境限制無法調整 Cgroup quota，可直接在 `elasticsearch.yml` 或環境變數強制指定核心數：

```yaml
# elasticsearch.yml
node.processors: 8
```

或透過環境變數注入：

```bash
NODE_PROCESSORS=8
```

## 階段三：變更後驗證

變更套用並滾動重啟後，執行以下檢查清單：

- [ ] 執行 `GET /_nodes/os` 確認 `allocated_processors` 已更新為目標數值
- [ ] 執行 `GET /_cat/thread_pool/write` 確認 `size` 增加為 `allocated_processors + 1`
- [ ] 重新執行 `elk-diagnostics diagnose --symptom write-bottleneck` 確認狀態轉為 `PASS`
- [ ] 觀察業務端 429 寫入拒絕錯誤是否完全消失

# 常見誤區與風險提示

- [ ] **誤區一：盲目加大 Queue 大小**：
  修改 `thread_pool.write.queue_size`（預設 10000）只會將積壓藏在記憶體中，增加 OOM 風險，無法解決根本吞吐瓶頸。
- [ ] **誤區二：以為加記憶體就能解決**：
  寫入執行緒池瓶頸與 JVM Heap 大小無直接關係，單純放大 Heap 無法提升 CPU 併發處理能力。
- [ ] **誤區三：將 `node.processors` 設得超過實體硬體**：
  過度超額配置會引發嚴重的 CPU 上下文切換（Context Switch）開銷，建議配置不超過主機實體可用核數。

# 附錄 <!-- appendix -->

## 參考文件

- [Elasticsearch 官方文件：Thread Pool Settings](https://www.elastic.co/docs/reference/elasticsearch/configuration-reference/thread-pool-settings)
- [Elasticsearch 官方文件：Processors Settings](https://www.elastic.co/docs/reference/elasticsearch/configuration-reference/node-settings#node-processors)
- 專案內部規格書：`docs/內部/規格/寫入瓶頸規格.md`（#16）
