// Package collector 負責唯讀採集（見 docs/內部/規格/設定規格.md、命令列規格.md §4）。
package collector

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// HTTPStatusError 保留端點與狀態碼，讓選配功能能區分「此 API 不可用」和一般採集失敗。
// Error() 維持既有文字契約，避免報告與測試因型別化而改變措辭。
type HTTPStatusError struct {
	Status int
	Path   string
}

func (e *HTTPStatusError) Error() string { return fmt.Sprintf("ES 回應 %d: %s", e.Status, e.Path) }

// HTTPStatus 從 collector 錯誤取出 HTTP status；網路、解析與 bundle 缺檔回 false。
func HTTPStatus(err error) (int, bool) {
	var statusErr *HTTPStatusError
	if errors.As(err, &statusErr) {
		return statusErr.Status, true
	}
	return 0, false
}

// Options 由 main 從 config 翻譯而來，避免 collector 依賴 config 套件。
type Options struct {
	Hosts      []string
	AuthType   string // none | basic | api_key | bearer
	Username   string
	Password   string
	APIKey     string
	Token      string
	CACert     string
	ClientCert string
	ClientKey  string
	Insecure   bool
	Timeout    time.Duration
	Retries    int // 單一請求的暫時性失敗（網路錯誤/5xx）額外重試次數，見 韌性規格 §2
}

type Client struct {
	hosts      []string
	hc         *http.Client
	authHeader string
	base       string // 故障轉移後選定的 host（bundle 模式為 "(bundle) <dir>"）
	name       string
	uuid       string
	version    string
	retries    int

	// manifest* 僅 bundle 模式、且 bundle 含 _manifest.json 時才有值（見 採集包規格 §4.2）。
	// 連線模式與舊版（無 manifest）bundle 一律留空，呼叫端據此判斷是否顯示採集時間。
	manifestCollectedAt   string
	manifestScriptVersion string
	manifestSchemaVersion int
	manifestServices      []string
	expectedESNodes       []string

	// node resource stats 同時供既有 JVM 壓力與新增 Node Context 使用。單次 check 只採集
	// 一次，避免對每個 consumer 重複送出相同的大型 Nodes Stats 請求。
	nodeResourceStatsOnce sync.Once
	nodeResourceStatsBody []byte
	nodeResourceStatsErr  error

	// fetch 是取得單一端點原始 bytes 的傳輸層：連線模式為 HTTP，bundle 模式為讀檔。
	// 抽成欄位是為了讓兩種模式共用 get() 的重試與錯誤語意——所有固定端點、
	// 所有 analyzer 的行為都完全一致，差別只在 bytes 從哪來。
	fetch func(path string) (body []byte, status int, err error)
}

func (c *Client) nodeResourceStats() ([]byte, error) {
	c.nodeResourceStatsOnce.Do(func() {
		c.nodeResourceStatsBody, c.nodeResourceStatsErr = c.get(EpNodesResourceStats)
	})
	return c.nodeResourceStatsBody, c.nodeResourceStatsErr
}

// retryDelay 是重試間的固定延遲（測試可覆寫以避免實際等待）。
var retryDelay = 150 * time.Millisecond

// New 建立 client：組 TLS、認證標頭，並對 hosts 故障轉移取得版本資訊。
func New(opts Options) (*Client, error) {
	tlsCfg, err := buildTLS(opts)
	if err != nil {
		return nil, err
	}
	authHeader, err := buildAuthHeader(opts)
	if err != nil {
		return nil, err
	}
	if opts.Retries < 0 {
		opts.Retries = 0
	}
	c := &Client{
		hosts:      opts.Hosts,
		authHeader: authHeader,
		retries:    opts.Retries,
		hc: &http.Client{
			Timeout:   opts.Timeout,
			Transport: &http.Transport{TLSClientConfig: tlsCfg},
		},
	}
	c.fetch = c.doGet
	if err := c.connect(); err != nil {
		return nil, err
	}
	return c, nil
}

// NewFromBundle 建立離線分析用的 client：資料來自採集腳本產出的 bundle 目錄，
// 全程不連線任何叢集（見 docs/內部/規格/採集包規格.md）。
//
// 用途是把「採集」與「判斷」拆開執行——使用者環境只跑一份看得懂的 curl 腳本，
// 二進位檔在自己機器上分析 bundle，使用者不必為了健檢導入未知執行檔。
//
// bundle 缺檔一律回錯誤（不是回空值），讓對應診斷落到 unknown 而非 pass：
// 查不到就說查不到，這是 韌性規格 §1/§3 的既有規則。
func NewFromBundle(dir string) (*Client, error) {
	manifest := readBundleManifest(dir)
	schemaVersion := manifest.BundleSchemaVersion
	if schemaVersion == 0 {
		schemaVersion = 1
	}
	if schemaVersion < 1 || schemaVersion > BundleSchemaVersion {
		return nil, fmt.Errorf("不支援的 bundle schema version: %d（本工具最高支援 %d）", schemaVersion, BundleSchemaVersion)
	}
	dataDir := dir
	if schemaVersion >= 2 {
		dataDir = filepath.Join(dir, BundleElasticsearchDir)
	}
	statuses := readBundleStatuses(dir)
	c := &Client{
		base:                  "(bundle) " + dir,
		retries:               0, // 讀本地檔案沒有暫時性失敗，重試無意義
		manifestCollectedAt:   manifest.CollectedAt,
		manifestScriptVersion: manifest.CollectScriptVersion,
		manifestSchemaVersion: schemaVersion,
		manifestServices:      append([]string(nil), manifest.Services...),
	}
	c.fetch = func(path string) ([]byte, int, error) {
		file, ok := FileForEndpoint(path)
		if !ok {
			// 動態端點（如 per-index settings）本來就無法預先採集，見 EpIndexSettings。
			return nil, 0, fmt.Errorf("bundle 模式不支援動態端點: %s", path)
		}
		relFile := file
		if schemaVersion >= 2 {
			relFile = BundleElasticsearchDir + "/" + file
		}
		b, err := os.ReadFile(filepath.Join(dataDir, file))
		if err != nil {
			if os.IsNotExist(err) {
				return nil, 0, fmt.Errorf("bundle 缺少 %s（採集腳本未執行此項或該端點當時失敗）", relFile)
			}
			return nil, 0, err
		}
		// 回放採集當下的真實狀態碼，讓 get() 對 4xx/5xx 的處理與連線模式完全一致。
		if status, ok := statuses[relFile]; ok {
			return b, status, nil
		}
		// 相容早期試作過的 v2 bundle：資料已分目錄，但 _status.txt 仍只記檔名。
		if status, ok := statuses[file]; ok {
			return b, status, nil
		}
		return b, http.StatusOK, nil
	}

	name, uuid, ver, err := c.info()
	if err != nil {
		return nil, fmt.Errorf("讀取 bundle 失敗（%s 不存在或格式錯誤？）: %w", FileOf(EpRoot), err)
	}
	c.name, c.uuid, c.version = name, uuid, ver
	if nodes, readErr := ReadExpectedESNodes(filepath.Join(dir, BundleExpectedESNodesFile)); readErr == nil {
		c.expectedESNodes = nodes
	} else if !os.IsNotExist(readErr) {
		return nil, fmt.Errorf("讀取預期 ES 節點清單失敗: %w", readErr)
	}
	return c, nil
}

const (
	// BundleSchemaVersion 是目前採集包目錄契約版本；它與工具／採集腳本版本不同。
	BundleSchemaVersion       = 2
	BundleManifestFile        = "_manifest.json"
	BundleStatusFile          = "_status.txt"
	BundleExpectedESNodesFile = "_expected_es_nodes.txt"
	BundleElasticsearchDir    = "elasticsearch"
)

type bundleManifest struct {
	BundleSchemaVersion  int      `json:"bundle_schema_version"`
	CollectScriptVersion string   `json:"collect_script_version"`
	CollectedAt          string   `json:"collected_at"`
	Services             []string `json:"services"`
}

// readBundleManifest 讀 bundle 的採集中繼資料。檔案不存在或無 schema 欄位時，
// NewFromBundle 依 v1 平鋪格式讀取；不得用檔案 mtime 或目錄名猜測採集時間。
func readBundleManifest(dir string) bundleManifest {
	b, err := os.ReadFile(filepath.Join(dir, BundleManifestFile))
	if err != nil {
		return bundleManifest{}
	}
	var m bundleManifest
	if err := json.Unmarshal(b, &m); err != nil {
		return bundleManifest{}
	}
	return m
}

// ReadExpectedESNodes 讀取每行一個 node.name 的基準清單。空白、註解與重複值會忽略。
func ReadExpectedESNodes(file string) ([]string, error) {
	b, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var nodes []string
	for _, line := range strings.Split(string(b), "\n") {
		name := strings.TrimSpace(line)
		if name == "" || strings.HasPrefix(name, "#") || seen[name] {
			continue
		}
		seen[name] = true
		nodes = append(nodes, name)
	}
	if len(nodes) == 0 {
		return nil, fmt.Errorf("%s 未包含任何節點名稱", file)
	}
	sort.Strings(nodes)
	return nodes, nil
}

// readBundleStatuses 讀 bundle 的狀態清單（每行 "<檔名> <狀態碼>"）。
//
// 沒有這份清單就無法區分「這個檔是正常回應」與「這個檔是 ES 的錯誤訊息」：
//   - allocation/explain 在健康叢集回 400，而那個錯誤 body 正是「無未分配 shard」的答案
//   - 反過來，若某端點因權限不足回 403，錯誤 body 被當成 200 解析後會得到零值，
//     讓診斷回報「一切正常」——正是 驗證狀態.md §1 那類假綠燈
//
// 檔案不存在時回 nil，呼叫端一律視為 200（相容於直接拿 fixture 目錄當 bundle 的用法）。
func readBundleStatuses(dir string) map[string]int {
	b, err := os.ReadFile(filepath.Join(dir, BundleStatusFile))
	if err != nil {
		return nil
	}
	out := map[string]int{}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		code, err := strconv.Atoi(fields[1])
		if err != nil {
			continue
		}
		out[fields[0]] = code
	}
	return out
}

// FileOf 回傳端點對應的 bundle 檔名，供錯誤訊息與文件使用；未知端點回原 path。
func FileOf(path string) string {
	if f, ok := FileForEndpoint(path); ok {
		return f
	}
	return path
}

func buildTLS(opts Options) (*tls.Config, error) {
	cfg := &tls.Config{InsecureSkipVerify: opts.Insecure} //nolint:gosec // 由使用者明確啟用，報告會警告
	if opts.CACert != "" {
		pem, err := os.ReadFile(opts.CACert)
		if err != nil {
			return nil, fmt.Errorf("讀取 CA 憑證失敗: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("CA 憑證格式無效: %s", opts.CACert)
		}
		cfg.RootCAs = pool
	}
	if opts.ClientCert != "" && opts.ClientKey != "" {
		cert, err := tls.LoadX509KeyPair(opts.ClientCert, opts.ClientKey)
		if err != nil {
			return nil, fmt.Errorf("讀取 client 憑證失敗: %w", err)
		}
		cfg.Certificates = []tls.Certificate{cert}
	}
	return cfg, nil
}

func buildAuthHeader(opts Options) (string, error) {
	switch opts.AuthType {
	case "", "none":
		return "", nil
	case "basic":
		raw := opts.Username + ":" + opts.Password
		return "Basic " + base64.StdEncoding.EncodeToString([]byte(raw)), nil
	case "api_key":
		return "ApiKey " + opts.APIKey, nil
	case "bearer":
		return "Bearer " + opts.Token, nil
	default:
		return "", fmt.Errorf("不支援的認證類型: %q", opts.AuthType)
	}
}

// connect 依序嘗試 hosts，第一個 GET / 成功者為本次連線。
func (c *Client) connect() error {
	var lastErr error
	for _, h := range c.hosts {
		h = strings.TrimRight(h, "/")
		c.base = h
		name, uuid, ver, err := c.info()
		if err == nil {
			c.name, c.uuid, c.version = name, uuid, ver
			return nil
		}
		lastErr = err
	}
	c.base = ""
	return fmt.Errorf("所有 host 連線失敗: %w", lastErr)
}

// get 對暫時性失敗（網路層錯誤、5xx）重試最多 c.retries 次；4xx 視為永久失敗，
// 不重試、直接連同 body 一併回傳（部分 ES 端點用 4xx 表達語意化的非錯誤狀態，
// 如 AllocationExplain 的「無未分配 shard」，呼叫端需要讀到 body 才能判斷），
// 見 韌性規格 §2。
func (c *Client) get(path string) ([]byte, error) {
	// 零值 Client（未經 New/NewFromBundle 建構）預設走 HTTP，不因欄位未設而 panic。
	fetch := c.fetch
	if fetch == nil {
		fetch = c.doGet
	}
	var lastBody []byte
	var lastErr error
	for attempt := 0; attempt <= c.retries; attempt++ {
		body, status, err := fetch(path)
		if err == nil && status < 400 {
			return body, nil
		}
		if err == nil && status < 500 {
			return body, &HTTPStatusError{Status: status, Path: path}
		}
		lastBody, lastErr = body, err
		if lastErr == nil {
			lastErr = &HTTPStatusError{Status: status, Path: path}
		}
		if attempt < c.retries {
			time.Sleep(retryDelay)
		}
	}
	return lastBody, lastErr
}

func (c *Client) doGet(path string) (body []byte, status int, err error) {
	req, err := http.NewRequest(http.MethodGet, c.base+path, nil)
	if err != nil {
		return nil, 0, err
	}
	if c.authHeader != "" {
		req.Header.Set("Authorization", c.authHeader)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err = io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, err
	}
	return body, resp.StatusCode, nil
}

func (c *Client) info() (name, uuid, version string, err error) {
	b, err := c.get(EpRoot)
	if err != nil {
		return "", "", "", err
	}
	var root struct {
		ClusterName string `json:"cluster_name"`
		ClusterUUID string `json:"cluster_uuid"`
		Version     struct {
			Number string `json:"number"`
		} `json:"version"`
	}
	if err := json.Unmarshal(b, &root); err != nil {
		return "", "", "", err
	}
	return root.ClusterName, root.ClusterUUID, root.Version.Number, nil
}

// ClusterName / ClusterUUID / Version：連線時已取得。
func (c *Client) ClusterName() string { return c.name }
func (c *Client) ClusterUUID() string { return c.uuid }
func (c *Client) Version() string     { return c.version }

// CollectedAt / CollectScriptVersion：bundle 模式下若 bundle 含 _manifest.json 才有值，
// 供報告 meta 標示「資料取自何時」（採集包規格 §4.2）。連線模式與舊版 bundle 一律空字串。
func (c *Client) CollectedAt() string          { return c.manifestCollectedAt }
func (c *Client) CollectScriptVersion() string { return c.manifestScriptVersion }
func (c *Client) BundleSchemaVersion() int     { return c.manifestSchemaVersion }
func (c *Client) CollectedServices() []string  { return append([]string(nil), c.manifestServices...) }
func (c *Client) ExpectedESNodes() []string    { return append([]string(nil), c.expectedESNodes...) }

// HealthReport 取並解析 GET /_health_report。
func (c *Client) HealthReport() (*HealthReport, error) {
	b, err := c.get(EpHealthReport)
	if err != nil {
		return nil, err
	}
	return ParseHealthReport(b)
}
