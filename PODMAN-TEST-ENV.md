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
| ES 8 三節點（選配） | `elk-diagnostics-es8-mn1`～`mn3` | `https://localhost:9218` |
| 三節點專用 Kibana 8（選配） | `elk-diagnostics-kibana8-mn` | `https://localhost:5611` |

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
export ELK_DIAGNOSTICS_CA_CERT="$PWD/dev/phase0/certs/ca/ca.crt"

make build
./elk-diagnostics check --output text
```

執行 `check` 時依提示輸入測試密碼；輸入不回顯，不要把密碼寫入環境變數或命令列。

全新單節點基準線預期：`0 critical、0 unknown`，唯一 warning 是 master 單點結構；
`node_api_coverage`、`node_swap_usage`、`node_file_descriptor_pressure`、
`node_cgroup_memory_pressure` 應全部 pass。pass 總數不鎖死，避免新增診斷時產生假失敗。

採集與離線分析：

```bash
./collect.sh \
  -h https://localhost:9208 -u elastic \
  --ca-cert "$PWD/dev/phase0/certs/ca/ca.crt" \
  -o bundle-baseline
./elk-diagnostics check --from-bundle bundle-baseline --output text
```

`collect.sh` 會互動詢問密碼且不回顯。

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

## 6. ES 8 三節點驗證環境

此環境與單節點基準線分離，專門驗證 Master 選舉、node runtime 一致性、allocation
awareness、data tier 與 partial Nodes API。三個節點都具備 `master` role：

| 節點 | zone | data roles |
|---|---|---|
| `es8-mn1` | `zone-a` | `data_hot`、`data_content` |
| `es8-mn2` | `zone-b` | `data_hot`、`data_content` |
| `es8-mn3` | `zone-c` | `data_warm` |

先停止單節點服務以釋放 Podman VM 記憶體；不需刪除：

```bash
podman stop \
  elk-diagnostics-kibana8 \
  elk-diagnostics-es8 \
  elk-diagnostics-kibana9 \
  elk-diagnostics-es9
```

啟動三節點環境：

```bash
./dev/phase0/podman-test-env.sh up-multinode
```

腳本會在首次形成叢集後移除 `cluster.initial_master_nodes`，避免重啟時誤建新叢集。
驗收：

```bash
CA="$PWD/dev/phase0/certs/ca/ca.crt"
PASSWORD=elk-diagnostics-test-only

curl --fail --cacert "$CA" -u "elastic:$PASSWORD" \
  'https://localhost:9218/_cluster/health?pretty'

curl --fail --cacert "$CA" -u "elastic:$PASSWORD" \
  'https://localhost:9218/_nodes?filter_path=nodes.*.name,nodes.*.roles,nodes.*.attributes' |
  jq -S '.nodes | to_entries |
    map({name:.value.name, roles:.value.roles, zone:.value.attributes.zone}) |
    sort_by(.name)'
```

預期 `number_of_nodes=3`、`number_of_data_nodes=3`，三個節點分別顯示
`zone-a`、`zone-b`、`zone-c`。

需要 Dev Tools、Index Management 或畫面交叉核對時，再啟動專用 Kibana：

```bash
./dev/phase0/podman-test-env.sh up-multinode-kibana
```

- URL：`https://localhost:5611`
- 帳號：`elastic`
- 密碼：`elk-diagnostics-test-only`

此指令不會自動啟用 Stack Monitoring collection。需要時間序列監控時應另行明確開啟，
並記錄為測試條件；部分故障案例會把 monitoring 設定納入基準線，不應無聲變更。

M03～M08 由故障控制器按需建立 `es8-mn4` helper node。helper 的 runtime、角色與
暫存磁碟依案例切換，不需要第二套腳本。M08 為了穩定重現 Nodes API partial response，
測試設定把 follower-check retry count 延長為 12；這只服務測試時序，不是正式環境建議。
採集腳本對 Nodes／Tasks API 同時使用 ES `timeout=5s` 與 curl 10 秒上限，避免故障節點
讓每個 fan-out 請求都等待完整 30 秒；CAT nodes／thread pool 僅套用 curl 10 秒上限。
案例內容與復原閘門見
[`docs/VERIFICATION-MULTINODE-PLAYBOOK.md`](./docs/VERIFICATION-MULTINODE-PLAYBOOK.md)。

完成後只移除三節點環境：

```bash
./dev/phase0/podman-test-env.sh down-multinode
```

這是同一台 Podman VM 內的真實三節點 Elasticsearch 叢集，可驗證 ES 分配邏輯與工具判定，
但不能證明跨主機、跨機房或真實 availability zone 的故障隔離能力。

## 7. 造壓與復原

先從 [`docs/VERIFICATION-PLAYBOOK.md`](./docs/VERIFICATION-PLAYBOOK.md) 選擇 Live 或
Bundle 路線；一次只執行一份 Playbook，且一次只跑一案。造壓前後都必須通過基準線閘門。
工具輸出與叢集實況不符時，記回
[`docs/VERIFICATION.md`](./docs/VERIFICATION.md)，不得只修改測試預期。

## 8. 清理

```bash
./dev/phase0/podman-test-env.sh down
```

容器內資料會刪除；CA 與憑證保留供下次重用。

## 9. 疑難排解

| 症狀 | 處置 |
|---|---|
| `vm.max_map_count` 太低 | 重跑 §1 的 `sysctl` |
| ES 未就緒 | `podman logs elk-diagnostics-es8` 或對應容器 |
| `certificate signed by unknown authority` | 使用 `certs/ca/ca.crt`，不可改用 `-k` |
| `401 Unauthorized` | 確認帳密為 `elastic / elk-diagnostics-test-only` |
| 舊版 `elkdoctor-*` 容器存在 | 確認資料可刪除後執行 `./podman-test-env.sh down` |

## 10. Agent 守則

- 只操作 `elk-diagnostics-*`；`elkdoctor-*` 僅限清理歷史測試容器。
- 未取得使用者同意，不得刪除既有測試容器或資料。
- 不得提交 `certs/` 或私鑰。
- TLS 驗證不得使用 `--insecure`／`-k`。
