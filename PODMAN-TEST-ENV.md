# Podman ES 測試環境指引（macOS）

目的：啟動 `elk-diagnostics` 的 ES 8.14.3、ES 9.0.0 本機健檢環境。

固定基線：

- 自簽 CA
- HTTPS
- Basic Auth
- 僅綁 `127.0.0.1`
- 固定測試帳密：`elastic / elk-diagnostics-test-only`
- 預設只啟動 ES；Kibana 是人工交叉核對用的選配服務

固定帳密是公開測試資料，不是秘密，且不得用於正式環境。

**本文所有指令一律從 repo 根目錄執行**（`podman-test-env.sh` 以自身位置定位，不依賴工作目錄）。

| 服務 | 容器名 | URL |
|---|---|---|
| ES 8 | `elk-diagnostics-es8` | `https://localhost:9208` |
| ES 9 | `elk-diagnostics-es9` | `https://localhost:9209` |
| Kibana 8（選配） | `elk-diagnostics-kibana8` | `https://localhost:5601` |
| Kibana 9（選配） | `elk-diagnostics-kibana9` | `https://localhost:5602` |

## 1. Podman machine

```bash
podman machine ls
# 若不存在：
podman machine init --memory 6144 --cpus 4
podman machine start

# VM 重開後需重新確認：
podman machine ssh "sudo sysctl -w vm.max_map_count=262144"
```

## 2. 啟動 ES

```bash
./dev/phase0/podman-test-env.sh up
```

腳本會產生 `dev/phase0/certs/` 並啟動 ES 8／9。憑證與私鑰已被 Git 忽略。

## 3. 安全驗收

```bash
CA="$PWD/dev/phase0/certs/ca/ca.crt"
PASSWORD=elk-diagnostics-test-only

# 正確 CA + Basic Auth：200
curl --fail --cacert "$CA" -u "elastic:$PASSWORD" https://localhost:9208
curl --fail --cacert "$CA" -u "elastic:$PASSWORD" https://localhost:9209

# 無帳密：401
test "$(curl -s -o /dev/null -w '%{http_code}' --cacert "$CA" https://localhost:9208)" = 401

# 錯誤帳密：401
test "$(curl -s -o /dev/null -w '%{http_code}' --cacert "$CA" -u elastic:wrong https://localhost:9208)" = 401

# 不提供 CA：必須失敗
if curl --fail https://localhost:9208 >/dev/null 2>&1; then
  echo '錯誤：未提供 CA 竟然連線成功' >&2
  exit 1
fi
```

標準驗證不可使用 `--insecure`／`-k`，否則沒有測到 CA trust path。

## 4. 驗證 elk-diagnostics

```bash
export ELK_DIAGNOSTICS_HOSTS=https://localhost:9208
export ELK_DIAGNOSTICS_AUTH_TYPE=basic
export ELK_DIAGNOSTICS_USERNAME=elastic
export ELK_DIAGNOSTICS_PASSWORD=elk-diagnostics-test-only
export ELK_DIAGNOSTICS_CA_CERT="$PWD/dev/phase0/certs/ca/ca.crt"

make build
./elk-diagnostics check --output text
```

全新單節點基準線預期：`0 critical、0 unknown`，唯一 warning 是 master 單點結構；
`node_api_coverage`、`node_swap_usage`、`node_file_descriptor_pressure`、
`node_cgroup_memory_pressure` 應全部 pass。pass 總數不鎖死，避免新增診斷時產生假失敗。

採集與離線分析：

```bash
ES_PASSWORD=elk-diagnostics-test-only ./collect.sh \
  -h https://localhost:9208 -u elastic \
  --ca-cert "$PWD/dev/phase0/certs/ca/ca.crt" \
  -o bundle-baseline
./elk-diagnostics check --from-bundle bundle-baseline --output text
```

## 5. Kibana（選配）

ES 健檢不依賴 Kibana。只有需要 Dev Tools 或畫面交叉核對時才啟動：

```bash
./dev/phase0/podman-test-env.sh up-kibana
```

登入資訊：

- URL：`https://localhost:5601` 或 `https://localhost:5602`
- 帳號：`elastic`
- 密碼：`elk-diagnostics-test-only`

瀏覽器需信任 `dev/phase0/certs/ca/ca.crt`。

## 6. 造壓與復原

先從 [`docs/VERIFICATION-PLAYBOOK.md`](./docs/VERIFICATION-PLAYBOOK.md) 選擇 Live 或
Bundle 路線；一次只執行一份 Playbook，且一次只跑一案。造壓前後都必須通過基準線閘門。
工具輸出與叢集實況不符時，記回
[`docs/VERIFICATION.md`](./docs/VERIFICATION.md)，不得只修改測試預期。

## 7. 清理

```bash
./dev/phase0/podman-test-env.sh down
```

容器內資料會刪除；CA 與憑證保留供下次重用。

## 8. 疑難排解

| 症狀 | 處置 |
|---|---|
| `vm.max_map_count` 太低 | 重跑 §1 的 `sysctl` |
| ES 未就緒 | `podman logs elk-diagnostics-es8` 或對應容器 |
| `certificate signed by unknown authority` | 使用 `certs/ca/ca.crt`，不可改用 `-k` |
| `401 Unauthorized` | 確認帳密為 `elastic / elk-diagnostics-test-only` |
| 舊版 `elkdoctor-*` 容器存在 | 確認資料可刪除後執行 `./podman-test-env.sh down` |

## 9. Agent 守則

- 只操作 `elk-diagnostics-*`；`elkdoctor-*` 僅限清理歷史測試容器。
- 未取得使用者同意，不得刪除既有測試容器或資料。
- 不得提交 `certs/` 或私鑰。
- TLS 驗證不得使用 `--insecure`／`-k`。
