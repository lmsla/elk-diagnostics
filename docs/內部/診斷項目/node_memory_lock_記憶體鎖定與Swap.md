---
title: "ELK 診斷手札：Memory lock / swap 設定 — 記憶體鎖定原理、換頁停頓與關閉 Swap SOP"
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
| 2026/08/23 | 1.0 | 初版：mlockall 鎖定原理、Linux Swap 對 JVM GC 停頓的致命影響與配置指南 | 診斷工具架構組 |

# 文件說明

## 報告診斷卡對照

| 屬性 | 內容 |
|---|---|
| 報告分類區塊 | 節點環境（Node Context） |
| 診斷卡中文名稱 | Memory lock / swap 設定 |
| 診斷卡 ID | `node_memory_lock` |
| 典型嚴重度 | `WARNING` / `CRITICAL` |
| 觸發關鍵特徵 | `bootstrap.memory_lock: false` 且系統存在 Swap 空間，存在 JVM 記憶體被作業系統換頁至硬碟的風險 |

## 文件目的

本手札用於協助第一線 ELK 維運工程師與顧問在巡檢報告發現記憶體鎖定未啟用時，向客戶解釋為什麼 Java 虛擬機嚴禁被作業系統 Swapping，並指導配置 `bootstrap.memory_lock: true` 與 Linux 系統層級限制。

## 適用範圍

本手札適用於所有運行在 Linux / 容器實體機上之 Elasticsearch 7.x 及 8.x 節點。

# 核心原理與 Swap 危害

## 為什麼 Elasticsearch 必須關閉 Swap 或鎖定記憶體？

Linux 作業系統在記憶體吃緊時，會自動將較少存取的記憶體頁面（Page）移出 RAM，寫入硬碟的 **Swap 分割區（交換空間）**。

這對一般應用程式是保護機制，但**對 Java 虛擬機（JVM）是毀滅性的災難**：

```text
JVM 執行垃圾回收（Garbage Collection）
       ↓
GC 需要遍歷整座 Heap 中的數百萬個物件
       ↓
┌─────────────────────────────────────────────────────────────┐
│ 若部分 Heap 被換頁（Swap Out）到硬碟：                        │
│ 原本 10 毫秒的記憶體掃描，變成需要等硬碟讀取（Disk I/O）       │
│ GC 停頓時間被放大 1,000 倍（從 10ms 暴增至 30 秒甚至數分鐘！） │
└─────────────────────────────────────────────────────────────┘
       ↓
【連鎖災難】：節點無法在 30 秒內回應心跳 ping → Master 判定節點死亡 → 引發選主與分片搬遷風暴！
```

## 雙重保護機制

1. **`bootstrap.memory_lock: true`**：
   - 呼叫 Linux 系統呼叫 `mlockall()`，強制將 JVM 記憶體空間常駐在實體 RAM 中，嚴禁作業系統換頁。
2. **作業系統層級關閉 Swap**：
   - 永久停用 swap 分割區，或設定 `vm.swappiness = 1`。

# 報告指標解讀指引

在 `check.html` 報告中，請檢視以下資訊：

| 欄位名稱 | 期望安全值 | 診斷意涵 | 處置建議 |
|---|---|---|---|
| `mlockall` | `true` | JVM 記憶體是否成功鎖定 | 若為 false 需配置 |
| `swap_total` | `0 B` | 實體機是否啟用 Swap | 建議停用 Swap |
| `swap_used` | `0 B` | 當前已被換頁的空間 | 若 >0 代表效能已受損 |

# 業務影響與技術說明建議

## 說明要點與原則

- **用「硬碟讀取比記憶體慢 10 萬倍」建立危機感**：向維運人員說明 Swap 換頁會把 JVM 的記憶體操作變成硬碟讀取。
- **強調穩定性勝於一切**：說明即使伺服器因為記憶體不足直接 Crash（OOM），也比陷入數分鐘的「假死/慢死」更容易快速重啟恢復。

## 說明範例：對客戶 Linux 維運主管

> **技術說明範例**：
> 「系統維運主管您好，報告中顯示節點未啟用 `bootstrap.memory_lock`，且主機開啟了 8GB 的 Swap 交換空間。
> 
> 在大數據與 Java 系統中，Swap 是引發節點莫名失聯（Unresponsive）的頭號殺手。一旦作業系統將部分 JVM 記憶體寫入硬碟，垃圾回收器在掃描時就會被硬碟 I/O 卡死，導致原本只需幾毫秒的操作延遲放大到幾十秒，進而引發整座叢集的選主混亂。
> 
> 官方的強制最佳實踐是：**在主機層級關閉 Swap，並在 Elasticsearch 設定中開啟記憶體鎖定**。這能保證 JVM 永遠在純實體記憶體中高速運行，消除 90% 以上的非預期心跳超時災難。」

# 現場排查與安全處置 SOP

## 階段一：現場唯讀佐證

1. 檢查各節點 memory lock 是否生效：

```http
GET /_nodes/process?filter_path=nodes.*.name,nodes.*.process.mlockall
```

2. 檢查各節點目前 Swap 使用量：

```http
GET /_nodes/os?filter_path=nodes.*.name,nodes.*.os.swap
```

## 階段二：整改修復方案

### 步驟 1：配置 Elasticsearch 啟用記憶體鎖定

在 `elasticsearch.yml` 中加入：

```yaml
bootstrap.memory_lock: true
```

### 步驟 2：配置 Linux 系統允許記憶體鎖定（systemd）

編輯 `/etc/systemd/system/elasticsearch.service.d/override.conf`（或 `/lib/systemd/system/elasticsearch.service`）：

```ini
[Service]
LimitMEMLOCK=infinity
```

執行 `systemctl daemon-reload`。

### 步驟 3：在主機層級關閉 Swap（最乾淨方案）

```bash
# 暫時立即關閉
sudo swapoff -a

# 永久關閉：編輯 /etc/fstab，註解掉所有包含 swap 的行
```

若因企業政策無法關閉 Swap，將核心換頁傾向調至最低：

```bash
# /etc/sysctl.conf
vm.swappiness = 1
```

## 階段三：變更後驗證

- [ ] 執行 `GET /_nodes/process` 確認各節點 `mlockall` 均為 `true`
- [ ] 重新執行 `elk-diagnostics check` 確認 `node_memory_lock` 轉為 `PASS`

# 常見誤區與風險提示

- [ ] **誤區一：只改 `elasticsearch.yml` 卻沒給 OS `LimitMEMLOCK` 權限**：
  若沒有配置 `LimitMEMLOCK=infinity`，Elasticsearch 啟動時會因無法取得 `mlockall` 權限而直接報錯退出（Bootstrap Check 失敗）。

# 附錄 <!-- appendix -->

## 參考文件

- [Elasticsearch 官方文件：Disable swapping](https://www.elastic.co/docs/reference/elasticsearch/configuration-reference/advanced-configuration#disable-swapping)
- 專案內部規格書：`docs/內部/規格/節點環境規格.md`
