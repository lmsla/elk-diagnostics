package main

import (
	"strings"
	"testing"

	"elk-diagnostics/internal/config"
)

func TestEnsureBasicAuthPasswordSkipsExistingPassword(t *testing.T) {
	cfg := config.Default()
	cfg.Cluster.Auth = config.Auth{
		Type:     "basic",
		Username: "elastic",
		Password: "already-set",
	}

	if err := ensureBasicAuthPassword(&cfg); err != nil {
		t.Fatalf("已有密碼時不應要求互動輸入: %v", err)
	}
	if cfg.Cluster.Auth.Password != "already-set" {
		t.Fatalf("既有密碼不應被改寫")
	}
}

func TestApplyBasicAuthPasswordUsesSecurePrompt(t *testing.T) {
	cfg := config.Default()
	cfg.Cluster.Auth = config.Auth{
		Type:     "basic",
		Username: "elastic",
	}
	promptedFor := ""

	err := applyBasicAuthPassword(&cfg, func(username string) ([]byte, error) {
		promptedFor = username
		return []byte("test-only"), nil
	})
	if err != nil {
		t.Fatalf("互動輸入應成功: %v", err)
	}
	if promptedFor != "elastic" {
		t.Fatalf("提示帳號錯誤: %q", promptedFor)
	}
	if cfg.Cluster.Auth.Password != "test-only" {
		t.Fatal("互動輸入的密碼未套用")
	}
}

func TestApplyBasicAuthPasswordRejectsNonInteractiveInput(t *testing.T) {
	cfg := config.Default()
	cfg.Cluster.Auth = config.Auth{
		Type:     "basic",
		Username: "elastic",
	}

	err := applyBasicAuthPassword(&cfg, nil)
	if err == nil || !strings.Contains(err.Error(), "秘密管理機制") {
		t.Fatalf("非互動式缺少密碼應提供安全操作指引，實際錯誤: %v", err)
	}
}

func TestValidateBasicAuthRequiresUsernameAndPassword(t *testing.T) {
	cfg := config.Default()
	cfg.Cluster.Hosts = []string{"https://es.example.local:9200"}
	cfg.Cluster.Auth.Type = "basic"

	if err := cfg.Validate(); err == nil {
		t.Fatal("Basic Auth 缺少帳號與密碼時應驗證失敗")
	}

	cfg.Cluster.Auth.Username = "elastic"
	if err := cfg.Validate(); err == nil {
		t.Fatal("Basic Auth 缺少密碼時應驗證失敗")
	}

	cfg.Cluster.Auth.Password = "test-only"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("完整 Basic Auth 設定應通過: %v", err)
	}
}
