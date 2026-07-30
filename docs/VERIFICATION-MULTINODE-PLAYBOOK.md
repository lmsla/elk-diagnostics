# ES 8 多節點人工驗證 Playbook

目的：在內部可丟棄的 ES 8 三節點叢集，依序驗證多節點才有意義的診斷。
這不是客戶操作手冊，禁止對正式或共用叢集執行。

底層共用既有 [`fault-scenarios.sh`](../dev/phase0/fault-scenarios.sh)、
[`bundle-case.sh`](../dev/phase0/bundle-case.sh) 與 `collect.sh`，沒有第二套多節點腳本。
P01～P16 的採集能力仍適用多節點；只有其故障配方被 topology guard 限制在單節點。

## 1. 案例總表

| 案例 | 驗證情境 | 預期主要診斷 |
|---|---|---|
| M00 | 3 master、3 data、3 zone、hot／warm、awareness 基準線 | `allocation_awareness=pass` |
| M01 | awareness 指向所有 data node 都不存在的 attribute | `allocation_awareness=warning` |
| M02 | ILM hot→warm 遷移因 warm tier 容量不足而停留 | `ilm_tier_migration=warning` |
| M03 | 相同角色節點的 heap 設定漂移 | `node_runtime_consistency=warning` |
| M04 | master-only 節點磁碟使用率成為離群值 | `hot_spotting=warning` |
| M05 | data-hot／content 節點磁碟使用率成為離群值 | `hot_spotting=warning` |
| M06 | allocation 暫停後形成 undesired shards | `unbalanced_cluster=warning` |
| M07 | master-eligible 節點變成偶數個 | `master_stability_context=warning` |
| M08 | Nodes Stats／Info 僅部分節點成功回應 | `node_api_coverage=unknown` |
| M09 | 一般業務 index 設為 0 replica | `replica_resilience=warning` |

M04、M05 都驗證 `hot_spotting`，但節點角色不同，用來確認診斷不會只看 data node
或只看 master node。M07 只驗證結構性選舉風險，不代表已重現反覆 master election。

## 2. 前置環境

```bash
./dev/phase0/podman-test-env.sh up-multinode
./dev/phase0/podman-test-env.sh up-multinode-kibana
./dev/phase0/podman-test-env.sh status
```

- ES：`https://localhost:9218`
- Kibana：`https://localhost:5611`
- 測試帳密：`elastic / elk-diagnostics-test-only`

此環境位於同一台 Podman VM，可驗證 Elasticsearch 分配邏輯與工具判定，
不能證明跨主機或真實 availability zone 的故障隔離能力。

## 3. 設定控制環境

從 repo 根目錄執行：

```bash
export ES_LABEL=es8-multinode
export ES_URL=https://localhost:9218
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

export FAULT_CMD="$PWD/dev/phase0/fault-scenarios.sh"
export BUNDLE_CASE_CMD="$PWD/dev/phase0/bundle-case.sh"
export COLLECT_SH="$PWD/collect.sh"
export EVIDENCE_ROOT="$PWD/bundle-playbook-${ES_LABEL}-$(date +%Y%m%d-%H%M%S)"
mkdir -p "$EVIDENCE_ROOT"

command -v curl jq podman >/dev/null
test -x "$FAULT_CMD"
test -x "$BUNDLE_CASE_CMD"
test -x "$COLLECT_SH"
test -f "$CA_CERT"

bundle_case() {
  "$BUNDLE_CASE_CMD" "$@"
}

type bundle_case
```

密碼輸入不回顯，也不會寫入 shell history。Terminal 只保留權限 `600` 的臨時密碼檔
路徑；控制器透過 curl config 使用認證，不會把密碼放進程序參數或匯出的
`ES_PASSWORD`。離開 Terminal 時會自動刪除密碼檔。此故障控制器只供可丟棄測試環境，
不得用於客戶環境。

`type bundle_case` 應顯示為 shell function。此入口與單節點 Bundle Playbook 相同；
P／M 系列只替換案例 ID，不需記兩套指令。

先驗基準：

```bash
"$FAULT_CMD" M00 verify
```

必須看到 `multi_baseline=clean`；否則不得開始案例。

## 4. 單案例操作

一次只跑一案。以下以 M03 為例，其他案例只需替換 ID。

### 4.1 Route A：Live

```bash
"$FAULT_CMD" M03 trigger
"$FAULT_CMD" M03 verify

mkdir -p "$EVIDENCE_ROOT/M03-fault"
./elk-diagnostics check \
  --host "$ES_URL" \
  --username "$ES_USER" \
  --ca-cert "$CA_CERT" \
  --output json \
  --output-file "$EVIDENCE_ROOT/M03-fault/live-report.json"

"$FAULT_CMD" M03 restore
"$FAULT_CMD" M00 verify
```

Live binary 會另外互動詢問密碼且不回顯，不沿用案例控制器的密碼檔。

### 4.2 Route B：Bundle

Route B 必須從 trigger 起改由 controller 管理，不得和 Route A 混用：

```bash
bundle_case M03 trigger
bundle_case M03 collect
bundle_case M03 restore
bundle_case M03 collect-post
```

離線分析：

```bash
mkdir -p "$EVIDENCE_ROOT/reports/M03-fault"
./elk-diagnostics check \
  --from-bundle "$EVIDENCE_ROOT/M03-fault/bundle" \
  --output html \
  --output-file "$EVIDENCE_ROOT/reports/M03-fault/bundle-report.html"
```

需要自動完成四階段時可執行：

```bash
bundle_case M03 run
```

但人工截圖應使用分階段模式，否則 `run` 會在採集後立即 restore。

## 5. 各案例的 API 交叉驗證

執行 `trigger`、`verify` 後，再執行 Live 或 Bundle 診斷。

| 案例 | Kibana Dev Tools／shell 交叉驗證 |
|---|---|
| M01 | `GET /_cluster/settings?flat_settings=true`、`GET /_nodes?filter_path=_nodes,nodes.*.name,nodes.*.attributes` |
| M02 | `GET /playbook-mn-tier/_ilm/explain` |
| M03 | `GET /_nodes/jvm,plugins?filter_path=_nodes,nodes.*.name,nodes.*.roles,nodes.*.jvm.mem.heap_max_in_bytes` |
| M04／M05 | `GET /_cat/nodes?v&h=name,node.role,disk.used_percent` |
| M06 | `GET /_cat/allocation?v&h=node,shards,shards.undesired,disk.percent` |
| M07 | `GET /_nodes?filter_path=_nodes,nodes.*.name,nodes.*.roles` |
| M08 | `GET /_nodes/stats/os,process,fs,jvm?timeout=5s&filter_path=_nodes,nodes.*.name` |
| M09 | `GET /playbook-mn-no-replica/_settings?flat_settings=true` |

M08 預期 `_nodes.total=4`、`successful=3`、`failed=1`。本機環境為了讓 partial
response 穩定存在到採集完成，延長了 test-only follower-check 重試窗口；
這不是正式環境調校建議。採集腳本對 Nodes／Tasks API 使用 ES `timeout=5s`
與 curl 10 秒上限；CAT nodes／thread pool 僅使用 curl 10 秒上限。各端點仍依序採集，
因此 M08 仍可能花費數十秒，但不會讓每個節點 fan-out 端點各自等待完整 30 秒。

## 6. Restore 閘門

任何診斷、採集或截圖失敗，都不得跳過 restore：

```bash
"$FAULT_CMD" M03 restore
"$FAULT_CMD" M00 verify
```

若 Bundle controller 記錄 active case，必須改用：

```bash
bundle_case M03 restore
```

M00 未重新通過前，不得進入下一案例。M03～M08 會建立臨時 `es8-mn4`
helper node；不得手動留下該容器。

## 7. 已知能力邊界

- M02 只證明採集當下可見的 ILM `check-migration` 卡點，不代表列出所有歷史 lifecycle 異常。
- M04／M05 驗證磁碟離群值；同一 Podman VM 無法隔離每個容器的 host CPU，
  因此未把 CPU hotspot 當成本地可重現案例。
- M07 是靜態 topology 風險，不是長時間 master election 穩定性測試。
- M08 驗證 partial response 必須回 `unknown`；查不到的節點不得被當成正常。
- ES9 多節點、跨主機、真實負載與長時間穩定性仍未由本 Playbook 覆蓋。

## 8. 使用後清理

```bash
rm -f "${ES_PASSWORD_FILE:-}"
unset ES_LABEL ES_URL ES_USER ES_PASSWORD_FILE CA_CERT
unset FAULT_CMD BUNDLE_CASE_CMD COLLECT_SH EVIDENCE_ROOT
trap - EXIT
```
