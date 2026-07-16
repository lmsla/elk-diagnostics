#!/usr/bin/env bash
# macOS + Podman 的本機 ES 測試環境。
# 固定基線：自簽 CA、HTTPS、Basic Auth、只綁 localhost；Kibana 選配。

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CERTS_DIR="${ROOT_DIR}/certs"
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
  if [[ -f "$CA_CERT" && -f "${CERTS_DIR}/es8/es8.crt" ]] &&
     openssl x509 -in "${CERTS_DIR}/es8/es8.crt" -text -noout 2>/dev/null |
       grep -q 'DNS:elk-diagnostics-es8'; then
    return
  fi

  echo "產生本機自簽 CA 與 server certificates..."
  podman run --rm --user 0 \
    -v "${CERTS_DIR}:/usr/share/elasticsearch/config/certs" \
    "$ES8_IMAGE" bash -c '
      set -euo pipefail
      cd /usr/share/elasticsearch/config/certs
      if [ ! -f ca/ca.crt ]; then
        /usr/share/elasticsearch/bin/elasticsearch-certutil ca --silent --pem \
          -out /usr/share/elasticsearch/config/certs/ca.zip
        unzip -q /usr/share/elasticsearch/config/certs/ca.zip \
          -d /usr/share/elasticsearch/config/certs
        rm -f /usr/share/elasticsearch/config/certs/ca.zip
      fi
      rm -rf es8 es9 kibana8 kibana9
      printf "%s\n" \
        "instances:" \
        "  - name: es8" \
        "    dns: [es8, elk-diagnostics-es8, localhost]" \
        "    ip: [127.0.0.1]" \
        "  - name: es9" \
        "    dns: [es9, elk-diagnostics-es9, localhost]" \
        "    ip: [127.0.0.1]" \
        "  - name: kibana8" \
        "    dns: [kibana8, elk-diagnostics-kibana8, localhost]" \
        "    ip: [127.0.0.1]" \
        "  - name: kibana9" \
        "    dns: [kibana9, elk-diagnostics-kibana9, localhost]" \
        "    ip: [127.0.0.1]" > instances.yml
      /usr/share/elasticsearch/bin/elasticsearch-certutil cert --silent --pem \
        --in /usr/share/elasticsearch/config/certs/instances.yml \
        --out /usr/share/elasticsearch/config/certs/certs.zip \
        --ca-cert /usr/share/elasticsearch/config/certs/ca/ca.crt \
        --ca-key /usr/share/elasticsearch/config/certs/ca/ca.key
      unzip -q /usr/share/elasticsearch/config/certs/certs.zip \
        -d /usr/share/elasticsearch/config/certs
      rm -f /usr/share/elasticsearch/config/certs/certs.zip instances.yml
      chown -R root:root .
      find . -type d -exec chmod 755 {} \;
      find . -type f -name "*.crt" -exec chmod 644 {} \;
      find . -type f -name "*.key" -exec chmod 640 {} \;
    '
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

  body="$(printf '{\"password\":\"%s\"}' "$TEST_PASSWORD")"
  for port in 9208 9209; do
    curl --silent --fail --cacert "$CA_CERT" -u "elastic:${TEST_PASSWORD}" \
      -H 'Content-Type: application/json' -X POST \
      "https://localhost:${port}/_security/user/kibana_system/_password" \
      -d "$body" >/dev/null
  done
  start_kibana elk-diagnostics-kibana8 kibana8 5601 kibana8 es8 "$KIBANA8_IMAGE"
  start_kibana elk-diagnostics-kibana9 kibana9 5602 kibana9 es9 "$KIBANA9_IMAGE"
  wait_https https://localhost:5601/api/status "Kibana 8"
  wait_https https://localhost:5602/api/status "Kibana 9"
  echo "Kibana 已就緒；登入帳密 elastic / ${TEST_PASSWORD}"
}

down() {
  for name in "${managed_containers[@]}" "${legacy_containers[@]}"; do
    container_exists "$name" && podman rm -f "$name" >/dev/null
  done
  for network in "$NETWORK" elkdoctor; do
    podman network exists "$network" && podman network rm "$network" >/dev/null || true
  done
  echo "已移除 elk-diagnostics 測試容器；憑證保留在 ${CERTS_DIR}。"
}

status() {
  podman ps -a --filter 'name=elk-diagnostics-' \
    --format 'table {{.Names}}\t{{.Status}}\t{{.Ports}}'
}

case "${1:-}" in
  up) up ;;
  up-kibana) up_kibana ;;
  down) down ;;
  status) status ;;
  *) usage; exit 2 ;;
esac
