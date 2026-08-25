# Route B：Live 直連操作手冊

本路線只適用於分析機獲准執行 `elk-diagnostics`，且可連線目標 ES 的環境。本手冊不使用 `collect.sh` 或採集包。

## 1. 準備交付檔案

將 Route B 交付包解壓後，切換到該目錄：

```bash
cd /交付包實際路徑
```

應看到其中一個符合作業系統與 CPU 架構的檔案：

```text
elk-diagnostics-linux-amd64
elk-diagnostics-linux-arm64
```

交付包通常只會保留適用當前機器的一個版本。

## 2. 填寫本次直連參數

| 參數／檔案 | 是否必填 | 要改成什麼 |
|---|---|---|
| `/交付包實際路徑` | 是 | 解壓後、直接包含 `elk-diagnostics-linux-*` 的目錄。 |
| `ES_URL` | 是 | 實際 ES HTTPS URL，例如 `https://es.example.local:9200`。 |
| `ES_USER` | 是 | 有必要唯讀權限的 ES 帳號。 |
| `CA_CERT` | 自簽／私有 CA 必填 | CA 憑證檔實際路徑；公有 CA 環境依第 4.2 節操作。 |
| `expected-es-nodes.txt` | 建議必做 | 每行一個正確的 ES `node.name`，不填 IP 或 role。 |
| `REPORT_ROOT` | 指令已自動產生 | 用時間建立新報告目錄，不要改成舊報告目錄。 |
| `CLIENT_LOGO` | 選用 | SVG／PNG／JPEG 檔絕對路徑；檔案上限 512 KiB。 |

密碼不在表格或指令中設定；工具會在執行時互動詢問，輸入不回顯。

## 3. 建立預期 ES 節點清單

```bash
if [ ! -f expected-es-nodes.txt ]; then
  cp expected-es-nodes.txt.example expected-es-nodes.txt
fi
```

用文字編輯器開啟 `expected-es-nodes.txt`，將範例內容全部替換為現場實際的 ES `node.name`：

```text
es-node-01
es-node-02
es-node-03
```

這是該叢集的執行前拓撲基準。同一份清單不得沿用到另一個叢集。

## 4. 執行 Live 直連健檢

### 4.1 自簽或私有 CA（標準做法）

整段複製前，只修改 `cd` 路徑、`ES_URL`、`ES_USER` 與 `CA_CERT` 四行：

```bash
cd /交付包實際路徑
ES_URL='https://es.example.local:9200'
ES_USER='elastic'
CA_CERT='/實際憑證路徑/ca.crt'
REPORT_ROOT="$PWD/reports/live-$(date +%Y%m%d-%H%M%S)"

case "$(uname -m)" in
  x86_64|amd64) TOOL_BIN="$PWD/elk-diagnostics-linux-amd64" ;;
  arm64|aarch64) TOOL_BIN="$PWD/elk-diagnostics-linux-arm64" ;;
  *) echo '找不到適用當前 CPU 架構的交付執行檔'; exit 2 ;;
esac

test -x "$TOOL_BIN" && \
test -r "$CA_CERT" && \
test -s "$PWD/expected-es-nodes.txt" || exit 2

unset ELK_DIAGNOSTICS_HOSTS ELK_DIAGNOSTICS_AUTH_TYPE
unset ELK_DIAGNOSTICS_USERNAME ELK_DIAGNOSTICS_PASSWORD
unset ELK_DIAGNOSTICS_CA_CERT ELK_DIAGNOSTICS_API_KEY ELK_DIAGNOSTICS_TOKEN

mkdir -p "$REPORT_ROOT"
"$TOOL_BIN" check \
  --host "$ES_URL" \
  --username "$ES_USER" \
  --ca-cert "$CA_CERT" \
  --expected-es-nodes-file "$PWD/expected-es-nodes.txt" \
  --output html \
  --output-file "$REPORT_ROOT/check.html" \
  --metrics-output "$REPORT_ROOT/metrics.ndjson"
```

### 4.2 公有 CA

只在憑證是作業系統已信任的公有 CA 時使用；複製前修改 `cd` 路徑、`ES_URL` 與 `ES_USER`：

```bash
cd /交付包實際路徑
ES_URL='https://es.example.local:9200'
ES_USER='elastic'
REPORT_ROOT="$PWD/reports/live-$(date +%Y%m%d-%H%M%S)"

case "$(uname -m)" in
  x86_64|amd64) TOOL_BIN="$PWD/elk-diagnostics-linux-amd64" ;;
  arm64|aarch64) TOOL_BIN="$PWD/elk-diagnostics-linux-arm64" ;;
  *) echo '找不到適用當前 CPU 架構的交付執行檔'; exit 2 ;;
esac

test -x "$TOOL_BIN" && \
test -s "$PWD/expected-es-nodes.txt" || exit 2

unset ELK_DIAGNOSTICS_HOSTS ELK_DIAGNOSTICS_AUTH_TYPE
unset ELK_DIAGNOSTICS_USERNAME ELK_DIAGNOSTICS_PASSWORD
unset ELK_DIAGNOSTICS_CA_CERT ELK_DIAGNOSTICS_API_KEY ELK_DIAGNOSTICS_TOKEN

mkdir -p "$REPORT_ROOT"
"$TOOL_BIN" check \
  --host "$ES_URL" \
  --username "$ES_USER" \
  --expected-es-nodes-file "$PWD/expected-es-nodes.txt" \
  --output html \
  --output-file "$REPORT_ROOT/check.html" \
  --metrics-output "$REPORT_ROOT/metrics.ndjson"
```

正式環境不得使用 `--insecure`。

### 4.3 選用：在報告加入使用者 Logo

請以下列完整指令取代第 4.1 節最後的 `"$TOOL_BIN" check ...` 指令：

```bash
CLIENT_LOGO='/實際 Logo 路徑/logo.svg'
test -f "$CLIENT_LOGO" || exit 2

"$TOOL_BIN" check \
  --host "$ES_URL" \
  --username "$ES_USER" \
  --ca-cert "$CA_CERT" \
  --expected-es-nodes-file "$PWD/expected-es-nodes.txt" \
  --output html \
  --output-file "$REPORT_ROOT/check.html" \
  --client-logo "$CLIENT_LOGO" \
  --metrics-output "$REPORT_ROOT/metrics.ndjson"
```

`--client-logo` 只影響 HTML，不改變診斷結果。

## 5. 確認報告

```bash
test -s "$REPORT_ROOT/check.html" && \
test -s "$REPORT_ROOT/metrics.ndjson" && \
echo 'Live 報告產物：OK'
```

顯示 `Live 報告產物：OK` 代表兩個檔案已產生；健康狀態仍以 `check.html` 內的診斷卡為準。

| 結束碼 | 意義 |
|---:|---|
| 0 | 全部 pass／info，或只有 skipped |
| 1 | 含 warning |
| 2 | 含 critical |
| 3 | 含 unknown，且沒有 warning／critical |
| 10 | 參數或設定錯誤 |
| 11 | 連線或認證失敗 |
| 20 | 工具或輸出錯誤 |

`1`／`2` 代表診斷結果，不代表報告產生失敗。

## 6. 選用：使用設定檔取代連線參數

本節是第 4 節的**完整替代方式**，不得與 `--host`、`--username`、`--ca-cert` 或 `ELK_DIAGNOSTICS_*` 混用。

由交付人員先提供不含密碼的 `config.yaml`，使用者只確認下列值：

```yaml
cluster:
  hosts:
    - "https://es.example.local:9200"
  auth:
    type: basic
    username: "elastic"
  tls:
    ca_cert: "/實際憑證路徑/ca.crt"
    insecure_skip_verify: false
  timeout_seconds: 10
  retries: 2
```

執行：

```bash
cd /交付包實際路徑
CONFIG_FILE="$PWD/config.yaml"
REPORT_ROOT="$PWD/reports/config-$(date +%Y%m%d-%H%M%S)"

case "$(uname -m)" in
  x86_64|amd64) TOOL_BIN="$PWD/elk-diagnostics-linux-amd64" ;;
  arm64|aarch64) TOOL_BIN="$PWD/elk-diagnostics-linux-arm64" ;;
  *) echo '找不到適用當前 CPU 架構的交付執行檔'; exit 2 ;;
esac

test -x "$TOOL_BIN" && \
test -r "$CONFIG_FILE" && \
test -s "$PWD/expected-es-nodes.txt" || exit 2

unset ELK_DIAGNOSTICS_HOSTS ELK_DIAGNOSTICS_AUTH_TYPE
unset ELK_DIAGNOSTICS_USERNAME ELK_DIAGNOSTICS_PASSWORD
unset ELK_DIAGNOSTICS_CA_CERT ELK_DIAGNOSTICS_API_KEY ELK_DIAGNOSTICS_TOKEN

mkdir -p "$REPORT_ROOT"
"$TOOL_BIN" check \
  --config "$CONFIG_FILE" \
  --expected-es-nodes-file "$PWD/expected-es-nodes.txt" \
  --output html \
  --output-file "$REPORT_ROOT/check.html" \
  --metrics-output "$REPORT_ROOT/metrics.ndjson"
```

Basic Auth 密碼仍由執行時互動輸入，不寫入 `config.yaml`。

## 7. 能力邊界

- Live 直連以 ES API 的單次快照為主；CPU、I/O、GC、CCR lag 與 indexing pressure 等項目需搭配 Monitoring 或時間序列判讀。
- 403 或缺少權限會標示 `UNKNOWN` 或 `SKIPPED`，不會當成正常。
- 工具不提供自動修復；所有修改由維運人員人工審核。
