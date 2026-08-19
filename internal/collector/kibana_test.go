package collector

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestReadKibanaBundle(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "kibana")
	if err := os.MkdirAll(filepath.Join(root, "b"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a", BundleStatusFile), []byte("status.json 200\nstats.json 403\ntask_manager_health.json 200\nalerting_health.json 200\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a", "status.json"), []byte(`{"name":"kibana","status":{"overall":{"level":"available"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a", "task_manager_health.json"), []byte(`{"status":"OK"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a", "alerting_health.json"), []byte(`{"alerting_framework_health":{"decryption_health":{"status":"ok"},"execution_health":{"status":"ok"},"read_health":{"status":"ok"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "b", BundleStatusFile), []byte("status.json 000\nstats.json 200\ntask_manager_health.json 403\nalerting_health.json 404\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := ReadKibanaBundle(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "a" || got[1].ID != "b" {
		t.Fatalf("instances = %+v, want sorted [a b]", got)
	}
	if got[0].StatusCode != http.StatusOK || got[0].StatsCode != http.StatusForbidden {
		t.Fatalf("a status = %d/%d, want 200/403", got[0].StatusCode, got[0].StatsCode)
	}
	if got[0].TaskManagerCode != http.StatusOK || got[0].AlertingCode != http.StatusOK || len(got[0].TaskManagerBody) == 0 || len(got[0].AlertingBody) == 0 {
		t.Fatalf("a optional evidence = %+v, want 200/200 with bodies", got[0])
	}
	if got[1].StatusCode != 0 || got[1].StatsCode != http.StatusOK || got[1].TaskManagerCode != http.StatusForbidden || got[1].AlertingCode != http.StatusNotFound || len(got[1].StatusBody) != 0 {
		t.Fatalf("b evidence = %+v, want 000/200 with missing status body", got[1])
	}
}

func TestReadKibanaBundleAbsentIsOptional(t *testing.T) {
	got, err := ReadKibanaBundle(t.TempDir())
	if err != nil || got != nil {
		t.Fatalf("absent kibana dir = %v, %v; want nil, nil", got, err)
	}
}
