# Route A：採集操作手冊

本路線是主要交付方式。使用者端只執行 Shell 採集腳本；交付包不需要診斷執行檔。

操作流程：

```text
準備參數 → 建立預期節點清單 → 執行 collect.sh → 確認採集結果 → 交付 .tar.gz
```

## 1. 準備交付檔案

將 Route A 交付包解壓到可寫入目錄，並切換到該目錄：

```bash
cd /交付包實際路徑
```

應看到：

```text
collect.sh
collectors/
expected-es-nodes.txt.example
kibana-instances.conf.example
logstash-instances.conf.example
API清單.md
路線A-採集操作手冊.md
```

前置檢查：

```bash
command -v curl >/dev/null && \
test -x ./collect.sh && \
echo '採集前置檢查：OK'
```

若未顯示 `採集前置檢查：OK`，不要繼續。

## 2. 填寫本次採集參數

| 參數 | 是否必填 | 要改成什麼 |
|---|---|---|
| `/交付包實際路徑` | 是 | 解壓後、直接包含 `collect.sh` 的目錄。 |
| `ES_URL` | 是 | 實際 ES HTTPS URL，例如 `https://es.example.local:9200`。 |
| `ES_USER` | 是 | 有必要唯讀權限的 ES 帳號。 |
| `CA_CERT` | 自簽／私有 CA 必填 | CA 憑證檔實際路徑；公有 CA 環境依第 4.2 節操作。 |
| `expected-es-nodes.txt` | 建議必做 | 每行一個正確的 ES `node.name`，不填 IP 或 role。 |
| `kibana-instances.conf` | 多 Kibana 時使用 | 每行 `instance-label|Kibana URL`；label 是自訂且不可重複的目錄／報告名稱。 |
| `logstash-instances.conf` | 多 Logstash 時使用 | 每行 `instance-label|Logstash Node API URL`；label 是自訂且不可重複的目錄／報告名稱。 |
| `BUNDLE_ROOT` | 指令已自動產生 | 每次採集都會用時間建立新目錄，不要改成舊採集包。 |

密碼不在表格或指令中設定；腳本會在執行時互動詢問，輸入不回顯。

## 3. 建立預期 ES 節點清單

這份清單用來找出「採集開始前已離線」的節點。

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

檢查清單：

```bash
test -s ./expected-es-nodes.txt && \
echo '預期 ES 節點清單：OK'
```

同一份清單只適用一個叢集，不得沿用到另一個叢集。

## 4. 執行採集

### 4.1 自簽或私有 CA（標準做法）

整段複製前，只修改 `cd` 路徑、`ES_URL`、`ES_USER` 與 `CA_CERT` 四行：

```bash
cd /交付包實際路徑
ES_URL='https://es.example.local:9200'
ES_USER='elastic'
CA_CERT='/實際憑證路徑/ca.crt'
BUNDLE_ROOT="$PWD/bundle-$(date +%Y%m%d-%H%M%S)"

test -r "$CA_CERT" && \
test -s "$PWD/expected-es-nodes.txt" || exit 2

unset ES_PASSWORD_FILE ES_PASSWORD ES_API_KEY
./collect.sh \
  --services es \
  --host "$ES_URL" \
  --username "$ES_USER" \
  --ca-cert "$CA_CERT" \
  --expected-es-nodes-file "$PWD/expected-es-nodes.txt" \
  --output "$BUNDLE_ROOT"
```

### 4.2 公有 CA

只在憑證是作業系統已信任的公有 CA 時使用；複製前修改 `cd` 路徑、`ES_URL` 與 `ES_USER`：

```bash
cd /交付包實際路徑
ES_URL='https://es.example.local:9200'
ES_USER='elastic'
BUNDLE_ROOT="$PWD/bundle-$(date +%Y%m%d-%H%M%S)"

test -s "$PWD/expected-es-nodes.txt" || exit 2

unset ES_PASSWORD_FILE ES_PASSWORD ES_API_KEY
./collect.sh \
  --services es \
  --host "$ES_URL" \
  --username "$ES_USER" \
  --expected-es-nodes-file "$PWD/expected-es-nodes.txt" \
  --output "$BUNDLE_ROOT"
```

正式環境不得以 `--insecure` 取代憑證驗證。

## 5. 選配採集 Kibana 與 Logstash

只採集 ES 時略過本節。若需在同一採集包內加入 Kibana 與 Logstash，先複製清單範本：

```bash
cp kibana-instances.conf.example kibana-instances.conf
cp logstash-instances.conf.example logstash-instances.conf
```

編輯清單，每行格式如下；`label` 由使用者自行命名，必須唯一，不是要向 Elastic 查詢的 UUID。URL 才是實際連線目標：

```text
kibana-01|https://kibana-01.example.local:5601
kibana-02|https://kibana-02.example.local:5601
```

若某服務只有一個 instance，也可不建立清單，改用單一 `--kibana-url`／`--kibana-id` 或 `--logstash-url`／`--logstash-id`。多 instance 時使用清單；兩種方式不可同時指定同一服務。

另開一次新採集，只修改 `cd` 路徑與下列現場值：

| 值 | 內容 |
|---|---|
| `ES_URL`／`ES_USER` | ES URL 與帳號 |
| `CA_CERT` | 預設 CA 憑證檔 |
| `kibana-instances.conf`／`logstash-instances.conf` | 實際 instance 清單與 URL；單一 instance 可改用 URL 參數 |
| `KIBANA_USER`／`LOGSTASH_USER` | 對應服務帳號；未使用 Basic Auth 時留空 |

```bash
cd /交付包實際路徑
ES_URL='https://es.example.local:9200'
ES_USER='elastic'
CA_CERT='/實際憑證路徑/ca.crt'
KIBANA_USER='elastic'
LOGSTASH_USER=''
BUNDLE_ROOT="$PWD/bundle-elk-$(date +%Y%m%d-%H%M%S)"

test -r "$CA_CERT" && \
test -s "$PWD/expected-es-nodes.txt" && \
test -d "$PWD/collectors" || exit 2

export KIBANA_USERNAME="$KIBANA_USER"
export LOGSTASH_USERNAME="$LOGSTASH_USER"
unset ES_PASSWORD_FILE ES_PASSWORD ES_API_KEY
unset KIBANA_PASSWORD_FILE KIBANA_API_KEY KIBANA_CA_CERT
unset LOGSTASH_PASSWORD_FILE LOGSTASH_API_KEY LOGSTASH_CA_CERT

./collect.sh \
  --services es,kibana,logstash \
  --host "$ES_URL" \
  --username "$ES_USER" \
  --kibana-list "$PWD/kibana-instances.conf" \
  --logstash-list "$PWD/logstash-instances.conf" \
  --ca-cert "$CA_CERT" \
  --expected-es-nodes-file "$PWD/expected-es-nodes.txt" \
  --output "$BUNDLE_ROOT"

unset KIBANA_USERNAME LOGSTASH_USERNAME
```

採集時終端機會逐一列出每個 label 與 URL 的結果，並在最後輸出 ES、Kibana、Logstash 摘要。`連線失敗` 只表示該 URL 當下無法取得核心 API，不能單憑此結果判定 instance 已離線；可能原因包括 URL、網路、TLS、帳號或權限設定錯誤。採集腳本會繼續處理其他目標，並將每個目標的 `_status.txt` 保留在對應目錄。

只加入其中一項服務時，請由交付人員先準備對應指令，不要由使用者自行拆改上述區塊。

## 6. 確認採集結果

採集完成後，在同一個 Terminal 執行：

```bash
test -s "$BUNDLE_ROOT/elasticsearch/version.json" && \
test -s "$BUNDLE_ROOT/_manifest.json" && \
test -s "$BUNDLE_ROOT/_status.txt" && \
test -s "$BUNDLE_ROOT.tar.gz" && \
echo '採集包與壓縮檔：OK'
```

只有顯示 `採集包與壓縮檔：OK` 才代表必要檔案存在。接著查看端點狀態：

```bash
sed -n '1,200p' "$BUNDLE_ROOT/_status.txt"
```

個別端點的 HTTP 400／403／timeout 可能來自版本、權限或當下語意；不得因為壓縮檔已產生就忽略。

## 7. 交付採集包

交付檔案：

```text
$BUNDLE_ROOT.tar.gz
```

採集包可能包含 index、node、IP、hostname 與 mapping 欄位名稱。傳輸前必須依現場資料治理流程審閱與核准。

使用者端的 Route A 操作到此結束。後續報告產生由獲准的分析端流程處理。

## 8. 分析端產生 HTML 報告

採集包交付給分析端後，先解壓 `.tar.gz`。`--from-bundle` 必須指定「包含 `_manifest.json` 的目錄」，不能直接指定 `.tar.gz` 檔案。

分析端需要準備：

| 項目 | 說明 |
|---|---|
| `BUNDLE_ROOT` | 解壓後的採集資料目錄；應包含 `_manifest.json`、`_status.txt` 與 `elasticsearch/`。 |
| `TOOL_BIN` | 與分析機作業系統／CPU 架構相符的 `elk-diagnostics` binary。 |
| `REPORT_ROOT` | 報告輸出目錄；可自行指定。 |

分析端執行：

```bash
cd /分析端實際路徑
BUNDLE_ROOT='/解壓後/包含_manifest.json的採集目錄'
TOOL_BIN="$PWD/elk-diagnostics-linux-amd64"
REPORT_ROOT="$PWD/reports/$(basename "$BUNDLE_ROOT")"

test -x "$TOOL_BIN" && \
test -s "$BUNDLE_ROOT/_manifest.json" && \
test -s "$BUNDLE_ROOT/_status.txt" || exit 2

mkdir -p "$REPORT_ROOT"
"$TOOL_BIN" check \
  --from-bundle "$BUNDLE_ROOT" \
  --output html \
  --output-file "$REPORT_ROOT/check.html" \
  --metrics-output "$REPORT_ROOT/metrics.ndjson"

test -s "$REPORT_ROOT/check.html" && \
test -s "$REPORT_ROOT/metrics.ndjson" && \
echo 'HTML 報告與趨勢資料：OK'
```

若分析端是 ARM64，將 `TOOL_BIN` 改為 `elk-diagnostics-linux-arm64`。這個階段只讀取 Bundle，不需要 ES／Kibana／Logstash URL、帳號、密碼或 CA 憑證；HTML 報告會保留採集時的端點狀態與服務 instance 資訊。
