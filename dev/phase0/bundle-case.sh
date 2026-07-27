#!/usr/bin/env bash
# 內部 Bundle 故障案例控制器。
#
# 故障配方只委派給 fault-scenarios.sh；本腳本負責把 trigger、collect、restore
# 與完整 run 拆成可跨指令執行的安全階段。禁止在客戶或共用環境使用。

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

FAULT_CMD="${FAULT_CMD:-${SCRIPT_DIR}/fault-scenarios.sh}"
COLLECT_SH="${COLLECT_SH:-${REPO_ROOT}/collect.sh}"

usage() {
  cat >&2 <<'USAGE'
用法：
  ./dev/phase0/bundle-case.sh P00 collect
  ./dev/phase0/bundle-case.sh P01 trigger
  ./dev/phase0/bundle-case.sh P01 collect
  ./dev/phase0/bundle-case.sh P01 restore
  ./dev/phase0/bundle-case.sh P01 collect-post
  ./dev/phase0/bundle-case.sh P01 run

動作：
  trigger       確認基準線後製造並確認故障；故障保持生效
  collect       再次確認故障仍生效，採集 PXX-fault/bundle
  restore       還原故障並確認基準線
  collect-post  確認基準線後採集 post-PXX/bundle
  run           trigger → collect → restore → collect-post

必要環境變數：
  ES_URL ES_USER ES_PASSWORD CA_CERT EVIDENCE_ROOT
USAGE
}

fail() {
  echo "STOP：$*" >&2
  return 1
}

require_env() {
  local name="$1"
  [[ -n "${!name:-}" ]] || fail "尚未設定 ${name}"
}

validate_case() {
  case "$1" in
    P00|P01|P02|P03|P04|P05|P06|P07|P08|P09|P10|P11|P12|P13|P14|P15|P16) ;;
    *) usage; return 10 ;;
  esac
}

prepare() {
  require_env ES_URL
  require_env ES_USER
  require_env ES_PASSWORD
  require_env CA_CERT
  require_env EVIDENCE_ROOT
  [[ -x "$FAULT_CMD" ]] || fail "找不到故障控制器：${FAULT_CMD}"
  [[ -x "$COLLECT_SH" ]] || fail "找不到採集腳本：${COLLECT_SH}"
  [[ -f "$CA_CERT" ]] || fail "找不到 CA：${CA_CERT}"
  mkdir -p "$EVIDENCE_ROOT"
  ACTIVE_FILE="${EVIDENCE_ROOT}/.bundle-case-active"
}

active_case() {
  [[ -f "$ACTIVE_FILE" ]] && tr -d '\r\n' <"$ACTIVE_FILE"
}

collect_bundle() {
  local label="$1"
  local dir="${EVIDENCE_ROOT}/${label}/bundle"
  mkdir -p "${EVIDENCE_ROOT}/${label}"
  ES_PASSWORD="$ES_PASSWORD" "$COLLECT_SH" \
    -h "$ES_URL" -u "$ES_USER" --ca-cert "$CA_CERT" -o "$dir"
  [[ -f "${dir}/_manifest.json" ]] || fail "Bundle 缺少 _manifest.json：${dir}"
  [[ -f "${dir}/_status.txt" ]] || fail "Bundle 缺少 _status.txt：${dir}"
  echo "bundle=${dir}"
}

collect_baseline() {
  "$FAULT_CMD" baseline verify
  collect_bundle P00
}

trigger_case() {
  local case_id="$1"
  local current
  current="$(active_case || true)"
  [[ -z "$current" ]] || fail "已有未復原案例 ${current}；請先執行 bundle_case ${current} restore"
  "$FAULT_CMD" baseline verify

  if ! "$FAULT_CMD" "$case_id" trigger; then
    "$FAULT_CMD" "$case_id" restore || true
    "$FAULT_CMD" baseline verify || true
    return 1
  fi
  if ! "$FAULT_CMD" "$case_id" verify; then
    "$FAULT_CMD" "$case_id" restore || true
    "$FAULT_CMD" baseline verify || true
    return 1
  fi
  if ! printf '%s\n' "$case_id" >"$ACTIVE_FILE"; then
    "$FAULT_CMD" "$case_id" restore || true
    "$FAULT_CMD" baseline verify || true
    return 1
  fi
  echo "active_case=${case_id}"
  echo "注意：ES 保持故障狀態；完成觀察／採集後必須執行 restore。"
}

collect_fault() {
  local case_id="$1"
  local current
  current="$(active_case || true)"
  [[ "$current" == "$case_id" ]] ||
    fail "目前記錄的 active case=${current:-none}，不是 ${case_id}；不得採集未確認狀態"
  "$FAULT_CMD" "$case_id" verify
  collect_bundle "${case_id}-fault"
}

restore_case() {
  local case_id="$1"
  local current
  current="$(active_case || true)"
  if [[ -n "$current" && "$current" != "$case_id" ]]; then
    fail "目前記錄的 active case=${current}，拒絕用 ${case_id} 的配方還原"
  fi
  "$FAULT_CMD" "$case_id" restore
  "$FAULT_CMD" baseline verify
  rm -f "$ACTIVE_FILE"
  echo "restored_case=${case_id}"
}

collect_post() {
  local case_id="$1"
  local current
  current="$(active_case || true)"
  [[ -z "$current" ]] || fail "${current} 尚未復原，不得採集 post bundle"
  "$FAULT_CMD" baseline verify
  collect_bundle "post-${case_id}"
}

run_case() {
  local case_id="$1"
  local cleanup_needed=0

  cleanup_on_exit() {
    local rc=$?
    trap - EXIT
    if [[ "$cleanup_needed" -eq 1 ]]; then
      echo "run 中斷；嘗試還原 ${case_id}..." >&2
      restore_case "$case_id" || echo "STOP：自動還原失敗，請人工執行 restore" >&2
    fi
    exit "$rc"
  }
  trap cleanup_on_exit EXIT

  trigger_case "$case_id"
  cleanup_needed=1
  collect_fault "$case_id"
  restore_case "$case_id"
  cleanup_needed=0
  collect_post "$case_id"

  trap - EXIT
  echo "case_complete=${case_id}"
}

case_id="${1:-}"
action="${2:-}"

validate_case "$case_id"
prepare

if [[ "$case_id" == P00 ]]; then
  [[ "$action" == collect ]] || {
    usage
    exit 10
  }
  collect_baseline
  exit 0
fi

case "$action" in
  trigger) trigger_case "$case_id" ;;
  collect) collect_fault "$case_id" ;;
  restore) restore_case "$case_id" ;;
  collect-post) collect_post "$case_id" ;;
  run) run_case "$case_id" ;;
  *) usage; exit 10 ;;
esac
