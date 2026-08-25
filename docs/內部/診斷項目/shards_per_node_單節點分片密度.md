---
title: "Shard 總量與分佈 — 單節點分片密度、Heap 承載極限與上限防禦"
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
| 2026/08/23 | 1.0 | 初版：分片密度黃金比例（20 Shards/GB Heap）、單節點負載上限與收斂指引 | 診斷工具架構組 |

# 文件說明

## 報告診斷卡對照

| 屬性 | 內容 |
|---|---|
| 報告分類區塊 | 叢集（Cluster） / 靜態健檢 |
| 診斷卡中文名稱 | Shard 總量與分佈 |
| 診斷卡 ID | `shards_per_node` |
| 典型嚴重度 | `WARNING`（密度 > 20 shards/GB）/ `CRITICAL`（密度 > 30 shards/GB 或單節點 > 1,000 shards） |
| 觸發關鍵特徵 | 單一節點承載的分片數量超過其 JVM Heap 承受極限 |

## 文件目的

本手札用於協助第一線 ELK 維運工程師與顧問在巡檢報告發現分片密度超標時，向客戶解釋分片開銷公式，並指導收斂分片總數以保護節點記憶體穩定性。

## 適用範圍

本手札適用於所有版本之 Elasticsearch 8.x 及 9.x 叢集。

# 核心原理與分片密度公式

## 官方權威分片密度黃金比例

Elasticsearch 官方權威指南明確指出：
$$\text{單節點最大分片數上限} \le \text{JVM Heap (GB)} \times 20$$

| JVM Heap 配置 | 推薦安全分片數上限 | 警戒危險門檻 |
|---|---|---|
| **8 GB Heap** | $\le 160$ 個 Shards | $> 240$ 個 Shards |
| **16 GB Heap** | $\le 320$ 個 Shards | $> 480$ 個 Shards |
| **30 GB Heap** | $\le 600$ 個 Shards | $> 900$ 個 Shards |

```text
分片過多帶來的代價：
1. 每個分片常駐佔用約 4MB～10MB 的 JVM 元數據記憶體
2. 叢集狀態更新（Cluster State）廣播體積隨分片數呈線性增長
3. 節點重啟時，恢復數千個分片需要長達數十分鐘的恢復期
```

# 報告指標解讀指引

在 `check.html` 報告中，請檢視以下數值：

| 欄位名稱 | 警戒門檻 | 診斷意涵 |
|---|---|---|
| `shards_per_node` | > 20/GB | 分片密度偏高 |
| `max_node_shards` | > 600 | 單節點分片數過高 |

# 業務影響與技術說明建議

## 說明範例：單節點分片密度過高

> **技術說明範例**：
> 「維運同仁您好，報告中顯示目前資料節點（配置 16GB Heap）平均承載了 550 個分片，已超出 20 shards/GB（320 個）的官方安全水準。
> 
> 這代表節點有超過 30% 的 JVM 記憶體被單純拿來維護分片目錄結構，而不是用在業務搜尋與寫入上。
> 
> 我們建議透過 ILM 生命週期將歷史只讀索引進行 `_shrink` 縮分片或合併按月分區，將分片密度降至 300 個以內，整座叢集的穩定性與反應速度將大幅提升。」

# 現場排查與安全處置 SOP

## 階段一：現場唯讀佐證

```http
GET /_cat/allocation?v&h=node,shards,disk.percent
```

## 階段二：整改修復方案

- 刪除過期歷史日誌索引。
- 採用 ILM 策略將歷史冷資料索引透過 Shrink API 縮減為 1 個分片。

## 階段三：變更後驗證

- [ ] 執行 `GET /_cat/allocation` 確認各節點分片數均在安全標準以內
- [ ] 重新執行 `elk-diagnostics check` 確認 `shards_per_node` 轉為 `PASS`

# 附錄 <!-- appendix -->

## 參考文件

- [Elasticsearch 官方文件：Shard count limits](https://www.elastic.co/docs/reference/elasticsearch/configuration-reference/advanced-configuration#shard-limit-settings)
- 專案內部規格書：`docs/內部/規格/靜態健檢規格.md`（ES-GAP-02）
