package collector

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const minimalVersionJSON = `{"name":"n1","cluster_name":"test","cluster_uuid":"u1","version":{"number":"8.14.3"}}`

func TestNewFromBundleSupportsV1AndV2(t *testing.T) {
	tests := []struct {
		name    string
		schema  int
		dataDir string
	}{
		{name: "v1 flat", schema: 1},
		{name: "v2 service directory", schema: 2, dataDir: BundleElasticsearchDir},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if tc.schema == 2 {
				manifest := `{"bundle_schema_version":2,"collect_script_version":"test","collected_at":"2026-08-17T00:00:00Z"}`
				if err := os.WriteFile(filepath.Join(dir, BundleManifestFile), []byte(manifest), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			dataDir := filepath.Join(dir, tc.dataDir)
			if err := os.MkdirAll(dataDir, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dataDir, FileOf(EpRoot)), []byte(minimalVersionJSON), 0o600); err != nil {
				t.Fatal(err)
			}

			client, err := NewFromBundle(dir)
			if err != nil {
				t.Fatal(err)
			}
			if client.BundleSchemaVersion() != tc.schema || client.ClusterName() != "test" {
				t.Fatalf("schema=%d cluster=%q", client.BundleSchemaVersion(), client.ClusterName())
			}
		})
	}
}

func TestNewFromBundleRejectsUnsupportedSchema(t *testing.T) {
	dir := t.TempDir()
	manifest := `{"bundle_schema_version":99}`
	if err := os.WriteFile(filepath.Join(dir, BundleManifestFile), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewFromBundle(dir); err == nil || !strings.Contains(err.Error(), "不支援") {
		t.Fatalf("err=%v, want unsupported schema", err)
	}
}

func TestReadExpectedESNodes(t *testing.T) {
	file := filepath.Join(t.TempDir(), "nodes.txt")
	if err := os.WriteFile(file, []byte("# baseline\nnode-b\n\nnode-a\nnode-b\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := ReadExpectedESNodes(file)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"node-a", "node-b"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got=%v want=%v", got, want)
	}
}
