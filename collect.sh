#!/bin/sh
# elk-diagnostics 採集腳本
#
# 由 `elk-diagnostics collect-script` 依端點清單自動產生，請勿手動編輯。
#   工具版本：0.0.4-mvp
#
# 這支腳本只做一件事：對 Elasticsearch 送出 24 個唯讀 GET 請求，把原始回應存成檔案。
# 它不做任何判斷、不修改叢集、不對外傳送任何資料。
#
# 產出的目錄（bundle）可帶到別台機器離線分析，客戶環境不需要安裝或執行本工具：
#     elk-diagnostics check --from-bundle <目錄>
#
# 需求：curl。不需要 jq、python、Java 或任何額外套件。
#
# 用法：
#     ./collect.sh -h https://es.example.local:9200 [-o 輸出目錄] [-u 帳號] [--ca-cert 檔案] [--insecure]
#
# 認證（擇一；一律從環境變數讀取，避免出現在 ps 輸出與 shell 歷史）：
#     ES_PASSWORD='...' ./collect.sh -h https://... -u elastic
#     ES_API_KEY='...'  ./collect.sh -h https://...
#   -u 未給 ES_PASSWORD 時會互動詢問（不回顯）。
#
# 產出內容（交付前請確認）：
#   - 全部為叢集／節點層級的中繼資料，不含任何文件（document）內容
#   - 但含 index 名稱、node 名稱與 IP，以及 mapping 的欄位名稱
#   - _status.txt 記錄每個端點的 HTTP 狀態碼，供分析端還原採集當下的真實情況
#   - _manifest.json 記錄採集開始時間（UTC）與本腳本版本，讓報告能標出「資料取自何時」
#   - 離開此環境前請自行檢視

set -eu

HOST=""
OUT=""
USERNAME=""
CA_CERT=""
INSECURE=""

usage() {
    cat >&2 <<'USAGE'
用法: ./collect.sh -h <ES base URL> [-o 輸出目錄] [-u 帳號] [--ca-cert 檔案] [--insecure]

  -h, --host      ES base URL，例如 https://es.example.local:9200（必填）
  -o, --output    輸出目錄（預設 elk-bundle-<時間戳>）
  -u, --username  basic auth 帳號（密碼取自 ES_PASSWORD，未設則互動詢問）
      --ca-cert   自簽 CA 憑證檔
      --insecure  略過 TLS 憑證驗證（不建議）
      --help      顯示本說明

環境變數: ES_PASSWORD / ES_API_KEY
USAGE
}

while [ $# -gt 0 ]; do
    case "$1" in
        -h|--host)     HOST="${2:-}"; shift 2 ;;
        -o|--output)   OUT="${2:-}"; shift 2 ;;
        -u|--username) USERNAME="${2:-}"; shift 2 ;;
        --ca-cert)     CA_CERT="${2:-}"; shift 2 ;;
        --insecure)    INSECURE=1; shift ;;
        --help)        usage; exit 0 ;;
        *)             echo "未知參數: $1" >&2; usage; exit 2 ;;
    esac
done

if [ -z "$HOST" ]; then
    echo "需提供 -h <ES base URL>" >&2
    usage
    exit 2
fi
HOST="${HOST%/}"
if [ -z "$OUT" ]; then
    OUT="elk-bundle-$(date +%Y%m%d-%H%M%S)"
fi

# 認證與 TLS 選項寫進權限 600 的 curl 設定檔，不放在命令列——命令列參數會出現在
# ps 輸出，同機其他使用者看得到。
umask 077
CURL_CFG="$(mktemp)"
trap 'rm -f "$CURL_CFG"' EXIT INT TERM

if [ -n "${ES_API_KEY:-}" ]; then
    printf 'header = "Authorization: ApiKey %s"\n' "$ES_API_KEY" >> "$CURL_CFG"
elif [ -n "$USERNAME" ]; then
    if [ -z "${ES_PASSWORD:-}" ]; then
        printf '%s 的密碼: ' "$USERNAME" >&2
        stty -echo 2>/dev/null || true
        read -r ES_PASSWORD
        stty echo 2>/dev/null || true
        echo >&2
    fi
    printf 'user = "%s:%s"\n' "$USERNAME" "$ES_PASSWORD" >> "$CURL_CFG"
fi
if [ -n "$CA_CERT" ]; then
    printf 'cacert = "%s"\n' "$CA_CERT" >> "$CURL_CFG"
fi
if [ -n "$INSECURE" ]; then
    printf 'insecure\n' >> "$CURL_CFG"
    echo "警告：已略過 TLS 憑證驗證，僅限測試環境使用" >&2
fi

mkdir -p "$OUT"

# 採集中繼資料：開始時間（UTC）與本腳本版本，讓分析端能標出「資料取自何時」，
# 不必事後靠檔案 mtime 或目錄名猜測（見 docs/specs/spec-bundle.md §4.2）。
# 手寫 JSON 而非用 jq：欄位固定、值本身不含需要跳脫的字元，不值得為此引入依賴。
MANIFEST="$OUT/_manifest.json"
COLLECTED_AT="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
cat > "$MANIFEST" <<MANIFESTEOF
{
  "collect_script_version": "0.0.4-mvp",
  "collected_at": "$COLLECTED_AT",
  "host": "$HOST",
  "endpoints_total": 24
}
MANIFESTEOF

STATUS="$OUT/_status.txt"
ERRLOG="$OUT/_errors.log"
: > "$STATUS"
: > "$ERRLOG"

TOTAL=24
n=0
failed=0

# fetch <端點> <檔名>
#
# 不使用 curl -f：部分端點以 4xx 表達語意而非錯誤（例如叢集健康時，
# allocation/explain 會回 400「沒有未分配的 shard 可解釋」——那個回應本身就是答案）。
# 因此一律保留 body，並把真實狀態碼記進 _status.txt 交給分析端判讀。
fetch() {
    n=$((n + 1))
    code="$(curl -sS --max-time 30 -K "$CURL_CFG" -o "$OUT/$2" -w '%{http_code}' "$HOST$1" 2>>"$ERRLOG" || echo 000)"
    printf '%s %s\n' "$2" "$code" >> "$STATUS"
    case "$code" in
        2*)  printf '  [%2d/%2d] %s\n' "$n" "$TOTAL" "$2" ;;
        000) printf '  [%2d/%2d] %s — 連線失敗（詳見 _errors.log）\n' "$n" "$TOTAL" "$2"
             rm -f "$OUT/$2"
             failed=$((failed + 1)) ;;
        *)   printf '  [%2d/%2d] %s — HTTP %s\n' "$n" "$TOTAL" "$2" "$code"
             failed=$((failed + 1)) ;;
    esac
}

echo "採集目標：$HOST"
echo "輸出目錄：$OUT"
echo

# 版本偵測與 cluster_name（決定走 health_report 或 fallback）
fetch '/' 'version.json'
# 叢集健康總表，A/B 類診斷的地基
fetch '/_health_report' 'health_report.json'
# ILM 服務狀態（RUNNING/STOPPING/STOPPED）
fetch '/_ilm/status' 'ilm_status.json'
# 卡在 ERROR step 的 index（health_report 的 ilm indicator 會延遲，須直接問）
fetch '/_all/_ilm/explain?only_errors=true&only_managed=true' 'ilm_explain_errors.json'
# thread pool 佇列與拒絕數
fetch '/_cat/thread_pool?format=json&h=node_name,name,active,queue,rejected,completed' 'cat_thread_pool.json'
# JVM old pool 記憶體壓力
fetch '/_nodes/stats?filter_path=nodes.*.name,nodes.*.jvm.mem.pools.old' 'nodes_stats_jvm.json'
# circuit breaker 跳閘累積次數
fetch '/_nodes/stats/breaker?filter_path=nodes.*.name,nodes.*.breakers' 'nodes_stats_breaker.json'
# 各節點 CPU／heap／disk 使用率與 allocated_processors
fetch '/_cat/nodes?format=json&h=name,node.role,cpu,load_1m,allocated_processors,heap.percent,disk.used_percent' 'cat_nodes.json'
# 各節點 shard 分布與待搬移數
fetch '/_cat/allocation?format=json&h=node,shards,shards.undesired,disk.percent' 'cat_allocation.json'
# 各 index 的 mapping（僅欄位結構，不含文件內容）
fetch '/_mapping' 'mapping.json'
# ingest pipeline 處理數與失敗數
fetch '/_nodes/stats/ingest?filter_path=nodes.*.ingest.pipelines' 'nodes_stats_ingest.json'
# 各 index 健康與開關狀態
fetch '/_cat/indices?format=json&h=index,health,status' 'cat_indices.json'
# Watcher 服務是否被手動停止
fetch '/_watcher/stats' 'watcher_stats.json'
# transform 執行狀態
fetch '/_transform/_stats' 'transform_stats.json'
# remote cluster 連線狀態
fetch '/_remote/info' 'remote_info.json'
# 升版 deprecation 警告
fetch '/_migration/deprecations' 'migration_deprecations.json'
# 叢集層級設定（allocation.enable、monitoring collection 等生效值）
fetch '/_cluster/settings?include_defaults=true&flat_settings=true' 'cluster_settings.json'
# 各 index 設定（search slow log 門檻）
fetch '/_settings?flat_settings=true' 'all_settings.json'
# 未分配 shard 的 decider 級根因
fetch '/_cluster/allocation/explain' 'allocation_explain.json'
# 受管理 index 的 ILM 階段（tier 遷移候選）
fetch '/_all/_ilm/explain?only_managed=true' 'ilm_explain_managed.json'
# 叢集節點數（master 穩定性佐證）
fetch '/_cluster/health' 'cluster_health.json'
# 各節點角色（master-eligible 數、data tier 分布）
fetch '/_nodes?filter_path=nodes.*.roles' 'nodes_roles.json'
# 進行中的 snapshot 還原進度
fetch '/_recovery?active_only=true' 'recovery.json'
# write thread pool 大小與積壓（寫入瓶頸因果鏈）
fetch '/_cat/thread_pool/write?format=json&h=node_name,name,size,active,queue,rejected' 'cat_thread_pool_write.json'

echo
echo "完成：$TOTAL 個端點，其中 $failed 個非 2xx。"
echo
echo "下一步（在可執行 elk-diagnostics 的機器上）："
echo "    elk-diagnostics check --from-bundle $OUT"
echo
echo "註：非 2xx 不一定代表有問題——部分端點以 4xx 表達語意（例如叢集健康時，"
echo "    allocation/explain 會回 400「沒有未分配的 shard」）。分析端會依 _status.txt"
echo "    的實際狀態碼判讀；真正無法判讀者一律標示為「無法判定」，不會被當成正常。"
