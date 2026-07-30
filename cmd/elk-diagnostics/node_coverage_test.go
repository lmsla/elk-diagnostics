package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"elk-diagnostics/internal/collector"
	"elk-diagnostics/internal/diagnostic"
)

func TestCheckPartialNodeCoverageConvergesInLiveAndBundleModes(t *testing.T) {
	stats, info := partialNodeContextPayloads()

	t.Run("live", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body any
			switch r.URL.RequestURI() {
			case collector.EpRoot:
				body = map[string]any{"cluster_name": "partial-cluster", "version": map[string]any{"number": "8.14.3"}}
			case collector.EpNodesResourceStats:
				body = stats
			case collector.EpNodesResourceInfo:
				body = info
			default:
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(body); err != nil {
				t.Errorf("encode response: %v", err)
			}
		}))
		defer srv.Close()

		outFile := filepath.Join(t.TempDir(), "report.json")
		code := runCheck(newTestConnFlags(t, []string{srv.URL}, "", ""), "", "", "json", outFile, false)
		if code == 11 {
			t.Fatal("partial Nodes API 不應使 Live check 中止")
		}
		assertPartialNodeCoverageReport(t, readReport(t, outFile), false)
	})

	t.Run("bundle", func(t *testing.T) {
		bundle := t.TempDir()
		writeJSON(t, filepath.Join(bundle, "version.json"), map[string]any{
			"cluster_name": "partial-cluster",
			"version":      map[string]any{"number": "8.14.3"},
		})
		writeJSON(t, filepath.Join(bundle, "nodes_stats_jvm.json"), stats)
		writeJSON(t, filepath.Join(bundle, "nodes_info_os_process.json"), info)

		report, code := runBundleCheck(t, bundle)
		if code == 11 {
			t.Fatal("partial Nodes API bundle 不應使離線分析中止")
		}
		assertPartialNodeCoverageReport(t, report, true)
	})
}

func assertPartialNodeCoverageReport(t *testing.T, report diagnostic.Report, bundle bool) {
	t.Helper()
	byID := resultsByID(report.Results)
	root, ok := byID["node_api_coverage"]
	if !ok {
		t.Fatal("缺少 node_api_coverage")
	}
	if root.Status != diagnostic.StatusUnknown {
		t.Fatalf("node_api_coverage status=%s, want unknown", root.Status)
	}
	if len(root.Findings) != 2 {
		t.Fatalf("node_api_coverage findings=%v，應只有 Stats／Info 各一行", root.Findings)
	}
	if root.Findings[0] != "Nodes Stats: successful=3/4 failed=1 returned=3" {
		t.Errorf("Stats coverage=%q", root.Findings[0])
	}
	if bundle && !strings.Contains(root.Summary, "bundle") {
		t.Errorf("Bundle root summary 應標示資料來源: %q", root.Summary)
	}

	for _, id := range []string{
		"node_swap_usage",
		"node_file_descriptor_pressure",
		"node_cgroup_memory_pressure",
		"recent_node_restart",
		"node_memory_lock",
	} {
		result, ok := byID[id]
		if !ok {
			t.Errorf("缺少衍生診斷 %s", id)
			continue
		}
		if result.Status != diagnostic.StatusUnknown {
			t.Errorf("%s status=%s, want unknown", id, result.Status)
		}
		if !strings.Contains(result.Summary, "node_api_coverage") {
			t.Errorf("%s summary 未指向完整性根因: %q", id, result.Summary)
		}
		if len(result.Findings) != 0 {
			t.Errorf("%s 重複 coverage findings: %v", id, result.Findings)
		}
	}
}

func readReport(t *testing.T, path string) diagnostic.Report {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var report diagnostic.Report
	if err := json.Unmarshal(body, &report); err != nil {
		t.Fatal(err)
	}
	return report
}

func partialNodeContextPayloads() (map[string]any, map[string]any) {
	statsNodes := make(map[string]any, 3)
	infoNodes := make(map[string]any, 3)
	for i := 1; i <= 3; i++ {
		id := "node-" + string(rune('0'+i))
		name := "n" + string(rune('0'+i))
		statsNodes[id] = map[string]any{
			"name":  name,
			"roles": []string{"data_hot"},
			"os": map[string]any{
				"swap":   map[string]any{"total_in_bytes": 0, "used_in_bytes": 0, "free_in_bytes": 0},
				"cgroup": map[string]any{"memory": map[string]any{"limit_in_bytes": "max"}},
			},
			"process": map[string]any{"open_file_descriptors": 10, "max_file_descriptors": 1000},
			"jvm":     map[string]any{"uptime_in_millis": 7_200_000},
		}
		infoNodes[id] = map[string]any{
			"name":    name,
			"roles":   []string{"data_hot"},
			"os":      map[string]any{"name": "Linux"},
			"process": map[string]any{"mlockall": true},
		}
	}
	return map[string]any{
			"_nodes": map[string]any{"total": 4, "successful": 3, "failed": 1},
			"nodes":  statsNodes,
		}, map[string]any{
			"_nodes": map[string]any{"total": 3, "successful": 3, "failed": 0},
			"nodes":  infoNodes,
		}
}
