package collector

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestReadLogstashBundle(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "logstash", "default")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, BundleStatusFile), []byte("root.json 200\nhealth_report.json 404\nnode_info.json 200\nnode_stats.json 200\nhot_threads.txt 200\nnode_plugins.json 200\nnode_pipelines.json 200\npipelines_sample_1.json 200\npipelines_sample_2.json 000\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"root.json", "node_info.json", "node_stats.json", "hot_threads.txt", "node_plugins.json", "node_pipelines.json", "pipelines_sample_1.json"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	got, err := ReadLogstashBundle(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "default" {
		t.Fatalf("evidence = %+v, want one default instance", got)
	}
	if got[0].RootCode != http.StatusOK || got[0].HealthReportCode != http.StatusNotFound || got[0].PipelineSample2Code != 0 {
		t.Fatalf("status codes = %+v", got[0])
	}
	if len(got[0].PluginsBody) == 0 || len(got[0].PipelineSample1Body) == 0 || got[0].PipelineSample2Body != nil {
		t.Fatalf("optional bodies = %+v", got[0])
	}
}

func TestReadLogstashBundleAbsentIsOptional(t *testing.T) {
	got, err := ReadLogstashBundle(t.TempDir())
	if err != nil || got != nil {
		t.Fatalf("absent logstash dir = %v, %v; want nil, nil", got, err)
	}
}
