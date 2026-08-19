#!/bin/sh
# Kibana 唯讀 API 子採集器。只保存原始證據，不下診斷結論。

set -eu

URL=""
OUT=""
CA_CERT=""
INSECURE=""

usage() {
    echo "用法: kibana.sh --url <Kibana URL> --output <目錄> [--ca-cert 檔案] [--insecure]" >&2
}

while [ $# -gt 0 ]; do
    case "$1" in
        --url) URL="${2:-}"; shift 2 ;;
        --output) OUT="${2:-}"; shift 2 ;;
        --ca-cert) CA_CERT="${2:-}"; shift 2 ;;
        --insecure) INSECURE=1; shift ;;
        --help) usage; exit 0 ;;
        *) echo "未知參數: $1" >&2; usage; exit 2 ;;
    esac
done

[ -n "$URL" ] && [ -n "$OUT" ] || { usage; exit 2; }
SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
. "$SCRIPT_DIR/http-common.sh"

umask 077
HTTP_CFG=""
trap http_cleanup EXIT INT TERM
http_init "$OUT" "${KIBANA_USERNAME:-}" "${KIBANA_PASSWORD_FILE:-}" \
    "${KIBANA_API_KEY:-}" "$CA_CERT" "$INSECURE"

echo "Kibana 採集目標：${URL%/}"
http_fetch "$URL" '/api/status' 'status.json' 30
http_fetch "$URL" '/api/stats?extended=true&legacy=true' 'stats.json' 30
http_fetch "$URL" '/api/task_manager/_health' 'task_manager_health.json' 30
http_fetch "$URL" '/api/alerting/_health' 'alerting_health.json' 30

echo "Kibana 完成：4 個端點，其中 $HTTP_FAILED 個非 2xx。"
