---
title: "ELK 診斷手札：ILM 錯誤統計 — 生命週期執行失敗、步驟死鎖與 Retry 解鎖 SOP"
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
| 2026/08/23 | 1.0 | 初版：ILM 錯誤狀態統計、Step Info 異常解析與手動 Retry 解鎖 | 診斷工具架構組 |

# 文件說明

## 報告診斷卡對照

| 屬性 | 內容 |
|---|---|
| 報告分類區塊 | 管理（Management） / 擴充健檢 |
| 診斷卡中文名稱 | ILM 錯誤統計 |
| 診斷卡 ID | `ilm_errors` |
| 典型嚴重度 | `WARNING` / `CRITICAL` |
| 觸發關鍵特徵 | `_ilm/explain` 偵測到有索引處於 `step_info.type: ERROR` 狀態（ILM 執行失敗） |

## 文件目的

本手札用於協助第一線 ELK 維運工程師與顧問在巡檢報告發現 ILM 執行錯誤時，快速提取失敗的具體 Step（如 Force Merge 失敗、Shrink 失敗、Rollover 失敗），並指導修正錯誤後重試。

## 適用範圍

本手札適用於所有使用 ILM 之 Elasticsearch 7.x 及 8.x 叢集。

# 核心原理與 ILM 錯誤機制

## 為什麼 ILM 會報錯（`is_auto_retryable_error: false`）？

ILM 在執行每個 Phase 的每個 Action 時，若遇到致命錯誤（例如：目標節點空間不足、Shrink 索引已存在、唯讀鎖定失敗），會自動將索引標記為 **ERROR** 並停止推進：

```text
ILM 推進 Phase: Warm → Action: Shrink
       ↓ 執行失敗 (例如目標節點磁碟已滿)
【標記 ERROR】── 記錄 step_info 異常原因
       ↓
ILM 停止推進，直到管理員介入修復並執行 _ilm/retry！
```

# 報告指標解讀指引

在 `check.html` 報告中，請檢視以下資訊：

| 欄位名稱 | 範例值 | 診斷意涵 | 處置建議 |
|---|---|---|---|
| `error_indices_count` | `3` | 處於 ILM 錯誤狀態的索引數 | 需介入修復 |
| `failed_step` | `shrink` / `forcemerge` | 發生錯誤的具體步驟 | 定位問題領域 |

# 客戶溝通話術與情境模擬

## 話術範例：ILM 步驟失敗停滯

> **顧問說明範例**：
> 「維運主管您好，報告中顯示有 3 個歷史日誌索引在執行 ILM 生命週期時發生錯誤停滯。
> 
> 經檢視，是因為在執行 Force Merge 壓縮段時，目標磁碟空間不足，觸發了保護中斷。
> 
> 我們只需騰出少許磁碟空間，並執行一行 `_ilm/retry` 重試指令，系統就會自動接續完成段壓縮並推進至下一階段。」

# 現場排查與安全處置 SOP

## 階段一：現場唯讀佐證（提取錯誤詳情）

```http
GET /*/_ilm/explain?only_errors=true
```

檢視回傳 JSON 中的 `step_info.reason`。

## 階段二：整改修復方案（手動重試）

修復環境問題後，對報錯索引執行重試：

```http
POST /<FAILED_INDEX>/_ilm/retry
```

## 階段三：變更後驗證

- [ ] 執行 `GET /*/_ilm/explain?only_errors=true` 確認回傳清單為空
- [ ] 重新執行 `elk-diagnostics check` 確認 `ilm_errors` 轉為 `PASS`

# 附錄 <!-- appendix -->

## 參考文件

- [Elasticsearch 官方文件：Index lifecycle management errors](https://www.elastic.co/docs/reference/elasticsearch/data-management/index-lifecycle-management#ilm-error-handling)
- 專案內部規格書：`docs/內部/規格/管理功能規格.md`
