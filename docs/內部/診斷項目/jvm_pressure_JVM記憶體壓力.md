---
title: "High JVM memory pressure — Old Gen 晉升、GC 停頓與調優策略"
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
| 2026/08/23 | 1.0 | 初版：JVM 老年代壓力、GC 演算法演進、晉升速率與停頓排查 | 診斷工具架構組 |

# 文件說明

## 報告診斷卡對照

| 屬性 | 內容 |
|---|---|
| 報告分類區塊 | 效能（Performance） |
| 診斷卡中文名稱 | High JVM memory pressure |
| 診斷卡 ID | `jvm_pressure` |
| 典型嚴重度 | `WARNING`（持續 > 85%）/ `CRITICAL`（持續 > 95%） |
| 觸發關鍵特徵 | `_nodes/stats/jvm` 顯示 Old Gen 記憶體池佔用比例居高不下，頻繁觸發 GC |

## 文件目的

本手札用於協助第一線 ELK 維運工程師與顧問在巡檢報告發現 JVM 記憶體壓力過高時，精準診斷老年代（Old Gen）居高不下的根本原因，評估 GC 停頓對叢集心跳與查詢延遲的影響，並給出科學的調優指引。

## 適用範圍

本手札適用於所有執行於 HotSpot JVM 或 OpenJDK 上之 Elasticsearch 8.x 及 9.x 節點。

# 核心原理與 JVM 記憶體體系

## 為什麼 JVM Heap 不能超過 31GB（Compressed OOPs）？

Elasticsearch 官方強烈建議：**JVM Heap 絕對不要配置超過 31GB（通常設為 30GB 或 31GB）**。

其原因在於 **Java 指標壓縮技術（Compressed Ordinary Object Pointers, Compressed OOPs）**：
- 當 Heap ≤ 31GB 時，JVM 使用 32 位元指標指向 64 位元物件，大幅節省 30%～40% 的記憶體開銷。
- 一旦 Heap 超過 32GB，指標自動退化為 64 位元，指標本身就會吃掉大量記憶體，導致 40GB Heap 的實際可用空間反而比 31GB 還小！
- 剩下的主機實體記憶體（例如 64GB 主機的其餘 33GB），必須保留給 **Linux Page Cache** 供 Lucene 進行高效的檔案快取。

```text
【實體伺服器 64GB 記憶體的黃金比例】
┌──────────────────────────────┬──────────────────────────────┐
│ JVM Heap（≤ 31GB）            │ Linux OS & Page Cache（33GB） │
│ 負責：查詢聚合、過濾快取、連接 │ 負責：Lucene 段檔案、讀寫快取 │
└──────────────────────────────┴──────────────────────────────┘
```

## 老年代（Old Gen）高壓的成因分析

1. **分片數量過多（Shard Over-allocation）**：
   - 每個 Shard 在記憶體中都需要維護 Lucene Segment 結構（FST、Term Dictionary）。若單一節點承載超過 600～1000 個 Shard，常駐記憶體會吃滿 Old Gen。
2. **高基數聚合與深度分頁**：
   - 昂貴的 `terms` 聚合或未釋放的 `Scroll` 查詢將大量長生命週期物件直接推入 Old Gen。
3. **Fielddata 快取未釋放**：
   - 在 `text` 欄位上的排序操作會常駐於 Old Gen，直到觸發 Circuit Breaker。

# 報告指標解讀指引

在 `check.html` 報告中，請重點檢視以下數值：

| 欄位名稱 | 警戒門檻 | 診斷意涵 | 處置優先級 |
|---|---|---|---|
| `jvm_old_pool_used_pct` | > 85%（Warning）<br>> 95%（Critical） | 老年代常駐記憶體壓力 | 極高 |
| `gc_old_collection_time` | 持續快速增加 | 垃圾回收發生頻繁停頓 | 高 |
| `heap_max_in_bytes` | > 32 GB | 違反指標壓縮原則 | 需立即調整配置 |

# 業務影響與技術說明建議

## 說明要點與原則

- **澄清「為什麼不加記憶體到 128G」的誤區**：解釋指標壓縮（Compressed OOPs）與 Lucene 需要 Page Cache 的雙層記憶體設計。
- **引導分片收斂與生命週期管理**：說明解決 JVM 高壓最有效的方式是清理大量小 Shard 與優化查詢。

## 說明範例：節點 Heap 長期處於 90% 高壓

> **技術說明範例**：
> 「客戶架構組您好，我們在檢查中發現資料節點的 JVM 老年代（Old Gen）記憶體使用率長期維持在 90% 左右。
> 
> 這代表垃圾回收器已經無法有效清空記憶體，系統隨時可能因為一次突發的大查詢而觸發長達數秒的 Stop-The-World（全停頓），甚至引發節點斷線與選主風暴。
> 
> 經排查，主要原因在於節點上堆積了超過 800 個小型分片（每個僅幾十 MB）。這些分片常駐佔用了 15GB 以上的底層結構記憶體。
> 
> 我們建議透過 ILM 生命週期策略將歷史小索引進行合併（Shrink）或轉為每月分區，將分片數減少 70%，即可直接將 JVM 壓力釋放至安全的 60% 以下，徹底消除當機風險。」

# 現場排查與安全處置 SOP

## 階段一：現場唯讀佐證

1. 檢查各節點目前 JVM Heap 與 GC 狀況：

```http
GET /_nodes/stats/jvm?filter_path=nodes.*.name,nodes.*.jvm.mem,nodes.*.jvm.gc
```

2. 檢查各節點分片數量是否超標：

```http
GET /_cat/allocation?v&h=node,shards,disk.percent,disk.used
```

## 階段二：整改修復方案

### 方案 A：收斂小分片（治本）
- 透過 ILM 策略將超過保留期限的歷史日誌索引安全刪除。
- 對歷史只讀索引執行 `_shrink` API 將多分片收斂為 1 個分片。

### 方案 B：檢查 JVM Heap 設定（確保 ≤ 31GB）
在 `jvm.options` 或環境變數中確認 `Xms` 與 `Xmx` 相同且不超過 31GB：

```text
# jvm.options
-Xms30g
-Xmx30g
```

## 階段三：變更後驗證

- [ ] 執行 `GET /_nodes/stats/jvm` 確認各節點 Old Gen 平時維持在 50%～75% 的健康區間
- [ ] 重新執行 `elk-diagnostics check` 確認 `jvm_pressure` 轉為 `PASS`

# 常見誤區與風險提示

- [ ] **誤區一：手動強制觸發 System.gc()**：
  在生產環境手動觸發 Full GC 會引發嚴重的不可預期停頓，切勿在線上高峰期下達強制 GC 指令。
- [ ] **誤區二：Xms 與 Xmx 設為不同數值**：
  若 `Xms`（初始堆）與 `Xmx`（最大堆）不同，JVM 在動態擴展記憶體時會引發停頓與記憶體抖動，官方嚴格規範兩者必須完全一致。

# 附錄 <!-- appendix -->

## 參考文件

- [Elasticsearch 官方文件：Setting the JVM heap size](https://www.elastic.co/docs/reference/elasticsearch/configuration-reference/advanced-configuration#set-jvm-heap-size)
- 專案內部規格書：`docs/內部/規格/效能規格.md`（#7）
