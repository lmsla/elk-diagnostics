#!/bin/sh
# Kibana／Logstash 子採集器共用的唯讀 HTTP 傳輸層。
# 本檔由子採集器 source，不直接執行。

http_init() {
    HTTP_OUT="$1"
    HTTP_USERNAME="$2"
    HTTP_PASSWORD_FILE="$3"
    HTTP_API_KEY="$4"
    HTTP_CA_CERT="$5"
    HTTP_INSECURE="$6"

    command -v curl >/dev/null 2>&1 || {
        echo "缺少 curl，無法執行 API 採集" >&2
        return 2
    }
    mkdir -p "$HTTP_OUT"
    HTTP_STATUS="$HTTP_OUT/_status.txt"
    HTTP_ERRLOG="$HTTP_OUT/_errors.log"
    HTTP_CFG="$HTTP_OUT/.curl-config.$$"
    : > "$HTTP_STATUS"
    : > "$HTTP_ERRLOG"
    : > "$HTTP_CFG"
    chmod 600 "$HTTP_CFG"
    HTTP_FAILED=0

    if [ -n "$HTTP_API_KEY" ]; then
        printf 'header = "Authorization: ApiKey %s"\n' "$HTTP_API_KEY" >> "$HTTP_CFG"
    elif [ -n "$HTTP_USERNAME" ]; then
        if [ -z "$HTTP_PASSWORD_FILE" ] || [ ! -r "$HTTP_PASSWORD_FILE" ]; then
            echo "已設定 API 帳號，但密碼檔不存在或不可讀" >&2
            return 2
        fi
        HTTP_PASSWORD="$(cat "$HTTP_PASSWORD_FILE")"
        case "$HTTP_PASSWORD" in
            *'
'*) echo "API 密碼不可包含換行" >&2; return 2 ;;
        esac
        HTTP_USER="$(printf '%s:%s' "$HTTP_USERNAME" "$HTTP_PASSWORD" | sed 's/\\/\\\\/g; s/"/\\"/g')"
        printf 'user = "%s"\n' "$HTTP_USER" >> "$HTTP_CFG"
        unset HTTP_PASSWORD HTTP_USER
    fi
    if [ -n "$HTTP_CA_CERT" ]; then
        [ -r "$HTTP_CA_CERT" ] || { echo "CA 憑證不可讀：$HTTP_CA_CERT" >&2; return 2; }
        printf 'cacert = "%s"\n' "$HTTP_CA_CERT" >> "$HTTP_CFG"
    fi
    if [ -n "$HTTP_INSECURE" ]; then
        printf 'insecure\n' >> "$HTTP_CFG"
    fi
    return 0
}

http_cleanup() {
    [ -n "${HTTP_CFG:-}" ] && rm -f "$HTTP_CFG"
    return 0
}

# http_fetch <base URL> <path> <output file> [timeout]
http_fetch() {
    http_base="${1%/}"
    http_path="$2"
    http_file="$3"
    http_timeout="${4:-30}"
    if http_code="$(curl -q -sS --max-time "$http_timeout" -K "$HTTP_CFG" \
        -o "$HTTP_OUT/$http_file" -w '%{http_code}' "$http_base$http_path" 2>>"$HTTP_ERRLOG")"; then
        :
    else
        http_code=000
    fi
    printf '%s %s\n' "$http_file" "$http_code" >> "$HTTP_STATUS"
    case "$http_code" in
        2*) printf '  %s\n' "$http_file" ;;
        000)
            printf '  %s — 連線失敗\n' "$http_file" >&2
            rm -f "$HTTP_OUT/$http_file"
            HTTP_FAILED=$((HTTP_FAILED + 1))
            ;;
        *)
            printf '  %s — HTTP %s\n' "$http_file" "$http_code" >&2
            HTTP_FAILED=$((HTTP_FAILED + 1))
            ;;
    esac
}
