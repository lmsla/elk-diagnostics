#!/bin/sh
# 本機真機驗證用故障控制器。
#
# 只允許操作可丟棄的 elk-diagnostics 測試叢集；禁止用於客戶或共用環境。
# 每個案例提供三個動作：trigger（製造故障）、verify（以 ES API 確認故障成立）、
# restore（復原）。診斷與報告不在此腳本內，避免 Live／Bundle 路線耦合。

set -eu

: "${ES_URL:?請先設定 ES_URL}"
: "${ES_USER:?請先設定 ES_USER}"
: "${ES_PASSWORD:?請先設定 ES_PASSWORD}"
: "${CA_CERT:?請先設定 CA_CERT}"

command -v curl >/dev/null 2>&1 || { echo '缺少 curl' >&2; exit 10; }
command -v jq >/dev/null 2>&1 || { echo '缺少 jq（僅內部故障驗證需要）' >&2; exit 10; }
test -f "$CA_CERT" || { echo "找不到 CA：$CA_CERT" >&2; exit 10; }

es() {
  method="$1"
  endpoint="$2"
  shift 2
  curl --silent --show-error --fail \
    --connect-timeout 5 --max-time 30 \
    --cacert "$CA_CERT" \
    -u "$ES_USER:$ES_PASSWORD" \
    -H 'Content-Type: application/json' \
    -X "$method" "$ES_URL$endpoint" "$@"
}

# 復原可重入：資源已不存在（404）視為成功，其餘 4xx/5xx 仍失敗。
es_allow_404() {
  method="$1"
  endpoint="$2"
  shift 2
  body_file=$(mktemp)
  if ! status=$(curl --silent --show-error \
    --connect-timeout 5 --max-time 30 \
    --cacert "$CA_CERT" \
    -u "$ES_USER:$ES_PASSWORD" \
    -H 'Content-Type: application/json' \
    -X "$method" "$ES_URL$endpoint" \
    -o "$body_file" -w '%{http_code}' "$@"); then
    rm -f "$body_file"
    return 1
  fi
  cat "$body_file"
  rm -f "$body_file"
  case "$status" in
    2??|404) return 0 ;;
    *) echo "HTTP $status: $method $endpoint" >&2; return 1 ;;
  esac
}

wait_for_value() {
  endpoint="$1"
  filter="$2"
  expected="$3"
  label="$4"
  attempt=1
  while [ "$attempt" -le 30 ]; do
    actual=$(es GET "$endpoint" | jq -r "$filter")
    if [ "$actual" = "$expected" ]; then
      echo "$label=$actual"
      return 0
    fi
    sleep 2
    attempt=$((attempt + 1))
  done
  echo "等待逾時：$label，實際=$actual，預期=$expected" >&2
  return 1
}

wait_for_min() {
  endpoint="$1"
  filter="$2"
  minimum="$3"
  label="$4"
  attempt=1
  while [ "$attempt" -le 30 ]; do
    actual=$(es GET "$endpoint" | jq -r "$filter")
    if [ "$actual" -ge "$minimum" ] 2>/dev/null; then
      echo "$label=$actual"
      return 0
    fi
    sleep 2
    attempt=$((attempt + 1))
  done
  echo "等待逾時：$label，實際=$actual，預期>=${minimum}" >&2
  return 1
}

baseline_verify() {
  wait_for_value '/_cluster/health' '.status' 'green' 'cluster_health'
  # cluster health 先恢復不代表 _health_report 的各 indicator 已完成下一輪計算。
  # 尤其 P06 移除 watermark 後，disk indicator 可能短暫保留 red；必須等報告地基也收斂。
  wait_for_value '/_health_report' '.status' 'green' 'health_report'
  wait_for_value '/_ilm/status' '.operation_mode' 'RUNNING' 'ilm_mode'
  wait_for_value '/_watcher/stats' '.manually_stopped | tostring' 'false' 'watcher_stopped'

  settings=$(es GET '/_cluster/settings?flat_settings=true')
  printf '%s\n' "$settings" | jq -e '
    .transient["cluster.routing.allocation.enable"] == null and
    .transient["cluster.max_shards_per_node"] == null and
    .transient["cluster.routing.allocation.disk.watermark.low"] == null and
    .transient["cluster.routing.allocation.disk.watermark.high"] == null and
    .transient["cluster.routing.allocation.disk.watermark.flood_stage"] == null and
    .persistent["xpack.monitoring.collection.enabled"] == null and
    .persistent["cluster.remote.playbook-remote.seeds"] == null and
    .persistent["cluster.remote.playbook-remote.skip_unavailable"] == null
  ' >/dev/null

  wait_for_value '/_all/_settings?flat_settings=true&expand_wildcards=all' \
    '[to_entries[] | .key as $index | .value.settings | to_entries[] | select(
      (.key | startswith("index.blocks.")) and (.value | tostring) == "true"
    ) | $index] | unique | length' '0' 'blocked_indices'

  wait_for_value '/_remote/info' \
    'has("playbook-remote") | tostring' 'false' 'playbook_remote_exists'
  echo 'baseline=clean'
}

p01_trigger() {
  es PUT '/_cluster/settings' --data '{
    "transient":{"cluster.routing.allocation.enable":"none"}
  }' | jq .
  es PUT '/playbook-cluster-block?wait_for_active_shards=0' --data '{
    "settings":{"number_of_shards":1,"number_of_replicas":0}
  }' | jq .
}

p01_verify() {
  wait_for_value '/_cluster/health/playbook-cluster-block' '.status' 'red' 'index_health'
  wait_for_value '/_cluster/settings?flat_settings=true' \
    '.transient["cluster.routing.allocation.enable"] // ""' 'none' 'cluster_allocation'
}

p01_restore() {
  es PUT '/_cluster/settings' --data '{
    "transient":{"cluster.routing.allocation.enable":null}
  }' | jq .
  es_allow_404 DELETE '/playbook-cluster-block' | jq .
}

p02_trigger() {
  es PUT '/playbook-index-block?wait_for_active_shards=0' --data '{
    "settings":{
      "number_of_shards":1,
      "number_of_replicas":0,
      "index.routing.allocation.enable":"none"
    }
  }' | jq .
}

p02_verify() {
  wait_for_value '/_cluster/health/playbook-index-block' '.status' 'red' 'index_health'
  wait_for_value '/playbook-index-block/_settings?flat_settings=true' \
    '.["playbook-index-block"].settings["index.routing.allocation.enable"]' \
    'none' 'index_allocation'
}

p02_restore() {
  es_allow_404 DELETE '/playbook-index-block' | jq .
}

p03_trigger() {
  es PUT '/playbook-replica' --data '{
    "settings":{"number_of_shards":1,"number_of_replicas":1}
  }' | jq .
}

p03_verify() {
  wait_for_value '/_cluster/health/playbook-replica' '.status' 'yellow' 'index_health'
  wait_for_min '/_cluster/health/playbook-replica' '.unassigned_shards' 1 'unassigned_shards'
}

p03_restore() {
  es_allow_404 DELETE '/playbook-replica' | jq .
}

p04_trigger() {
  es PUT '/playbook-shard-limit' --data '{
    "settings":{
      "number_of_shards":2,
      "number_of_replicas":0,
      "index.routing.allocation.total_shards_per_node":1
    }
  }' | jq .
}

p04_verify() {
  wait_for_value '/_cluster/health/playbook-shard-limit' '.status' 'red' 'index_health'
  wait_for_min '/_cluster/health/playbook-shard-limit' '.unassigned_shards' 1 'unassigned_shards'
}

p04_restore() {
  es_allow_404 DELETE '/playbook-shard-limit' | jq .
}

p05_trigger() {
  es PUT '/playbook-capacity-a' --data '{
    "settings":{"number_of_replicas":0}
  }' | jq .
  es PUT '/playbook-capacity-b' --data '{
    "settings":{"number_of_replicas":0}
  }' | jq .
  active_shards=$(es GET '/_cluster/health' | jq -r '.active_shards')
  shard_limit=$((active_shards - 1))
  test "$shard_limit" -ge 1
  es PUT '/_cluster/settings' --data "{
    \"transient\":{\"cluster.max_shards_per_node\":$shard_limit}
  }" | jq .
}

p05_verify() {
  wait_for_value '/_cluster/health' '.status' 'green' 'cluster_health'
  wait_for_value '/_health_report' '.indicators.shards_capacity.status' 'red' 'shards_capacity'
}

p05_restore() {
  es PUT '/_cluster/settings' --data '{
    "transient":{"cluster.max_shards_per_node":null}
  }' | jq .
  es_allow_404 DELETE '/playbook-capacity-a,playbook-capacity-b' | jq .
}

p06_trigger() {
  used=$(es GET '/_cat/allocation?format=json&h=disk.percent' |
    jq -r '.[0]["disk.percent"] | tonumber | floor')
  test "$used" -ge 4
  low=$((used - 3))
  high=$((used - 2))
  flood=$((used - 1))
  es PUT '/_cluster/settings' --data "{
    \"transient\":{
      \"cluster.routing.allocation.disk.watermark.low\":\"${low}%\",
      \"cluster.routing.allocation.disk.watermark.high\":\"${high}%\",
      \"cluster.routing.allocation.disk.watermark.flood_stage\":\"${flood}%\"
    }
  }" | jq .
}

p06_verify() {
  wait_for_value '/_health_report' '.indicators.disk.status' 'red' 'disk_indicator'
}

p06_restore() {
  es PUT '/_cluster/settings' --data '{
    "transient":{
      "cluster.routing.allocation.disk.watermark.low":null,
      "cluster.routing.allocation.disk.watermark.high":null,
      "cluster.routing.allocation.disk.watermark.flood_stage":null
    }
  }' | jq .
}

p07_trigger() {
  es POST '/_ilm/stop' | jq .
}

p07_verify() {
  wait_for_value '/_ilm/status' '.operation_mode' 'STOPPED' 'ilm_mode'
}

p07_restore() {
  es POST '/_ilm/start' | jq .
  wait_for_value '/_ilm/status' '.operation_mode' 'RUNNING' 'ilm_mode'
}

p08_trigger() {
  es PUT '/playbook-ilmerr-000001' --data '{
    "settings":{
      "number_of_replicas":0,
      "index.lifecycle.name":"playbook-policy-does-not-exist",
      "index.lifecycle.rollover_alias":"playbook-ilmerr"
    }
  }' | jq .
}

p08_verify() {
  wait_for_value '/_cluster/health/playbook-ilmerr-000001' \
    '.status' 'green' 'index_health'
  wait_for_value '/playbook-ilmerr-000001/_ilm/explain' \
    '.indices[]?.step' 'ERROR' 'ilm_step'
}

p08_restore() {
  es_allow_404 DELETE '/playbook-ilmerr-000001' | jq .
}

p09_trigger() {
  template=$(jq -nc '
    reduce range(0; 999) as $i (
      {
        "index_patterns":["playbook-explosion-*"],
        "data_stream":{},
        "template":{
          "settings":{"number_of_replicas":0},
          "mappings":{"properties":{"@timestamp":{"type":"date"}}}
        }
      };
      .template.mappings.properties["field_\($i)"]={"type":"keyword"}
    )')
  es PUT '/_index_template/playbook-explosion-template' --data "$template" | jq .
  es POST '/playbook-explosion-default/_doc?refresh=true' --data '{
    "@timestamp":"2026-07-16T00:00:00Z"
  }' | jq .
}

p09_verify() {
  wait_for_value '/_cluster/health/playbook-explosion-default' \
    '.status' 'green' 'index_health'
  wait_for_min '/playbook-explosion-default/_mapping' \
    '[.[] | .mappings.properties | length] | max // 0' 1000 'mapping_fields'
}

p09_restore() {
  es_allow_404 DELETE '/_data_stream/playbook-explosion-default' | jq .
  es_allow_404 DELETE '/_index_template/playbook-explosion-template' | jq .
}

p10_trigger() {
  es PUT '/playbook-slowlog' --data '{
    "settings":{
      "number_of_replicas":0,
      "index.search.slowlog.threshold.query.warn":"10s"
    }
  }' | jq .
}

p10_verify() {
  wait_for_value '/_cluster/health/playbook-slowlog' \
    '.status' 'green' 'index_health'
  wait_for_value '/playbook-slowlog/_settings?flat_settings=true' \
    '.["playbook-slowlog"].settings["index.search.slowlog.threshold.query.warn"]' \
    '10s' 'slowlog_threshold'
}

p10_restore() {
  es_allow_404 DELETE '/playbook-slowlog' | jq .
}

p11_trigger() {
  es POST '/_watcher/_stop' | jq .
}

p11_verify() {
  wait_for_value '/_watcher/stats' '.manually_stopped | tostring' 'true' 'watcher_stopped'
}

p11_restore() {
  es POST '/_watcher/_start' | jq .
  wait_for_value '/_watcher/stats' '.manually_stopped | tostring' 'false' 'watcher_stopped'
}

p12_trigger() {
  es PUT '/playbook-tf-src' --data '{
    "settings":{"number_of_replicas":0},
    "mappings":{"properties":{"g":{"type":"keyword"}}}
  }' | jq .
  es POST '/playbook-tf-src/_doc?refresh=true' --data '{"g":"abc"}' | jq .
  es PUT '/playbook-tf-dest' --data '{
    "settings":{"number_of_replicas":0},
    "mappings":{"properties":{"g":{"type":"long"}}}
  }' | jq .
  es PUT '/_transform/playbook-fail' --data '{
    "source":{"index":"playbook-tf-src"},
    "dest":{"index":"playbook-tf-dest"},
    "pivot":{
      "group_by":{"g":{"terms":{"field":"g"}}},
      "aggregations":{"cnt":{"value_count":{"field":"g"}}}
    },
    "settings":{"num_failure_retries":0}
  }' | jq .
  es POST '/_transform/playbook-fail/_start' | jq .
}

p12_verify() {
  wait_for_value '/_cluster/health/playbook-tf-src,playbook-tf-dest' \
    '.status' 'green' 'index_health'
  wait_for_value '/_transform/playbook-fail/_stats' \
    '.transforms[0].state' 'failed' 'transform_state'
}

p12_restore() {
  es_allow_404 POST '/_transform/playbook-fail/_stop?force=true' --data '{}' | jq .
  es_allow_404 DELETE '/_transform/playbook-fail?force=true' | jq .
  es_allow_404 DELETE '/playbook-tf-src,playbook-tf-dest' | jq .
}

p13_trigger() {
  es PUT '/_cluster/settings' --data '{
    "persistent":{"xpack.monitoring.collection.enabled":true}
  }' | jq .
}

p13_verify() {
  wait_for_value '/_cluster/settings?flat_settings=true' \
    '.persistent["xpack.monitoring.collection.enabled"] | tostring' \
    'true' 'monitoring_collection'
}

p13_restore() {
  es PUT '/_cluster/settings' --data '{
    "persistent":{"xpack.monitoring.collection.enabled":null}
  }' | jq .
}

p14_trigger() {
  es PUT '/_cluster/settings' --data '{
    "persistent":{
      "cluster.remote.playbook-remote.seeds":["127.0.0.1:9399"],
      "cluster.remote.playbook-remote.skip_unavailable":false
    }
  }' | jq .
}

p14_verify() {
  wait_for_value '/_remote/info' \
    '.["playbook-remote"].connected | tostring' 'false' 'remote_connected'
}

p14_restore() {
  es PUT '/_cluster/settings' --data '{
    "persistent":{
      "cluster.remote.playbook-remote.seeds":null,
      "cluster.remote.playbook-remote.skip_unavailable":null
    }
  }' | jq .
}

p15_trigger() {
  es PUT '/playbook-ingest' --data '{
    "settings":{"number_of_replicas":0}
  }' | jq .
  es PUT '/_ingest/pipeline/playbook-failpipe' --data '{
    "processors":[{"fail":{"message":"playbook deliberate failure"}}]
  }' | jq .
  i=1
  while [ "$i" -le 10 ]; do
    es POST '/playbook-ingest/_doc?pipeline=playbook-failpipe' \
      --data "{\"seq\":$i}" >/dev/null 2>&1 || true
    i=$((i + 1))
  done
}

p15_verify() {
  wait_for_value '/_cluster/health/playbook-ingest' \
    '.status' 'green' 'index_health'
  wait_for_min '/_nodes/stats/ingest?filter_path=nodes.*.ingest.pipelines.playbook-failpipe' \
    '[.nodes[].ingest.pipelines["playbook-failpipe"].failed // 0] | add // 0' \
    10 'ingest_failed'
}

p15_restore() {
  es_allow_404 DELETE '/_ingest/pipeline/playbook-failpipe' | jq .
  es_allow_404 DELETE '/playbook-ingest' | jq .
}

p16_trigger() {
  es PUT '/playbook-write-block' --data '{
    "settings":{"number_of_shards":1,"number_of_replicas":0}
  }' | jq .
  es PUT '/playbook-write-block/_settings' --data '{
    "index.blocks.write":true
  }' | jq .
}

p16_verify() {
  wait_for_value '/playbook-write-block/_settings?flat_settings=true' \
    '.["playbook-write-block"].settings["index.blocks.write"] | tostring' \
    'true' 'index_write_block'
}

p16_restore() {
  es_allow_404 DELETE '/playbook-write-block' | jq .
}

usage() {
  cat >&2 <<'USAGE'
用法：./dev/phase0/fault-scenarios.sh <P01..P16|baseline> <trigger|verify|restore>

範例：
  ./dev/phase0/fault-scenarios.sh baseline verify
  ./dev/phase0/fault-scenarios.sh P01 trigger
  ./dev/phase0/fault-scenarios.sh P01 verify
  ./dev/phase0/fault-scenarios.sh P01 restore
USAGE
}

scenario="${1:-}"
action="${2:-}"

if [ "$scenario" = baseline ] && [ "$action" = verify ]; then
  baseline_verify
  exit 0
fi

case "$scenario" in
  P01|P02|P03|P04|P05|P06|P07|P08|P09|P10|P11|P12|P13|P14|P15|P16) ;;
  *) usage; exit 10 ;;
esac

case "$action" in
  trigger|verify|restore) ;;
  *) usage; exit 10 ;;
esac

fn=$(printf '%s_%s' "$scenario" "$action" | tr '[:upper:]' '[:lower:]')
echo "[$scenario] $action"
"$fn"
