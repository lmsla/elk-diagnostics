---
title: "ELK 診斷手札：Data corruption 資料毀損徵兆 — Checksum 異常、段損毀與安全救磚"
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
| 2026/08/23 | 1.0 | 初版：Lucene Segment 毀損判定、Translog 損毀救磚工具與資料復原 SOP | 診斷工具架構組 |

# 文件說明

## 報告診斷卡對照

| 屬性 | 內容 |
|---|---|
| 報告分類區塊 | 資料（Data） |
| 診斷卡中文名稱 | Data corruption 資料毀損徵兆 |
| 診斷卡 ID | `data_corruption` |
| 典型嚴重度 | `CRITICAL`（資料毀損） |
| 觸發關鍵特徵 | `_cat/indices` 偵測到有索引處於 `red` 且伴隨 `CorruptIndexException`、`TranslogCorruptedException` 或 Checksum 驗證失敗 |

## 文件目的

本手札用於協助第一線 ELK 維運工程師與顧問在巡檢報告發現資料毀損徵兆時，精準定性損毀層級（是硬碟壞軌、非預期斷電、還是 Lucene 段損毀），並指導使用官方離線工具進行安全救磚或從快照還原。

## 適用範圍

本手札適用於所有版本之 Elasticsearch 7.x 及 8.x 叢集。

# 核心原理與資料毀損層級

## 資料毀損的兩大常見成因

1. **主機非預期強制斷電（Unclean Shutdown）**：
   - 伺服器突發斷電，導致寫入中的 **Translog（交易日誌）** 尾端尚未 flush 即被截斷，再次開機時拋出 `TranslogCorruptedException`。
2. **底層儲存硬體壞軌或記憶體位元翻轉（Hardware Bit Rot）**：
   - 硬碟出現 Bad Sector，導致 Lucene Segment 檔案的 Checksum（CRC32）校驗值不符，拋出 `CorruptIndexException`。

```text
分片載入
   ↓
┌───────────────────────────────────────────────┐
│ 1. 讀取 Segment Checksum 校驗碼               │
│ 2. 驗證 Translog 交易日誌完整性                │
└───────────────────────────────────────────────┘
   ↓
【發現校驗失敗】──→ 立即隔離分片，拒絕載入並標記為 RED！
                    ▲ 防止損毀的錯誤資料進一步擴散至其他節點
```

## 救磚原則：快照還原 > 副本重建 > 工具截斷

1. **第一優先（有快照）**：從最新的 SLM 快照還原該索引（最安全、零失真）。
2. **第二優先（副本健康）**：若僅是單一節點上的分片損壞但其他節點的 Replica 完好，直接刪除損壞節點上的分片目錄，讓 ES 自動從健康的 Replica 同步重建。
3. **最後手段（無備份且主副全毀）**：使用離線工具 `elasticsearch-shard` 截斷損毀的日誌段（會丟失最後幾筆未落盤資料，但能救回 99.9% 歷史資料）。

# 報告指標解讀指引

在 `check.html` 報告中，請檢視以下數值：

| 欄位名稱 | 範例值 | 診斷意涵 |
|---|---|---|
| `corrupted_indices_count` | `1` | 偵測到毀損的索引數量 |
| `corrupted_indices` | `["system-audit-2026.08"]` | 具體毀損的索引名稱 |

# 業務影響與技術說明建議

## 說明要點與原則

- **實事求是、冷靜處置**：明確向客戶說明是底層硬體或斷電引發的局部檔案異常，系統已自動隔離防止災情擴大。
- **提供明確救磚選項**：依據客戶是否有快照備份，給出具體可行的資料復原方案。

## 說明範例：主機斷電導致 Translog 毀損

> **技術說明範例**：
> 「主管您好，報告中顯示 `system-audit` 索引的分片在上次伺服器突發斷電後，出現了日誌校驗失敗（Corruption）。
> 
> 這是因為斷電瞬間有一筆資料正在寫入磁碟，導致日誌結尾不完整。Elasticsearch 的自我保護機制為了防止錯誤資料被讀取，選擇先將該分片鎖定為 Red。
> 
> 我們的處置方案如下：
> 1. 我們優先檢查 SLM 快照，直接從昨日備份中還原該索引；
> 2. 若為近期日誌，我們可以使用官方的 `elasticsearch-shard` 救磚工具，剔除斷電瞬間損壞的零碎日誌段，完整救回該索引 99.9% 以上的歷史數據。」

# 現場排查與安全處置 SOP

## 階段一：現場唯讀佐證（定位毀損細節）

檢查 Elasticsearch 節點日誌：

```bash
grep -E "CorruptIndexException|TranslogCorruptedException" /var/log/elasticsearch/<cluster-name>.log
```

## 階段二：整改修復方案

### 方案 A：從快照還原（最推薦）

```http
POST /_snapshot/<MY_REPOSITORY>/<SNAPSHOT_NAME>/_restore
{
  "indices": "<CORRUPTED_INDEX_NAME>"
}
```

### 方案 B：使用官方工具離線救磚（最後手段）

若確定無備份且主副本皆毀損，先停止該節點的 Elasticsearch 服務，並在終端機執行：

```bash
/usr/share/elasticsearch/bin/elasticsearch-shard remove-corrupted-data --index <INDEX_NAME> --shard-id 0
```
工具會自動掃描並截斷損毀的 translog/segment，確認後輸入 `y` 執行修復，完成後重新啟動節點。

## 階段三：變更後驗證

- [ ] 節點啟動後，執行 `GET /_cluster/health` 確認狀態轉為 `green`
- [ ] 執行 `GET /<INDEX_NAME>/_count` 確認文件可正常查詢
- [ ] 重新執行 `elk-diagnostics check` 確認 `data_corruption` 轉為 `PASS`

# 常見誤區與風險提示

- [ ] **誤區一：在未備份資料目錄的情況下直接跑救磚工具**：
  執行 `remove-corrupted-data` 前，必須先將整個分片資料夾完整複製備份一份，避免操作失誤失去最後挽救機會。

# 附錄 <!-- appendix -->

## 參考文件

- [Elasticsearch 官方文件：elasticsearch-shard tool](https://www.elastic.co/docs/reference/elasticsearch/command-line-tools/elasticsearch-shard)
- 專案內部規格書：`docs/內部/規格/資料規格.md`（#32）
