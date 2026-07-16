#!/usr/bin/env bash
# Phase 0 擷取腳本：對單一 ES 端點打規格涉及的唯讀 API，存成 fixture，
# 並輸出 _health_report 涵蓋率摘要（驗證架構假設）。
#
# 用法:
#   ./capture.sh <base_url> <label>
# 範例:
#   ./capture.sh https://localhost:9208 es8
#   ./capture.sh https://localhost:9209 es9
#
# 選用環境變數（連線到有安全防護的叢集時）:
#   ES_USER / ES_PASS        basic auth
#   ELASTIC_PASSWORD         未指定 ES_USER 時，等同 elastic / ELASTIC_PASSWORD
#   ES_API_KEY               api key（會送 Authorization: ApiKey ...）
#   CA_CERT                  自簽 CA 路徑（curl --cacert）
#   INSECURE=1               curl -k（略過憑證驗證，僅測試用）
#
# 需求: curl, jq

set -uo pipefail

BASE="${1:?需提供 base_url，例如 https://localhost:9208}"
LABEL="${2:?需提供 label，例如 es8}"
OUT="fixtures/${LABEL}"
mkdir -p "$OUT"

# ---- 組 curl 參數（唯讀；本腳本只發 GET/HEAD）----
CURL=(curl -sS --max-time 15)
[[ -n "${INSECURE:-}" ]] && CURL+=(-k)
[[ -n "${CA_CERT:-}" ]] && CURL+=(--cacert "$CA_CERT")
if [[ -n "${ES_API_KEY:-}" ]]; then
  CURL+=(-H "Authorization: ApiKey ${ES_API_KEY}")
elif [[ -n "${ES_USER:-}" ]]; then
  CURL+=(-u "${ES_USER}:${ES_PASS:-}")
elif [[ -n "${ELASTIC_PASSWORD:-}" ]]; then
  CURL+=(-u "elastic:${ELASTIC_PASSWORD}")
fi

# fetch <name> <method> <path> [body]
fetch() {
  local name="$1" method="$2" path="$3" body="${4:-}"
  local file="${OUT}/${name}.json"
  if [[ -n "$body" ]]; then
    "${CURL[@]}" -X "$method" -H 'Content-Type: application/json' -d "$body" "${BASE}${path}" >"$file" 2>"${file}.err"
  else
    "${CURL[@]}" -X "$method" "${BASE}${path}" >"$file" 2>"${file}.err"
  fi
  if jq -e . "$file" >/dev/null 2>&1; then
    rm -f "${file}.err"
    echo "  ✓ ${name}"
  else
    echo "  ⚠ ${name}（非 JSON 或錯誤，見 ${name}.json / .err）"
  fi
}

echo "== 擷取 ${LABEL} 自 ${BASE} =="

# 版本
fetch version GET "/"

# 採集基座 + A/B 類來源
fetch health_report            GET "/_health_report"
fetch health_report_verbose    GET "/_health_report?verbose=true"
fetch cluster_health           GET "/_cluster/health?filter_path=status,*_shards,number_of_nodes"
fetch cat_shards               GET "/_cat/shards?format=json&h=index,shard,prirep,state,node,unassigned.reason&s=state"
fetch allocation_explain       GET "/_cluster/allocation/explain" '{}'   # 健康叢集會回錯誤，屬正常，照存
fetch cluster_settings_wm      GET "/_cluster/settings?include_defaults=true&flat_settings=true&filter_path=*.cluster.routing.allocation.disk.watermark*"
fetch cluster_settings_shards  GET "/_cluster/settings?include_defaults=true&flat_settings=true&filter_path=*.cluster.max_shards_per_node*"
fetch nodes_stats_fs           GET "/_nodes/stats/fs"
fetch nodes_roles              GET "/_nodes?filter_path=nodes.*.roles,nodes.*.name"
fetch ilm_status               GET "/_ilm/status"
fetch slm_status               GET "/_slm/status"
fetch snapshot_status          GET "/_snapshot/_status"

# C 類缺口來源
fetch cat_thread_pool          GET "/_cat/thread_pool?format=json&h=node_name,name,active,queue,rejected,completed"
fetch nodes_stats_thread_pool  GET "/_nodes/stats/thread_pool"
fetch nodes_stats_jvm          GET "/_nodes/stats/jvm"
fetch nodes_stats_breaker      GET "/_nodes/stats/breaker"
fetch nodes_stats_ingest       GET "/_nodes/stats/ingest"
fetch nodes_stats_indices      GET "/_nodes/stats/indices"
fetch cat_nodes                GET "/_cat/nodes?format=json&h=name,node.role,cpu,load_1m,disk.used_percent"
fetch tasks                    GET "/_tasks?detailed=true&group_by=none"
fetch cat_indices              GET "/_cat/indices?format=json&h=index,health,status,pri,rep,docs.count"
fetch migration_deprecations   GET "/_migration/deprecations"
fetch remote_info              GET "/_remote/info"
fetch transform_stats          GET "/_transform/_stats"
fetch watcher_stats            GET "/_watcher/stats"

# hot_threads 為純文字，單獨存
"${CURL[@]}" "${BASE}/_nodes/hot_threads" >"${OUT}/hot_threads.txt" 2>/dev/null && echo "  ✓ hot_threads.txt"

# ---- health_report 涵蓋率摘要 ----
echo
echo "== ${LABEL} _health_report 涵蓋率 =="
ver=$(jq -r '.version.number // "?"' "${OUT}/version.json" 2>/dev/null)
echo "ES 版本: ${ver}"
if jq -e '.indicators' "${OUT}/health_report.json" >/dev/null 2>&1; then
  echo "頂層 status: $(jq -r '.status // "?"' "${OUT}/health_report.json")"
  echo "indicators 與狀態:"
  jq -r '.indicators | to_entries[] | "  - \(.key): \(.value.status)  symptom=\(.value.symptom // "")"' "${OUT}/health_report.json"
  echo "有 diagnosis 的 indicator（非 green 才有參考價值）:"
  jq -r '.indicators | to_entries[] | select(.value.diagnosis != null) | "  - \(.key): \(.value.diagnosis | length) 筆 diagnosis"' "${OUT}/health_report.json"
else
  echo "  ⚠ 無 indicators（此版本可能 < 8.4 或回應異常）"
fi
echo
echo "fixture 已存於 ${OUT}/。下一步：人工檢視各 indicator 的 diagnosis 是否足以取代 A 類逐 API 判斷（見 dev/phase0/README.md 檢查表）。"
