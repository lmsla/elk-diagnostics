---
title: "Voting configuration exclusions — 節點維護殘留與 Quorum 穩定性"
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
| 2026/08/23 | 1.0 | 初版：Raft 投票機制、Voting Exclusions 生命週期與殘留清理 SOP | 診斷工具架構組 |

# 文件說明

## 報告診斷卡對照

| 屬性 | 內容 |
|---|---|
| 報告分類區塊 | 叢集（Cluster） |
| 診斷卡中文名稱 | Voting configuration exclusions |
| 診斷卡 ID | `voting_config_exclusions` |
| 典型嚴重度 | `WARNING` / `CRITICAL` |
| 觸發關鍵特徵 | 叢集狀態中存在未清除的投票排除清單（Voting Configuration Exclusions） |

## 文件目的

本手札用於協助第一線 ELK 維運工程師與顧問在巡檢報告發現 `voting_config_exclusions` 警告時，評估叢集 Master 節點群的共識機制健康度，並指導客戶安全清理維護殘留之排除清單，防止 Quorum 容錯能力下降。

## 適用範圍

本手札適用於採用 Raft 協調協議之 Elasticsearch 7.x 與 8.x 叢集。

# 核心原理與 Raft 投票機制

## 什麼是 Voting Configuration？

在 Elasticsearch 7.x 之後，叢集採用基於 Raft 的分散式共識協議。只有具備 `master` 角色的節點（Master-eligible nodes）有資格參與叢集狀態變更的投票。

為了確保叢集一致性並防止腦裂（Split-Brain），任何變更必須獲得 **法定多數派（Quorum）** 的同意：

```text
Quorum 門檻 = (Master 節點總數 / 2) + 1
例如 3 台 Master 節點時，Quorum = 2（允許掛掉 1 台）
```

## 為什麼需要 Voting Configuration Exclusions？

當維運人員需要**永久縮容或替換某台 Master 節點**時，若直接暴力關機，叢集仍會將該離線節點計入 Quorum 分母，導致叢集容錯度瞬間降為 0（再掛一台叢集即癱瘓）。

因此，官方標準流程要求在下線前，先呼叫 API 將該節點加入 **Voting Configuration Exclusions（投票排除名單）**：

```text
3 台 Master (A, B, C)
  ↓ 欲下線 C，先登記 Exclusion
Voting Exclusion: [ C ]
  ↓ 叢集自動將 Quorum 基礎收斂為 A, B
安全關閉 C，叢集穩定運行
```

## 殘留排除名單的致命風險

很多維運團隊在替換完新節點或維護結束後，**忘記呼叫清理 API**。

```text
【殘留名單的隱患】
舊節點 C 雖然下線了，新節點 D 上線加入
但叢集仍保有「排除 C」的設定，若後續又登記了其他節點（上限預設 16 個）
累積過多殘留會使 Cluster State 膨脹，並可能在新舊選主切換時發生非預期鎖定！
```

# 報告指標解讀指引

在 `check.html` 報告中，請檢視以下資訊：

| 欄位名稱 | 範例值 | 診斷意涵 |
|---|---|---|
| `exclusions_count` | `2` | 目前被排除在投票名單外的節點數量 |
| `excluded_nodes` | `["es-master-old-01"]` | 具體被排除的節點名稱或節點 ID |

# 業務影響與技術說明建議

## 說明要點與原則

- **肯定客戶的安全下線意識**：說明這通常是維運人員按正規流程下線節點留下的紀錄。
- **提醒收尾清潔重要性**：說明維護就像手術，換完零件後需要把「暫時固定鉗（Exclusions）」拆除，叢集才能恢復最佳彈性。

## 說明範例：維護後殘留排除名單

> **技術說明範例**：
> 「客戶維運團隊您好，報告中偵測到 `es-master-old-01` 依然停留在叢集的投票排除名單中。
> 
> 這通常代表過去在進行 Master 節點升級或縮容時，同仁依照標準規範登記了下線排除，但在新節點正常接管後，漏掉了最後一步的收尾清理指令。
> 
> 雖然目前叢集正常運行，但長期保留排除紀錄會影響未來 Master 節點的動態仲裁彈性。我們只需執行一行唯讀確認與清理指令，即可讓叢集回歸最健康的狀態。」

# 現場排查與安全處置 SOP

## 階段一：現場唯讀佐證

1. 檢查目前生效的投票排除名單：

```http
GET /_cluster/state?filter_path=metadata.cluster_coordination.voting_config_exclusions
```

2. 確認目前現役的 Master 節點清單：

```http
GET /_cat/nodes?v&h=name,node.role,master
```

## 階段二：整改修復方案（安全清理）

確認現役 Master 節點數量正常（建議 3 或 5 台）且叢集 Green 後，呼叫 API 清理排除名單：

```http
DELETE /_cluster/voting_config_exclusions?wait_for_removal=false
```

## 階段三：變更後驗證

- [ ] 執行 `GET /_cluster/state?filter_path=metadata.cluster_coordination.voting_config_exclusions` 確認回傳清單為空 `[]`
- [ ] 執行 `GET /_cat/nodes` 確認現役 Master 節點狀態正常
- [ ] 重新執行 `elk-diagnostics check` 確認 `voting_config_exclusions` 轉為 `PASS`

# 常見誤區與風險提示

- [ ] **誤區一：在 Master 節點不足時盲目清理**：
  若目前 Master 節點只有 2 台且其中 1 台不穩，請勿在沒有確認 Quorum 安全的情況下盲目清理。
- [ ] **誤區二：一次性關閉超過半數 Master 節點**：
  任何時候都不可在未登記 Exclusion 的情況下同時關閉超過一半的 Master 節點，否則叢集將立即失去 Quorum 並徹底停擺。

# 附錄 <!-- appendix -->

## 參考文件

- [Elasticsearch 官方文件：Voting configuration exclusions API](https://www.elastic.co/docs/reference/elasticsearch/rest-apis/voting-config-exclusions)
- [Elasticsearch 官方文件：Cluster coordination troubleshooting](https://www.elastic.co/docs/troubleshoot/elasticsearch/troubleshooting-unstable-cluster)
- 專案內部規格書：`docs/內部/規格/擴充健檢規格.md`（ES-GAP-12）
