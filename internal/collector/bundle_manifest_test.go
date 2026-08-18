package collector

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

// TestNewFromBundle_Manifest 涵蓋 採集包規格 §4.2：bundle 含 _manifest.json 時
// collected_at／collect_script_version 可被讀出；不存在時一律空字串，不得用
// mtime 或目錄名猜測。
func TestNewFromBundle_Manifest(t *testing.T) {
	t.Run("含 manifest", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "version.json"), []byte(versionBody), 0644); err != nil {
			t.Fatal(err)
		}
		manifest := `{
  "collect_script_version": "0.0.5",
  "collected_at": "2026-07-16T02:10:00Z",
  "services": ["elasticsearch", "kibana"],
  "host": "https://es.example.local:9200",
  "endpoints_total": 24
}
`
		if err := os.WriteFile(filepath.Join(dir, BundleManifestFile), []byte(manifest), 0644); err != nil {
			t.Fatal(err)
		}
		c, err := NewFromBundle(dir)
		if err != nil {
			t.Fatalf("NewFromBundle() 失敗: %v", err)
		}
		if got := c.CollectedAt(); got != "2026-07-16T02:10:00Z" {
			t.Errorf("CollectedAt() = %q, want 2026-07-16T02:10:00Z", got)
		}
		if got := c.CollectScriptVersion(); got != "0.0.5" {
			t.Errorf("CollectScriptVersion() = %q, want 0.0.5", got)
		}
		if got := c.CollectedServices(); len(got) != 2 || got[0] != "elasticsearch" || got[1] != "kibana" {
			t.Errorf("CollectedServices() = %v, want [elasticsearch kibana]", got)
		}
	})

	t.Run("無 manifest（舊版 bundle／fixture 目錄直接當 bundle）", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "version.json"), []byte(versionBody), 0644); err != nil {
			t.Fatal(err)
		}
		c, err := NewFromBundle(dir)
		if err != nil {
			t.Fatalf("NewFromBundle() 失敗: %v", err)
		}
		if got := c.CollectedAt(); got != "" {
			t.Errorf("CollectedAt() = %q, want 空字串（不得用 mtime／目錄名猜測）", got)
		}
		if got := c.CollectScriptVersion(); got != "" {
			t.Errorf("CollectScriptVersion() = %q, want 空字串", got)
		}
	})

	t.Run("manifest 內容損毀不致命，回空字串", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "version.json"), []byte(versionBody), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, BundleManifestFile), []byte("{not json"), 0644); err != nil {
			t.Fatal(err)
		}
		c, err := NewFromBundle(dir)
		if err != nil {
			t.Fatalf("NewFromBundle() 不應因 manifest 損毀而失敗: %v", err)
		}
		if got := c.CollectedAt(); got != "" {
			t.Errorf("CollectedAt() = %q, want 空字串", got)
		}
	})
}

// TestConnectedClient_HasNoManifestFields 連線模式（非 bundle）沒有採集時間的概念，
// 兩個 getter 應維持空字串，不誤植連線時間。
func TestConnectedClient_HasNoManifestFields(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(versionBody))
	})
	if got := c.CollectedAt(); got != "" {
		t.Errorf("連線模式 CollectedAt() = %q, want 空字串", got)
	}
	if got := c.CollectScriptVersion(); got != "" {
		t.Errorf("連線模式 CollectScriptVersion() = %q, want 空字串", got)
	}
}
