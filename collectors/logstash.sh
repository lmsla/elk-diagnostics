#!/bin/sh
# Logstash 唯讀 Node API 子採集器。只保存原始證據，不下診斷結論。

set -eu

URL=""
OUT=""
CA_CERT=""
INSECURE=""
SAMPLE_INTERVAL="${LOGSTASH_SAMPLE_INTERVAL:-5}"

usage() {
    echo "用法: logstash.sh --url <Logstash API URL> --output <目錄> [--sample-interval 秒] [--ca-cert 檔案] [--insecure]" >&2
}

while [ $# -gt 0 ]; do
    case "$1" in
        --url) URL="${2:-}"; shift 2 ;;
        --output) OUT="${2:-}"; shift 2 ;;
        --sample-interval) SAMPLE_INTERVAL="${2:-}"; shift 2 ;;
        --ca-cert) CA_CERT="${2:-}"; shift 2 ;;
        --insecure) INSECURE=1; shift ;;
        --help) usage; exit 0 ;;
        *) echo "未知參數: $1" >&2; usage; exit 2 ;;
    esac
done

[ -n "$URL" ] && [ -n "$OUT" ] || { usage; exit 2; }
case "$SAMPLE_INTERVAL" in *[!0-9]*|'') echo "sample interval 必須是非負整數" >&2; exit 2 ;; esac
SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
. "$SCRIPT_DIR/http-common.sh"

umask 077
HTTP_CFG=""
trap http_cleanup EXIT INT TERM
http_init "$OUT" "${LOGSTASH_USERNAME:-}" "${LOGSTASH_PASSWORD_FILE:-}" \
    "${LOGSTASH_API_KEY:-}" "$CA_CERT" "$INSECURE"

echo "Logstash 採集目標：${URL%/}"
http_fetch "$URL" '/_node' 'node_info.json' 30
http_fetch "$URL" '/_node/stats' 'node_stats.json' 30
http_fetch "$URL" '/_node/hot_threads?human=true' 'hot_threads.txt' 60
http_fetch "$URL" '/_node/stats/pipelines' 'pipelines_sample_1.json' 30
if [ "$SAMPLE_INTERVAL" -gt 0 ]; then
    sleep "$SAMPLE_INTERVAL"
    http_fetch "$URL" '/_node/stats/pipelines' 'pipelines_sample_2.json' 30
fi

echo "Logstash 完成：其中 $HTTP_FAILED 個非 2xx。"
