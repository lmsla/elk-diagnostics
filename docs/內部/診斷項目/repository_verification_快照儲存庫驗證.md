---
title: "ELK 診斷手札：Snapshot 儲存庫驗證 — 全節點讀寫校驗、權限測試與遠端儲存除錯"
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
| 2026/08/23 | 1.0 | 初版：Repository Verify API 驗證機制、全節點讀寫一致性與雲端權限排查 | 診斷工具架構組 |

# 文件說明

## 報告診斷卡對照

| 屬性 | 內容 |
|---|---|
| 報告分類區塊 | 資料（Data） / 快照（Snapshots） |
| 診斷卡中文名稱 | Snapshot 儲存庫驗證 |
| 診斷卡 ID | `repository_verification` |
| 典型嚴重度 | `CRITICAL`（儲存庫驗證失敗） |
| 觸發關鍵特徵 | `POST _snapshot/<repo>/_verify` 失敗，部分或所有節點無法對 Repository 進行讀寫測試 |

## 文件目的

本手札用於協助第一線 ELK 維運工程師與顧問在巡檢報告發現儲存庫驗證失敗時，排查 GCS Service Account、AWS S3 IAM 角色、或 NFS/共用檔案系統在各節點上的權限與掛載一致性問題。

## 適用範圍

本手札適用於所有配置有 Snapshot Repository 之 Elasticsearch 7.x 及 8.x 叢集。

# 核心原理與 Repository 驗證機制

## 什麼是 Repository Verify？

在執行快照備份時，**叢集中的每一台 Master 節點與 Data 節點都必須能夠直接讀寫儲存庫**：
- Master 節點負責寫入快照元數據（Metadata）。
- Data 節點負責平行上傳自己承載的分片 Lucene 段檔案。

```text
驗證 API: POST _snapshot/<repo>/_verify
       ↓
Master 指示所有節點在儲存庫寫入並讀回一個暫存測試檔案
       ↓
┌─────────────────────────────────────────────────────────────┐
│ 成功：所有節點回報 Verification OK                           │
│ 失敗：某台節點報錯（如 Permission Denied, 403 Forbidden）     │
└─────────────────────────────────────────────────────────────┘
```

## 常見失敗原因

1. **公有雲權限或 Token 缺失（S3/GCS）**：
   - 某台新擴容的節點，其 Keystore 中漏掉了 S3 Access Key / GCS Service Account Key。
2. **NFS / 共用檔案系統掛載不一致**：
   - 使用 `fs` 儲存庫時，某台伺服器的 NFS 掛載點失效，或 Linux 使用者 `elasticsearch`（UID）缺乏寫入權限。

# 報告指標解讀指引

在 `check.html` 報告中，請檢視以下資訊：

| 欄位名稱 | 範例值 | 診斷意涵 |
|---|---|---|
| `repository_name` | `backup_gcs` | 儲存庫名稱 |
| `verification_status` | `FAILED` | 驗證失敗告警 |
| `failed_nodes` | `["es-data-04"]` | 無法存取儲存庫的特定節點 |

# 業務影響與技術說明建議

## 說明範例：新節點無法存取快照儲存庫

> **技術說明範例**：
> 「雲端維運團隊您好，報告中顯示快照儲存庫 `backup_s3` 驗證失敗，其中新加入的 `es-data-04` 無法正常寫入。
> 
> 經排查，是因為該台新節點在建立時，未正確綁定 AWS S3 的 IAM 存取角色（或 Keystore 漏設定）。
> 
> 由於快照需要所有節點各自上傳分片資料，若單一節點缺乏存取權限，後續整座叢集的快照將持續失敗。
> 
> 我們只需為 `es-data-04` 補齊 S3 存取權限，儲存庫即可恢復健康。」

# 現場排查與安全處置 SOP

## 階段一：現場唯讀佐證（執行儲存庫驗證）

```http
POST /_snapshot/<REPOSITORY_NAME>/_verify
```

檢視回傳結果，確認是哪一台節點拋出異常。

## 階段二：整改修復方案

### 狀況 A：若是 GCS / S3 雲端儲存
登入報錯節點，檢查並重新載入 Keystore 安全憑證：

```bash
# 重新載入叢集 Keystore 設定（無需重啟節點）
POST /_nodes/reload_secure_settings
```

### 狀況 B：若是 NFS 檔案系統
檢查掛載點讀寫權限：

```bash
sudo -u elasticsearch touch /mnt/es_backup/test.tmp && rm /mnt/es_backup/test.tmp
```

## 階段三：變更後驗證

- [ ] 執行 `POST /_snapshot/<REPOSITORY_NAME>/_verify` 確認回傳包含所有節點且無報錯
- [ ] 重新執行 `elk-diagnostics check` 確認 `repository_verification` 轉為 `PASS`

# 附錄 <!-- appendix -->

## 參考文件

- [Elasticsearch 官方文件：Verify snapshot repository API](https://www.elastic.co/docs/reference/elasticsearch/rest-apis/verify-snapshot-repo-api)
- 專案內部規格書：`docs/內部/規格/資料規格.md`
