# Bundle 客戶流程人工驗證 Playbook

目的：在內部可丟棄叢集上，驗證客戶只用 `collect.sh` 採集資料，分析端再以 `elk-diagnostics` 離線診斷 bundle。

本文件只驗 Bundle 路線。直連模式請改用 [`VERIFICATION-LIVE-PLAYBOOK.md`](./VERIFICATION-LIVE-PLAYBOOK.md)。

## 1. 路線與隔離邊界

```text
階段一／採集端：Shell 觸發故障 → collect.sh 採集 bundle → Shell 復原 ES
                                      │
                                      └── 交接 bundle 目錄
階段二／分析端：                         elk-diagnostics check --from-bundle
```

- 階段一不得準備、檢查或執行 `elk-diagnostics` binary。
- 階段一完成並確認 ES 復原後，才可進入階段二。
- 階段二開新的 Terminal，不設定 ES host、帳密或 CA；binary 只能讀 bundle。
- 禁止在客戶或共用環境造壓；故障注入只用於內部測試叢集。
- `collect.sh` 是被驗物，只依賴 POSIX shell 與 curl；故障控制器使用 jq 不屬客戶依賴。
- Bundle 可能含 index、node、IP 與 mapping 欄位名稱，不得直接外傳。

---

# 階段一：只採集 Bundle

## 2. 準備採集端

從 repo 根目錄執行：

```bash
export ES_LABEL=es8
export ES_URL=https://localhost:9208
export ES_USER=elastic
export ES_PASSWORD=elk-diagnostics-test-only
export CA_CERT="$PWD/dev/phase0/certs/ca/ca.crt"

export COLLECT_SH="$PWD/collect.sh"
export FAULT_CMD="$PWD/dev/phase0/fault-scenarios.sh"
export BUNDLE_CASE_CMD="$PWD/dev/phase0/bundle-case.sh"
export EVIDENCE_ROOT="$PWD/bundle-playbook-${ES_LABEL}-$(date +%Y%m%d-%H%M%S)"
mkdir -p "$EVIDENCE_ROOT"

command -v curl jq >/dev/null
test -x "$COLLECT_SH"
test -x "$FAULT_CMD"
test -x "$BUNDLE_CASE_CMD"
test -f "$CA_CERT"
"$FAULT_CMD" baseline verify
```

這個階段不需要 `make build`、`TOOL_BIN` 或任何 binary 參數。所有採集資料都會放在 repo 根目錄下的單一 `bundle-playbook-*` 目錄，且已被 `.gitignore` 排除。

若同時啟動 Kibana，可用 Dev Tools／API 交叉觀察故障，但不要在 Stack Monitoring
點選 **Enable monitoring**。該操作會將
`xpack.monitoring.collection.enabled=true` 寫入 persistent cluster settings，
這正是 P13 的故障條件，會使基準線閘門拒絕後續案例。若已啟用，先執行：

```bash
"$FAULT_CMD" P13 restore
"$FAULT_CMD" baseline verify
```

改驗 ES 9 時開新的 Terminal，將前兩行改為：

```bash
export ES_LABEL=es9
export ES_URL=https://localhost:9209
```

## 3. 載入案例控制函式

控制器會以 `$EVIDENCE_ROOT/.bundle-case-active` 記錄尚未復原的案例，故分段指令可跨
Terminal 執行；重新開 Terminal 時需重新設定第 2 節的環境變數並再次載入下方函式，
但不會遺失 active case。

整段貼入採集端 Terminal：

```bash
bundle_case() {
  "$BUNDLE_CASE_CMD" "$@"
}

# 舊入口保留相容性；語意等同自動 run。
collect_bundle_case() {
  bundle_case "$1" run
}

type bundle_case
type collect_bundle_case
```

兩個 `type` 指令應顯示對應名稱為 shell function；它們只驗證函式已載入，不會執行
案例。控制器只呼叫 `fault-scenarios.sh` 與 `collect.sh`，不會執行 binary。

## 4. 採集 P00 基準線

```bash
bundle_case P00 collect
```

此時只確認 bundle 產生成功，尚未進行任何診斷。

## 5. 分段或自動採集 P01～P16

### 5.1 分段模式

每個階段都可以分開執行。以下指令會讓 ES 保持故障，方便人工透過 Kibana Dev
Tools／API 查看即時狀態。不要用 Stack Monitoring 儀表板判讀本 Playbook 的故障；
正式基準線會關閉 Legacy monitoring collection，儀表板只會停留在最後一筆歷史資料：

```bash
bundle_case P01 trigger
```

若案例需要保留投影片或人工審閱佐證，截圖統一放在案例目錄的 `screenshots/`，
與正式 Bundle 分開；不要把圖片放進 `bundle/`：

```text
$EVIDENCE_ROOT/PXX-fault/
├── bundle/
└── screenshots/
    └── PXX-觀測內容.png
```

截圖與 `collect` 的先後不影響 ES 狀態；兩者都是唯讀觀察。正式驗證建議先執行
`collect` 保存主要證據，再於 `restore` 前補截圖。若先截圖也可以，因為 `collect`
會再次確認案例故障仍成立後才採集。

確認後可選擇採集或直接還原：

```bash
bundle_case P01 collect       # 重新確認故障後採集 P01-fault/bundle
bundle_case P01 restore       # 還原並確認基準線
bundle_case P01 collect-post  # 選配：採集 post-P01/bundle
```

`collect` 在 active case 不符或故障已消失時會拒絕採集；`collect-post` 在尚未還原時也會
拒絕執行。只想測試故障配方時，可執行 `trigger → restore`，不必採集。

### 5.2 自動模式

`run` 固定執行正確順序：

```text
trigger → collect fault bundle → restore → collect post bundle
```

任一步失敗時會嘗試還原 ES。一次只執行一案，成功回到 prompt 後才繼續：

```bash
bundle_case P01 run
bundle_case P02 run
bundle_case P03 run
bundle_case P04 run
bundle_case P05 run
bundle_case P06 run
bundle_case P07 run
bundle_case P08 run
bundle_case P09 run
bundle_case P10 run
bundle_case P11 run
bundle_case P12 run
bundle_case P13 run
bundle_case P14 run
bundle_case P15 run
bundle_case P16 run
```

每案會保留兩份 bundle：

```text
$EVIDENCE_ROOT/PXX-fault/bundle   # 故障期間採集
$EVIDENCE_ROOT/post-PXX/bundle    # 復原後採集
```

若採集過程中斷，留在採集端執行：

```bash
bundle_case PXX restore
```

把 `PXX` 換成中斷案例。基準線未恢復前不得繼續。

## 6. 結束採集並交接

完成全部採集後：

```bash
"$FAULT_CMD" baseline verify
echo "export EVIDENCE_ROOT='$EVIDENCE_ROOT'"
```

複製輸出的整行 `export` 指令，供分析端貼上。到此為止，採集端不得執行 binary。

---

# 階段二：只分析 Bundle

## 7. 準備分析端

開新的 Terminal，回到 repo 根目錄。先貼上階段一輸出的整行 `export EVIDENCE_ROOT='實際路徑'`；不要把說明文字當成路徑。

接著整段貼入：

```bash
prepare_bundle_analysis() {
  if [ -z "${EVIDENCE_ROOT:-}" ]; then
    echo "STOP：尚未設定 EVIDENCE_ROOT；請貼上階段一輸出的 export 指令" >&2
    return 1
  fi
  if [ ! -d "$EVIDENCE_ROOT" ]; then
    echo "STOP：EVIDENCE_ROOT 不是有效採集目錄：$EVIDENCE_ROOT" >&2
    return 1
  fi
  if ! command -v jq >/dev/null; then
    echo "STOP：找不到 jq" >&2
    return 1
  fi

  export TOOL_BIN="$PWD/elk-diagnostics"
  if [ ! -x "$TOOL_BIN" ]; then
    echo "STOP：找不到可執行 binary：$TOOL_BIN" >&2
    return 1
  fi

  export REPORT_ROOT="$PWD/reports/$(basename "$EVIDENCE_ROOT")"
  mkdir -p "$REPORT_ROOT" || return 1
  unset ES_LABEL ES_URL ES_USER ES_PASSWORD CA_CERT COLLECT_SH FAULT_CMD BUNDLE_CASE_CMD
  echo "bundle_root=$EVIDENCE_ROOT"
  echo "report_root=$REPORT_ROOT"
}

prepare_bundle_analysis
```

這個 Terminal 不需要連線 ES，也不得重新執行 `collect.sh`。原始 bundle 留在 `EVIDENCE_ROOT`；JSON 與 HTML 報告只寫入 `REPORT_ROOT`。

## 8. 載入分析函式

整段貼入分析端 Terminal：

```bash
assert_result() {
  local report="$1" id="$2" expected="$3"
  if ! jq -e --arg id "$id" --arg expected "$expected" '
    any(.results[]; .id == $id and .status == $expected)
  ' "$report" >/dev/null; then
    echo "STOP：$id 未得到 $expected" >&2
    return 1
  fi
  echo "$id=$expected"
}

assert_summary_contains() {
  local report="$1" id="$2" expected="$3"
  if ! jq -e --arg id "$id" --arg expected "$expected" '
    any(.results[]; .id == $id and (.summary | contains($expected)))
  ' "$report" >/dev/null; then
    echo "STOP：$id summary 未包含 $expected" >&2
    return 1
  fi
}

offline_analyze() {
  local bundle="$1" label="$2" report_json='' rc=0
  local report="$REPORT_ROOT/$label/bundle-report.json"
  mkdir -p "$REPORT_ROOT/$label"

  report_json=$(env -i PATH='/usr/bin:/bin' \
    "$TOOL_BIN" check --from-bundle "$bundle" --output json) || rc=$?
  printf '%s\n' "$report_json" > "$report"
  echo "bundle_report=$report exit=$rc"
  test "$rc" -lt 10
  BUNDLE_REPORT="$report"
}

analyze_baseline_bundle() {
  local label="$1"
  offline_analyze "$EVIDENCE_ROOT/$label/bundle" "$label"
  jq '{overall_status,summary}' "$BUNDLE_REPORT"
	jq -e '
	  .summary.critical == 0 and
	  .summary.unknown == 0 and
	  ([.results[] | select(.status == "warning") | .id] - ["master_stability_context","recent_node_restart"] | length) == 0 and
	  any(.results[]; .id == "master_stability_context" and .status == "warning") and
	  (["node_api_coverage","node_swap_usage","node_file_descriptor_pressure","node_cgroup_memory_pressure","node_memory_lock"]
	    - [.results[] | select(.status == "pass") | .id] | length == 0) and
	  any(.results[]; .id == "recent_node_restart" and (.status == "pass" or .status == "warning")) and
	  any(.results[]; .id == "indexing_pressure" and .status == "pass") and
	  any(.results[]; .id == "index_read_write_blocks" and .status == "pass") and
	  any(.results[]; .id == "ccr_health" and (.status == "pass" or .status == "skipped")) and
	  any(.results[]; .id == "ml_jobs_datafeeds" and (.status == "pass" or .status == "skipped")) and
	  any(.results[]; .id == "planned_shutdown" and (.status == "pass" or .status == "skipped")) and
	  any(.results[]; .id == "voting_config_exclusions" and .status == "pass") and
	  (.node_context.stats_coverage | .available and .failed == 0 and .successful == .total and .returned == .total) and
	  (.node_context.info_coverage | .available and .failed == 0 and .successful == .total and .returned == .total)
	' "$BUNDLE_REPORT" >/dev/null
}

assert_bundle_case() {
  local case_id="$1" report="$2"
  case "$case_id" in
    P01)
      assert_result "$report" cluster_health critical
      assert_result "$report" data_allocation_blocked critical
      assert_result "$report" data_corruption warning
      assert_result "$report" allocation_guidance warning
      assert_result "$report" index_allocation_blocked unknown
      ;;
    P02)
      assert_result "$report" cluster_health critical
      assert_result "$report" data_allocation_blocked pass
      assert_result "$report" data_corruption warning
      assert_result "$report" index_allocation_blocked unknown
      ;;
    P03)
      assert_result "$report" cluster_health warning
      assert_result "$report" allocation_guidance warning
      assert_result "$report" index_allocation_blocked unknown
      ;;
    P04)
      assert_result "$report" cluster_health critical
      assert_result "$report" allocation_guidance warning
      assert_result "$report" index_allocation_blocked unknown
      ;;
    P05) assert_result "$report" shards_capacity critical ;;
    P06) assert_result "$report" disk critical ;;
    P07|P08) assert_result "$report" ilm_slm_status critical ;;
    P09) assert_result "$report" mapping_explosion critical ;;
    P10)
      assert_result "$report" search_slow_log pass
      assert_summary_contains "$report" search_slow_log '已於'
      ;;
    P11) assert_result "$report" watcher warning ;;
    P12) assert_result "$report" transforms critical ;;
    P13)
      assert_result "$report" monitoring pass
      assert_result "$report" upgrade_deprecations warning
      ;;
    P14) assert_result "$report" remote_clusters warning ;;
    P15) assert_result "$report" ingest_pipeline_errors warning ;;
    P16) assert_result "$report" index_read_write_blocks critical ;;
    *) echo "未知案例：$case_id" >&2; return 1 ;;
  esac
}

analyze_bundle_case() {
  local case_id="$1" passed=0
  echo "=== $case_id / offline analysis ==="

  if offline_analyze \
      "$EVIDENCE_ROOT/$case_id-fault/bundle" "$case_id-fault" && \
    assert_bundle_case "$case_id" "$BUNDLE_REPORT"; then
    passed=1
  fi

  analyze_baseline_bundle "post-$case_id" || return 1
  test "$passed" -eq 1
}
```

`env -i` 會清除所有 ES 連線環境。分析若仍得到故障結果，證據只能來自 bundle。

## 9. 診斷 P00 基準線

```bash
analyze_baseline_bundle P00
```

通過條件：無 critical／unknown；warning 只允許單節點 Master 與測試機剛啟動造成的 `recent_node_restart`。Node API coverage 完整，ES-GAP-07～12 的基準線結果符合函式中的明確斷言。不要鎖死 pass 總數。

## 10. 診斷 P01～P16 Bundle

一次只執行一行：

```bash
analyze_bundle_case P01
analyze_bundle_case P02
analyze_bundle_case P03
analyze_bundle_case P04
analyze_bundle_case P05
analyze_bundle_case P06
analyze_bundle_case P07
analyze_bundle_case P08
analyze_bundle_case P09
analyze_bundle_case P10
analyze_bundle_case P11
analyze_bundle_case P12
analyze_bundle_case P13
analyze_bundle_case P14
analyze_bundle_case P15
analyze_bundle_case P16
```

每案會產生：

```text
$REPORT_ROOT/PXX-fault/bundle-report.json
$REPORT_ROOT/post-PXX/bundle-report.json
```

## 11. 用 Binary 顯示單一 Bundle 診斷

若只要在 Terminal 顯示 P02 故障報告：

```bash
env -i PATH='/usr/bin:/bin' \
  ./elk-diagnostics check \
  --from-bundle "$EVIDENCE_ROOT/P02-fault/bundle" \
  --output text
```

將 `P02` 換成實際案例。不要使用 `post-P02/bundle` 判讀故障；該目錄是復原後的基準線。

若要產生 P02 HTML 報告：

```bash
mkdir -p "$REPORT_ROOT/P02-fault"
env -i PATH='/usr/bin:/bin' \
  ./elk-diagnostics check \
  --from-bundle "$EVIDENCE_ROOT/P02-fault/bundle" \
  --output html \
  > "$REPORT_ROOT/P02-fault/bundle-report.html"
```

報告會集中在 repo 根目錄下的 `reports/<bundle 執行批次>/`；`reports/` 已被 `.gitignore` 排除。

## 12. P01～P16 驗證項目

| 案例 | 故障情境 | Bundle 主要驗證項目 |
|---|---|---|
| P01 | 叢集 allocation 封鎖 | #1／#2 `cluster_health=critical`、#19 `data_allocation_blocked=critical`、#32 `data_corruption=warning`、#37 `allocation_guidance=warning` |
| P02 | Index allocation 封鎖 | #1／#2 `cluster_health=critical`、#19 `data_allocation_blocked=pass`（不得誤報）、#32 `data_corruption=warning` |
| P03 | 單節點 replica 無法配置 | #21 `cluster_health=warning`、#37 `allocation_guidance=warning`（`same_shard`） |
| P04 | Index shards-per-node 超限 | #22 `cluster_health=critical`、#37 `allocation_guidance=warning`（`shards_limit`） |
| P05 | 叢集 shard 容量超限 | #10／#23 `shards_capacity=critical`；cluster 維持 green |
| P06 | 磁碟 watermark 超限 | #3／#4 `disk=critical` |
| P07 | ILM stopped | #5 `ilm_slm_status=critical` |
| P08 | ILM 進入 ERROR step | 測試 Index 維持 green；#5 `ilm_slm_status=critical` |
| P09 | Mapping 欄位膨脹 | #11 `mapping_explosion=critical`，包含 data stream backing index；測試 index 維持 green |
| P10 | Search slow log 開啟 | #31 `search_slow_log=pass`，摘要必須指出已開啟且維持 green 的 index |
| P11 | Watcher stopped | #27 `watcher=warning` |
| P12 | Transform failed | #28 `transforms=critical`；source／destination index 維持 green |
| P13 | Legacy monitoring 設定 | #33 `monitoring=pass`、#34 `upgrade_deprecations=warning` |
| P14 | Remote cluster 斷線 | #35 `remote_clusters=warning` |
| P15 | Ingest pipeline 持續失敗 | #13 `ingest_pipeline_errors=warning`；測試 index 維持 green |
| P16 | Index write block | ES-GAP-08 `index_read_write_blocks=critical` |

P01～P04 另會確認 `index_allocation_blocked=unknown`，原因見第 13 節。

## 13. #20 已知限制

P01～P04 會讓 `shards_availability` 指出受影響 index。Live 模式可再查 `GET /<index>/_settings`；固定 bundle 無法預知 index 名稱，因此：

- P01、P03、P04：主要診斷仍須正確，`index_allocation_blocked=unknown` 是預期差異。
- P02：Bundle 能證明 red index 與 allocation 根因資料存在，但不能單獨確認 #20；`unknown` 是正確結果，不得改成 pass。

這是產品已知限制，不代表 Bundle 路線執行失敗。
