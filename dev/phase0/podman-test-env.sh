#!/usr/bin/env bash
# macOS + Podman 的本機 ES 測試環境。
# 固定基線：自簽 CA、HTTPS、Basic Auth、只綁 localhost；Kibana 選配。

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CERTS_DIR="${ROOT_DIR}/certs"
RUNTIME_DIR="${ROOT_DIR}/runtime"
MULTINODE_RUNTIME_DIR="${RUNTIME_DIR}/multinode"
CA_CERT="${CERTS_DIR}/ca/ca.crt"
NETWORK="elk-diagnostics"
TEST_PASSWORD="${ELK_DIAGNOSTICS_TEST_PASSWORD:-elk-diagnostics-test-only}"
ES8_VERSION="${ES8_VERSION:-8.14.3}"
ES9_VERSION="${ES9_VERSION:-9.0.0}"
ES8_IMAGE="docker.elastic.co/elasticsearch/elasticsearch:${ES8_VERSION}"
ES9_IMAGE="docker.elastic.co/elasticsearch/elasticsearch:${ES9_VERSION}"
KIBANA8_IMAGE="docker.elastic.co/kibana/kibana:${ES8_VERSION}"
KIBANA9_IMAGE="docker.elastic.co/kibana/kibana:${ES9_VERSION}"

managed_containers=(
  elk-diagnostics-certs-setup
  elk-diagnostics-es8 elk-diagnostics-kibana8-password elk-diagnostics-kibana8
  elk-diagnostics-es9 elk-diagnostics-kibana9-password elk-diagnostics-kibana9
  elk-diagnostics-es8-mn1 elk-diagnostics-es8-mn2 elk-diagnostics-es8-mn3 elk-diagnostics-es8-mn4
  elk-diagnostics-kibana8-mn
)
multinode_containers=(
  elk-diagnostics-kibana8-mn
  elk-diagnostics-es8-mn4
  elk-diagnostics-es8-mn1
  elk-diagnostics-es8-mn2
  elk-diagnostics-es8-mn3
)
legacy_containers=(
  elkdoctor-certs-setup
  elkdoctor-es8 elkdoctor-kibana8-password elkdoctor-kibana8
  elkdoctor-es9 elkdoctor-kibana9-password elkdoctor-kibana9
)

usage() {
  cat <<'EOF'
用法：
  ./podman-test-env.sh up          # 預設：只啟動 ES 8/9
  ./podman-test-env.sh up-kibana   # 選配：在 ES 已啟動後加入 Kibana 8/9
  ./podman-test-env.sh up-multinode    # ES 8 三節點、多角色、多 tier（:9218）
  ./podman-test-env.sh up-multinode-kibana  # 加入三節點專用 Kibana 8（:5611）
  ./podman-test-env.sh down-multinode  # 只移除三節點環境
  ./podman-test-env.sh status
  ./podman-test-env.sh down

本機測試帳密：elastic / elk-diagnostics-test-only
EOF
}

container_exists() {
  podman container exists "$1"
}

generate_certs() {
  mkdir -p "$CERTS_DIR"
  if [[ -f "$CA_CERT" &&
        -f "${CERTS_DIR}/es8/es8.crt" &&
        -f "${CERTS_DIR}/es8-mn1/es8-mn1.crt" &&
        -f "${CERTS_DIR}/es8-mn2/es8-mn2.crt" &&
        -f "${CERTS_DIR}/es8-mn3/es8-mn3.crt" &&
        -f "${CERTS_DIR}/es8-mn4/es8-mn4.crt" ]] &&
     openssl x509 -in "${CERTS_DIR}/es8/es8.crt" -text -noout 2>/dev/null |
       grep -q 'DNS:elk-diagnostics-es8'; then
    return
  fi

  # SAN 不含新容器名時視為舊版憑證，刪掉讓共用腳本重生（此檢查只決定「要不要跑」，
  # 產生邏輯本身只有 gen-certs-in-container.sh 一份，與 docker-compose.yml 共用）
  if [[ -f "${CERTS_DIR}/es8/es8.crt" ]] &&
     ! openssl x509 -in "${CERTS_DIR}/es8/es8.crt" -text -noout 2>/dev/null |
         grep -q 'DNS:elk-diagnostics-es8'; then
    rm -rf "${CERTS_DIR}/es8" "${CERTS_DIR}/es9" "${CERTS_DIR}/kibana8" "${CERTS_DIR}/kibana9"
  fi

  echo "產生本機自簽 CA 與 server certificates..."
  podman run --rm --user 0 \
    -v "${CERTS_DIR}:/usr/share/elasticsearch/config/certs" \
    -v "${ROOT_DIR}/gen-certs-in-container.sh:/gen-certs-in-container.sh:ro" \
    "$ES8_IMAGE" bash /gen-certs-in-container.sh
}

container_running() {
  [[ "$(podman inspect --format '{{.State.Running}}' "$1" 2>/dev/null || true)" == "true" ]]
}

wait_https() {
  local url="$1" label="$2" auth="${3:-}"
  for _ in $(seq 1 60); do
    if [[ -n "$auth" ]]; then
      curl --silent --fail --cacert "$CA_CERT" -u "$auth" "$url" >/dev/null && {
        echo "${label} ready"; return;
      }
    else
      curl --silent --fail --cacert "$CA_CERT" "$url" >/dev/null && {
        echo "${label} ready"; return;
      }
    fi
    sleep 5
  done
  echo "${label} 未在 300 秒內就緒；請查看 podman logs" >&2
  exit 1
}

start_es() {
  local name="$1" host="$2" port="$3" cert="$4" image="$5"
  export ELASTIC_PASSWORD="$TEST_PASSWORD"
  podman run -d --name "$name" --hostname "$host" --network "$NETWORK" \
    -p "127.0.0.1:${port}:9200" \
    -e ELASTIC_PASSWORD \
    -e discovery.type=single-node \
    -e xpack.security.enabled=true \
    -e xpack.security.http.ssl.enabled=true \
    -e "xpack.security.http.ssl.key=certs/${cert}/${cert}.key" \
    -e "xpack.security.http.ssl.certificate=certs/${cert}/${cert}.crt" \
    -e xpack.security.http.ssl.certificate_authorities=certs/ca/ca.crt \
    -e xpack.security.transport.ssl.enabled=true \
    -e xpack.security.transport.ssl.verification_mode=certificate \
    -e "xpack.security.transport.ssl.key=certs/${cert}/${cert}.key" \
    -e "xpack.security.transport.ssl.certificate=certs/${cert}/${cert}.crt" \
    -e xpack.security.transport.ssl.certificate_authorities=certs/ca/ca.crt \
    -e "ES_JAVA_OPTS=-Xms512m -Xmx512m" \
    -v "${CERTS_DIR}:/usr/share/elasticsearch/config/certs:ro" \
    "$image" >/dev/null
  unset ELASTIC_PASSWORD
}

write_multinode_config() {
  local node="$1" zone="$2" roles="$3" bootstrap="${4:-yes}"
  mkdir -p "$MULTINODE_RUNTIME_DIR"
  cat > "${MULTINODE_RUNTIME_DIR}/${node}.yml" <<EOF
cluster.name: elk-diagnostics-es8-multinode
node.name: ${node}
node.roles: ${roles}
node.attr.zone: ${zone}
network.host: 0.0.0.0
discovery.seed_hosts: [es8-mn1, es8-mn2, es8-mn3]
cluster.routing.allocation.awareness.attributes: zone
# Test-only: M08 必須讓暫停中的 helper node 在 binary 完成 Nodes API 採集前
# 仍保留於 cluster membership，才能穩定重現 _nodes.total > successful。
cluster.fault_detection.follower_check.retry_count: 12
xpack.security.enabled: true
xpack.security.http.ssl.enabled: true
xpack.security.http.ssl.key: certs/${node}/${node}.key
xpack.security.http.ssl.certificate: certs/${node}/${node}.crt
xpack.security.http.ssl.certificate_authorities: certs/ca/ca.crt
xpack.security.transport.ssl.enabled: true
xpack.security.transport.ssl.verification_mode: certificate
xpack.security.transport.ssl.key: certs/${node}/${node}.key
xpack.security.transport.ssl.certificate: certs/${node}/${node}.crt
xpack.security.transport.ssl.certificate_authorities: certs/ca/ca.crt
EOF
  if [[ "$bootstrap" == yes ]]; then
    printf '%s\n' \
      'cluster.initial_master_nodes: [es8-mn1, es8-mn2, es8-mn3]' \
      >> "${MULTINODE_RUNTIME_DIR}/${node}.yml"
  fi
}

start_multinode_node() {
  local node="$1" port="${2:-}" heap="${3:-512m}" data_tmpfs="${4:-}"
  local args=(
    -d
    --name "elk-diagnostics-${node}"
    --hostname "$node"
    --network "$NETWORK"
    -e "ELASTIC_PASSWORD=${TEST_PASSWORD}"
    -e "ES_JAVA_OPTS=-Xms${heap} -Xmx${heap}"
    -v "${CERTS_DIR}:/usr/share/elasticsearch/config/certs:ro"
    -v "${MULTINODE_RUNTIME_DIR}/${node}.yml:/usr/share/elasticsearch/config/elasticsearch.yml:ro"
  )
  if [[ -n "$port" ]]; then
    args+=(-p "127.0.0.1:${port}:9200")
  fi
  if [[ -n "$data_tmpfs" ]]; then
    args+=(--tmpfs "/usr/share/elasticsearch/data:rw,size=${data_tmpfs},mode=0777")
  fi
  podman run "${args[@]}" "$ES8_IMAGE" >/dev/null
}

wait_multinode_count() {
  local expected="$1"
  for _ in $(seq 1 60); do
    if curl --silent --fail --cacert "$CA_CERT" -u "elastic:${TEST_PASSWORD}" \
      "https://localhost:9218/_cluster/health?wait_for_nodes=${expected}&wait_for_status=yellow&timeout=5s" |
      grep -q "\"number_of_nodes\":${expected}"; then
      return
    fi
    sleep 2
  done
  echo "ES 8 multi-node 未在 120 秒內到達 ${expected} 個節點" >&2
  exit 1
}

multinode_helper_up() {
  local profile="${1:-}"
  for name in elk-diagnostics-es8-mn1 elk-diagnostics-es8-mn2 elk-diagnostics-es8-mn3; do
    container_running "$name" || {
      echo "三節點環境未完整執行；無法啟動 helper node" >&2
      exit 1
    }
  done
  if container_exists elk-diagnostics-es8-mn4; then
    echo "helper node 已存在；請先執行 multinode-helper-down" >&2
    exit 1
  fi

  generate_certs
  local roles heap tmpfs=''
  case "$profile" in
    runtime)
      roles='[master, data_hot, data_content, ingest, transform, remote_cluster_client]'
      heap='384m'
      ;;
    master)
      roles='[master]'
      heap='256m'
      ;;
    master-disk)
      roles='[master]'
      heap='256m'
      tmpfs='256m'
      ;;
    data-disk)
      roles='[data_hot, data_content, ingest, transform, remote_cluster_client]'
      heap='256m'
      tmpfs='256m'
      ;;
    partial)
      roles='[]'
      heap='256m'
      ;;
    *)
      echo "未知 helper profile：${profile}" >&2
      exit 2
      ;;
  esac

  write_multinode_config es8-mn4 zone-d "$roles" no
  start_multinode_node es8-mn4 '' "$heap" "$tmpfs"
  printf '%s\n' "$profile" > "${MULTINODE_RUNTIME_DIR}/helper-profile"
  wait_multinode_count 4
  echo "ES 8 helper ready: profile=${profile} node=es8-mn4"
}

multinode_helper_down() {
  if container_exists elk-diagnostics-es8-mn4; then
    podman unpause elk-diagnostics-es8-mn4 >/dev/null 2>&1 || true
    podman rm -f elk-diagnostics-es8-mn4 >/dev/null
  fi
  rm -f "${MULTINODE_RUNTIME_DIR}/es8-mn4.yml" \
    "${MULTINODE_RUNTIME_DIR}/helper-profile"
  wait_multinode_count 3
  echo "ES 8 helper removed"
}

up_multinode() {
  local running=()
  for name in elk-diagnostics-es8 elk-diagnostics-kibana8 elk-diagnostics-es9 elk-diagnostics-kibana9; do
    container_running "$name" && running+=("$name")
  done
  if ((${#running[@]} > 0)); then
    echo "啟動三節點環境前請先停止單節點服務：${running[*]}" >&2
    echo "使用 podman stop <容器名>；不需要刪除容器。" >&2
    exit 1
  fi
  for name in "${multinode_containers[@]}"; do
    if container_exists "$name"; then
      echo "三節點容器已存在：${name}；請先執行 ./podman-test-env.sh down-multinode。" >&2
      exit 1
    fi
  done

  podman network exists "$NETWORK" || podman network create "$NETWORK" >/dev/null
  generate_certs
  rm -rf "$MULTINODE_RUNTIME_DIR"
  write_multinode_config es8-mn1 zone-a '[master, data_hot, data_content, ingest, transform, remote_cluster_client]'
  write_multinode_config es8-mn2 zone-b '[master, data_hot, data_content, ingest, transform, remote_cluster_client]'
  write_multinode_config es8-mn3 zone-c '[master, data_warm]'

  start_multinode_node es8-mn1 9218
  start_multinode_node es8-mn2
  start_multinode_node es8-mn3

  for _ in $(seq 1 60); do
    if curl --silent --fail --cacert "$CA_CERT" -u "elastic:${TEST_PASSWORD}" \
      "https://localhost:9218/_cluster/health?wait_for_nodes=3&wait_for_status=yellow&timeout=5s" |
      grep -q '"number_of_nodes":3'; then
      # 官方要求 cluster 首次形成後移除 bootstrap 設定，避免重啟時誤建新叢集。
      for node in es8-mn1 es8-mn2 es8-mn3; do
        sed -i.bak '/^cluster\.initial_master_nodes:/d' "${MULTINODE_RUNTIME_DIR}/${node}.yml"
        rm -f "${MULTINODE_RUNTIME_DIR}/${node}.yml.bak"
      done
      echo "ES 8 multi-node ready: https://localhost:9218"
      echo "節點：es8-mn1(zone-a/hot)、es8-mn2(zone-b/hot)、es8-mn3(zone-c/warm)"
      echo "帳密：elastic / ${TEST_PASSWORD}"
      return
    fi
    sleep 5
  done

  echo "ES 8 multi-node 未在 300 秒內形成三節點叢集；請查看 podman logs elk-diagnostics-es8-mn1" >&2
  exit 1
}

start_kibana() {
  local name="$1" host="$2" port="$3" cert="$4" es_host="$5" image="$6"
  export ELASTICSEARCH_PASSWORD="$TEST_PASSWORD"
  podman run -d --name "$name" --hostname "$host" --network "$NETWORK" \
    -p "127.0.0.1:${port}:5601" \
    -e SERVERNAME="$host" \
    -e SERVER_SSL_ENABLED=true \
    -e "SERVER_SSL_CERTIFICATE=/usr/share/kibana/config/certs/${cert}/${cert}.crt" \
    -e "SERVER_SSL_KEY=/usr/share/kibana/config/certs/${cert}/${cert}.key" \
    -e "ELASTICSEARCH_HOSTS=https://${es_host}:9200" \
    -e ELASTICSEARCH_USERNAME=kibana_system \
    -e ELASTICSEARCH_PASSWORD \
    -e ELASTICSEARCH_SSL_CERTIFICATEAUTHORITIES=/usr/share/kibana/config/certs/ca/ca.crt \
    -e ELASTICSEARCH_SSL_VERIFICATIONMODE=full \
    -v "${CERTS_DIR}:/usr/share/kibana/config/certs:ro" \
    "$image" >/dev/null
  unset ELASTICSEARCH_PASSWORD
}

up_multinode_kibana() {
  for name in elk-diagnostics-es8-mn1 elk-diagnostics-es8-mn2 elk-diagnostics-es8-mn3; do
    container_running "$name" || {
      echo "三節點環境未完整執行；請先執行 ./podman-test-env.sh up-multinode" >&2
      exit 1
    }
  done
  if container_exists elk-diagnostics-kibana8-mn; then
    echo "三節點專用 Kibana 已存在" >&2
    exit 1
  fi

  local body="{\"password\":\"${TEST_PASSWORD}\"}"
  curl --silent --fail --cacert "$CA_CERT" -u "elastic:${TEST_PASSWORD}" \
    -H 'Content-Type: application/json' -X POST \
    'https://localhost:9218/_security/user/kibana_system/_password' \
    -d "$body" >/dev/null

  start_kibana \
    elk-diagnostics-kibana8-mn kibana8-mn 5611 kibana8 es8-mn1 "$KIBANA8_IMAGE"
  wait_https https://localhost:5611/api/status "Kibana 8 multi-node" "elastic:${TEST_PASSWORD}"
  echo "Kibana 8 multi-node 已就緒：https://localhost:5611"
  echo "登入帳密：elastic / ${TEST_PASSWORD}"
}

up() {
  local existing=()
  for name in "${managed_containers[@]}" "${legacy_containers[@]}"; do
    container_exists "$name" && existing+=("$name")
  done
  if ((${#existing[@]} > 0)); then
    echo "已有同名或舊版容器：${existing[*]}" >&2
    echo "確認資料可捨棄後，先執行 ./podman-test-env.sh down。" >&2
    exit 1
  fi

  podman network exists "$NETWORK" || podman network create "$NETWORK" >/dev/null
  generate_certs
  start_es elk-diagnostics-es8 es8 9208 es8 "$ES8_IMAGE"
  start_es elk-diagnostics-es9 es9 9209 es9 "$ES9_IMAGE"
  wait_https https://localhost:9208/_cluster/health "ES 8" "elastic:${TEST_PASSWORD}"
  wait_https https://localhost:9209/_cluster/health "ES 9" "elastic:${TEST_PASSWORD}"
  echo "ES 測試環境已就緒；帳密 elastic / ${TEST_PASSWORD}"
}

up_kibana() {
  container_exists elk-diagnostics-es8 && container_exists elk-diagnostics-es9 || {
    echo "請先執行 ./podman-test-env.sh up" >&2; exit 1;
  }
  if container_exists elk-diagnostics-kibana8 || container_exists elk-diagnostics-kibana9; then
    echo "Kibana 已存在" >&2
    exit 1
  fi

  body="{\"password\":\"${TEST_PASSWORD}\"}"
  for port in 9208 9209; do
    curl --silent --fail --cacert "$CA_CERT" -u "elastic:${TEST_PASSWORD}" \
      -H 'Content-Type: application/json' -X POST \
      "https://localhost:${port}/_security/user/kibana_system/_password" \
      -d "$body" >/dev/null
  done
  start_kibana elk-diagnostics-kibana8 kibana8 5601 kibana8 es8 "$KIBANA8_IMAGE"
  start_kibana elk-diagnostics-kibana9 kibana9 5602 kibana9 es9 "$KIBANA9_IMAGE"
  # /api/status 在 8.x 預設匿名可讀，但部分版本／設定會要求認證；帶上帳密兩者皆通，
  # 避免 401 空轉 300 秒後誤判「未就緒」
  wait_https https://localhost:5601/api/status "Kibana 8" "elastic:${TEST_PASSWORD}"
  wait_https https://localhost:5602/api/status "Kibana 9" "elastic:${TEST_PASSWORD}"
  echo "Kibana 已就緒；登入帳密 elastic / ${TEST_PASSWORD}"
}

down() {
  for name in "${managed_containers[@]}" "${legacy_containers[@]}"; do
    container_exists "$name" && podman rm -f "$name" >/dev/null
  done
  rm -rf "$RUNTIME_DIR"
  for network in "$NETWORK" elkdoctor; do
    podman network exists "$network" && podman network rm "$network" >/dev/null || true
  done
  echo "已移除 elk-diagnostics 測試容器；憑證保留在 ${CERTS_DIR}。"
}

down_multinode() {
  for name in "${multinode_containers[@]}"; do
    container_exists "$name" && podman rm -f "$name" >/dev/null
  done
  rm -rf "$MULTINODE_RUNTIME_DIR"
  echo "已移除 ES 8 三節點測試環境。"
}

status() {
  podman ps -a --filter 'name=elk-diagnostics-' \
    --format 'table {{.Names}}\t{{.Status}}\t{{.Ports}}'
}

case "${1:-}" in
  up) up ;;
  up-kibana) up_kibana ;;
  up-multinode) up_multinode ;;
  up-multinode-kibana) up_multinode_kibana ;;
  multinode-helper-up) multinode_helper_up "${2:-}" ;;
  multinode-helper-down) multinode_helper_down ;;
  down-multinode) down_multinode ;;
  down) down ;;
  status) status ;;
  *) usage; exit 2 ;;
esac
