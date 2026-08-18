package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"elk-diagnostics/internal/diagnostic"
)

func TestCheckMetricsOutput(t *testing.T) {
	cmd := newCheckCmd()
	if cmd.Flags().Lookup("metrics-output") == nil {
		t.Fatal("check 缺少 --metrics-output")
	}

	report := diagnostic.NewReport(diagnostic.Meta{
		CollectedAt: "2026-08-15T01:00:00Z",
		Cluster:     diagnostic.ClusterMeta{UUID: "cluster-uuid", Name: "test", ESVersion: "8.14.3"},
	}, []diagnostic.Result{{ID: "cluster_health", Title: "health", Status: diagnostic.StatusPass}})
	dir := t.TempDir()
	reportFile := filepath.Join(dir, "check.json")
	metricsFile := filepath.Join(dir, "metrics.ndjson")
	if code := emitCheck(report, "json", reportFile, false, metricsFile); code != 0 {
		t.Fatalf("emitCheck code=%d", code)
	}
	b, err := os.ReadFile(metricsFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"metric":"diagnostic.status"`) {
		t.Fatalf("趨勢資料缺少診斷狀態: %s", b)
	}
}

func TestUnknownFromDropsUnobservedMeasurements(t *testing.T) {
	result := diagnostic.Result{Measurements: []diagnostic.Measurement{{Metric: "should.not.exist", Value: 0}}}
	if got := unknownFrom(result, errors.New("forbidden"), false); len(got.Measurements) != 0 {
		t.Fatalf("API 失敗時不應輸出零值 measurement: %+v", got.Measurements)
	}
}
