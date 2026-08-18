#!/bin/sh
# elk-diagnostics 採集腳本
#
# 由 `elk-diagnostics collect-script` 依端點清單自動產生，請勿手動編輯。
#   工具版本：0.0.4-mvp
#
# 預設只對 Elasticsearch 送出 43 個唯讀 GET 請求；透過 --services 可選擇
# Host OS、Kibana、Logstash 子採集器。腳本只保存原始證據，不做判斷、不修改環境。
#
# 產出的目錄（bundle）可帶到別台機器離線分析，使用者環境不需要安裝或執行本工具：
#     elk-diagnostics check --from-bundle <目錄>
#
# ES／Kibana／Logstash 需要 curl；遠端 Host OS 採集另需 ssh、tar 與既有 key／agent。
#
# 用法：
#     ./collect.sh -h https://es.example.local:9200 [-o 輸出目錄]
#     ./collect.sh --services es,kibana,logstash -h https://es:9200 \
#       --kibana-url https://kibana:5601 --logstash-url http://logstash:9600
#
# 認證（擇一；密碼檔優先，避免密碼出現在 ps 輸出、環境變數與 shell 歷史）：
#     ES_PASSWORD_FILE=/path/to/mode-600-file ./collect.sh -h https://... -u elastic
#     ES_API_KEY='...'  ./collect.sh -h https://...
#   既有受控自動化仍可由秘密管理系統注入 ES_PASSWORD；不要手動輸入含密碼的指令。
#   -u 未給密碼檔或 ES_PASSWORD 時會互動詢問（不回顯）。
#
# 產出內容（交付前請確認）：
#   - 全部為叢集／節點層級的中繼資料，不含任何文件（document）內容
#   - 但含 index 名稱、node 名稱與 IP，以及 mapping 的欄位名稱
#   - _status.txt 記錄每個端點的 HTTP 狀態碼，供分析端還原採集當下的真實情況
#   - _manifest.json 記錄採集包格式、採集時間（UTC）與腳本版本
#   - 若提供預期節點清單，會複製成 _expected_es_nodes.txt，供分析端找出採集前已離線節點
#   - 離開此環境前請自行檢視

set -eu

HOST=""
OUT=""
USERNAME=""
CA_CERT=""
INSECURE=""
EXPECTED_ES_NODES_FILE=""
SERVICES="es"
KIBANA_URL_VALUE="${KIBANA_URL:-}"
LOGSTASH_URL_VALUE="${LOGSTASH_URL:-}"
KIBANA_ID="default"
LOGSTASH_ID="default"
HOST_ID=""
SSH_HOSTS_FILE=""
LOGSTASH_SAMPLE_INTERVAL_VALUE="${LOGSTASH_SAMPLE_INTERVAL:-5}"
SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
MODULE_DIR="${COLLECT_MODULE_DIR:-$SCRIPT_DIR/collectors}"

usage() {
    cat >&2 <<'USAGE'
用法: ./collect.sh [--services es,host,kibana,logstash] [各服務連線參數] [-o 輸出目錄]

  -h, --host      ES base URL；services 包含 es 時必填
  -o, --output    輸出目錄（預設 artifacts/bundles/elk-bundle-<時間戳>）
  -u, --username  basic auth 帳號（密碼取自 ES_PASSWORD_FILE／ES_PASSWORD，未設則互動詢問）
      --services  採集模組，逗號分隔；預設 es
      --kibana-url      Kibana base URL
      --logstash-url    Logstash Node API base URL
      --kibana-id       Kibana 產出目錄名稱；預設 default
      --logstash-id     Logstash 產出目錄名稱；預設 default
      --host-id         本機 Host OS 產出目錄名稱；預設 hostname
      --ssh-hosts-file  遠端 Host 清單，每行 host-id|ssh-user@hostname
      --logstash-sample-interval  pipeline 兩次取樣間隔；預設 5 秒，0 表示只取一次
      --ca-cert   自簽 CA 憑證檔
      --expected-es-nodes-file  預期 ES node.name 清單（每行一個；選填）
      --insecure  略過 TLS 憑證驗證（不建議）
      --help      顯示本說明

環境變數:
  ES_PASSWORD_FILE / ES_PASSWORD / ES_API_KEY
  KIBANA_USERNAME / KIBANA_PASSWORD_FILE / KIBANA_API_KEY / KIBANA_CA_CERT
  LOGSTASH_USERNAME / LOGSTASH_PASSWORD_FILE / LOGSTASH_API_KEY / LOGSTASH_CA_CERT
  COLLECT_MODULE_DIR / SSH_CONNECT_TIMEOUT
USAGE
}

while [ $# -gt 0 ]; do
    case "$1" in
        -h|--host)     HOST="${2:-}"; shift 2 ;;
        -o|--output)   OUT="${2:-}"; shift 2 ;;
        -u|--username) USERNAME="${2:-}"; shift 2 ;;
        --services) SERVICES="${2:-}"; shift 2 ;;
        --kibana-url) KIBANA_URL_VALUE="${2:-}"; shift 2 ;;
        --logstash-url) LOGSTASH_URL_VALUE="${2:-}"; shift 2 ;;
        --kibana-id) KIBANA_ID="${2:-}"; shift 2 ;;
        --logstash-id) LOGSTASH_ID="${2:-}"; shift 2 ;;
        --host-id) HOST_ID="${2:-}"; shift 2 ;;
        --ssh-hosts-file) SSH_HOSTS_FILE="${2:-}"; shift 2 ;;
        --logstash-sample-interval) LOGSTASH_SAMPLE_INTERVAL_VALUE="${2:-}"; shift 2 ;;
        --ca-cert)     CA_CERT="${2:-}"; shift 2 ;;
        --expected-es-nodes-file) EXPECTED_ES_NODES_FILE="${2:-}"; shift 2 ;;
        --insecure)    INSECURE=1; shift ;;
        --help)        usage; exit 0 ;;
        *)             echo "未知參數: $1" >&2; usage; exit 2 ;;
    esac
done

case "$SERVICES" in
    ''|,*|*,|*,,*|*[!a-z,]*) echo "--services 格式錯誤：$SERVICES" >&2; exit 2 ;;
esac
for service in $(printf '%s' "$SERVICES" | tr ',' ' '); do
    case "$service" in
        es|host|kibana|logstash) ;;
        *) echo "不支援的採集模組：$service" >&2; exit 2 ;;
    esac
done
service_enabled() {
    case ",$SERVICES," in *",$1,"*) return 0 ;; *) return 1 ;; esac
}

if service_enabled es && [ -z "$HOST" ]; then
    echo "需提供 -h <ES base URL>" >&2
    usage
    exit 2
fi
if [ -n "$EXPECTED_ES_NODES_FILE" ] && [ ! -r "$EXPECTED_ES_NODES_FILE" ]; then
    echo "預期節點清單不可讀：$EXPECTED_ES_NODES_FILE" >&2
    exit 2
fi
if service_enabled kibana && [ -z "$KIBANA_URL_VALUE" ]; then
    echo "services 包含 kibana 時需提供 --kibana-url 或 KIBANA_URL" >&2
    exit 2
fi
if service_enabled logstash && [ -z "$LOGSTASH_URL_VALUE" ]; then
    echo "services 包含 logstash 時需提供 --logstash-url 或 LOGSTASH_URL" >&2
    exit 2
fi
if [ -n "$SSH_HOSTS_FILE" ] && { ! service_enabled host || [ ! -r "$SSH_HOSTS_FILE" ]; }; then
    echo "--ssh-hosts-file 必須搭配 host 模組，且檔案需可讀" >&2
    exit 2
fi
case "$KIBANA_ID" in ''|*[!A-Za-z0-9._-]*) echo "不合法的 kibana-id：$KIBANA_ID" >&2; exit 2 ;; esac
case "$LOGSTASH_ID" in ''|*[!A-Za-z0-9._-]*) echo "不合法的 logstash-id：$LOGSTASH_ID" >&2; exit 2 ;; esac
case "$LOGSTASH_SAMPLE_INTERVAL_VALUE" in *[!0-9]*|'') echo "logstash sample interval 必須是非負整數" >&2; exit 2 ;; esac

for service in host kibana logstash; do
    if service_enabled "$service" && [ ! -x "$MODULE_DIR/$service.sh" ]; then
        echo "找不到可執行的子採集器：$MODULE_DIR/$service.sh" >&2
        exit 2
    fi
done
if [ -n "$SSH_HOSTS_FILE" ] && [ ! -x "$MODULE_DIR/ssh.sh" ]; then
    echo "找不到可執行的 SSH 模組：$MODULE_DIR/ssh.sh" >&2
    exit 2
fi

HOST="${HOST%/}"
if [ -z "$OUT" ]; then
    OUT="artifacts/bundles/elk-bundle-$(date +%Y%m%d-%H%M%S)"
fi

# 認證與 TLS 選項寫進權限 600 的 curl 設定檔，不放在命令列——命令列參數會出現在
# ps 輸出，同機其他使用者看得到。
umask 077
CURL_CFG="$(mktemp)"
trap 'rm -f "$CURL_CFG"' EXIT INT TERM

if service_enabled es && [ -n "${ES_API_KEY:-}" ]; then
    printf 'header = "Authorization: ApiKey %s"\n' "$ES_API_KEY" >> "$CURL_CFG"
elif service_enabled es && [ -n "$USERNAME" ]; then
    if [ -n "${ES_PASSWORD_FILE:-}" ]; then
        if [ ! -r "$ES_PASSWORD_FILE" ]; then
            echo "密碼檔不可讀：$ES_PASSWORD_FILE" >&2
            exit 2
        fi
        ES_PASSWORD="$(cat "$ES_PASSWORD_FILE")"
    elif [ -z "${ES_PASSWORD:-}" ]; then
        printf '%s 的密碼: ' "$USERNAME" >&2
        stty -echo 2>/dev/null || true
        read -r ES_PASSWORD
        stty echo 2>/dev/null || true
        echo >&2
    fi
    case "$ES_PASSWORD" in
        *'
'*) echo "密碼不可包含換行" >&2; exit 2 ;;
    esac
    CURL_USER="$(printf '%s:%s' "$USERNAME" "$ES_PASSWORD" | sed 's/\\/\\\\/g; s/"/\\"/g')"
    printf 'user = "%s"\n' "$CURL_USER" >> "$CURL_CFG"
    unset ES_PASSWORD CURL_USER
fi
if service_enabled es && [ -n "$CA_CERT" ]; then
    printf 'cacert = "%s"\n' "$CA_CERT" >> "$CURL_CFG"
fi
if [ -n "$INSECURE" ]; then
    printf 'insecure\n' >> "$CURL_CFG"
    echo "警告：已略過 TLS 憑證驗證，僅限測試環境使用" >&2
fi

mkdir -p "$OUT"
ES_OUT="$OUT/elasticsearch"
if service_enabled es; then
    mkdir -p "$ES_OUT"
fi
if [ -n "$EXPECTED_ES_NODES_FILE" ]; then
    cp "$EXPECTED_ES_NODES_FILE" "$OUT/_expected_es_nodes.txt"
fi

# 採集中繼資料：開始時間（UTC）與本腳本版本，讓分析端能標出「資料取自何時」，
# 不必事後靠檔案 mtime 或目錄名猜測（見 docs/內部/規格/採集包規格.md §4.2）。
# 手寫 JSON 而非用 jq：欄位固定、值本身不含需要跳脫的字元，不值得為此引入依賴。
MANIFEST="$OUT/_manifest.json"
COLLECTED_AT="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
MANIFEST_SERVICES=""
for service in es host kibana logstash; do
    if service_enabled "$service"; then
        manifest_name="$service"
        [ "$service" = es ] && manifest_name=elasticsearch
        if [ -n "$MANIFEST_SERVICES" ]; then MANIFEST_SERVICES="$MANIFEST_SERVICES, "; fi
        MANIFEST_SERVICES="$MANIFEST_SERVICES\"$manifest_name\""
    fi
done
ES_ENDPOINTS_TOTAL=0
service_enabled es && ES_ENDPOINTS_TOTAL=43
cat > "$MANIFEST" <<MANIFESTEOF
{
  "bundle_schema_version": 2,
  "collect_script_version": "0.0.4-mvp",
  "collected_at": "$COLLECTED_AT",
  "host": "$HOST",
  "endpoints_total": $ES_ENDPOINTS_TOTAL,
  "services": [$MANIFEST_SERVICES]
}
MANIFESTEOF

STATUS="$OUT/_status.txt"
ERRLOG="$ES_OUT/_errors.log"
: > "$STATUS"
service_enabled es && : > "$ERRLOG"

TOTAL=$ES_ENDPOINTS_TOTAL
n=0
failed=0

# fetch <端點> <檔名> <curl 最長秒數>
#
# 不使用 curl -f：部分端點以 4xx 表達語意而非錯誤（例如叢集健康時，
# allocation/explain 會回 400「沒有未分配的 shard 可解釋」——那個回應本身就是答案）。
# 因此一律保留 body，並把真實狀態碼記進 _status.txt 交給分析端判讀。
fetch() {
    n=$((n + 1))
    request_timeout="${3:-30}"
    if code="$(curl -q -sS --max-time "$request_timeout" -K "$CURL_CFG" -o "$ES_OUT/$2" -w '%{http_code}' "$HOST$1" 2>>"$ERRLOG")"; then
        :
    else
        # curl 在 DNS／連線／逾時失敗時，可能先由 -w 輸出 000 再以非零狀態退出。
        # 不可再串接 echo 000，否則 command substitution 會得到 000000。
        code=000
    fi
    printf 'elasticsearch/%s %s\n' "$2" "$code" >> "$STATUS"
    case "$code" in
        2*)  printf '  [%2d/%2d] %s\n' "$n" "$TOTAL" "$2" ;;
        000) printf '  [%2d/%2d] %s — 連線失敗（詳見 elasticsearch/_errors.log）\n' "$n" "$TOTAL" "$2"
             rm -f "$ES_OUT/$2"
             failed=$((failed + 1)) ;;
        *)   printf '  [%2d/%2d] %s — HTTP %s\n' "$n" "$TOTAL" "$2" "$code"
             failed=$((failed + 1)) ;;
    esac
}

echo "採集模組：$SERVICES"
[ -n "$HOST" ] && echo "ES 採集目標：$HOST"
echo "輸出目錄：$OUT"
if [ -n "$EXPECTED_ES_NODES_FILE" ]; then
    echo "預期節點：$OUT/_expected_es_nodes.txt"
fi
echo
if service_enabled es; then

# 版本偵測與 cluster_name（決定走 health_report 或 fallback）
fetch '/' 'version.json' '30'
# 叢集健康總表，A/B 類診斷的地基
fetch '/_health_report' 'health_report.json' '30'
# ILM 服務狀態（RUNNING/STOPPING/STOPPED）
fetch '/_ilm/status' 'ilm_status.json' '30'
# 卡在 ERROR step 的 index（health_report 的 ilm indicator 會延遲，須直接問）
fetch '/_all/_ilm/explain?only_errors=true&only_managed=true' 'ilm_explain_errors.json' '30'
# ILM policy 的 phase/action 與使用關聯（不含文件內容）
fetch '/_ilm/policy?filter_path=*.version,*.modified_date_millis,*.policy.phases,*.in_use_by' 'ilm_policies.json' '30'
# thread pool 佇列與拒絕數
fetch '/_cat/thread_pool?format=json&h=node_name,name,active,queue,rejected,completed' 'cat_thread_pool.json' '10'
# 各節點 OS／process／filesystem／JVM 快照與 JVM old pool 記憶體壓力
fetch '/_nodes/stats/os,process,fs,jvm?timeout=5s&filter_path=_nodes,nodes.*.name,nodes.*.ip,nodes.*.roles,nodes.*.os.cpu,nodes.*.os.load_average,nodes.*.os.mem,nodes.*.os.swap,nodes.*.os.cgroup,nodes.*.process.cpu,nodes.*.process.mem,nodes.*.process.open_file_descriptors,nodes.*.process.max_file_descriptors,nodes.*.fs.total,nodes.*.fs.data,nodes.*.fs.io_stats,nodes.*.jvm.uptime_in_millis,nodes.*.jvm.mem,nodes.*.jvm.gc' 'nodes_stats_jvm.json' '10'
# 各節點 OS 版本／架構／processors、PID 與 memory lock 狀態
fetch '/_nodes/os,process?timeout=5s&filter_path=_nodes,nodes.*.name,nodes.*.ip,nodes.*.roles,nodes.*.os.name,nodes.*.os.pretty_name,nodes.*.os.arch,nodes.*.os.version,nodes.*.os.available_processors,nodes.*.os.allocated_processors,nodes.*.process.id,nodes.*.process.mlockall' 'nodes_info_os_process.json' '10'
# circuit breaker 跳閘累積次數
fetch '/_nodes/stats/breaker?timeout=5s&filter_path=nodes.*.name,nodes.*.breakers' 'nodes_stats_breaker.json' '10'
# 各節點 CPU／heap／disk 使用率與 allocated_processors
fetch '/_cat/nodes?format=json&h=name,node.role,cpu,load_1m,allocated_processors,heap.percent,disk.used_percent' 'cat_nodes.json' '10'
# 各節點 shard 分布與待搬移數
fetch '/_cat/allocation?format=json&h=node,shards,shards.undesired,disk.percent' 'cat_allocation.json' '30'
# 各 index 的 mapping（僅欄位結構，不含文件內容）
fetch '/_mapping' 'mapping.json' '30'
# ingest pipeline 處理數與失敗數
fetch '/_nodes/stats/ingest?timeout=5s&filter_path=nodes.*.ingest.pipelines' 'nodes_stats_ingest.json' '10'
# 各 index 健康與開關狀態
fetch '/_cat/indices?format=json&h=index,health,status' 'cat_indices.json' '30'
# Watcher 服務是否被手動停止
fetch '/_watcher/stats' 'watcher_stats.json' '30'
# transform 執行狀態
fetch '/_transform/_stats' 'transform_stats.json' '30'
# remote cluster 連線狀態
fetch '/_remote/info' 'remote_info.json' '30'
# 升版 deprecation 警告
fetch '/_migration/deprecations' 'migration_deprecations.json' '30'
# 叢集層級設定（allocation.enable、monitoring collection 等生效值）
fetch '/_cluster/settings?include_defaults=true&flat_settings=true' 'cluster_settings.json' '30'
# 各 index 設定（search slow log 門檻）
fetch '/_settings?flat_settings=true' 'all_settings.json' '30'
# 未分配 shard 的 decider 級根因
fetch '/_cluster/allocation/explain' 'allocation_explain.json' '30'
# 受管理 index 的 ILM 階段（tier 遷移候選）
fetch '/_all/_ilm/explain?only_managed=true' 'ilm_explain_managed.json' '30'
# 叢集節點數（master 穩定性佐證）
fetch '/_cluster/health' 'cluster_health.json' '30'
# 各節點角色（master-eligible 數、data tier 分布）
fetch '/_nodes?timeout=5s&filter_path=nodes.*.roles' 'nodes_roles.json' '10'
# 進行中的 snapshot 還原進度
fetch '/_recovery?active_only=true' 'recovery.json' '30'
# write thread pool 大小與積壓（寫入瓶頸因果鏈）
fetch '/_cat/thread_pool/write?format=json&h=node_name,name,size,active,queue,rejected' 'cat_thread_pool_write.json' '10'
# 尚未套用的 cluster state task 與排隊時間
fetch '/_cluster/pending_tasks' 'cluster_pending_tasks.json' '30'
# 目前執行中的 task 與執行時間（不採集 request body/header）
fetch '/_tasks?timeout=5s&detailed=true&group_by=none&filter_path=tasks.*.node,tasks.*.type,tasks.*.action,tasks.*.description,tasks.*.running_time_in_nanos,tasks.*.cancellable' 'running_tasks.json' '10'
# 各 shard 大小、文件數與配置節點（shard sizing）
fetch '/_cat/shards?format=json&bytes=b&h=index,shard,prirep,state,node,store,docs' 'cat_shards_sizing.json' '30'
# SLM policy 最近成功／失敗時間與下次執行時間
fetch '/_slm/policy?filter_path=*.policy.repository,*.modified_date_millis,*.next_execution_millis,*.last_success.snapshot_name,*.last_success.time,*.last_failure.snapshot_name,*.last_failure.time,*.stats.snapshots_taken,*.stats.snapshots_failed' 'slm_policies.json' '30'
# Snapshot repository 名稱與類型（不採集 location/bucket 等 settings）
fetch '/_snapshot/_all?filter_path=*.type,*.uuid' 'snapshot_repositories.json' '30'
# Data stream 健康、backing index、template 與生命週期管理關聯
fetch '/_data_stream?filter_path=data_streams.name,data_streams.status,data_streams.template,data_streams.generation,data_streams.ilm_policy,data_streams.next_generation_managed_by,data_streams.indices.index_name,data_streams.indices.ilm_policy,data_streams.indices.managed_by' 'data_streams.json' '30'
# 各節點 ES/JDK/heap/plugin 一致性與 Nodes API coverage
fetch '/_nodes/jvm,plugins?timeout=5s&filter_path=_nodes,nodes.*.name,nodes.*.roles,nodes.*.version,nodes.*.build_hash,nodes.*.jvm.version,nodes.*.jvm.vm_version,nodes.*.jvm.mem.heap_init_in_bytes,nodes.*.jvm.mem.heap_max_in_bytes,nodes.*.plugins.name,nodes.*.plugins.version' 'nodes_runtime.json' '10'
# 各節點 fielddata cache 記憶體與 eviction 累積值
fetch '/_nodes/stats/indices/fielddata?timeout=5s&filter_path=_nodes,nodes.*.name,nodes.*.indices.fielddata.memory_size_in_bytes,nodes.*.indices.fielddata.evictions' 'nodes_fielddata.json' '10'
# 回應節點載入的 TLS 憑證與到期日（單節點視角）
fetch '/_ssl/certificates' 'ssl_certificates.json' '30'
# 叢集 License 狀態、類型與到期日
fetch '/_license?filter_path=license.status,license.type,license.issued_to,license.expiry_date_in_millis' 'license.json' '30'
# 節點角色與 allocation awareness attributes
fetch '/_nodes?timeout=5s&filter_path=_nodes,nodes.*.name,nodes.*.roles,nodes.*.attributes' 'nodes_topology.json' '10'
# 各節點當下 indexing pressure 記憶體使用量與限制（不使用累積 rejection 下趨勢結論）
fetch '/_nodes/stats/indexing_pressure?timeout=5s&filter_path=_nodes,nodes.*.name,nodes.*.indexing_pressure.memory.current.combined_coordinating_and_primary_in_bytes,nodes.*.indexing_pressure.memory.current.replica_in_bytes,nodes.*.indexing_pressure.memory.current.all_in_bytes,nodes.*.indexing_pressure.memory.limit_in_bytes' 'nodes_indexing_pressure.json' '10'
# CCR follower checkpoint lag、fatal/read exception 與 auto-follow 失敗
fetch '/_ccr/stats?filter_path=auto_follow_stats.number_of_failed_follow_indices,auto_follow_stats.number_of_failed_remote_cluster_state_requests,auto_follow_stats.recent_auto_follow_errors,follow_stats.indices.index,follow_stats.indices.total_global_checkpoint_lag,follow_stats.indices.shards.shard_id,follow_stats.indices.shards.leader_global_checkpoint,follow_stats.indices.shards.follower_global_checkpoint,follow_stats.indices.shards.fatal_exception,follow_stats.indices.shards.read_exceptions' 'ccr_stats.json' '30'
# Machine Learning anomaly detection job 執行狀態
fetch '/_ml/anomaly_detectors/_stats?allow_no_match=true&filter_path=count,jobs.job_id,jobs.state,jobs.assignment_explanation' 'ml_job_stats.json' '30'
# Machine Learning datafeed 執行與 assignment 狀態
fetch '/_ml/datafeeds/_stats?allow_no_match=true&filter_path=count,datafeeds.datafeed_id,datafeeds.state,datafeeds.assignment_explanation,datafeeds.timing_stats.job_id' 'ml_datafeed_stats.json' '30'
# 已登記的 planned shutdown 狀態（選配高權限檢查）
fetch '/_nodes/shutdown' 'planned_shutdown.json' '30'
# cluster state 中尚未清除的 voting configuration exclusions
fetch '/_cluster/state/metadata?filter_path=metadata.cluster_coordination.voting_config_exclusions' 'voting_exclusions.json' '30'
fi

module_failed=0
if service_enabled host; then
    if [ -n "$SSH_HOSTS_FILE" ]; then
        if ! "$MODULE_DIR/ssh.sh" --hosts-file "$SSH_HOSTS_FILE" \
            --host-collector "$MODULE_DIR/host.sh" --output "$OUT/host"; then
            echo "Host SSH 採集部分或全部失敗，詳見 host/_status.txt" >&2
            module_failed=$((module_failed + 1))
        fi
    else
        if [ -z "$HOST_ID" ]; then
            HOST_ID="$(hostname 2>/dev/null | sed 's/[^A-Za-z0-9._-]/_/g')"
            [ -n "$HOST_ID" ] || HOST_ID=local
        fi
        case "$HOST_ID" in *[!A-Za-z0-9._-]*) echo "不合法的 host-id：$HOST_ID" >&2; exit 2 ;; esac
        if ! "$MODULE_DIR/host.sh" --output "$OUT/host/$HOST_ID"; then
            echo "本機 Host OS 採集失敗" >&2
            module_failed=$((module_failed + 1))
        fi
    fi
fi

if service_enabled kibana; then
    set -- --url "$KIBANA_URL_VALUE" --output "$OUT/kibana/$KIBANA_ID"
    KIBANA_CA_CERT_VALUE="${KIBANA_CA_CERT:-$CA_CERT}"
    [ -n "$KIBANA_CA_CERT_VALUE" ] && set -- "$@" --ca-cert "$KIBANA_CA_CERT_VALUE"
    [ -n "$INSECURE" ] && set -- "$@" --insecure
    if ! "$MODULE_DIR/kibana.sh" "$@"; then
        echo "Kibana 採集失敗，詳見 kibana/$KIBANA_ID/_status.txt" >&2
        module_failed=$((module_failed + 1))
    fi
fi

if service_enabled logstash; then
    set -- --url "$LOGSTASH_URL_VALUE" --output "$OUT/logstash/$LOGSTASH_ID" \
        --sample-interval "$LOGSTASH_SAMPLE_INTERVAL_VALUE"
    LOGSTASH_CA_CERT_VALUE="${LOGSTASH_CA_CERT:-$CA_CERT}"
    [ -n "$LOGSTASH_CA_CERT_VALUE" ] && set -- "$@" --ca-cert "$LOGSTASH_CA_CERT_VALUE"
    [ -n "$INSECURE" ] && set -- "$@" --insecure
    if ! "$MODULE_DIR/logstash.sh" "$@"; then
        echo "Logstash 採集失敗，詳見 logstash/$LOGSTASH_ID/_status.txt" >&2
        module_failed=$((module_failed + 1))
    fi
fi

echo
echo "完成：ES $n/$TOTAL 個端點，其中 $failed 個非 2xx；子採集器失敗 $module_failed 個。"
echo
if service_enabled es; then
    echo "下一步（在可執行 elk-diagnostics 的機器上）："
    echo "    elk-diagnostics check --from-bundle $OUT"
    echo
    echo "註：非 2xx 不一定代表有問題——部分端點以 4xx 表達語意（例如叢集健康時，"
    echo "    allocation/explain 會回 400「沒有未分配的 shard」）。分析端會依 _status.txt"
    echo "    的實際狀態碼判讀；真正無法判讀者一律標示為「無法判定」，不會被當成正常。"
else
    echo "此採集包未包含 Elasticsearch，現行 check 無法單獨分析；請作為原始證據保存。"
fi
