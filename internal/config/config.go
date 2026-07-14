// Package config 載入連線設定（見 docs/specs/spec-config.md）。
// 優先序：flag > 環境變數 > config.yaml > 內建預設。
package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type Auth struct {
	Type     string `yaml:"type"` // none | basic | api_key | bearer
	Username string `yaml:"username"`
	Password string `yaml:"password"`
	APIKey   string `yaml:"api_key"`
	Token    string `yaml:"token"`
}

type TLS struct {
	CACert             string `yaml:"ca_cert"`
	ClientCert         string `yaml:"client_cert"`
	ClientKey          string `yaml:"client_key"`
	InsecureSkipVerify bool   `yaml:"insecure_skip_verify"`
}

type Cluster struct {
	Hosts          []string `yaml:"hosts"`
	Auth           Auth     `yaml:"auth"`
	TLS            TLS      `yaml:"tls"`
	TimeoutSeconds int      `yaml:"timeout_seconds"`
	Retries        int      `yaml:"retries"`
}

type Config struct {
	Cluster Cluster `yaml:"cluster"`
}

// Default 內建預設（零設定可跑的基礎）。
func Default() Config {
	return Config{Cluster: Cluster{
		Auth:           Auth{Type: "none"},
		TimeoutSeconds: 10,
		Retries:        2,
	}}
}

// Load：預設 ← 套上 config.yaml（存在才讀；缺檔不報錯，回預設）。
func Load(path string) (Config, error) {
	cfg := Default()
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, err
	}
	// 直接 unmarshal 到帶預設值的 cfg：YAML 有的鍵覆寫，沒有的保留預設。
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return cfg, fmt.Errorf("解析 %s 失敗: %w", path, err)
	}
	return cfg, nil
}

// ApplyEnv 以環境變數覆寫（僅當變數非空）。
func (c *Config) ApplyEnv() {
	if v := os.Getenv("ELK_DIAGNOSTICS_HOSTS"); v != "" {
		c.Cluster.Hosts = splitCSV(v)
	}
	if v := os.Getenv("ELK_DIAGNOSTICS_AUTH_TYPE"); v != "" {
		c.Cluster.Auth.Type = v
	}
	if v := os.Getenv("ELK_DIAGNOSTICS_USERNAME"); v != "" {
		c.Cluster.Auth.Username = v
		if c.Cluster.Auth.Type == "" || c.Cluster.Auth.Type == "none" {
			c.Cluster.Auth.Type = "basic"
		}
	}
	if v := os.Getenv("ELK_DIAGNOSTICS_PASSWORD"); v != "" {
		c.Cluster.Auth.Password = v
	}
	if v := os.Getenv("ELK_DIAGNOSTICS_API_KEY"); v != "" {
		c.Cluster.Auth.APIKey = v
		c.Cluster.Auth.Type = "api_key"
	}
	if v := os.Getenv("ELK_DIAGNOSTICS_TOKEN"); v != "" {
		c.Cluster.Auth.Token = v
		c.Cluster.Auth.Type = "bearer"
	}
	if v := os.Getenv("ELK_DIAGNOSTICS_CA_CERT"); v != "" {
		c.Cluster.TLS.CACert = v
	}
}

// FlagOverrides 為 main 收集的 flag 值（空字串/nil 表未提供）。
type FlagOverrides struct {
	Hosts    []string
	Username *string
	Password *string
	APIKey   *string
	CACert   *string
	Insecure *bool
	Timeout  *int
}

// ApplyFlags 以 flag 覆寫（最高優先）。
func (c *Config) ApplyFlags(f FlagOverrides) {
	if len(f.Hosts) > 0 {
		c.Cluster.Hosts = f.Hosts
	}
	if f.Username != nil && *f.Username != "" {
		c.Cluster.Auth.Username = *f.Username
		if c.Cluster.Auth.Type == "" || c.Cluster.Auth.Type == "none" {
			c.Cluster.Auth.Type = "basic"
		}
	}
	if f.Password != nil && *f.Password != "" {
		c.Cluster.Auth.Password = *f.Password
	}
	if f.APIKey != nil && *f.APIKey != "" {
		c.Cluster.Auth.APIKey = *f.APIKey
		c.Cluster.Auth.Type = "api_key"
	}
	if f.CACert != nil && *f.CACert != "" {
		c.Cluster.TLS.CACert = *f.CACert
	}
	if f.Insecure != nil && *f.Insecure {
		c.Cluster.TLS.InsecureSkipVerify = true
	}
	if f.Timeout != nil && *f.Timeout > 0 {
		c.Cluster.TimeoutSeconds = *f.Timeout
	}
}

// Validate：連線資訊為唯一必填。
func (c Config) Validate() error {
	if len(c.Cluster.Hosts) == 0 {
		return fmt.Errorf("缺少連線資訊：請以 config.yaml 的 cluster.hosts、--host、或 ELK_DIAGNOSTICS_HOSTS 擇一提供")
	}
	return nil
}

func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
