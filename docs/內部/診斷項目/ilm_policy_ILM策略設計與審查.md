---
title: "ELK 診斷手札：ILM Policy 設定概況 — 生命週期策略設計評審與分階段調優"
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
| 2026/08/23 | 1.0 | 初版：ILM 策略五大階段設計、Rollover 門檻審查與生命週期合規 | 診斷工具架構組 |

# 文件說明

## 報告診斷卡對照

| 屬性 | 內容 |
|---|---|
| 報告分類區塊 | 管理（Management） |
| 診斷卡中文名稱 | ILM Policy 設定概況 |
| 診斷卡 ID | `ilm_policy_inventory` |
| 典型嚴重度 | `INFO` / `WARNING` |
| 觸發關鍵特徵 | `_ilm/policy` 清查發現 Policy 設計不合理（如未設 Delete 階段導致磁碟只增不減、或 Rollover 條件過寬） |

## 文件目的

本手札用於協助第一線 ELK 維運工程師與顧問在巡檢報告中評審客戶現行的 ILM（Index Lifecycle Management）策略，評估 Hot、Warm、Cold、Frozen、Delete 五階段配置是否符合業務 RTO/RPO 與容量規劃。

## 適用範圍

本手札適用於所有使用 ILM 管理日誌與時間序列資料之 Elasticsearch 7.x 及 8.x 叢集。

# 核心原理與 ILM 五階段標準架構

## ILM 標準生命週期五階段

```text
【資料熱度與成本遞減模型】
┌─────────────────────────────────────────────────────────────┐
│ 1. HOT 階段（寫入 + 即時查詢）：高性能 NVMe SSD               │
│    - 觸發 Rollover：單分片達 30GB～50GB 或時間達 1～7 天     │
├─────────────────────────────────────────────────────────────┤
│ 2. WARM 階段（唯讀 + 頻繁查詢）：標準 SSD                   │
│    - 執行 Read-only、Force Merge（段合併為 1 段）、Shrink 縮分片 │
├─────────────────────────────────────────────────────────────┤
│ 3. COLD 階段（唯讀 + 低頻查詢）：大容量 HDD / 低成本儲存     │
│    - 副本移轉至冷儲存，甚至使用 Searchable Snapshots         │
├─────────────────────────────────────────────────────────────┤
│ 4. FROZEN 階段（極低頻檢索）：物件儲存（GCS / S3）           │
│    - 完全依靠 Searchable Snapshots 依需快取                  │
├─────────────────────────────────────────────────────────────┤
│ 5. DELETE 階段（生命週期結束）：安全銷毀                     │
│    - 資料保留期滿（如 90 天或 180 天），自動整批刪除释放空間  │
└─────────────────────────────────────────────────────────────┘
```

## 常見 ILM 設計缺陷

1. **只有 Rollover，沒有 Delete 階段**：
   - 導致硬碟容量永久只增不減，最終引發 95% 磁碟洪水鎖定。
2. **Rollover 條件太寬或未設 `max_primary_shard_size`**：
   - 只按時間（如 30 天）滾動，遇到突發大流量時，單一分片膨脹到 300GB，後台段合併（Merge）直接卡死。
3. **在 Hot 階段執行 Force Merge**：
   - 仍在頻繁寫入的活躍索引絕對不可做 Force Merge，這會引發嚴重的 CPU 與 I/O 阻塞。

# 報告指標解讀指引

在 `check.html` 報告中，請檢視 Policy 清單：

| 欄位名稱 | 檢驗要點 | 最佳實踐建議 |
|---|---|---|
| `policy_name` | 策略名稱 | 應依業務分級（如 30d/90d/1y） |
| `phases` | 涵蓋階段 | 必須包含 Hot 與 Delete |
| `rollover_conditions` | 滾動觸發門檻 | 強烈建議設 `max_primary_shard_size: 50gb` |

# 業務影響與技術說明建議

## 說明範例：ILM 未配置自動刪除階段

> **技術說明範例**：
> 「主管您好，我們在清查叢集的資料生命週期策略（ILM）時發現，目前 `app-log-policy` 僅配置了 Hot 寫入，但缺少自動轉移與 Delete 階段。
> 
> 這會使所有歷史日誌無限制常駐在最昂貴的 Hot 高速硬碟上，每個月都需要人工手動刪除來騰出空間，一旦維運人員疏漏，就會觸發整座叢集的磁碟唯讀鎖定。
> 
> 我們建議為 Policy 補上標準的生命週期規則：**資料在 7 天後自動下沉至 Warm 節點並執行段壓縮（節省 30% 容量），並在 90 天後自動安全銷毀**，實現 100% 無人值守的自動化容量管理。」

# 現場排查與安全處置 SOP

## 階段一：現場唯讀佐證（導出所有 Policy）

```http
GET /_ilm/policy
```

## 階段二：整改修復方案（標準 Policy 範本）

更新為企業級標準生命週期策略：

```json
PUT /_ilm/policy/enterprise_logs_policy
{
  "policy": {
    "phases": {
      "hot": {
        "actions": {
          "rollover": {
            "max_primary_shard_size": "50gb",
            "max_age": "7d"
          }
        }
      },
      "warm": {
        "min_age": "7d",
        "actions": {
          "forcemerge": {
            "max_num_segments": 1
          },
          "shrink": {
            "number_of_shards": 1
          }
        }
      },
      "delete": {
        "min_age": "90d",
        "actions": {
          "delete": {}
        }
      }
    }
  }
}
```

## 階段三：變更後驗證

- [ ] 執行 `GET /_ilm/policy/enterprise_logs_policy` 確認設定無誤
- [ ] 重新執行 `elk-diagnostics check` 確認 `ilm_policy_inventory` 轉為 `PASS`

# 附錄 <!-- appendix -->

## 參考文件

- [Elasticsearch 官方文件：Index lifecycle management](https://www.elastic.co/docs/reference/elasticsearch/data-management/index-lifecycle-management)
- 專案內部規格書：`docs/內部/規格/擴充健檢規格.md`（ES-GAP-13）
