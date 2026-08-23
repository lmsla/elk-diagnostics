---
title: "ELK 診斷手札：Logstash Pipeline 效能 — 隊列積壓、Worker 飽和與 Grok 回溯調優"
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
| 2026/08/23 | 1.0 | 初版：Logstash Pipeline 三階段效能監控、Worker 飽和度與 Filter 耗時調優 | 診斷工具架構組 |

# 文件說明

## 報告診斷卡對照

| 屬性 | 內容 |
|---|---|
| 報告分類區塊 | 服務：Logstash（Service: Logstash） |
| 診斷卡中文名稱 | Logstash Pipeline 效能統計 |
| 診斷卡 ID | `logstash_pipeline_stats` |
| 典型嚴重度 | `WARNING`（Filter 耗時 > 10ms/event）/ `CRITICAL`（Queue 嚴重積壓） |
| 觸發關鍵特徵 | Logstash `_node/stats/pipelines` 顯示事件處理延遲過高、Worker 線程長時間 100% 飽和、或持久化隊列（PQ）積壓 |

## 文件目的

本手札用於協助第一線 ELK 維運工程師與顧問在巡檢報告發現 Logstash 管線效能低下時，精準拆解 Input、Filter、Output 三階段的耗時分佈，找出是 Grok 正則回溯慢、還是後端 Elasticsearch 寫入慢導致的背壓阻塞。

## 適用範圍

本手札適用於所有版本之 Logstash 7.x 及 8.x 實例。

# 核心原理與 Logstash Pipeline 效能瓶頸

## Logstash Pipeline 三階段架構

```text
資料來源 (Beats / Kafka / Syslog)
       ↓
【Input 階段】接收事件並寫入 Memory / Persistent Queue
       ↓
【Filter 階段】Pipeline Workers (預設 = CPU 核心數) 執行 Grok, JSON, Mutate
       ↓
【Output 階段】批次 Bulk 發送至 Elasticsearch
```

## 瓶頸判斷心法：Filter 慢 還是 Output 慢？

1. **Filter 階段瓶頸（CPU 滿載）**：
   - `filter.events.duration_in_millis` 佔總耗時 80% 以上。
   - 原因：複雜的 Grok 正規表達式回溯、未編譯的 Ruby 代碼、大量 DNS 查詢。
2. **Output 階段瓶頸（Backpressure 阻塞）**：
   - `output.events.duration_in_millis` 極高，但 Logstash 本機 CPU 很低。
   - 原因：後端 Elasticsearch 發生寫入背壓（429 拒絕）或網路頻寬瓶頸，Logstash Worker 被迫等待 ES 回應。

# 報告指標解讀指引

在 `check.html` 報告中，請檢視各 Pipeline 統計數據：

| 欄位名稱 | 警戒門檻 | 診斷意涵 | 處置優先級 |
|---|---|---|---|
| `events_filtered_rate` | 吞吐驟降 | Filter 處理能力受限 | 高 |
| `events_duration_per_event` | > 10 ms | 單一事件處理耗時過長 | 關鍵指標 |
| `queue_size_in_bytes` | 持續擴大 | 隊列正在積壓 | 需排查輸出端 |

# 業務影響與技術說明建議

## 說明範例：Grok 解析效率低拖垮 Logstash

> **技術說明範例**：
> 「系統維運主管您好，報告中顯示 Logstash 的 `main` Pipeline 單筆日誌平均處理耗時達到了 18 毫秒（標準期望在 2 毫秒以內），導致日誌傳遞出現數十分鐘的延遲。
> 
> 經分析，主因在於日誌過濾規則中包含了多層巢狀的 Grok 貪婪正則匹配（`.*`），當遇到格式異常的日誌時，CPU 會陷入數十萬次的反覆回溯計算。
> 
> 我們建議：
> 1. 將 Grok 正則優化為精確匹配；
> 2. 將 `pipeline.workers` 調整為與實體 CPU 核心數相符；
> 3. 將 `pipeline.batch.size` 由 125 提高至 500～1000，吞吐量即可獲得 3～5 倍的巨大提升。」

# 現場排查與安全處置 SOP

## 階段一：現場唯讀佐證（提取 Pipeline 各階段耗時）

```http
GET http://<LOGSTASH_HOST>:9600/_node/stats/pipelines?pretty
```

查看各 plugin 的 `duration_in_millis` 與 `events.in`。

## 階段二：整改修復方案

### 步驟 1：調優 logstash.yml 參數
```yaml
pipeline.workers: 8          # 與實體 CPU 核心數一致
pipeline.batch.size: 500     # 提高每次 Bulk 批次量
pipeline.batch.delay: 50     # 毫秒延遲以湊齊批次
```

### 步驟 2：優化 Grok 正則規則
- 避免使用 `.*` 貪婪匹配，改用 `\S+` 或自訂 pattern。
- 在 Grok 開頭加上錨點 `^` 加速匹配。

## 階段三：變更後驗證

- [ ] 執行 `GET http://<LOGSTASH_HOST>:9600/_node/stats/pipelines` 確認事件平均耗時降至 2ms 以下
- [ ] 重新執行 `elk-diagnostics check` 確認 `logstash_pipeline_stats` 轉為 `PASS`

# 附錄 <!-- appendix -->

## 參考文件

- [Logstash 官方文件：Tuning and Profiling Logstash Performance](https://www.elastic.co/docs/reference/logstash/tuning-logstash)
- 專案內部規格書：`docs/內部/規格/擴充健檢規格.md`
