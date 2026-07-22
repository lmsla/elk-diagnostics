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
export EVIDENCE_ROOT="$PWD/bundle-playbook-${ES_LABEL}-$(date +%Y%m%d-%H%M%S)"
mkdir -p "$EVIDENCE_ROOT"

command -v curl jq >/dev/null
test -x "$COLLECT_SH"
test -x "$FAULT_CMD"
test -f "$CA_CERT"
"$FAULT_CMD" baseline verify
```

這個階段不需要 `make build`、`TOOL_BIN` 或任何 binary 參數。所有採集資料都會放在 repo 根目錄下的單一 `bundle-playbook-*` 目錄，且已被 `.gitignore` 排除。

改驗 ES 9 時開新的 Terminal，將前兩行改為：

```bash
export ES_LABEL=es9
export ES_URL=https://localhost:9209
```

## 3. 載入採集函式

整段貼入採集端 Terminal：

```bash
fault() {
  "$FAULT_CMD" "$1" "$2"
}

collect_only() {
  local label="$1" dir="$EVIDENCE_ROOT/$1/bundle"
  mkdir -p "$EVIDENCE_ROOT/$1"
  ES_PASSWORD="$ES_PASSWORD" "$COLLECT_SH" \
    -h "$ES_URL" -u "$ES_USER" --ca-cert "$CA_CERT" -o "$dir"
  test -f "$dir/_manifest.json"
  test -f "$dir/_status.txt"
  echo "bundle=$dir"
}

collect_bundle_case() {
  local case_id="$1"
  echo "=== $case_id / collect only ==="

  fault baseline verify || return 1

  if ! fault "$case_id" trigger; then
    fault "$case_id" restore || true
    return 1
  fi
  if ! fault "$case_id" verify; then
    fault "$case_id" restore || true
    return 1
  fi
  if ! collect_only "$case_id-fault"; then
    fault "$case_id" restore || true
    return 1
  fi

  fault "$case_id" restore || return 1
  fault baseline verify || return 1
  collect_only "post-$case_id"
}
```

這兩個函式只呼叫 `fault-scenarios.sh` 與 `collect.sh`，不會執行 binary。

## 4. 採集 P00 基準線

```bash
fault baseline verify
collect_only P00
```

此時只確認 bundle 產生成功，尚未進行任何診斷。

## 5. 採集 P01～P15 故障 Bundle

一次只執行一行；確認 ES 已復原後才執行下一案：

```bash
collect_bundle_case P01
collect_bundle_case P02
collect_bundle_case P03
collect_bundle_case P04
collect_bundle_case P05
collect_bundle_case P06
collect_bundle_case P07
collect_bundle_case P08
collect_bundle_case P09
collect_bundle_case P10
collect_bundle_case P11
collect_bundle_case P12
collect_bundle_case P13
collect_bundle_case P14
collect_bundle_case P15
```

每案會保留兩份 bundle：

```text
$EVIDENCE_ROOT/PXX-fault/bundle   # 故障期間採集
$EVIDENCE_ROOT/post-PXX/bundle    # 復原後採集
```

若採集過程中斷，留在採集端執行：

```bash
fault PXX restore
fault baseline verify
```

把 `PXX` 換成中斷案例。基準線未恢復前不得繼續。

## 6. 結束採集並交接

完成全部採集後：

```bash
fault baseline verify
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
  unset ES_LABEL ES_URL ES_USER ES_PASSWORD CA_CERT COLLECT_SH FAULT_CMD
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
	  ([.results[] | select(.status == "warning") | .id] | sort) == ["master_stability_context"] and
	  (["node_api_coverage","node_swap_usage","node_file_descriptor_pressure","node_cgroup_memory_pressure"]
	    - [.results[] | select(.status == "pass") | .id] | length == 0) and
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

通過條件：無 critical／unknown；唯一 warning 是單節點 Master 結構；四項 Node Context 診斷均 pass，且 Stats／Info coverage 完整。不要鎖死 pass 總數。

## 10. 診斷 P01～P15 Bundle

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

## 12. P01～P15 驗證項目

| 案例 | 故障情境 | Bundle 主要驗證項目 |
|---|---|---|
| P01 | 叢集 allocation 封鎖 | #1／#2 `cluster_health=critical`、#19 `data_allocation_blocked=critical`、#32 `data_corruption=warning`、#37 `allocation_guidance=warning` |
| P02 | Index allocation 封鎖 | #1／#2 `cluster_health=critical`、#19 `data_allocation_blocked=pass`（不得誤報）、#32 `data_corruption=warning` |
| P03 | 單節點 replica 無法配置 | #21 `cluster_health=warning`、#37 `allocation_guidance=warning`（`same_shard`） |
| P04 | Index shards-per-node 超限 | #22 `cluster_health=critical`、#37 `allocation_guidance=warning`（`shards_limit`） |
| P05 | 叢集 shard 容量超限 | #10／#23 `shards_capacity=critical` |
| P06 | 磁碟 watermark 超限 | #3／#4 `disk=critical` |
| P07 | ILM stopped | #5 `ilm_slm_status=critical` |
| P08 | ILM 進入 ERROR step | #5 `ilm_slm_status=critical` |
| P09 | Mapping 欄位膨脹 | #11 `mapping_explosion=critical`，包含 data stream backing index |
| P10 | Search slow log 開啟 | #31 `search_slow_log=pass`，摘要必須指出已開啟的 index |
| P11 | Watcher stopped | #27 `watcher=warning` |
| P12 | Transform failed | #28 `transforms=critical` |
| P13 | Legacy monitoring 設定 | #33 `monitoring=pass`、#34 `upgrade_deprecations=warning` |
| P14 | Remote cluster 斷線 | #35 `remote_clusters=warning` |
| P15 | Ingest pipeline 持續失敗 | #13 `ingest_pipeline_errors=warning` |

P01～P04 另會確認 `index_allocation_blocked=unknown`，原因見第 13 節。

## 13. #20 已知限制

P01～P04 會讓 `shards_availability` 指出受影響 index。Live 模式可再查 `GET /<index>/_settings`；固定 bundle 無法預知 index 名稱，因此：

- P01、P03、P04：主要診斷仍須正確，`index_allocation_blocked=unknown` 是預期差異。
- P02：Bundle 能證明 red index 與 allocation 根因資料存在，但不能單獨確認 #20；`unknown` 是正確結果，不得改成 pass。

這是產品已知限制，不代表 Bundle 路線執行失敗。
