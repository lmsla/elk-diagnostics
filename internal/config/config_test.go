package config

import "testing"

func TestApplyEnvUsernameOverridesLowerPriorityAuthType(t *testing.T) {
	t.Setenv("ELK_DIAGNOSTICS_AUTH_TYPE", "")
	t.Setenv("ELK_DIAGNOSTICS_USERNAME", "elastic")

	cfg := Default()
	cfg.Cluster.Auth.Type = "api_key"
	cfg.ApplyEnv()

	if cfg.Cluster.Auth.Type != "basic" {
		t.Fatalf("環境變數 username 應覆寫較低優先序的 auth type，實際為 %q", cfg.Cluster.Auth.Type)
	}
}

func TestApplyEnvExplicitAuthTypeWinsOverUsernameInference(t *testing.T) {
	t.Setenv("ELK_DIAGNOSTICS_AUTH_TYPE", "bearer")
	t.Setenv("ELK_DIAGNOSTICS_USERNAME", "ignored-for-auth-type")

	cfg := Default()
	cfg.ApplyEnv()

	if cfg.Cluster.Auth.Type != "bearer" {
		t.Fatalf("明確 auth type 不應被 username 推論覆寫，實際為 %q", cfg.Cluster.Auth.Type)
	}
}

func TestApplyFlagsUsernameForcesBasicAuth(t *testing.T) {
	username := "elastic"
	cfg := Default()
	cfg.Cluster.Auth.Type = "api_key"
	cfg.ApplyFlags(FlagOverrides{Username: &username})

	if cfg.Cluster.Auth.Type != "basic" {
		t.Fatalf("--username 應依 flag 優先序切換為 basic，實際為 %q", cfg.Cluster.Auth.Type)
	}
}
