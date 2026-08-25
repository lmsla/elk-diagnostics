---
title: "Cgroup 記憶體壓力 — 容器記憶體限制、Page Cache 佔用與 OOM Killer 防範"
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
| 2026/08/23 | 1.0 | 初版：Cgroup Memory 統計機制、Page Cache 與 OOM Killer 臨界點判讀 | 診斷工具架構組 |

# 文件說明

## 報告診斷卡對照

| 屬性 | 內容 |
|---|---|
| 報告分類區塊 | 節點環境（Node Context） |
| 診斷卡中文名稱 | Cgroup 記憶體壓力 |
| 診斷卡 ID | `node_cgroup_memory_pressure` |
| 典型嚴重度 | `INFO` / `WARNING`（使用率 > 90%） |
| 觸發關鍵特徵 | `_nodes/stats/os` 顯示容器的 `cgroup.memory.usage_in_bytes` 接近 `limit_in_bytes` |

## 文件目的

本手札用於協助第一線 ELK 維運工程師與顧問在巡檢報告發現容器記憶體壓力偏高時，向客戶區分「正常的 Linux Page Cache 佔用」與「真正的 OOM 崩潰風險」，並指導正確配置 Kubernetes 的 `resources.limits.memory`。

## 適用範圍

本手札適用於所有運行在 Kubernetes（ECK、Helm）或 Docker 容器中之 Elasticsearch 7.x 及 8.x 節點。

# 核心原理與 Cgroup 記憶體機制

## 為什麼容器顯示記憶體使用率常年 90% 以上？

在 Kubernetes 中，許多維運人員看到 Pod 記憶體使用率高達 90%～95% 就驚慌失措。

這是因為 **Linux Cgroup 的 `usage_in_bytes` 計算方式包含了「檔案快取（Page Cache / Inactive File）」**：

```text
Cgroup Memory Usage = JVM Heap + JVM Native 記憶體 + Page Cache (Lucene 檔案快取)
```

1. **Page Cache 是可回收的**：
   - 當 JVM 需要更多實體記憶體時，Linux 核心會**瞬間自動釋放 Page Cache**，不會引發 OOM。
   - 因此，單純看到 Cgroup 記憶體達 90% 屬於 Linux 正常的高效快取利用。
2. **真正的 OOM Killer 觸發條件（Resident Set Size 超限）**：
   - 只有當 **`JVM Heap + JVM Off-heap (Netty, Lucene Direct Memory)` 本身就大於 `limits.memory`** 時，Linux 才會觸發 OOM Killer（退出代碼 Exit Code 137）強殺 Pod！

## 容器環境黃金配置公式

在 Kubernetes 部署 Elasticsearch 時，**容器的 Memory Limit 必須為 JVM Heap 的 2 倍**：

```text
┌─────────────────────────────────────────────────────────────┐
│ 推薦配置黃金公式：                                          │
│   resources.requests.memory = 2 * JVM Heap                  │
│   resources.limits.memory   = 2 * JVM Heap                  │
│ 例如：JVM Heap 設為 16Gi，則 Pod Memory Limit 必須設為 32Gi！ │
└─────────────────────────────────────────────────────────────┘
```

# 報告指標解讀指引

在 `check.html` 報告中，請檢視以下數值：

| 欄位名稱 | 警戒門檻 | 診斷意涵 |
|---|---|---|
| `cgroup_memory_used_pct` | > 90% | 容器記憶體佔用比例（含快取） |
| `cgroup_limit_bytes` | 容器配額 | 容器設定的記憶體上限 |

判讀原則：
- 單次快照標記為 `WARNING` 作為預警，不直接判定為 Critical，需結合是否曾發生 Pod Restart（OOMKilled）綜合評估。

# 業務影響與技術說明建議

## 說明要點與原則

- **科普 Page Cache 機制**：解釋 Linux 的哲學是「沒用到的記憶體就是浪費」，快取佔用 90% 是常態。
- **檢查容器 Heap 與 Limit 比例**：指出若 Limit 設得太貼近 Heap（如 Heap 16G 但 Limit 只給 18G），隨時會被 OOM 殺死。

## 說明範例：解釋 K8s 容器記憶體佔用

> **技術說明範例**：
> 「K8s 維運主管您好，報告中顯示 Elasticsearch Pod 的記憶體使用率達到了 92%。
> 
> 請不用過度擔心，在 Linux 容器架構中，這 92% 的大部分空間是被用作 Lucene 的硬碟讀取加速快取（Page Cache），當系統需要新記憶體時會自動秒級釋放。
> 
> 但我們需要特別注意配置比例：目前 Pod Limit 為 20GB，而 JVM Heap 給了 16GB。中間只有 4GB 的緩衝空間，一旦 Netty 網路請求或 Lucene 堆外記憶體突增，就可能觸發 K8s 的 OOM Killer。
> 
> 我們建議將 Pod 的 Memory Limit 放大至 32GB（維持 Heap:Limit = 1:2 的標準黃金比例），給系統充足的快取與緩衝空間，徹底消除 Pod 崩潰重啟風險。」

# 現場排查與安全處置 SOP

## 階段一：現場唯讀佐證（檢查是否曾被 OOM 殺死）

在 K8s 叢集中檢查 Pod 歷史終止狀態：

```bash
kubectl describe pod <ES_POD_NAME> -n <NAMESPACE> | grep -E "OOMKilled|Exit Code"
```

## 階段二：整改修復方案（調整 YAML 配置）

修改 StatefulSet 或 ECK Elasticsearch 配置：

```yaml
spec:
  containers:
  - name: elasticsearch
    resources:
      requests:
        memory: "32Gi"
      limits:
        memory: "32Gi"
    env:
    - name: ES_JAVA_OPTS
      value: "-Xms16g -Xmx16g"
```

## 階段三：變更後驗證

- [ ] 執行 `kubectl get pods -n <NAMESPACE>` 確認 Pod 穩定運行零重啟（Restarts = 0）
- [ ] 重新執行 `elk-diagnostics check` 產出最新報告

# 附錄 <!-- appendix -->

## 參考文件

- [Elasticsearch 官方文件：Operating system settings (Cgroup)](https://www.elastic.co/docs/reference/elasticsearch/configuration-reference/advanced-configuration#os-settings)
- 專案內部規格書：`docs/內部/規格/節點環境規格.md`
