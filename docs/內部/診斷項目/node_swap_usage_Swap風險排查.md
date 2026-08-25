---
title: "節點 Swap 使用 — 實體記憶體不足、換頁監控與緊急釋放 SOP"
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
| 2026/08/23 | 1.0 | 初版：Swap 佔用檢測、實體記憶體超額配置排查與安全釋放流程 | 診斷工具架構組 |

# 文件說明

## 報告診斷卡對照

| 屬性 | 內容 |
|---|---|
| 報告分類區塊 | 節點環境（Node Context） |
| 診斷卡中文名稱 | 節點 Swap 使用 |
| 診斷卡 ID | `node_swap_usage` |
| 典型嚴重度 | `WARNING`（Swap used > 0）/ `CRITICAL`（Swap 大量佔用且持續增加） |
| 觸發關鍵特徵 | `_nodes/stats/os` 偵測到節點的 `swap.used_in_bytes > 0` |

## 文件目的

本手札用於協助第一線 ELK 維運工程師與顧問在巡檢報告發現節點已有 Swap 佔用時，排查是實體記憶體不足、其他常駐行程搶佔、還是 JVM Heap 設得過大擠壓了作業系統空間，並給出緊急修復方案。

## 適用範圍

本手札適用於所有自建或虛擬化主機之 Elasticsearch 7.x 及 8.x 節點。

# 核心原理與 Swap 佔用根因

## 為什麼節點會開始使用 Swap？

即使沒有開啟昂貴查詢，當伺服器的 **實體記憶體總量 < 系統總需求** 時，Linux 核心就會被迫將部分資料置換到 Swap 中。

常見三大超額配置原因：
1. **JVM Heap 佔比過高**：
   - 伺服器總 RAM 為 32GB，維運人員將 JVM Heap 設為 30GB。剩下的 2GB 根本不足以支撐 OS 核心、SSH、監控 Agent 與 Lucene 讀寫，導致 OS 被迫將常駐程式換頁到 Swap。
2. **同主機運行其他高耗能行程**：
   - 節點上同時安裝了 Logstash、Kibana、監控 Daemon 或資料庫，多個進程互相爭搶記憶體。
3. **`vm.swappiness` 預設值過高**：
   - Linux 預設 `swappiness = 60`，代表在記憶體尚有剩餘時就會積極嘗試換頁。

# 報告指標解讀指引

在 `check.html` 報告中，請檢視各節點 Swap 數據：

| 欄位名稱 | 警戒門檻 | 診斷意涵 |
|---|---|---|
| `swap_used_bytes` | > 0 B | 當前已有記憶體頁面被置換至硬碟 |
| `swap_free_bytes` | 接近 0 | Swap 空間即將耗盡，OOM-Killer 即將觸發 |

# 業務影響與技術說明建議

## 說明範例：伺服器記憶體超額配置引發換頁

> **技術說明範例**：
> 「維運同仁您好，報告中顯示 `data-node-01` 已經使用了 2.5GB 的 Swap 交換空間。
> 
> 經分析，主機的實體記憶體為 32GB，但 Elasticsearch 的 JVM Heap 設了 28GB，同時主機上還運行著 Logstash。這導致作業系統在記憶體不足時，將部分 Elasticsearch 行程強制置換到硬碟中。
> 
> 這會導致查詢延遲大幅抖動。我們建議將 JVM Heap 調降至 16GB（或將 Logstash 遷移至專用主機），並執行 `swapoff -a` 清空交換空間，讓記憶體回歸健康狀態。」

# 現場排查與安全處置 SOP

## 階段一：現場唯讀佐證（找出誰在吃記憶體）

在伺服器終端機執行：

```bash
# 檢查整體記憶體分佈
free -m

# 依記憶體使用量排序行程
ps aux --sort=-%mem | head -n 10
```

## 階段二：整改修復方案

### 步驟 1：調整 JVM Heap 確保不超過實體 RAM 的 50%
```text
# jvm.options
-Xms16g
-Xmx16g
```

### 步驟 2：安全刷新並清空 Swap
在記憶體有足夠空間後，關閉並重新開啟 Swap 以清空佔用：

```bash
sudo swapoff -a && sudo swapon -a
```

## 階段三：變更後驗證

- [ ] 執行 `free -m` 確認 `Swap: used` 數值為 `0`
- [ ] 重新執行 `elk-diagnostics check` 確認 `node_swap_usage` 轉為 `PASS`

# 附錄 <!-- appendix -->

## 參考文件

- [Elasticsearch 官方文件：Operating system settings](https://www.elastic.co/docs/reference/elasticsearch/configuration-reference/advanced-configuration#os-settings)
- 專案內部規格書：`docs/內部/規格/節點環境規格.md`
