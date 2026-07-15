# 驗證狀態追蹤（Verification Tracker）

**這份文件回答一個問題：哪些診斷「被證明過真的會抓到問題」，哪些只是「跑起來沒出錯」。**

與 [`PROGRESS.md`](./PROGRESS.md) 的分工：PROGRESS 追蹤「有沒有實作」，本文追蹤「實作出來的東西對不對」。
兩者刻意分開，因為 2026-07-15 的真機驗證證明這是兩件完全不同的事。

---

## 1. 為什麼需要這份文件（2026-07-15 的發現）

把工具指向真實叢集跑了幾分鐘，找出 **6 條有實際缺陷的診斷**，其中 **4 條在結構上永遠不可能回報異常**：

| # | 診斷 | 實際行為 |
|---|---|---|
| 19 | Data allocation blocked | 不論 `cluster.routing.allocation.enable` 實際為何，永遠回報「無封鎖」 |
| 20 | Index allocation blocked | 同上，永遠回報「正常」 |
| 31 | Search slow log | 不論實際有沒有開，永遠回報「未開啟」 |
| 33 | Monitoring | 永遠讀不到設定值 |
| 11 | Mapping explosion | 任何裝了 Kibana 的叢集必定誤報 CRITICAL |
| 32 | Data corruption | 同源缺陷（系統 index 短暫 red 會被誤判客戶資料毀損） |

### 這批 bug 為什麼測試沒抓到

**因為單元測試驗的是「我以為 ES 會回什麼」，不是「ES 真的回什麼」。** 假設錯了，測試就跟著錯，測得再綠也只是把錯誤假設鎖得更牢。

具體來說 #19/#20/#31/#33 是兩層 bug 疊加：

1. `filter_path=**.a.b.c` 假設 JSON 是巢狀結構，但 `flat_settings=true` 回傳的 key 本身就含字面上的點（`"cluster.routing.allocation.enable"`）。兩者語意衝突，`filter_path` 永遠比對不到，回應直接是 `{}`。
2. 拿掉 `filter_path` 後才浮現第二層：`defaults` 區塊混雜非字串型別（真機實測 58 個這類 key，如 `network.host` 是陣列、`xpack.security.user` 是 null）。原本整段硬解 `map[string]string` 會直接失敗，而錯誤被靜默吞掉、fallback 成預設值——**明明 `transient` 層有正確的值，卻讀不到**。

**golden test 也沒抓到，而且情況更糟**：Phase 0 錄的 fixture 沒有這些端點，測試裡它們 404 之後被當成「預期跳過」。也就是說 golden test 一邊通過、一邊完全瞎掉。這是結構性的，不是漏寫。

### 對交付的意義

客戶是政府與金融。這批 bug 的失效模式不是「工具壞掉跳錯誤」（看得見、可補救），而是**「叢集有問題，工具說一切正常」**——這是顧問場景最壞的一種失敗。客戶事後自己發現問題而報告寫著綠燈，失去的不只是那條診斷的信任，是整份報告與顧問本人的信任。

---

## 2. 成熟度判定（2026-07-15）

| 面向 | 評估 |
|---|---|
| 架構 / 設計 | ✅ 成熟。health_report-first、A/B/C 分類、`requires_extra`／`unknown` 絕不當 pass 這些約定，都是這個場景該有的設計 |
| 功能完整度 | ✅ 完成。37 條診斷 + 5 條症狀樹 + JSON/HTML 報告 + cobra CLI + multi-arch 打包 |
| **正確性信心** | ❌ **低**。約 20 條診斷從未在真實叢集上被觀察到產出過非 pass 的結果 |
| 政府／金融交付就緒 | ❌ **尚未**。首次真機接觸即發現 4 條「永遠報綠燈」的診斷 |

**結論：不宜現在收斂 ES 功能。** 缺的不是功能，是「證明現有功能是對的」。
好消息是方法已經跑通（見 §4 造壓配方），剩下的是把它系統性做完，不是重新設計。

---

## 3. 驗證狀態總表

**驗證等級定義**：

- **✅ 觸發驗證**：真機刻意製造該故障，確認診斷正確報出異常。這是唯一有意義的驗證等級。
- **🟡 僅正常路徑**：真機跑過、回報 pass，且該 pass 是正確的——但「它到底抓不抓得到問題」未經證明。
- **❌ 未驗證 / 無法重現**。

> ⚠️ 注意：🟡 不等於安全。#19/#20/#31/#33 在被抓包前，全都停在 🟡（真機跑過、回報 pass、pass 看起來很合理），實際上它們永遠只會回 pass。

### 3.1 ✅ 已觸發驗證（12 條）

真機 es8=8.14.3，2026-07-15 造壓確認。

| # | 診斷 | 觸發方式 | 觀察到的結果 |
|---|---|---|---|
| 1, 2 | cluster_health / unassigned shards | 封鎖 allocation 後建 index | red → critical，root cause 正確點名 |
| 3, 4 | Watermark / data node out of disk | 水位壓到低於實際使用率 | red → critical，diagnosis/impacts 完整 |
| 5 | ILM stopped | `POST _ilm/stop` | critical + 正確建議 `POST _ilm/start` |
| 5 | ILM ERROR step | index 指向不存在的 policy | critical + 正確帶出 `step_info.reason` |
| 10, 23 | shards_capacity / per-node 超限 | `max_shards_per_node=20`（實際 28） | red → critical |
| 11 | Mapping explosion | 真實 data stream 塞 1017 欄位 | critical，且系統 index 正確排除 |
| 19 | Data allocation blocked | `allocation.enable=none` | critical（**修正後**才正確） |
| 26 | Broken snapshot repository | `rm -rf` repo 底層目錄後觸發寫入 | yellow → warning |
| 31 | Search slow log | 建 index 設 threshold ／ 移除 | 已開啟／未開啟兩態皆正確偵測 |
| 37 | Cluster allocation 引導 | 同 #19 | warning + 真實 decider 解析正確 |

### 3.2 🟡 僅驗證正常路徑（20 條）— **待辦主體**

依「要驗證需要什麼」分組，方便批次進行。

#### Group A｜單節點即可，設定切換就能觸發（優先，成本最低）

| # | 診斷 | 建議觸發方式 | 目前狀態 |
|---|---|---|---|
| 20 | Index allocation blocked | 對單一 index 設 `index.routing.allocation.enable=none` | 修正後仍只驗過 pass 分支 |
| 21 | Not enough nodes for replica | 單節點建 `number_of_replicas=1` 的 index | 未觀察到 |
| 22 | Shards per index exceeded | 設 index 層 `total_shards_per_node` 上限 | 只驗過 node 層（#23） |
| 27 | Watcher | `POST _watcher/_stop` | 真機為「運作中」 |
| 28 | Transforms | 建一個會 fail 的 transform | 真機為「未使用（不適用）」 |
| 32 | Data corruption | 製造 red index（如 #19 的 blocked-test） | 真機為「無 red index」 |
| 33 | Monitoring | 設 `xpack.monitoring.collection.enabled=true` | 已驗「未啟用」態讀值正確；啟用態未驗 |
| 34 | Upgrade deprecations | 加一個 deprecated 設定 | 真機為 0 筆 |
| 35 | Remote clusters | 設一個連不到的 remote cluster | 真機為「未設定（不適用）」 |
| 13 | Ingest pipeline errors | 建含必失敗 processor 的 pipeline 並灌資料 | 真機失敗率 0 |
| 24 | Preferred data tier missing | 建 index 指定叢集沒有的 tier | 單節點含全部 tier role |

#### Group B｜需要多節點叢集（`docker-compose.yml` 需擴充 3 節點）

| # | 診斷 | 為什麼需要多節點 |
|---|---|---|
| 14 | Master/Other nodes out of disk | 單節點同時是 master+data，無法區分角色 |
| 17 | Hot spotting | 需節點間資源分布不均才有比較基準 |
| 18 | Unbalanced cluster | 需 `shards.undesired > 0`，單節點無從搬移 |
| 30 | Unstable cluster | 結構性 warning 已驗；**真實選舉不穩定事件**需多節點 |
| 25 | Incomplete migration to tiers | 需多 tier 節點 + 進行中的遷移 |

#### Group C｜需要真實負載工具（esrally 或等效）

| # | 診斷 | 備註 |
|---|---|---|
| 6 | Rejected requests | 需真實 thread pool 拒絕 |
| 7 | High JVM memory pressure | 需真實 heap 壓力 |
| 8 | Circuit breaker errors | 需真實跳閘 |
| 9 | High CPU + hot threads | 需真實 CPU 負載 |
| 12 | Task queue backlog | 需真實佇列積壓 |
| 16 | **Write bottleneck（因果鏈）** | **產品的差異化核心**，只驗過負向路徑；觸發路徑需低 CPU + 真實 write queue 積壓 + 低 allocated_processors 同時成立 |
| 36 | Restore from snapshot | 需進行中的還原（大 snapshot 才來得及觀察） |

### 3.3 ❌ 未解 / 無法重現

| # | 診斷 | 情況 |
|---|---|---|
| 15 | Snapshot policy failures (SLM) | 手動 `_slm/policy/_execute` 觸發 **4 次真實失敗**，`slm` indicator 仍為 green。ES 似乎把「repo 已損壞」造成的失敗歸類到 `repository_integrity`（該項確實正確跳黃燈）而非 `slm`。**觸發 `slm` 本身變色的確切條件未知，需查官方原始碼或文件確認** |

---

## 4. 症狀樹與反向觸發

| 項目 | 狀態 | 備註 |
|---|---|---|
| `red-cluster` | ✅ 觸發驗證 | 經 disk red 與 allocation 封鎖兩路徑皆確認 |
| `ilm-stuck` | ✅ 觸發驗證 | ILM stopped 與 ERROR step 皆確認 |
| `ingest-lag` | 🟡 部分 | disk 環節有觸發；ingest／queue 環節未觸發（屬 Group C） |
| `high-heap` | ❌ | 全樹屬 Group C |
| `write-bottleneck` | ❌ | 屬 Group C |
| 反向觸發：ILM ERROR → `ilm-stuck` | ✅ 觸發驗證 | 真實 ERROR 時正確跳出提示 |
| 反向觸發：因果鏈 → `write-bottleneck` | ❌ | 屬 Group C |

---

## 5. 已知可重現的造壓配方

2026-07-15 實測有效，對 es8=8.14.3。**每個情境結束後務必復原**（本文所有配方皆已驗證可乾淨復原）。

```bash
# --- disk watermark → red ---
# 前提：查 GET _cat/allocation 取得實際使用率，水位設在其之下
PUT _cluster/settings {"transient":{
  "cluster.routing.allocation.disk.watermark.low":"10%",
  "cluster.routing.allocation.disk.watermark.high":"15%",
  "cluster.routing.allocation.disk.watermark.flood_stage":"20%"}}
# 復原：以上三項設為 null（health node 約 15 秒後回 green）
# 注意：enable_for_single_data_node 不可動態設定，單節點也不需要

# --- shards_capacity → red ---
# 前提：查 GET _cluster/health 的 active_shards，上限設在其之下
PUT _cluster/settings {"transient":{"cluster.max_shards_per_node":20}}

# --- repository_integrity → yellow ---
# 前提：docker-compose 需加 path.repo 環境變數，否則無法建 fs repo
#   - path.repo=/usr/share/elasticsearch/data/repo
PUT _snapshot/good_repo {"type":"fs","settings":{"location":"/usr/share/elasticsearch/data/repo/good"}}
PUT _snapshot/good_repo/snap1?wait_for_completion=true
# 從容器內破壞底層資料：docker exec <es8> sh -c "rm -rf /usr/share/elasticsearch/data/repo/good/*"
PUT _snapshot/good_repo/snap2?wait_for_completion=true   # 這步才會讓 ES 偵測到損壞
# 註：光是 rm -rf 後查 _health_report 仍是 green，必須有寫入嘗試才會偵測

# --- ILM stopped → critical ---
POST _ilm/stop        # 復原：POST _ilm/start

# --- ILM ERROR step → critical ---
PUT ilmerr-000001 {"settings":{
  "index.lifecycle.name":"nonexistent-policy-xyz",
  "index.lifecycle.rollover_alias":"ilmerr"}}
# 約 5 秒後 _ilm/explain 出現 step=ERROR

# --- cluster allocation blocked → critical（#19 + #37 + #1/#2）---
PUT _cluster/settings {"transient":{"cluster.routing.allocation.enable":"none"}}
PUT blocked-test {"settings":{"number_of_shards":1,"number_of_replicas":0}}

# --- mapping explosion → critical（#11，含 data stream 迴歸情境）---
# 送一筆含 1005+ 欄位的文件到 data stream，backing index 會是 .ds-* 開頭
POST logs-explosion2-default/_doc {"@timestamp":"...","field_0":0, ... ,"field_1004":1004}
```

**單一情境成本**：改設定 → 等 ES 重新評估（2–15 秒，視 indicator 而定）→ 跑 check（0.12 秒）→ 復原，約 **30 秒–1 分鐘**。
`check` 本身從來不是瓶頸，等 ES 內部健康輪詢才是。

---

## 6. 建議的收斂路徑

依投報率排序：

1. **Group A（11 條）** — 單節點設定切換即可，今天的配方直接沿用。預計 1 個 session 內可清完。
2. **重錄 golden fixture** — 從真機的健康／異常兩種狀態各錄一次，讓 golden test 真的覆蓋到端點，而不是靠 404 假裝通過。**這一步同時解決 golden test 目前給假信心的結構問題**，優先度應高於 Group B/C。
3. **Group B（5 條）** — `docker-compose.yml` 擴充 3 節點叢集，一次解決 hot spotting／unbalanced／master 穩定性／tier 遷移。
4. **Group C（7 條）** — 引入 esrally 或等效負載工具。**#16 write bottleneck 是產品差異化核心，值得單獨投資**。
5. **#15 SLM** — 需查官方原始碼確認 indicator 觸發條件。

**在 1–2 完成前，不建議對政府／金融客戶交付。**

---

## 7. 更新規則

- 每次真機驗證後更新本文，**只有「刻意觸發該故障並確認正確報出」才能標 ✅ 觸發驗證**。
- 「跑起來沒出錯」「回報 pass 且看起來合理」一律只能標 🟡。
- 發現新 bug 時，記錄**為什麼既有測試沒抓到**（§1 的模式），這比記錄 bug 本身更有價值。
