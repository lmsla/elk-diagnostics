#!/usr/bin/env bash
# 在 elasticsearch 映像「容器內」執行：產生自簽 CA 與各服務憑證到 config/certs。
#
# 這是唯一一份憑證產生邏輯——`docker-compose.yml` 的 certs-setup 與
# `podman-test-env.sh` 的 generate_certs 都掛載並執行本檔，不得各自手抄。
# （兩份手抄本必然漂移：SAN 清單改了一邊、另一邊靜默用舊憑證。
#   同款教訓見 docs/內部/歷史/驗證紀錄.md §1 的 golden test 端點副本。）
#
# 冪等：CA 與憑證齊全時不重做。要強制重生，刪掉宿主機的 dev/phase0/certs/ 即可。
set -euo pipefail

CERTS=/usr/share/elasticsearch/config/certs
cd "$CERTS"

if [ ! -f ca/ca.crt ]; then
  /usr/share/elasticsearch/bin/elasticsearch-certutil ca --silent --pem -out "$CERTS/ca.zip"
  unzip -q "$CERTS/ca.zip" -d "$CERTS"
  rm -f "$CERTS/ca.zip"
fi

if [ ! -f es8/es8.crt ] || [ ! -f es9/es9.crt ] || [ ! -f kibana8/kibana8.crt ] || [ ! -f kibana9/kibana9.crt ]; then
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
    "    ip: [127.0.0.1]" > "$CERTS/instances.yml"
  /usr/share/elasticsearch/bin/elasticsearch-certutil cert --silent --pem \
    --in "$CERTS/instances.yml" \
    --out "$CERTS/certs.zip" \
    --ca-cert "$CERTS/ca/ca.crt" \
    --ca-key "$CERTS/ca/ca.key"
  unzip -q "$CERTS/certs.zip" -d "$CERTS"
  rm -f "$CERTS/certs.zip" "$CERTS/instances.yml"
fi

if [ ! -f es8-mn1/es8-mn1.crt ] || [ ! -f es8-mn2/es8-mn2.crt ] ||
   [ ! -f es8-mn3/es8-mn3.crt ] || [ ! -f es8-mn4/es8-mn4.crt ]; then
  rm -rf es8-mn1 es8-mn2 es8-mn3 es8-mn4
  printf "%s\n" \
    "instances:" \
    "  - name: es8-mn1" \
    "    dns: [es8-mn1, elk-diagnostics-es8-mn1, localhost]" \
    "    ip: [127.0.0.1]" \
    "  - name: es8-mn2" \
    "    dns: [es8-mn2, elk-diagnostics-es8-mn2, localhost]" \
    "    ip: [127.0.0.1]" \
    "  - name: es8-mn3" \
    "    dns: [es8-mn3, elk-diagnostics-es8-mn3, localhost]" \
    "    ip: [127.0.0.1]" \
    "  - name: es8-mn4" \
    "    dns: [es8-mn4, elk-diagnostics-es8-mn4, localhost]" \
    "    ip: [127.0.0.1]" > "$CERTS/instances-multinode.yml"
  /usr/share/elasticsearch/bin/elasticsearch-certutil cert --silent --pem \
    --in "$CERTS/instances-multinode.yml" \
    --out "$CERTS/certs-multinode.zip" \
    --ca-cert "$CERTS/ca/ca.crt" \
    --ca-key "$CERTS/ca/ca.key"
  unzip -q "$CERTS/certs-multinode.zip" -d "$CERTS"
  rm -f "$CERTS/certs-multinode.zip" "$CERTS/instances-multinode.yml"
fi

# elastic 官方映像的程序以 uid 1000、gid 0 執行：key 給 640 + root 群組即可讀，
# 其他使用者不可讀。
chown -R root:root .
find . -type d -exec chmod 755 {} \;
find . -type f -name "*.crt" -exec chmod 644 {} \;
find . -type f -name "*.key" -exec chmod 640 {} \;
