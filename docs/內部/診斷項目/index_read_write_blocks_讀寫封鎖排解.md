---
title: "ELK 診斷手札：Index read/write blocks — 磁碟 Flood-stage 鎖定與唯讀封鎖解鎖 SOP"
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
| 2026/08/23 | 1.0 | 初版：Flood-stage 磁碟保護機制、唯讀鎖定根因與安全解鎖順序 | 診斷工具架構組 |

# 文件說明

## 報告診斷卡對照

| 屬性 | 內容 |
|---|---|
| 報告分類區塊 | 資料（Data） |
| 診斷卡中文名稱 | Index read/write blocks |
| 診斷卡 ID | `index_read_write_blocks` |
| 典型嚴重度 | `CRITICAL`（唯讀封鎖中） |
| 觸發關鍵特徵 | 索引被標記 `read_only` 或 `read_only_allow_delete`（通常由磁碟水位線 95% 觸發），客戶端寫入拋出 `ClusterBlockException` |

## 文件目的

本手札用於協助第一線 ELK 維運工程師與顧問在巡檢報告發現索引被鎖定為唯讀時，向客戶解釋保護機制原理，依序執行「空間釋放 → 手動解除唯讀」，避免因錯誤解鎖導致磁碟 100% 寫滿整機崩潰。

## 適用範圍

本手札適用於所有版本之 Elasticsearch 7.x 及 8.x 叢集。

# 核心原理與 Flood-stage 唯讀機制

## 什麼是 Flood-stage Watermark？

Elasticsearch 內建三級磁碟保護水位線：
1. **Low Watermark（預設 85%）**：停止向該節點指派新分片。
2. **High Watermark（預設 90%）**：主動將該節點上的現有分片搬遷至其他節點。
3. **Flood-stage Watermark（預設 95%）**：
   - 當磁碟突破 95% 時，系統為了避免硬碟 100% 寫滿導致 Lucene 檔案損毀與資料庫崩潰，**會強制將該節點上的所有索引自動鎖定為 `read_only_allow_delete: true`**！

```text
磁碟突破 95% 洪水線
       ↓
【自動緊急止血】強制將所有 Index 設為 read_only_allow_delete: true
       ↓
┌─────────────────────────────────────────────────────────────┐
│ 允許操作：刪除索引（DELETE /index）以釋放空間                  │
│ 禁止操作：所有寫入與更新（POST / PUT / Bulk），拋出 403 異常   │
└─────────────────────────────────────────────────────────────┘
```

## 為什麼清理磁碟後還是不能寫入？

很多維運工程師常犯的錯誤是：清理了磁碟（空間降到 70%），但發現還是寫不進去！

這是因為 **`read_only_allow_delete` 在空間釋放後「不會自動解鎖」**，必須由管理員手動下達指令將唯讀標記清空，防止磁碟在短時間內反覆抖動。

# 報告指標解讀指引

在 `check.html` 報告中，請檢視以下資訊：

| 欄位名稱 | 範例值 | 診斷意涵 | 處置優先級 |
|---|---|---|---|
| `blocked_indices_count` | `12` | 被強制唯讀的索引數量 | 極高 |
| `block_type` | `read_only_allow_delete` | 鎖定類型（通常為磁碟觸發） | 需先清空間 |
| `disk_percent` | `96%` | 當前磁碟使用率 | 必須先降至 85% 以下 |

# 業務影響與技術說明建議

## 說明要點與原則

- **強調保護意義**：說明這是系統為了保全既有資料不損毀而採取的緊急煞車。
- **強調先後順序**：向客戶說明**「必須先清出硬碟空間，才能動手解鎖」**，否則解鎖後 10 秒內又會重新鎖死。

## 說明範例：磁碟滿載觸發唯讀

> **技術說明範例**：
> 「維運主管您好，報告中顯示多個業務索引目前被標記為 `read_only_allow_delete`（唯讀鎖定），導致前端日誌寫入報警。
> 
> 這是因為 `data-node-02` 的硬碟使用率剛才達到了 96%，衝破了 95% 的緊急洪水保護線。Elasticsearch 為了防止硬碟 100% 寫滿造成檔案底層損毀，自動開啟了寫入保護。
> 
> 我們現在的標準處置步驟如下：
> 1. 先安全刪除 30 天前的歷史舊索引，將磁碟空間騰出降至 80% 以下；
> 2. 空間就緒後，我們手動執行一行解鎖指令，寫入功能即可立刻全面恢復。」

# 現場排查與安全處置 SOP

## 階段一：確認磁碟使用率

```http
GET /_cat/allocation?v&h=node,disk.percent,disk.avail,disk.used
```

確認所有節點磁碟已低於 85%（若高於 85%，先刪除歷史索引或擴容）。

## 階段二：整改修復方案（手動解鎖）

對所有受影響的索引（或全域索引）解除唯讀限制：

```http
PUT /*/_settings
{
  "index.blocks.read_only_allow_delete": null,
  "index.blocks.read_only": null
}
```

## 階段三：變更後驗證

- [ ] 執行 `GET /*/_settings?filter_path=*.settings.index.blocks` 確認回傳為空
- [ ] 觀察業務寫入請求恢復正常（HTTP 200/201）
- [ ] 重新執行 `elk-diagnostics check` 確認 `index_read_write_blocks` 轉為 `PASS`

# 常見誤區與風險提示

- [ ] **誤區一：空間未釋放就下達解鎖指令**：
  若磁碟依然在 95% 以上，解鎖指令送出後不到 30 秒，ES 後台排程會再度將其強制鎖回唯讀，徒勞無功。

# 附錄 <!-- appendix -->

## 參考文件

- [Elasticsearch 官方文件：Disk-based shard allocation settings](https://www.elastic.co/docs/reference/elasticsearch/configuration-reference/disk-based-shard-allocation-settings)
- 專案內部規格書：`docs/內部/規格/擴充健檢規格.md`（ES-GAP-08）
