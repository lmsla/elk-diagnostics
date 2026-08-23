---
title: "ELK 診斷手札：Remote clusters 跨叢集連線 — CCS/CCR 連線健康、Proxy 模式與網路排錯"
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
| 2026/08/23 | 1.0 | 初版：跨叢集搜尋（CCS）與複製（CCR）連線架構、Sniff vs Proxy 模式與連線修復 | 診斷工具架構組 |

# 文件說明

## 報告診斷卡對照

| 屬性 | 內容 |
|---|---|
| 報告分類區塊 | 管理（Management） |
| 診斷卡中文名稱 | Remote clusters 跨叢集連線 |
| 診斷卡 ID | `remote_clusters` |
| 典型嚴重度 | `WARNING` / `CRITICAL` |
| 觸發關鍵特徵 | `_remote/info` 顯示某個註冊的遠端叢集（Remote Cluster）處於 `connected: false` 斷線狀態 |

## 文件目的

本手札用於協助第一線 ELK 維運工程師與顧問在巡檢報告發現遠端叢集斷線時，排查跨叢集搜尋（Cross-Cluster Search, CCS）與跨叢集複製（Cross-Cluster Replication, CCR）的網路、TLS 憑證與 Proxy 模式連線問題。

## 適用範圍

本手札適用於具有多叢集互聯架構之 Elasticsearch 7.x 及 8.x 叢集。

# 核心原理與 Remote Cluster 連線模式

## 兩大遠端連線模式

1. **Sniff 模式（預設）**：
   - 適用於直連網路環境。本機叢集連上遠端一個 Seed 節點後，會自動探測（Sniff）出遠端所有節點的 IP 並建立連線。
   - **痛點**：在跨雲、K8s 跨叢集或經過 NAT/防火牆時，Sniff 拿到的內部私有 IP 無法連通，導致斷線。
2. **Proxy 模式（現代跨網路架構首選）**：
   - 透過固定的 Load Balancer / Proxy 域名連線，所有流量均經由單一代理轉發，適合跨 VPC 或跨公有雲連線。

```text
本機叢集 (Local Cluster)
       ↓
┌───────────────────────────────────────────────┐
│ 模式 1：Sniff Mode (需能直連遠端所有節點 IP)     │
│ 模式 2：Proxy Mode (只需連通遠端單一 LB 域名)    │
└───────────────────────────────────────────────┘
       ↓
遠端叢集 (Remote Cluster)
```

## 常見斷線原因

1. **TLS / CA 憑證互信缺失**：
   - 遠端叢集更新了自簽 CA 憑證，但本機節點的 Truststore 未同步更新，TLS 握手直接失敗。
2. **跨機房防火牆 9300 埠被阻擋**：
   - 跨叢集通訊使用的是 Transport 協定（預設 9300 埠），而非 HTTP 9200 埠。

# 報告指標解讀指引

在 `check.html` 報告中，請檢視以下資訊：

| 欄位名稱 | 範例值 | 診斷意涵 |
|---|---|---|
| `cluster_alias` | `cluster_backup_dr` | 遠端叢集別名 |
| `connected` | `false` | 連線中斷告警！ |
| `mode` | `proxy` 或 `sniff` | 連線模式 |
| `seeds` | `["10.0.10.5:9300"]` | 遠端 Seed 節點清單 |

# 客戶溝通話術與情境模擬

## 話術範例：遠端備份叢集斷線

> **顧問說明範例**：
> 「資安與維運團隊您好，報告中顯示與異地備份叢集（`cluster_backup_dr`）的跨叢集連線目前處於中斷狀態（`connected: false`）。
> 
> 這會直接導致跨叢集搜尋（CCS）查詢失敗，且異地即時複製（CCR）將停止同步。
> 
> 經檢測，主要原因在於上週機房防火牆規則更新時，阻擋了跨機房的 9300 Transport 通訊埠。
> 
> 我們建議網路團隊開放跨叢集 9300 連線，或將連線模式切換為現代的 Proxy 模式，即可立即恢復異地高可用同步。」

# 現場排查與安全處置 SOP

## 階段一：現場唯讀佐證

```http
GET /_remote/info
```

檢視具體哪一個 cluster alias 斷線，確認 `num_nodes_connected: 0`。

## 階段二：整改修復方案（重新配置遠端連線）

### 方案 A：使用 Proxy 模式（跨雲推薦）

```json
PUT /_cluster/settings
{
  "persistent": {
    "cluster.remote.cluster_backup_dr.mode": "proxy",
    "cluster.remote.cluster_backup_dr.proxy_address": "dr-es.example.com:9300"
  }
}
```

### 方案 B：重設 Sniff 模式 Seed 節點

```json
PUT /_cluster/settings
{
  "persistent": {
    "cluster.remote.cluster_backup_dr.seeds": [
      "10.0.10.5:9300",
      "10.0.10.6:9300"
    ]
  }
}
```

## 階段三：變更後驗證

- [ ] 執行 `GET /_remote/info` 確認 `connected` 轉為 `true` 且 `num_nodes_connected > 0`
- [ ] 重新執行 `elk-diagnostics check` 確認 `remote_clusters` 轉為 `PASS`

# 附錄 <!-- appendix -->

## 參考文件

- [Elasticsearch 官方文件：Remote clusters](https://www.elastic.co/docs/reference/elasticsearch/cluster-management/remote-clusters)
- 專案內部規格書：`docs/內部/規格/管理功能規格.md`（#35）
