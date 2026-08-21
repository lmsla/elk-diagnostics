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
		Short: "症狀排查，跑該症狀的診斷樹（見 症狀診斷規格）",
	}
	cf := addConnFlags(cmd)
	symptom := cmd.Flags().String("symptom", "", "症狀："+supportedSymptoms)
	output, outFile, noColor := addOutputFlags(cmd)

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if *symptom == "" {
			fmt.Fprintln(os.Stderr, "需提供 --symptom（目前支援："+supportedSymptoms+"）")
			os.Exit(10)
		}
		os.Exit(runDiagnose(cf, *symptom, *output, *outFile, *noColor))
		return nil
	}
	return cmd
}

func runDiagnose(cf *connFlags, symptom, output, outFile string, noColor bool) int {
	client, host, code := buildClient(cf)
	if code != 0 {
		return code
	}
	t := loadThresholds(cf)
	meta := diagnostic.ClusterMeta{Name: client.ClusterName(), UUID: client.ClusterUUID(), Host: host, ESVersion: client.Version()}

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
		} else {
			res = append(res, unknownFrom(analyzer.DataAllocationBlocked(""), e, false))
		}
		res = append(res, indexAllocationBlockedResult(client, hr, false)) // #20
		if r, ok := analyzer.HealthReportIndicator(hr, "disk"); ok {       // #3
			res = append(res, r)
		}
		return emit(buildReport(meta, res, "diagnose:red-cluster"), output, outFile, noColor, clientLogoPath(cf))

	case "write-bottleneck":
		cpus, e1 := client.CatNodesCPU()
		pools, e2 := client.WritePool()
		var res []diagnostic.Result
		if e1 == nil && e2 == nil {
			res = append(res, analyzer.WriteBottleneck(cpus, pools, t))
		} else {
			err := e1
			if err == nil {
				err = e2
			}
			res = append(res, unknownFrom(analyzer.WriteBottleneck(nil, nil, t), err, false))
		}
		return emit(buildReport(meta, res, "diagnose:write-bottleneck"), output, outFile, noColor, clientLogoPath(cf))

	case "high-heap":
		var res []diagnostic.Result
		if nodes, e := client.NodesJVMOldPool(); e == nil {
			res = append(res, analyzer.JVMPressure(nodes, t)) // #7
		} else {
			res = append(res, unknownFrom(analyzer.JVMPressure(nil, t), e, false))
		}
		if brks, e := client.NodesBreakers(); e == nil {
			res = append(res, analyzer.CircuitBreaker(brks)) // #8
		} else {
			res = append(res, unknownFrom(analyzer.CircuitBreaker(nil), e, false))
		}
		if rows, e := client.ThreadPool(); e == nil {
			res = append(res, analyzer.RejectedRequests(rows)) // #6
		} else {
			res = append(res, unknownFrom(analyzer.RejectedRequests(nil), e, false))
		}
		return emit(buildReport(meta, res, "diagnose:high-heap"), output, outFile, noColor, clientLogoPath(cf))

	case "ingest-lag":
		hr, err := client.HealthReport()
		if err != nil {
			fmt.Fprintln(os.Stderr, "採集失敗:", err)
			return 11
		}
		var res []diagnostic.Result
		if pipes, e := client.IngestPipelineStats(); e == nil {
			res = append(res, analyzer.IngestPipelineErrors(pipes, t)) // #13
		} else {
			res = append(res, unknownFrom(analyzer.IngestPipelineErrors(nil, t), e, false))
		}
		if rows, e := client.ThreadPool(); e == nil {
			res = append(res, analyzer.TaskBacklog(rows, t))   // #12
			res = append(res, analyzer.RejectedRequests(rows)) // #6（write）
		} else {
			res = append(res, unknownFrom(analyzer.TaskBacklog(nil, t), e, false), unknownFrom(analyzer.RejectedRequests(nil), e, false))
		}
		if r, ok := analyzer.HealthReportIndicator(hr, "disk"); ok { // #3
			res = append(res, r)
		}
		return emit(buildReport(meta, res, "diagnose:ingest-lag"), output, outFile, noColor, clientLogoPath(cf))

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
		} else {
			res = append(res, unknownFrom(analyzer.ILM("", nil), e, false))
		}
		if r, ok := analyzer.HealthReportIndicator(hr, "disk"); ok { // #3
			res = append(res, r)
		}
		if r, ok := analyzer.HealthReportIndicator(hr, "shards_capacity"); ok { // #10
			res = append(res, r)
		}
		return emit(buildReport(meta, res, "diagnose:ilm-stuck"), output, outFile, noColor, clientLogoPath(cf))

	default:
		fmt.Fprintln(os.Stderr, "不支援的症狀:", symptom, "（目前支援："+supportedSymptoms+"）")
		return 10
	}
}
