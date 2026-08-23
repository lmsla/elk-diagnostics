---
title: "ELK 診斷手札：節點版本 / JDK / Plugin 漂移 — 執行期環境一致性檢驗與非對稱風險"
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
| 2026/08/23 | 1.0 | 初版：節點環境一致性檢查、JDK/Plugin 漂移排查與非對稱效能治理 | 診斷工具架構組 |

# 文件說明

## 報告診斷卡對照

| 屬性 | 內容 |
|---|---|
| 報告分類區塊 | 節點環境（Node Context） |
| 診斷卡中文名稱 | 節點版本 / JDK / Plugin 漂移 |
| 診斷卡 ID | `node_runtime_consistency` |
| 典型嚴重度 | `WARNING` / `CRITICAL` |
| 觸發關鍵特徵 | 各節點間存在 Elasticsearch 版本不同步、JDK 版本差異、外掛（Plugins）安裝不一致、或 JVM Heap 配置非對稱 |

## 文件目的

本手札用於協助第一線 ELK 維運工程師與顧問在巡檢報告發現叢集各節點環境不一致時，向客戶指出版本漂移與配置非對稱引發的隱蔽性 Bug 與單點崩潰風險，並給出標準化對齊 SOP。

## 適用範圍

本手札適用於所有多節點 Elasticsearch 7.x 及 8.x 叢集。

# 核心原理與環境漂移風險

## 什麼是節點環境漂移（Runtime Drift）？

在長期營運的叢集中，由於伺服器擴容、逐步維護或由不同工程師手動配置，常出現各伺服器「版本與參數不一致」的現象：

```text
【叢集節點環境不對稱範例】
Node 01: ES 8.14.3, JDK 21.0.2, Heap 30GB, Plugins: [analysis-ik]
Node 02: ES 8.14.3, JDK 21.0.2, Heap 30GB, Plugins: [analysis-ik]
Node 03: ES 8.14.1 (舊版!), JDK 17 (舊版!), Heap 16GB (配置錯誤!), Plugins: [] (漏裝外掛!)
  ▲ 只要查詢分發到 Node 03，可能因為缺少分詞外掛直接拋錯，或因記憶體較小最先 OOM！
```

## 四大漂移隱患

1. **外掛（Plugin）不一致**：
   - 某台節點漏裝分詞外掛（如 IK、Jieba）或安全外掛，當查詢被路由到該節點時，直接拋出 `PluginMissingException` 導致業務查詢局部失敗。
2. **JVM Heap 非對稱（Asymmetric Heap）**：
   - 叢集中部分節點給 30GB，部分節點給 16GB。較小的節點會成為叢集的「短板（木桶效應）」，最容易發生 GC 停頓或 Breaker 跳閘。
3. **JDK 小版本與 GC 演算法差異**：
   - 不同的 JDK 小版本可能存在已知的 GC Bug 或記憶體洩漏漏洞。
4. **跨大版本升級未完成（Mixed ES Versions）**：
   - 滾動升級中途停止，新舊版本混跑時間過長，限制了新功能與新 Mapping 特性的使用。

# 報告指標解讀指引

在 `check.html` 報告中，請檢視以下資訊：

| 欄位名稱 | 檢驗維度 | 診斷意涵 |
|---|---|---|
| `es_versions` | ES 程式版本 | 必須全體單一版本 |
| `jvm_versions` | JDK 執行期版本 | 必須一致（含小版本） |
| `plugins_inventory` | 安裝外掛清單 | 必須所有節點一致 |
| `heap_allocations` | JVM Heap 大小 | 同角色節點必須一致 |

# 業務影響與技術說明建議

## 說明要點與原則

- **強調分散式系統的「等價原則」**：說明同角色的節點必須具備完全一致的軟硬體環境，否則分散式負載均衡會產生隨機的不可預期錯誤。
- **推動配置基礎設施即代碼（IaC）**：建議客戶透過 Ansible、Terraform 或 Kubernetes 管理配置，避免手動修改造成漂移。

## 說明範例：節點漏裝外掛與 Heap 不一致

> **技術說明範例**：
> 「維運同仁您好，我們在檢查中發現 `data-node-03` 與其他節點存在環境漂移：
> 1. 其他節點的 Heap 都配置了 30GB，但 `node-03` 只有 16GB；
> 2. `node-03` 漏裝了自訂的中文分詞外掛。
> 
> 這會帶來很大的隱患：當使用者搜尋包含特定中文詞彙時，如果請求被隨機分發給 `node-03`，查詢就會立刻報錯；且這台機器會比其他機器更容易發生記憶體用盡。
> 
> 我們建議立即為 `node-03` 補裝外掛，並將 Heap 配置對齊為 30GB，確保叢集維持嚴格的環境對稱性。」

# 現場排查與安全處置 SOP

## 階段一：現場唯讀佐證（比對各節點環境）

查詢所有節點的 JVM、外掛與版本詳情：

```http
GET /_nodes/jvm,plugins?filter_path=nodes.*.name,nodes.*.version,nodes.*.jvm.version,nodes.*.jvm.mem.heap_max_in_bytes,nodes.*.plugins
```

## 階段二：整改修復方案

1. 為缺失外掛的節點安裝外掛並重啟：
   ```bash
   bin/elasticsearch-plugin install <PLUGIN_NAME>
   ```
2. 在 `jvm.options` 中對齊各節點的 `-Xms` 與 `-Xmx` 數值。

## 階段三：變更後驗證

- [ ] 執行 `GET /_nodes/jvm,plugins` 確認所有節點回傳的 Version、JVM 與 Plugins 清單完全一致
- [ ] 重新執行 `elk-diagnostics check` 確認 `node_runtime_consistency` 轉為 `PASS`

# 附錄 <!-- appendix -->

## 參考文件

- [Elasticsearch 官方文件：Nodes info API](https://www.elastic.co/docs/reference/elasticsearch/rest-apis/nodes-info)
- 專案內部規格書：`docs/內部/規格/靜態健檢規格.md`（ES-GAP-04）
