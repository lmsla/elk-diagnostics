// Package collector 負責唯讀採集（見 docs/specs/spec-config.md、spec-cli.md §4）。
package collector

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

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
	Retries    int // 單一請求的暫時性失敗（網路錯誤/5xx）額外重試次數，見 spec-resilience §2
}

type Client struct {
	hosts      []string
	hc         *http.Client
	authHeader string
	base       string // 故障轉移後選定的 host（bundle 模式為 "(bundle) <dir>"）
	name       string
	version    string
	retries    int

	// fetch 是取得單一端點原始 bytes 的傳輸層：連線模式為 HTTP，bundle 模式為讀檔。
	// 抽成欄位是為了讓兩種模式共用 get() 的重試與錯誤語意——所有 24 個端點、
	// 所有 analyzer 的行為都完全一致，差別只在 bytes 從哪來。
	fetch func(path string) (body []byte, status int, err error)
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
// 全程不連線任何叢集（見 docs/specs/spec-bundle.md）。
//
// 用途是把「採集」與「判斷」拆開執行——客戶環境只跑一份看得懂的 curl 腳本，
// 二進位檔在自己機器上分析 bundle，客戶不必為了健檢導入未知執行檔。
//
// bundle 缺檔一律回錯誤（不是回空值），讓對應診斷落到 unknown 而非 pass：
// 查不到就說查不到，這是 spec-resilience §1/§3 的既有規則。
func NewFromBundle(dir string) (*Client, error) {
	c := &Client{
		base:    "(bundle) " + dir,
		retries: 0, // 讀本地檔案沒有暫時性失敗，重試無意義
	}
	c.fetch = func(path string) ([]byte, int, error) {
		file, ok := FileForEndpoint(path)
		if !ok {
			// 動態端點（如 per-index settings）本來就無法預先採集，見 EpIndexSettings。
			return nil, 0, fmt.Errorf("bundle 模式不支援動態端點: %s", path)
		}
		b, err := os.ReadFile(filepath.Join(dir, file))
		if err != nil {
			if os.IsNotExist(err) {
				return nil, 0, fmt.Errorf("bundle 缺少 %s（採集腳本未執行此項或該端點當時失敗）", file)
			}
			return nil, 0, err
		}
		return b, http.StatusOK, nil
	}

	name, ver, err := c.info()
	if err != nil {
		return nil, fmt.Errorf("讀取 bundle 失敗（%s 不存在或格式錯誤？）: %w", FileOf(EpRoot), err)
	}
	c.name, c.version = name, ver
	return c, nil
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
		name, ver, err := c.info()
		if err == nil {
			c.name, c.version = name, ver
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
// 見 spec-resilience §2。
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
			return body, fmt.Errorf("ES 回應 %d: %s", status, path)
		}
		lastBody, lastErr = body, err
		if lastErr == nil {
			lastErr = fmt.Errorf("ES 回應 %d: %s", status, path)
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

func (c *Client) info() (name, version string, err error) {
	b, err := c.get(EpRoot)
	if err != nil {
		return "", "", err
	}
	var root struct {
		ClusterName string `json:"cluster_name"`
		Version     struct {
			Number string `json:"number"`
		} `json:"version"`
	}
	if err := json.Unmarshal(b, &root); err != nil {
		return "", "", err
	}
	return root.ClusterName, root.Version.Number, nil
}

// ClusterName / Version：連線時已取得。
func (c *Client) ClusterName() string { return c.name }
func (c *Client) Version() string     { return c.version }

// HealthReport 取並解析 GET /_health_report。
func (c *Client) HealthReport() (*HealthReport, error) {
	b, err := c.get(EpHealthReport)
	if err != nil {
		return nil, err
	}
	return ParseHealthReport(b)
}
