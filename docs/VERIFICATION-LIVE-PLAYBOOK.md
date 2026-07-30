# Live 直連人工驗證 Playbook

目的：在可丟棄的本機 ES 測試叢集上，逐案驗證 `elk-diagnostics check` 直連模式能抓到真實故障。

本文件只驗 Live 路線。客戶交付流程請改用 [`VERIFICATION-BUNDLE-PLAYBOOK.md`](./VERIFICATION-BUNDLE-PLAYBOOK.md)。

## 1. 邊界

- 禁止在客戶或共用環境執行。
- P01～P16 一次只執行一案。
- 故障由 [`fault-scenarios.sh`](../dev/phase0/fault-scenarios.sh) 以 curl 製造與復原。
- `elk-diagnostics` 只做唯讀診斷。
- 每案結束必須回到基準線：無 critical／unknown；只允許單節點 Master 與啟動一小時內的 restart warning。

## 2. 準備 ES 8

從 repo 根目錄執行：

```bash
export ES_LABEL=es8
export ES_URL=https://localhost:9208
export ES_USER=elastic
export CA_CERT="$PWD/dev/phase0/certs/ca/ca.crt"

export ES_PASSWORD_FILE="$(mktemp "${TMPDIR:-/tmp}/elk-diagnostics-password.XXXXXX")"
chmod 600 "$ES_PASSWORD_FILE"
printf 'Elasticsearch password: ' >&2
IFS= read -r -s ES_PASSWORD_INPUT
printf '\n' >&2
printf '%s' "$ES_PASSWORD_INPUT" > "$ES_PASSWORD_FILE"
unset ES_PASSWORD_INPUT
trap 'rm -f "${ES_PASSWORD_FILE:-}"' EXIT

export ELK_DIAGNOSTICS_HOSTS="$ES_URL"
export ELK_DIAGNOSTICS_AUTH_TYPE=basic
export ELK_DIAGNOSTICS_USERNAME="$ES_USER"
export ELK_DIAGNOSTICS_CA_CERT="$CA_CERT"

export TOOL_BIN="$PWD/elk-diagnostics"
export FAULT_CMD="$PWD/dev/phase0/fault-scenarios.sh"
export EVIDENCE_ROOT="$PWD/reports/live-playbook-${ES_LABEL}-$(date +%Y%m%d-%H%M%S)"
mkdir -p "$EVIDENCE_ROOT"

test -x "$TOOL_BIN"
test -x "$FAULT_CMD"
"$FAULT_CMD" baseline verify
```

Terminal 只保留權限 `600` 的臨時密碼檔路徑；內部故障控制器透過 curl config 使用
認證，不會把密碼放進程序參數或匯出的 `ES_PASSWORD`。Live binary 不讀取此檔案，
執行時會另外安全詢問密碼。

Live JSON 報告集中在 repo 根目錄的 `reports/live-playbook-*`；`reports/` 已由
`.gitignore` 排除，不會混入待提交檔案。

改驗 ES 9 時開新的 Terminal，將前兩行改為：

```bash
export ES_LABEL=es9
export ES_URL=https://localhost:9209
```

## 3. 載入 Live 驗證函式

整段貼入同一個 Terminal：

```bash
fault() {
  "$FAULT_CMD" "$1" "$2"
}

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

live_capture() {
  local label="$1" dir="$EVIDENCE_ROOT/$1" report_json='' rc=0
  mkdir -p "$dir"
  report_json=$("$TOOL_BIN" check --output json) || rc=$?
  printf '%s\n' "$report_json" > "$dir/live-report.json"
  echo "live_report=$dir/live-report.json exit=$rc"
  test "$rc" -lt 10
  LIVE_REPORT="$dir/live-report.json"
}

live_baseline_gate() {
  local report_json='' rc=0 report="$EVIDENCE_ROOT/baseline-latest.json"
  report_json=$("$TOOL_BIN" check --output json) || rc=$?
  printf '%s\n' "$report_json" > "$report"
  jq '{overall_status,summary}' "$report"
  test "$rc" -eq 1
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
	' "$report" >/dev/null
}

assert_live_case() {
  local case_id="$1" report="$2"
  case "$case_id" in
    P01)
      assert_result "$report" cluster_health critical
      assert_result "$report" data_allocation_blocked critical
      assert_result "$report" data_corruption warning
      assert_result "$report" allocation_guidance warning
      ;;
    P02)
      assert_result "$report" cluster_health critical
      assert_result "$report" index_allocation_blocked warning
      assert_result "$report" data_allocation_blocked pass
      assert_result "$report" data_corruption warning
      ;;
    P03)
      assert_result "$report" cluster_health warning
      assert_result "$report" allocation_guidance warning
      ;;
    P04)
      assert_result "$report" cluster_health critical
      assert_result "$report" allocation_guidance warning
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

run_live_case() {
  local case_id="$1" passed=0
  echo "=== $case_id / Live ==="
  live_baseline_gate || return 1

  if ! fault "$case_id" trigger; then
    fault "$case_id" restore || true
    return 1
  fi
  if ! fault "$case_id" verify; then
    fault "$case_id" restore || true
    return 1
  fi

  if live_capture "$case_id" && assert_live_case "$case_id" "$LIVE_REPORT"; then
    passed=1
  fi

  fault "$case_id" restore || return 1
  fault baseline verify || return 1
  live_baseline_gate || return 1
  test "$passed" -eq 1
}
```

## 4. P00：Live 基準線

```bash
live_baseline_gate
```

通過條件：無 critical／unknown；warning 只允許單節點 Master 與測試機剛啟動造成的 `recent_node_restart`。Node API coverage 完整，ES-GAP-07～12 的基準線結果符合函式中的明確斷言。不要鎖死 pass 總數。

## 5. P01～P16

一次只執行一行；成功回到 prompt 後才執行下一案：

```bash
run_live_case P01
run_live_case P02
run_live_case P03
run_live_case P04
run_live_case P05
run_live_case P06
run_live_case P07
run_live_case P08
run_live_case P09
run_live_case P10
run_live_case P11
run_live_case P12
run_live_case P13
run_live_case P14
run_live_case P15
run_live_case P16
```

| 案例 | 故障 | 主要驗證 |
|---|---|---|
| P01 | 叢集 allocation 封鎖 | #1、#2、#19、#32、#37 |
| P02 | Index allocation 封鎖 | #20、#32，#19 不誤報 |
| P03 | 單節點 replica | #21、#37 `same_shard` |
| P04 | Index shards-per-node 超限 | #22、#37 `shards_limit` |
| P05 | 叢集 shard 容量超限；cluster 維持 green | #10、#23 |
| P06 | 磁碟 watermark | #3、#4 |
| P07 | ILM stopped | #5 |
| P08 | ILM ERROR step | 測試 Index 維持 green；#5、`ilm-stuck` |
| P09 | Mapping 欄位膨脹；測試 index 維持 green | #11、data stream backing index |
| P10 | Search slow log 開啟；測試 index 維持 green | #31 雙態讀值 |
| P11 | Watcher stopped | #27 |
| P12 | Transform failed；source／destination index 維持 green | #28 |
| P13 | Legacy monitoring | #33、#34 |
| P14 | Remote cluster 斷線 | #35 |
| P15 | Ingest pipeline 失敗；測試 index 維持 green | #13 |
| P16 | Index write block | ES-GAP-08 `index_read_write_blocks=critical` |

## 6. 用 Binary 顯示 Live 診斷

`run_live_case PXX` 內部已執行 binary 並保存 JSON。若要在 Terminal 直接查看文字報告，必須在故障已觸發、尚未復原時執行：

```bash
./elk-diagnostics check --output text
```

例如手動觀察 P02：

```bash
fault P02 trigger
fault P02 verify
./elk-diagnostics check --output text
fault P02 restore
fault baseline verify
```

`run_live_case P02` 結束後故障已復原，此時再執行 binary 只會得到基準線結果。

## 7. 中斷復原

若任何案例中斷，先執行：

```bash
fault PXX restore
fault baseline verify
live_baseline_gate
```

把 `PXX` 換成中斷的案例。基準線未恢復前不得繼續。

## 8. 使用後清理

```bash
rm -f "${ES_PASSWORD_FILE:-}"
unset ES_LABEL ES_URL ES_USER ES_PASSWORD_FILE CA_CERT
unset ELK_DIAGNOSTICS_HOSTS ELK_DIAGNOSTICS_AUTH_TYPE
unset ELK_DIAGNOSTICS_USERNAME ELK_DIAGNOSTICS_CA_CERT
unset TOOL_BIN FAULT_CMD EVIDENCE_ROOT
trap - EXIT
```
