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
}

type Client struct {
	hosts      []string
	hc         *http.Client
	authHeader string
	base       string // 故障轉移後選定的 host
	name       string
	version    string
}

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
	c := &Client{
		hosts:      opts.Hosts,
		authHeader: authHeader,
		hc: &http.Client{
			Timeout:   opts.Timeout,
			Transport: &http.Transport{TLSClientConfig: tlsCfg},
		},
	}
	if err := c.connect(); err != nil {
		return nil, err
	}
	return c, nil
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

func (c *Client) get(path string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, c.base+path, nil)
	if err != nil {
		return nil, err
	}
	if c.authHeader != "" {
		req.Header.Set("Authorization", c.authHeader)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return body, fmt.Errorf("ES 回應 %d: %s", resp.StatusCode, path)
	}
	return body, nil
}

func (c *Client) info() (name, version string, err error) {
	b, err := c.get("/")
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
	b, err := c.get("/_health_report")
	if err != nil {
		return nil, err
	}
	return ParseHealthReport(b)
}
