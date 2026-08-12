# Podman 測試環境

只供本機可丟棄測試。固定基線：自簽 CA、HTTPS、Basic Auth、只綁 localhost。測試帳密
`elastic / elk-diagnostics-test-only` 只適用本機，不得帶到正式環境。

## 服務

| 環境 | ES | Kibana（選配） |
|---|---|---|
| 單節點 | `https://localhost:9208`（ES8）<br>`https://localhost:9209`（ES9） | `5601`／`5602` |
| ES8 多節點 | `https://localhost:9218` | `https://localhost:5611` |

所有指令從 repo 根目錄執行。

## 1. 啟動 Podman

```bash
podman machine ls
podman machine start podman-machine-default
podman machine ssh "sudo sysctl -w vm.max_map_count=262144"
```

若 machine 不存在，依 Podman 版本執行 `podman machine init --memory 6144 --cpus 4`，再 start。

## 2. 啟動單節點 ES

```bash
./dev/phase0/podman-test-env.sh up
./dev/phase0/podman-test-env.sh status
```

腳本會建立 `dev/phase0/certs/`；憑證與私鑰不進版控。

基本安全驗收：

```bash
CA="$PWD/dev/phase0/certs/ca/ca.crt"
curl --fail --cacert "$CA" -u 'elastic:elk-diagnostics-test-only' https://localhost:9208
test "$(curl -s -o /dev/null -w '%{http_code}' --cacert "$CA" https://localhost:9208)" = 401
```

標準驗證不可使用 `-k`／`--insecure`。

## 3. 啟動 Kibana（選配）

ES 健檢不依賴 Kibana；需要 Dev Tools 或畫面觀察時才啟動：

```bash
./dev/phase0/podman-test-env.sh up-kibana
```

瀏覽器需信任 `dev/phase0/certs/ca/ca.crt`。不要在基準線測試中無聲啟用 Stack Monitoring；P13 會把 monitoring 設定當成測試條件。

## 4. 啟動 ES8 多節點

先停止單節點容器，再啟動三節點：

```bash
podman stop elk-diagnostics-kibana8 elk-diagnostics-es8 \
  elk-diagnostics-kibana9 elk-diagnostics-es9 2>/dev/null || true
./dev/phase0/podman-test-env.sh up-multinode
./dev/phase0/podman-test-env.sh status
```

需要 Kibana 時另執行：

```bash
./dev/phase0/podman-test-env.sh up-multinode-kibana
```

驗收三節點：

```bash
CA="$PWD/dev/phase0/certs/ca/ca.crt"
curl --fail --cacert "$CA" -u 'elastic:elk-diagnostics-test-only' \
  'https://localhost:9218/_cluster/health?pretty'
```

預期 `number_of_nodes=3`、`number_of_data_nodes=3`。M 系列操作見 [`多節點驗證手冊.md`](./多節點驗證手冊.md)。

## 5. 停止與清理

```bash
./dev/phase0/podman-test-env.sh down-multinode   # 多節點
./dev/phase0/podman-test-env.sh down             # 單節點與 Kibana
```

若 ES 未就緒，先看 `podman logs <container>`；若憑證錯誤，確認使用正確 CA，不要改用 `-k`。
