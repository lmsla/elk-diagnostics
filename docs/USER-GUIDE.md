# elk-diagnostics 操作手冊

本手冊供實際健檢操作使用，不包含內部故障注入。P01～P16 測試請改用 [`VERIFICATION-PLAYBOOK.md`](./VERIFICATION-PLAYBOOK.md)。

## 1. 選擇執行方式

| 方式 | 適用情境 | 客戶環境執行內容 |
|---|---|---|
| A：Live 直連 | 已允許執行 binary，且分析機器可連 ES | `elk-diagnostics` 直接唯讀查詢 ES |
| B：Bundle 離線分析 | 客戶環境不允許執行未知 binary | 客戶只執行可審閱的 `collect.sh` |

兩種方式共用相同診斷邏輯。完整健檢以 ES 8.4 以上為前提；低於 8.4 時，缺少 `_health_report` 的項目會標示 `skipped`，其餘項目附版本警告。

## 2. 共通準備

操作目錄至少需要：

```text
elk-diagnostics        # 分析端使用
collect.sh             # Route B 採集端使用
ca.crt                 # 使用自簽 CA 時需要
```

確認檔案：

```bash
test -x ./elk-diagnostics
./elk-diagnostics version
```

若顯示 `command not found`，請使用 `./elk-diagnostics`，不要省略 `./`。

## 3. Route A：Live 直連 ES

### 3.1 選擇連線設定來源

Route A 有三種設定方式，可混用；同一欄位重複設定時依下列優先序套用：

```text
flags > 環境變數 > config.yaml > 內建預設
```

| 方式 | 適用情境 | 生效範圍 |
|---|---|---|
| `config.yaml` | 正式環境、固定叢集（建議） | 保留於本機檔案 |
| 環境變數 | 臨時測試或既有自動化 | 目前 Shell 與其子程序 |
| flags | 單次覆寫或快速測試 | 只有本次指令 |

三種方式都不要填入密碼；Basic Auth 密碼預設由 binary 在執行時安全詢問。

#### 3.1.1 `config.yaml`（正式環境建議）

複製範本：

```bash
cp config.yaml.example config.yaml
```

編輯 `config.yaml`，只保存非敏感連線資訊：

```yaml
cluster:
  hosts:
    - "https://es.example.local:9200"
  auth:
    type: basic
    username: "elastic"
  tls:
    ca_cert: "/absolute/path/to/ca.crt"
    insecure_skip_verify: false
  timeout_seconds: 10
  retries: 2
```

`./elk-diagnostics check` 預設讀取目前目錄的 `config.yaml`。使用其他檔名時指定：

```bash
./elk-diagnostics check --config /absolute/path/to/customer.yaml --output text
```

#### 3.1.2 環境變數（臨時操作）

將 ES URL、帳號與 CA 路徑替換成實際值：

```bash
export ELK_DIAGNOSTICS_HOSTS='https://es.example.local:9200'
export ELK_DIAGNOSTICS_AUTH_TYPE=basic
export ELK_DIAGNOSTICS_USERNAME='elastic'
export ELK_DIAGNOSTICS_CA_CERT="$PWD/ca.crt"
```

環境變數只需設定一次，後續在同一個 Terminal 執行的 `check`／`diagnose` 都會沿用。

操作完成後應立即清除，避免後續執行時意外覆寫 `config.yaml`，或連線到錯誤叢集：

```bash
unset ELK_DIAGNOSTICS_HOSTS \
  ELK_DIAGNOSTICS_AUTH_TYPE \
  ELK_DIAGNOSTICS_USERNAME \
  ELK_DIAGNOSTICS_PASSWORD \
  ELK_DIAGNOSTICS_API_KEY \
  ELK_DIAGNOSTICS_TOKEN \
  ELK_DIAGNOSTICS_CA_CERT
```

`unset` 只清除目前 Shell 的環境變數，不會修改 `config.yaml` 或 Elasticsearch。

#### 3.1.3 Flags（單次覆寫）

```bash
./elk-diagnostics check \
  --host 'https://es.example.local:9200' \
  --username 'elastic' \
  --ca-cert "$PWD/ca.crt" \
  --output text
```

上述 flags 只對這一次執行生效，不會修改 `config.yaml` 或目前 Shell 的環境變數。

#### 3.1.4 密碼與 TLS 安全

執行 `check` 或 `diagnose` 時，binary 會在 Terminal 詢問密碼且不回顯。密碼不會進入 shell history；每次執行結束後即不再保留於 shell 環境。

非互動式自動化才使用 `ELK_DIAGNOSTICS_PASSWORD`，且必須由企業秘密管理系統或 CI/CD protected secret 注入；不要手動執行含有真實密碼的 `export`，也不要把密碼寫進 `config.yaml`、腳本或版控。

`--password` 僅為向下相容而保留，已棄用。正式環境不應使用 `--password` 或 `--insecure`；應採互動輸入並提供正確 CA 憑證。

### 3.2 執行全面健檢

先建立本次報告目錄：

```bash
export REPORT_ROOT="$PWD/reports/live-$(date +%Y%m%d-%H%M%S)"
mkdir -p "$REPORT_ROOT"
```

Terminal 文字報告：

```bash
./elk-diagnostics check \
  --output text \
  --output-file "$REPORT_ROOT/check-report.txt"
```

HTML 報告：

```bash
./elk-diagnostics check \
  --output html \
  --output-file "$REPORT_ROOT/check-report.html"
```

JSON 報告：

```bash
./elk-diagnostics check \
  --output json \
  --output-file "$REPORT_ROOT/check-report.json"
```

每次執行都會重新查詢 ES。若 HTML 與 JSON 必須基於完全相同的時間點，應改走 Route B，以同一份 bundle 分別產生兩種格式。

### Node Context

`check` 會透過 ES Nodes APIs 收集所有回應節點的 OS、Elasticsearch process、filesystem 與 JVM 快照。HTML 的「節點環境（Node Context）」可查看：

- OS／架構／processors、CPU/load、memory/swap、Linux cgroup。
- Elasticsearch PID、memory lock、CPU、virtual memory、open/max file descriptors。
- data path、mount、容量與 filesystem I/O 累積值。
- JVM heap／old pool、uptime 與 GC 累積值。

報告中的 `OS RAM（含 cache）` 是主機／容器層的單次使用率快照，可能包含可回收的 filesystem cache；`JVM Heap` 只代表 Elasticsearch JVM heap。兩者不能互換判讀，單次 OS RAM 高值也不單獨構成記憶體壓力告警。

`node_api_coverage` 會核對 Nodes Stats 與 Nodes Info 的 `_nodes.total/successful/failed`。任一節點未回應時標為 `unknown`，不會把已回應節點的正常結果當成全叢集正常。

這不是完整主機健檢：不使用 SSH，不包含 kernel/OOM/NTP/網路資訊。I/O、GC 與 CPU throttling 是累積 counter，單次報告不會把它們解讀成 latency 或 rate。

### 單次快照擴充檢查

`check` 另會檢查 cluster pending／長任務、shard 大小、SLM snapshot 新鮮度、node runtime 漂移、TLS／License、replica／allocation awareness，以及 indexing pressure、index block、近期重啟／memory lock、CCR、ML、planned shutdown／voting exclusions。門檻可用 `--rules` 覆寫，預設值見 [`rules/default.yaml`](../rules/default.yaml)。

一般檢查需要 cluster `monitor`、SLM `read_slm`、ML `monitor_ml`，以及可讀取目標 index settings/CAT shards 的 index `monitor`。缺權限會標 `unknown`，不會當成正常。Planned shutdown 是例外：官方要求 cluster `manage`，且不支援直接使用，因此定義為高權限選配檢查；HTTP 403/404 只讓該項 `skipped`，不要求一般健檢帳號升權。

CCR／ML 未使用或 license 未啟用時為 `skipped`。Indexing pressure、近期重啟、CCR lag 與 maintenance metadata 都是單次快照訊號，報告會要求時間序列或維護窗口佐證。`/_ssl/certificates` 只代表接到請求的單一 ES 節點；即使 pass，仍需逐節點確認完整叢集憑證。

### 3.3 症狀導向診斷

Route A 可針對症狀執行診斷樹：

```bash
./elk-diagnostics diagnose --symptom red-cluster --output text
./elk-diagnostics diagnose --symptom write-bottleneck --output text
./elk-diagnostics diagnose --symptom high-heap --output text
./elk-diagnostics diagnose --symptom ingest-lag --output text
./elk-diagnostics diagnose --symptom ilm-stuck --output text
```

Bundle 模式目前只支援完整的 `check --from-bundle`，不支援離線 `diagnose`。

### 3.4 清除連線環境變數

```bash
unset ELK_DIAGNOSTICS_HOSTS
unset ELK_DIAGNOSTICS_AUTH_TYPE
unset ELK_DIAGNOSTICS_USERNAME
unset ELK_DIAGNOSTICS_PASSWORD
unset ELK_DIAGNOSTICS_CA_CERT
```

## 4. Route B：採集 Bundle 後離線分析

Route B 分成兩個獨立階段。採集端不得執行 binary；分析端不需要連線客戶 ES。

### 4.1 階段一：客戶環境只採集 Bundle

需要 `collect.sh`、POSIX shell 與 curl，不需要 Go、jq 或 `elk-diagnostics`。

```bash
export BUNDLE_ROOT="$PWD/bundle-$(date +%Y%m%d-%H%M%S)"

./collect.sh \
  -h 'https://es.example.local:9200' \
  -u 'elastic' \
  --ca-cert "$PWD/ca.crt" \
  -o "$BUNDLE_ROOT"
```

若未提供密碼，腳本會在 Terminal 互動詢問且不回顯。非互動式正式採集優先由企業秘密
管理系統掛載權限 `600` 的檔案，再只傳入檔案路徑：

```bash
ES_PASSWORD_FILE=/run/secrets/elasticsearch-password ./collect.sh \
  -h 'https://es.example.local:9200' \
  -u 'elastic' \
  --ca-cert "$PWD/ca.crt" \
  -o "$BUNDLE_ROOT"
```

`ES_PASSWORD` 僅保留給無法掛載秘密檔案的既有受控自動化；不要手動輸入含真實密碼的
`export`，也不要把密碼寫進命令列、一般設定檔或版控。

採集完成後確認：

```bash
test -f "$BUNDLE_ROOT/version.json"
test -f "$BUNDLE_ROOT/_manifest.json"
test -f "$BUNDLE_ROOT/_status.txt"
echo "bundle_root=$BUNDLE_ROOT"
```

部分端點回傳非 2xx 不一定表示採集失敗。例如健康叢集沒有未分配 shard 時，allocation explain 可能回傳語意化的 HTTP 400；分析端會依 `_status.txt` 與回應內容判讀。

交接前必須人工檢視 Bundle。它不包含 Elasticsearch 文件內容，但可能包含 index、node、IP、hostname 與 mapping 欄位名稱；目前尚未實作自動遮罩。

### 4.2 階段二：分析端產生報告

將 Bundle 安全帶回分析機器並開新的 Terminal。每次案件使用獨立工作目錄，將收到的 Bundle 目錄放在其中並命名為 `bundle-input`；該目錄必須直接包含 `version.json`。

```bash
export BUNDLE_ROOT="$PWD/bundle-input"
export TOOL_BIN="$PWD/elk-diagnostics"
export REPORT_ROOT="$PWD/reports/$(basename "$BUNDLE_ROOT")"

test -x "$TOOL_BIN"
test -f "$BUNDLE_ROOT/version.json"
mkdir -p "$REPORT_ROOT"
```

產生 HTML：

```bash
env -i PATH='/usr/bin:/bin' \
  "$TOOL_BIN" check \
  --from-bundle "$BUNDLE_ROOT" \
  --output html \
  > "$REPORT_ROOT/check-report.html"
```

產生 JSON：

```bash
env -i PATH='/usr/bin:/bin' \
  "$TOOL_BIN" check \
  --from-bundle "$BUNDLE_ROOT" \
  --output json \
  > "$REPORT_ROOT/check-report.json"
```

`env -i` 會清除 ES host、帳密與 CA 等連線環境，證明分析結果只來自 Bundle。HTML 為單一離線檔案，不依賴外部 CDN、字型或 JavaScript 服務。

SLM、TLS 與 License 的時間判斷會以 Bundle `_manifest.json` 的 `collected_at` 為基準，不會因為晚幾天分析而改寫採集當下狀態。

預期目錄結構：

```text
工作目錄/
├── elk-diagnostics
├── bundle-input/                  # 收到的原始採集資料，不修改
│   ├── version.json
│   ├── _manifest.json
│   ├── _status.txt
│   └── ...
└── reports/
    └── bundle-input/
        ├── check-report.html
        └── check-report.json
```

## 5. 報告狀態與結束碼

報告產生成功仍可能回傳非 0，因為 exit code 同時代表最高診斷嚴重度：

| code | 意義 |
|---|---|
| `0` | 全部 pass，或只有 skipped |
| `1` | 最高為 warning |
| `2` | 存在 critical |
| `3` | 存在 unknown，且沒有 warning／critical |
| `10` | 參數或設定錯誤 |
| `11` | 連線、認證或 Bundle 讀取失敗 |
| `20` | 工具內部或輸出錯誤 |

因此看到 exit code `1` 或 `2` 時，先確認報告檔是否已產生，再查看診斷內容；不要直接當成程式執行失敗。

## 6. 常見錯誤

| 錯誤 | 原因與處理 |
|---|---|
| `command not found: elk-diagnostics` | binary 未安裝到 PATH；改用 `./elk-diagnostics` |
| `x509: certificate signed by unknown authority` | `--ca-cert` 或 `ELK_DIAGNOSTICS_CA_CERT` 未指向正確 CA |
| HTTP 401 | 帳密或 API key 錯誤 |
| HTTP 403／報告出現 unknown | 健檢帳號缺少部分唯讀 API 權限；比對 [`api-inventory.md`](./api-inventory.md) |
| `bundle 缺少 version.json` | `--from-bundle` 指錯層級；應指向直接包含 `version.json` 的目錄 |
| HTML 檔為空 | 通常是 Bundle 路徑或參數錯誤；查看 Terminal stderr 與 exit code |
| macOS／端點防護提示未知 binary | Route A 需完成公司允許程序；若客戶端不允許 binary，改走 Route B |

## 7. 安全邊界

- 工具只允許唯讀查詢，不提供自動修復或寫入 ES 的功能。
- 不要在指令歷史、報告或版控中保存密碼、API key、token 或私鑰。
- 不要為了省事在正式環境使用 `--insecure`。
- Bundle 離開客戶環境前必須依資料治理流程審閱與加密傳輸；Node Context 另含 OS 版本、data path、mount 與 device 名稱。
- 報告是診斷引導，不等於已確認根因；修復操作仍需人工審核。
