# spec-health-report — `_health_report` 解析（A/B 類，19 條）

**實作位置**：`collector/health_report.go`（採集+解析）＋ `cluster/capacity/management/snapshot` analyzer（各自解讀）。

## 通用採集流程

```
GET /            → 取 version.number；< 8.4 則本規格全數改走各條 fallback API
GET /_health_report
```

`_health_report` 回傳頂層 `status` 與一組 `indicators`，每個 indicator 結構：

| 欄位 | 用途 |
|---|---|
| `status` | `green` / `yellow` / `red` / `unknown` |
| `symptom` | 一句話症狀 |
| `diagnosis[]` | 官方根因與處置（`cause` / `action` / `help_url`） |
| `impacts[]` | 受影響的功能與嚴重度 |

**通用判定**：`green`→✅ ｜ `yellow`→⚠️ ｜ `red`→❌ ｜ `unknown`→⚠️ 並標「無法判定，建議檢查該子系統」。
**通用輸出**：將 indicator 的 `symptom` / `diagnosis.cause` / `impacts` 轉為繁中呈現，附 `diagnosis.action` 與 `help_url`。A 類到此即完成；B 類在此基礎上，若 indicator 為非 green，再補打 fallback API 取得更細節（見各條）。

> 通用限制：所有結論為單次快照；`master_is_stable` 等需時間序列者標「需額外條件」。

---

## indicator → 項目對照與各條規格

### `shards_availability` → #1, #2, #21（A）；#19, #20, #37（B）

- **目標**：偵測未分配 primary/replica 與其根因。
- **判定**：indicator `red`＝有未分配 primary（❌，已確認資料不可用）；`yellow`＝僅 replica 未分配（⚠️）；`green`＝✅。
- **B 類加深（非 green 時）**：
  - #2/#37：`GET _cluster/allocation/explain {index,shard,primary}`，取 `deciders[].decision=NO` 的 `explanation` 做根因分類。
  - #19/#20：`GET _cluster/settings` / `GET <index>/_settings`，檢查 `cluster.routing.allocation.enable` 與 allocation filtering 是否阻擋。
- **建議**：依 decider 對症（修 allocation 設定 / 復原 `allocation.enable` / 調 replica / `POST _cluster/reroute?retry_failed`）。
- **限制**：未分配 shard 數可能極大，allocation/explain 須設上限（預設前 20，`rules` 可調）逐一查，其餘僅統計。
- **官方文件**：https://www.elastic.co/docs/troubleshoot/elasticsearch/red-yellow-cluster-status ／ https://www.elastic.co/docs/troubleshoot/elasticsearch/diagnose-unassigned-shards ／ https://www.elastic.co/docs/troubleshoot/elasticsearch/cluster-allocation-api-examples
- `tested_versions`: []

### `disk` → #3, #4, #14（A）

- **目標**：偵測磁碟空間造成的健康問題（含 index 被設唯讀）。
- **判定**：indicator `red`＝flood-stage，index 唯讀、寫入受阻（❌）；`yellow`＝接近水位（⚠️）；`green`＝✅。
- **fallback / 加深**：`GET _nodes/stats/fs` 算各節點 `used_percent=(total-available)/total`；`GET _cluster/settings?...&filter_path=*.disk.watermark*` 取**實際生效**水位（**勿硬編 85/90/95**，可被覆寫，另有 `max_headroom` 與 frozen tier 變體）。#14 依 `roles` 區分 master/other 節點。
- **建議**：擴容（加節點/擴磁碟）或降用量（刪 index / 調 replica / forcemerge / shrink / ILM）；臨時可調高水位並解除 `read_only_allow_delete`，但有觸 100% 風險。
- **官方文件**：https://www.elastic.co/docs/troubleshoot/elasticsearch/fix-watermark-errors ／ https://www.elastic.co/docs/troubleshoot/elasticsearch/fix-data-node-out-of-disk
- `tested_versions`: []

### `shards_capacity` → #10, #22, #23（A）

- **目標**：偵測 shard 數量是否逼近 index 層或 node 層上限。
- **判定**：indicator 非 green ＝接近/超過 `cluster.max_shards_per_node`（含 frozen）或 index 層上限（⚠️/❌ 依 indicator）。
- **fallback / 加深**：`GET _cluster/settings`（`cluster.max_shards_per_node`）、`GET <index>/_settings`（`index.number_of_shards`、`routing.allocation.total_shards_per_node`）、`GET _cat/shards`。
- **建議**：調高上限（暫時）、減少 shard 數、合併小 index、套用 ILM rollover。
- **官方文件**：https://www.elastic.co/docs/troubleshoot/elasticsearch/troubleshooting-shards-capacity-issues ／ https://www.elastic.co/docs/troubleshoot/elasticsearch/increase-shard-limit ／ https://www.elastic.co/docs/troubleshoot/elasticsearch/increase-cluster-shard-limit
- `tested_versions`: []

### `ilm` → #5（A）；#25（B）

- **目標**：ILM 服務狀態與 index 執行錯誤。
- **判定**：indicator `red`＝ILM 停止或有 index 卡 `ERROR`（❌）；`yellow`＝過渡/停止中（⚠️）；`green`＝✅。
- **fallback / 加深**：`GET _ilm/status`（`operation_mode`: RUNNING/STOPPING/STOPPED）；非 green 時 `GET <index>/_ilm/explain` 取 `step=ERROR` 的 `failed_step`、`step_info.{type,reason}`。#25：`_ilm/explain` 檢查 tier 遷移是否卡住。
- **根因（常見 ERROR）**：rollover alias 重複/未設/錯置、index 名不符 `^.*-\d+`、CircuitBreaking、磁碟 watermark、`Policy does not exist`、tier 不符。
- **建議**：依 `step_info.reason` 修正後 `POST <index>/_ilm/retry`；維護後若服務被停 `POST _ilm/start`。
- **限制**：停 ILM 會連帶暫停 SLM，報告須表達此因果，勿把連帶 SLM 停用當獨立根因。
- **官方文件**：https://www.elastic.co/docs/troubleshoot/elasticsearch/start-ilm ／ https://www.elastic.co/docs/troubleshoot/elasticsearch/index-lifecycle-management-errors
- `tested_versions`: []

### `slm` → #15（A）

- **目標**：SLM 排程快照是否反覆失敗 / 服務是否停用。
- **判定**：indicator 非 green ＝近期 snapshot 失敗或 SLM 停用（⚠️/❌）。
- **fallback / 加深**：`GET _slm/status`、`GET _slm/policy`（檢查 `stats`、`last_failure`）。
- **建議**：查 `last_failure` 原因（多與 repository 或磁碟相關）；維護後 `POST _slm/start`。
- **官方文件**：https://www.elastic.co/docs/troubleshoot/elasticsearch/repeated-snapshot-failures
- `tested_versions`: []

### `repository_integrity` → #26（A）；#36（B）

- **目標**：snapshot repository 是否損壞/不可用；restore 狀態查詢。
- **判定**：indicator 非 green ＝repository 損壞/未知/無效（❌/⚠️）。
- **fallback / 加深**：`GET _snapshot/_status`、`GET _snapshot`（#36 唯讀查詢 restore 進度，**不執行 restore**）。
- **建議**：依損壞類型（corrupted/unknown/invalid）對應處置；多由多叢集寫同一 repository 造成。
- **官方文件**：https://www.elastic.co/docs/troubleshoot/elasticsearch/add-repository ／ https://www.elastic.co/docs/troubleshoot/elasticsearch/restore-from-snapshot
- `tested_versions`: []

### `master_is_stable` → #30（B）

- **目標**：是否有穩定的當選 master。
- **判定**：indicator `red`＝無穩定 master（❌）；`yellow`＝不穩定徵兆（⚠️）。
- **fallback / 加深**：`GET _cluster/health`、`GET _nodes`。
- **限制**：master 穩定性本質是時間序列問題，單次快照只能看當下；報告須標「需持續觀察」。
- **官方文件**：https://www.elastic.co/docs/troubleshoot/elasticsearch/troubleshooting-unstable-cluster
- `tested_versions`: []

### `data_stream_lifecycle` → #24（B）

- **目標**：data stream / tier 生命週期是否正常（含偏好 tier 缺節點）。
- **判定**：indicator 非 green ＝tier 配置或 DSL 問題（⚠️）。
- **fallback / 加深**：`GET _cat/nodes?h=node.role` 確認是否缺對應 tier 節點。
- **官方文件**：https://www.elastic.co/docs/troubleshoot/elasticsearch/add-tier ／ https://www.elastic.co/docs/troubleshoot/elasticsearch/troubleshoot-migrate-to-tiers
- `tested_versions`: []

---

## Phase 0 實測結果（8.14.3 / 9.0.0，2026-06）

實測 fixture：`dev/phase0/fixtures/`（es8/es9 各 healthy/unhealthy）。結論如下，據此調整 primary/fallback：

| indicator | 實測結論 | 對 A/B 的影響 |
|---|---|---|
| `shards_availability` | ✅ **顆粒度足夠**。diagnosis 帶 `cause`/`action`/`help_url`/`affected_resources.indices`（直接點出受影響 index），impacts 亦具體。 | #1,2,21 確認 primary=health_report 可用；逐 shard `allocation/explain` 確定為**選配加深**（需 decider 級細節時才打）。 |
| `ilm` | ⚠️ **會延遲**。植入必失敗的 ILM policy 後，indicator 仍為 green（ILM 輪詢未即時反映）。 | #5 **不可只信 indicator**：須一律補打 `_ilm/explain` 掃 `step=ERROR`，否則漏報剛壞掉的 ILM。primary 降為「indicator + 必要的 explain」。 |
| `disk` / `shards_capacity` / `slm` / `repository_integrity` | ❓ **未驗**。本次只製造 shard 未分配，這四項全 green，未觀察到非 green 時的 diagnosis。 | 維持 A 類假設，但**標記待驗**：實作到該條時須先以對應 seed（塞磁碟 / 超 shard 上限 / 壞 repo）補測。 |
| `master_is_stable` / `data_stream_lifecycle` | ❓ 未驗（同上）。 | 維持現狀。 |

**版本差異**：9.0.0 多出 `file_settings` indicator（8.14.3 無）。
→ **解析器鐵則**：以「遍歷 indicators map」處理，**容忍未知/新增 indicator**（未知者照通用規則呈現 status，不可因欄位沒見過而崩潰或漏掉）。

**淨結論**：health_report-first 架構成立；唯 `ilm` 必須搭 `_ilm/explain`，且 disk/capacity/slm/repository 的顆粒度待各自實作時補測。
