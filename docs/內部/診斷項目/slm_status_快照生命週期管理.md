---
title: "ELK 診斷手札：Snapshot Lifecycle Management（SLM）狀態 — 快照排程健康與保留策略審查"
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
| 2026/08/23 | 1.0 | 初版：SLM 原生排程引擎、快照保留過期清理（Retention）與策略審查 | 診斷工具架構組 |

# 文件說明

## 報告診斷卡對照

| 屬性 | 內容 |
|---|---|
| 報告分類區塊 | 資料（Data） / 快照（Snapshots） |
| 診斷卡中文名稱 | Snapshot Lifecycle Management（SLM）狀態 |
| 診斷卡 ID | `slm_status` |
| 典型嚴重度 | `WARNING`（未配置 SLM 策略或最近執行失敗） |
| 觸發關鍵特徵 | `_slm/policy` 顯示未建立自動快照策略，或特定 Policy 的 `last_failure` 記錄了執行異常 |

## 文件目的

本手札用於協助第一線 ELK 維運工程師與顧問在巡檢報告中評估叢集是否具備自動化備份能力，檢查 SLM（Snapshot Lifecycle Management）排程與保留過期清理（Retention）機制是否健康運行。

## 適用範圍

本手札適用於所有使用 SLM 自動化備份之 Elasticsearch 7.x 及 8.x 叢集。

# 核心原理與 SLM 自動化體系

## 什麼是 Snapshot Lifecycle Management？

SLM 是 Elasticsearch 內建的自動化備份引擎，解決了過去必須依賴外部 crontab 呼叫 API 的痛點。

SLM Policy 包含三大核心要素：
1. **Schedule（排程時間）**：使用 Cron 表達式定義何時備份（例如每日凌晨 2:00：`0 0 2 * * ?`）。
2. **Configuration（備份範圍）**：指定備份哪些索引、儲存到哪個 Repository。
3. **Retention（保留策略）**：自動刪除過期快照（例如保留 30 天，最少保留 5 份，最多保留 50 份），防止雲端儲存空間無限膨脹。

```text
SLM 自動循環：
定時觸發快照 (Schedule) → 執行增量上傳 → 執行過期快照清理 (Retention)
```

# 報告指標解讀指引

在 `check.html` 報告中，請檢視以下資訊：

| 欄位名稱 | 檢驗要點 | 診斷意涵 |
|---|---|---|
| `slm_policies_count` | 必須 > 0 | 是否已配置自動備份策略 |
| `last_success` | 時間戳 | 最近一次成功執行的時間 |
| `last_failure` | 異常堆疊 | 若有值代表最近一次自動備份失敗 |

# 業務影響與技術說明建議

## 說明範例：未配置自動化 SLM 快照策略

> **技術說明範例**：
> 「資安主管您好，報告中顯示目前叢集尚未配置 `SLM（Snapshot Lifecycle Management）` 自動快照策略。
> 
> 這意味著資料備份目前依賴人工手動操作或外部腳本，存在人為疏漏或備份中斷的風險。
> 
> 我們建議在叢集內啟用標準的 SLM 策略：**每日凌晨自動執行增量快照，並自動保留最近 30 天的歷史備份**。這能保證在發生勒索病毒攻擊或硬體災難時，RPO（資料遺失時間）控制在 24 小時以內。」

# 現場排查與安全處置 SOP

## 階段一：現場唯讀佐證（導出 SLM 策略）

```http
GET /_slm/policy
```

檢視各 Policy 的 `last_failure` 與 `retention` 設定。

## 階段二：整改修復方案（配置標準 SLM Policy）

建立標準每日備份策略：

```json
PUT /_slm/policy/daily-snapshots
{
  "schedule": "0 30 2 * * ?",
  "name": "<daily-snap-{now/d}>",
  "repository": "my_backup_repo",
  "config": {
    "indices": ["*"],
    "ignore_unavailable": true,
    "include_global_state": true
  },
  "retention": {
    "expire_after": "30d",
    "min_count": 7,
    "max_count": 30
  }
}
```

立即手動觸發一次以驗證有效性：

```http
POST /_slm/policy/daily-snapshots/_execute
```

## 階段三：變更後驗證

- [ ] 執行 `GET /_slm/policy/daily-snapshots` 確認 `last_success` 成功更新
- [ ] 重新執行 `elk-diagnostics check` 確認 `slm_status` 轉為 `PASS`

# 附錄 <!-- appendix -->

## 參考文件

- [Elasticsearch 官方文件：Snapshot lifecycle management](https://www.elastic.co/docs/reference/elasticsearch/snapshot-restore/snapshot-lifecycle-management)
- 專案內部規格書：`docs/內部/規格/資料規格.md`（#12）
