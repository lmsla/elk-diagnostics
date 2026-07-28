package main

import (
	"fmt"
	"os"

	"golang.org/x/term"

	"elk-diagnostics/internal/config"
)

// ensureBasicAuthPassword keeps interactive passwords out of command arguments,
// shell history and configuration files. Non-interactive automation must inject
// ELK_DIAGNOSTICS_PASSWORD through its secret-management mechanism.
func ensureBasicAuthPassword(cfg *config.Config) error {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return applyBasicAuthPassword(cfg, nil)
	}
	return applyBasicAuthPassword(cfg, func(username string) ([]byte, error) {
		fmt.Fprintf(os.Stderr, "%s 的 Elasticsearch 密碼: ", username)
		password, err := term.ReadPassword(fd)
		fmt.Fprintln(os.Stderr)
		return password, err
	})
}

func applyBasicAuthPassword(cfg *config.Config, prompt func(username string) ([]byte, error)) error {
	if cfg.Cluster.Auth.Type != "basic" || cfg.Cluster.Auth.Password != "" {
		return nil
	}
	if cfg.Cluster.Auth.Username == "" {
		return fmt.Errorf("Basic Auth 缺少帳號")
	}
	if prompt == nil {
		return fmt.Errorf("Basic Auth 缺少密碼，且目前不是互動式終端；自動化環境請由秘密管理機制注入 ELK_DIAGNOSTICS_PASSWORD")
	}

	password, err := prompt(cfg.Cluster.Auth.Username)
	if err != nil {
		return fmt.Errorf("讀取密碼失敗: %w", err)
	}
	if len(password) == 0 {
		return fmt.Errorf("Basic Auth 密碼不可為空")
	}

	cfg.Cluster.Auth.Password = string(password)
	for i := range password {
		password[i] = 0
	}
	return nil
}
