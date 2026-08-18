#!/bin/sh
# 以既有 SSH key／agent 對遠端 Linux 主機執行 host.sh；不保存 SSH 密碼。

set -eu

HOSTS_FILE=""
HOST_COLLECTOR=""
OUT=""
CONNECT_TIMEOUT="${SSH_CONNECT_TIMEOUT:-10}"
SSH_BIN="${SSH_BIN:-ssh}"

usage() {
    echo "用法: ssh.sh --hosts-file <檔案> --host-collector <host.sh> --output <host 根目錄>" >&2
    echo "hosts 格式：每行 host-id|ssh-user@hostname" >&2
}

while [ $# -gt 0 ]; do
    case "$1" in
        --hosts-file) HOSTS_FILE="${2:-}"; shift 2 ;;
        --host-collector) HOST_COLLECTOR="${2:-}"; shift 2 ;;
        --output) OUT="${2:-}"; shift 2 ;;
        --help) usage; exit 0 ;;
        *) echo "未知參數: $1" >&2; usage; exit 2 ;;
    esac
done

[ -r "$HOSTS_FILE" ] && [ -r "$HOST_COLLECTOR" ] && [ -n "$OUT" ] || { usage; exit 2; }
command -v "$SSH_BIN" >/dev/null 2>&1 || { echo "找不到 SSH 執行檔：$SSH_BIN" >&2; exit 2; }
command -v tar >/dev/null 2>&1 || { echo "控制端缺少 tar" >&2; exit 2; }
case "$CONNECT_TIMEOUT" in *[!0-9]*|'') echo "SSH_CONNECT_TIMEOUT 必須是正整數" >&2; exit 2 ;; esac

umask 077
mkdir -p "$OUT"
SUMMARY="$OUT/_status.txt"
: > "$SUMMARY"
failures=0
hosts_total=0

trim() { printf '%s' "$1" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//'; }

while IFS='|' read -r raw_id raw_target extra || [ -n "${raw_id}${raw_target}${extra}" ]; do
    host_id="$(trim "$raw_id")"
    target="$(trim "$raw_target")"
    case "$host_id" in ''|'#'*) continue ;; esac
    if [ -n "$(trim "$extra")" ]; then
        echo "hosts 檔欄位過多：$host_id" >&2
        exit 2
    fi
    case "$host_id" in *[!A-Za-z0-9._-]*) echo "不合法的 host-id：$host_id" >&2; exit 2 ;; esac
    case "$target" in ''|*[!A-Za-z0-9._@:-]*) echo "不合法的 SSH target：$target" >&2; exit 2 ;; esac
    hosts_total=$((hosts_total + 1))

    host_out="$OUT/$host_id"
    archive="$(mktemp "${TMPDIR:-/tmp}/elkdiag-host.XXXXXX")"
    errlog="$host_out/_ssh_errors.log"
    mkdir -p "$host_out"
    remote_command='tmp=$(mktemp -d "${TMPDIR:-/tmp}/elkdiag.XXXXXX") || exit 70; trap '\''rm -rf "$tmp"'\'' EXIT INT TERM; sh -s -- --output "$tmp" || exit $?; tar -C "$tmp" -cf - .'

    if "$SSH_BIN" -o BatchMode=yes -o "ConnectTimeout=$CONNECT_TIMEOUT" "$target" \
        "$remote_command" < "$HOST_COLLECTOR" > "$archive" 2> "$errlog"; then
        if tar -tf "$archive" | grep -Eq '(^/|(^|/)\.\.(/|$))'; then
            echo "遠端 $host_id 回傳不安全的 archive 路徑" >&2
            printf 'transport INVALID_ARCHIVE\n' > "$host_out/_status.txt"
            printf '%s INVALID_ARCHIVE\n' "$host_id" >> "$SUMMARY"
            failures=$((failures + 1))
        elif tar -xf "$archive" -C "$host_out"; then
            rm -f "$errlog"
            printf '%s OK\n' "$host_id" >> "$SUMMARY"
        else
            printf 'transport EXTRACT_FAILED\n' > "$host_out/_status.txt"
            printf '%s EXTRACT_FAILED\n' "$host_id" >> "$SUMMARY"
            failures=$((failures + 1))
        fi
    else
        printf 'transport UNREACHABLE_OR_AUTH_FAILED\n' > "$host_out/_status.txt"
        printf '%s UNREACHABLE_OR_AUTH_FAILED\n' "$host_id" >> "$SUMMARY"
        failures=$((failures + 1))
    fi
    rm -f "$archive"
done < "$HOSTS_FILE"

[ "$hosts_total" -gt 0 ] || { echo "hosts 檔沒有可採集的主機" >&2; exit 2; }
[ "$failures" -eq 0 ]
