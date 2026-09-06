#!/usr/bin/env bash
# 本機 9.5.2 整合測試線：3 個 Elasticsearch、2 個 Kibana、2 個 Logstash。
#
# 與 podman-test-env.sh 的 8.14.3 基線隔離：不同容器名稱、網路與 host port。
# 只供本機驗證，不使用持久化資料，也不應拿來部署正式環境。

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
RUNTIME_DIR="${ROOT_DIR}/runtime/9stack"
CERTS_DIR="${RUNTIME_DIR}/certs"
CONFIG_DIR="${RUNTIME_DIR}/config"
EVIDENCE_DIR="${RUNTIME_DIR}/evidence"
NETWORK="${ELK_DIAGNOSTICS_9_NETWORK:-elk-diagnostics-9}"
VERSION="${ELK_DIAGNOSTICS_9_VERSION:-9.5.2}"
PASSWORD="${ELK_DIAGNOSTICS_TEST_PASSWORD:-elk-diagnostics-test-only}"
ES_IMAGE="docker.elastic.co/elasticsearch/elasticsearch:${VERSION}"
KIBANA_IMAGE="docker.elastic.co/kibana/kibana:${VERSION}"
LOGSTASH_IMAGE="docker.elastic.co/logstash/logstash:${VERSION}"
ES_URL="https://127.0.0.1:9220"
KIBANA_URLS=("https://127.0.0.1:5620" "https://127.0.0.1:5621")
LOGSTASH_URLS=("http://127.0.0.1:9620" "http://127.0.0.1:9621")
ES_NODES=(es9-mn1 es9-mn2 es9-mn3)
CONTAINERS=(
  elk-diagnostics-9-es1 elk-diagnostics-9-es2 elk-diagnostics-9-es3
  elk-diagnostics-9-kibana1 elk-diagnostics-9-kibana2
  elk-diagnostics-9-logstash1 elk-diagnostics-9-logstash2
)

usage() {
  cat <<'EOF'
用法：
  ./podman-9stack.sh up       建立並啟動 3 ES + 2 Kibana + 2 Logstash
  ./podman-9stack.sh status   查看容器狀態
  ./podman-9stack.sh urls     顯示本機驗證連線資訊
  ./podman-9stack.sh down     停止並移除容器（保留 runtime 測試證據）
  ./podman-9stack.sh clean    down 後刪除本腳本產生的 runtime

預設版本：9.5.2（可用 ELK_DIAGNOSTICS_9_VERSION 覆寫，但應固定完整 patch）
帳密：elastic / ${ELK_DIAGNOSTICS_TEST_PASSWORD:-elk-diagnostics-test-only}
EOF
}

require_podman() {
  command -v podman >/dev/null 2>&1 || {
    echo "找不到 podman" >&2
    exit 2
  }
}

container_exists() {
  podman container exists "$1"
}

write_instances() {
  mkdir -p "$CONFIG_DIR"
  cat > "$CONFIG_DIR/instances.yml" <<EOF
instances:
  - name: es9-mn1
    dns: [es9-mn1, elk-diagnostics-9-es1, localhost]
    ip: [127.0.0.1]
  - name: es9-mn2
    dns: [es9-mn2, elk-diagnostics-9-es2, localhost]
    ip: [127.0.0.1]
  - name: es9-mn3
    dns: [es9-mn3, elk-diagnostics-9-es3, localhost]
    ip: [127.0.0.1]
  - name: kibana9-1
    dns: [kibana9-1, elk-diagnostics-9-kibana1, localhost]
    ip: [127.0.0.1]
  - name: kibana9-2
    dns: [kibana9-2, elk-diagnostics-9-kibana2, localhost]
    ip: [127.0.0.1]
EOF
}

generate_certs() {
  local cert
  write_instances
  for cert in es9-mn1 es9-mn2 es9-mn3 kibana9-1 kibana9-2; do
    if [[ ! -f "$CERTS_DIR/$cert/$cert.crt" ]]; then
      echo "產生 9.5.2 測試 CA／TLS 憑證..."
      mkdir -p "$CERTS_DIR"
      podman run --rm --user 0 \
        -v "$CERTS_DIR:/usr/share/elasticsearch/config/certs" \
        -v "$CONFIG_DIR/instances.yml:/instances.yml:ro" \
        "$ES_IMAGE" bash -ec '
          certs=/usr/share/elasticsearch/config/certs
          cd "$certs"
          if [[ ! -f ca/ca.crt ]]; then
            /usr/share/elasticsearch/bin/elasticsearch-certutil ca --silent --pem -out "$certs/ca.zip"
            unzip -q "$certs/ca.zip" -d "$certs"
            rm -f "$certs/ca.zip"
          fi
          rm -rf es9-mn1 es9-mn2 es9-mn3 kibana9-1 kibana9-2
          /usr/share/elasticsearch/bin/elasticsearch-certutil cert --silent --pem \
            --in /instances.yml --out "$certs/certs.zip" \
            --ca-cert "$certs/ca/ca.crt" --ca-key "$certs/ca/ca.key"
          unzip -q "$certs/certs.zip" -d "$certs"
          rm -f "$certs/certs.zip"
          chown -R root:root "$certs"
          find "$certs" -type d -exec chmod 755 {} \;
          find "$certs" -type f -name "*.crt" -exec chmod 644 {} \;
          find "$certs" -type f -name "*.key" -exec chmod 640 {} \;
        '
      break
    fi
  done
}

write_es_configs() {
  local node zone
  mkdir -p "$CONFIG_DIR/es"
  for node in "${ES_NODES[@]}"; do
    case "$node" in
      es9-mn1) zone=zone-a ;;
      es9-mn2) zone=zone-b ;;
      es9-mn3) zone=zone-c ;;
    esac
    cat > "$CONFIG_DIR/es/$node.yml" <<EOF
cluster.name: elk-diagnostics-es9-multinode
node.name: $node
node.roles: [master, data_hot, data_content, ingest, transform, remote_cluster_client]
node.attr.zone: $zone
network.host: 0.0.0.0
discovery.seed_hosts: [es9-mn1, es9-mn2, es9-mn3]
cluster.initial_master_nodes: [es9-mn1, es9-mn2, es9-mn3]
xpack.security.enabled: true
xpack.security.http.ssl.enabled: true
xpack.security.http.ssl.key: certs/$node/$node.key
xpack.security.http.ssl.certificate: certs/$node/$node.crt
xpack.security.http.ssl.certificate_authorities: certs/ca/ca.crt
xpack.security.transport.ssl.enabled: true
xpack.security.transport.ssl.verification_mode: certificate
xpack.security.transport.ssl.key: certs/$node/$node.key
xpack.security.transport.ssl.certificate: certs/$node/$node.crt
xpack.security.transport.ssl.certificate_authorities: certs/ca/ca.crt
EOF
  done
}

write_logstash_configs() {
  local n dir
  for n in 1 2; do
    dir="$CONFIG_DIR/logstash$n"
    mkdir -p "$dir/pipeline"
    cat > "$dir/logstash.yml" <<'EOF'
api.http.host: 0.0.0.0
api.http.port: 9600
xpack.monitoring.enabled: false
EOF
    cat > "$dir/pipeline/health.conf" <<'EOF'
input {
  tcp { port => 5000 }
}
output {
  null { }
}
EOF
  done
}

write_instance_lists() {
  cat > "$RUNTIME_DIR/expected-es-nodes.txt" <<'EOF'
es9-mn1
es9-mn2
es9-mn3
EOF
  cat > "$RUNTIME_DIR/kibana-instances.conf" <<'EOF'
kibana9-1|https://127.0.0.1:5620
kibana9-2|https://127.0.0.1:5621
EOF
  cat > "$RUNTIME_DIR/logstash-instances.conf" <<'EOF'
logstash9-1|http://127.0.0.1:9620
logstash9-2|http://127.0.0.1:9621
EOF
}

start_es() {
  local node="$1" name="elk-diagnostics-9-es${2}"; shift 2
  local args=(run -d --name "$name" --hostname "$node" --network "$NETWORK" --network-alias "$node"
    --memory 1g -e "ELASTIC_PASSWORD=$PASSWORD" -e "ES_JAVA_OPTS=-Xms256m -Xmx256m"
    -v "$CERTS_DIR:/usr/share/elasticsearch/config/certs:ro"
    -v "$CONFIG_DIR/es/$node.yml:/usr/share/elasticsearch/config/elasticsearch.yml:ro")
  if [[ "$node" == es9-mn1 ]]; then
    args+=(-p 127.0.0.1:9220:9200)
  fi
  podman "${args[@]}" "$ES_IMAGE" >/dev/null
}

wait_for_es() {
  local body
  for _ in $(seq 1 90); do
    if body="$(curl --silent --show-error --max-time 5 --cacert "$CERTS_DIR/ca/ca.crt" \
      -u "elastic:$PASSWORD" \
      "$ES_URL/_cluster/health?wait_for_nodes=3&wait_for_status=yellow&timeout=3s" 2>/dev/null)" &&
      printf '%s' "$body" | grep -Eq '"number_of_nodes"[[:space:]]*:[[:space:]]*3'; then
      echo "ES 9.5.2 三節點已就緒：$ES_URL"
      return
    fi
    sleep 3
  done
  echo "ES 三節點未在 270 秒內形成；請查看 podman logs elk-diagnostics-9-es1" >&2
  exit 1
}

start_kibana() {
  local n="$1" name="elk-diagnostics-9-kibana${1}" host="kibana9-${1}" port="$2"
  podman run -d --name "$name" --hostname "$host" --network "$NETWORK" --network-alias "$host" \
    --memory 1792m \
    -p "127.0.0.1:${port}:5601" \
    -e SERVERNAME="$host" \
    -e NODE_OPTIONS=--max-old-space-size=1024 \
    -e SERVER_SSL_ENABLED=true \
    -e "SERVER_SSL_CERTIFICATE=/usr/share/kibana/config/certs/${host}/${host}.crt" \
    -e "SERVER_SSL_KEY=/usr/share/kibana/config/certs/${host}/${host}.key" \
    -e ELASTICSEARCH_HOSTS=https://es9-mn1:9200 \
    -e ELASTICSEARCH_USERNAME=kibana_system \
    -e "ELASTICSEARCH_PASSWORD=$PASSWORD" \
    -e ELASTICSEARCH_SSL_CERTIFICATEAUTHORITIES=/usr/share/kibana/config/certs/ca/ca.crt \
    -e ELASTICSEARCH_SSL_VERIFICATIONMODE=full \
    -v "$CERTS_DIR:/usr/share/kibana/config/certs:ro" \
    "$KIBANA_IMAGE" >/dev/null
}

wait_for_kibana() {
  local n url
  for n in 0 1; do
    url="${KIBANA_URLS[$n]}"
    for _ in $(seq 1 90); do
      if curl --silent --show-error --fail --max-time 5 --cacert "$CERTS_DIR/ca/ca.crt" \
        -u "elastic:$PASSWORD" "$url/api/status" >/dev/null 2>&1; then
        echo "Kibana 9.5.2 instance $((n + 1)) 已就緒：$url"
        break
      fi
      if [[ "$_" == 90 ]]; then
        echo "Kibana instance $((n + 1)) 未在 270 秒內就緒；請查看 podman logs elk-diagnostics-9-kibana$((n + 1))" >&2
        exit 1
      fi
      sleep 3
    done
  done
}

start_logstash() {
  local n="$1" port="$2"
  podman run -d --name "elk-diagnostics-9-logstash${n}" --hostname "logstash9-${n}" \
    --network "$NETWORK" --network-alias "logstash9-${n}" -p "127.0.0.1:${port}:9600" \
    --memory 768m -e "LS_JAVA_OPTS=-Xms128m -Xmx128m" \
    -v "$CONFIG_DIR/logstash${n}/logstash.yml:/usr/share/logstash/config/logstash.yml:ro" \
    -v "$CONFIG_DIR/logstash${n}/pipeline:/usr/share/logstash/pipeline:ro" \
    "$LOGSTASH_IMAGE" >/dev/null
}

wait_for_logstash() {
  local n url root_file health_file health_summary
  mkdir -p "$EVIDENCE_DIR"
  for n in 0 1; do
    url="${LOGSTASH_URLS[$n]}"
    root_file="$EVIDENCE_DIR/logstash-$((n + 1))-root.json"
    health_file="$EVIDENCE_DIR/logstash-$((n + 1))-health_report.json"
    for _ in $(seq 1 90); do
      if curl --silent --show-error --fail --max-time 5 "$url/" -o "$root_file" 2>/dev/null; then
        break
      fi
      if [[ "$_" == 90 ]]; then
        echo "Logstash instance $((n + 1)) 未在 270 秒內就緒；請查看 podman logs elk-diagnostics-9-logstash$((n + 1))" >&2
        exit 1
      fi
      sleep 3
    done
    curl --silent --show-error --fail --max-time 10 "$url/_health_report" -o "$health_file"
    health_summary=unknown
    if health_summary="$(grep -o '"status"[[:space:]]*:[[:space:]]*"[^"]*"' "$health_file" | head -1)"; then
      :
    fi
    echo "Logstash 9.5.2 instance $((n + 1)) 已就緒：$url/_health_report（${health_summary}）"
  done
}

up() {
  local c
  for c in "${CONTAINERS[@]}"; do
    if container_exists "$c"; then
      echo "容器已存在：$c；請先執行 ./podman-9stack.sh down" >&2
      exit 1
    fi
  done
  podman network exists "$NETWORK" || podman network create "$NETWORK" >/dev/null
  mkdir -p "$RUNTIME_DIR"
  generate_certs
  write_es_configs
  write_logstash_configs
  write_instance_lists
  start_es es9-mn1 1
  start_es es9-mn2 2
  start_es es9-mn3 3
  wait_for_es
  for c in "${ES_NODES[@]}"; do
    sed -i.bak '/^cluster\.initial_master_nodes:/d' "$CONFIG_DIR/es/$c.yml"
    rm -f "$CONFIG_DIR/es/$c.yml.bak"
  done
  curl --silent --show-error --fail --max-time 10 --cacert "$CERTS_DIR/ca/ca.crt" \
    -u "elastic:$PASSWORD" -H 'Content-Type: application/json' -X POST \
    "$ES_URL/_security/user/kibana_system/_password" \
    -d "{\"password\":\"$PASSWORD\"}" >/dev/null
  start_kibana 1 5620
  start_kibana 2 5621
  wait_for_kibana
  start_logstash 1 9620
  start_logstash 2 9621
  wait_for_logstash
  echo
  echo "9.5.2 測試線完成；憑證、設定與 Logstash health evidence：$RUNTIME_DIR"
  urls
}

down() {
  local c
  for c in "${CONTAINERS[@]}"; do
    container_exists "$c" && podman rm -f "$c" >/dev/null || true
  done
  podman network exists "$NETWORK" && podman network rm "$NETWORK" >/dev/null || true
  echo "已移除 9.5.2 測試容器；runtime 保留在 $RUNTIME_DIR"
}

clean() {
  down
  rm -rf "$RUNTIME_DIR"
  echo "已刪除 9.5.2 測試 runtime：$RUNTIME_DIR"
}

status() {
  podman ps -a --format 'table {{.Names}}\t{{.Image}}\t{{.Status}}\t{{.Ports}}' \
    | awk 'NR == 1 || $1 ~ /^elk-diagnostics-9-(es|kibana|logstash)/'
}

urls() {
  cat <<EOF
ES（cluster entrypoint）：$ES_URL
Kibana 1：${KIBANA_URLS[0]}
Kibana 2：${KIBANA_URLS[1]}
Logstash 1：${LOGSTASH_URLS[0]}
Logstash 2：${LOGSTASH_URLS[1]}
帳號：elastic
密碼：由 ELK_DIAGNOSTICS_TEST_PASSWORD（預設測試密碼）設定
CA：$CERTS_DIR/ca/ca.crt
EOF
}

require_podman
case "${1:-}" in
  up) up ;;
  status) status ;;
  urls) urls ;;
  down) down ;;
  clean) clean ;;
  *) usage; exit 2 ;;
esac
