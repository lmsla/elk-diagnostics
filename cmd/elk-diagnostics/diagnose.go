package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"elk-diagnostics/internal/analyzer"
	"elk-diagnostics/internal/diagnostic"
)

const supportedSymptoms = "red-cluster / write-bottleneck / high-heap / ingest-lag / ilm-stuck"

func newDiagnoseCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "diagnose",
		Short: "症狀排查，跑該症狀的診斷樹（見 spec-diagnose-symptoms）",
	}
	cf := addConnFlags(cmd)
	symptom := cmd.Flags().String("symptom", "", "症狀："+supportedSymptoms)
	output, outFile := addOutputFlags(cmd)

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if *symptom == "" {
			fmt.Fprintln(os.Stderr, "需提供 --symptom（目前支援："+supportedSymptoms+"）")
			os.Exit(10)
		}
		os.Exit(runDiagnose(cf, *symptom, *output, *outFile))
		return nil
	}
	return cmd
}

func runDiagnose(cf *connFlags, symptom, output, outFile string) int {
	client, host, code := buildClient(cf)
	if code != 0 {
		return code
	}
	t := loadThresholds(cf)
	meta := diagnostic.ClusterMeta{Name: client.ClusterName(), Host: host, ESVersion: client.Version()}

	switch symptom {
	case "red-cluster":
		hr, err := client.HealthReport()
		if err != nil {
			fmt.Fprintln(os.Stderr, "採集失敗:", err)
			return 11
		}
		var res []diagnostic.Result
		if r, ok := analyzer.HealthReportIndicator(hr, "cluster_health"); ok { // #1/#2
			res = append(res, r)
		}
		if ce, e := client.ClusterAllocationEnable(); e == nil {
			res = append(res, analyzer.DataAllocationBlocked(ce)) // #19
		}
		res = append(res, analyzer.IndexAllocationBlocked(indexAllocationEnables(client, hr))) // #20
		if r, ok := analyzer.HealthReportIndicator(hr, "disk"); ok {                           // #3
			res = append(res, r)
		}
		return emit(buildReport(meta, res, "diagnose:red-cluster"), output, outFile)

	case "write-bottleneck":
		cpus, e1 := client.CatNodesCPU()
		pools, e2 := client.WritePool()
		if e1 != nil || e2 != nil {
			fmt.Fprintln(os.Stderr, "採集失敗")
			return 11
		}
		res := []diagnostic.Result{analyzer.WriteBottleneck(cpus, pools, t)}
		return emit(buildReport(meta, res, "diagnose:write-bottleneck"), output, outFile)

	case "high-heap":
		var res []diagnostic.Result
		if nodes, e := client.NodesJVMOldPool(); e == nil {
			res = append(res, analyzer.JVMPressure(nodes, t)) // #7
		}
		if brks, e := client.NodesBreakers(); e == nil {
			res = append(res, analyzer.CircuitBreaker(brks)) // #8
		}
		if rows, e := client.ThreadPool(); e == nil {
			res = append(res, analyzer.RejectedRequests(rows)) // #6
		}
		if len(res) == 0 {
			fmt.Fprintln(os.Stderr, "採集失敗")
			return 11
		}
		return emit(buildReport(meta, res, "diagnose:high-heap"), output, outFile)

	case "ingest-lag":
		hr, err := client.HealthReport()
		if err != nil {
			fmt.Fprintln(os.Stderr, "採集失敗:", err)
			return 11
		}
		var res []diagnostic.Result
		if pipes, e := client.IngestPipelineStats(); e == nil {
			res = append(res, analyzer.IngestPipelineErrors(pipes, t)) // #13
		}
		if rows, e := client.ThreadPool(); e == nil {
			res = append(res, analyzer.TaskBacklog(rows, t))   // #12
			res = append(res, analyzer.RejectedRequests(rows)) // #6（write）
		}
		if r, ok := analyzer.HealthReportIndicator(hr, "disk"); ok { // #3
			res = append(res, r)
		}
		return emit(buildReport(meta, res, "diagnose:ingest-lag"), output, outFile)

	case "ilm-stuck":
		hr, err := client.HealthReport()
		if err != nil {
			fmt.Fprintln(os.Stderr, "採集失敗:", err)
			return 11
		}
		var res []diagnostic.Result
		if mode, e := client.IlmStatus(); e == nil {
			errs, _ := client.IlmExplain()
			res = append(res, analyzer.ILM(mode, errs)) // #5
		}
		if r, ok := analyzer.HealthReportIndicator(hr, "disk"); ok { // #3
			res = append(res, r)
		}
		if r, ok := analyzer.HealthReportIndicator(hr, "shards_capacity"); ok { // #10
			res = append(res, r)
		}
		return emit(buildReport(meta, res, "diagnose:ilm-stuck"), output, outFile)

	default:
		fmt.Fprintln(os.Stderr, "不支援的症狀:", symptom, "（目前支援："+supportedSymptoms+"）")
		return 10
	}
}
