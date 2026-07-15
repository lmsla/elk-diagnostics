package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"elk-diagnostics/internal/analyzer"
	"elk-diagnostics/internal/collector"
	"elk-diagnostics/internal/diagnostic"
	"elk-diagnostics/rules"
)

func newCheckCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "check",
		Short: "全面巡檢，跑所有適用診斷，產出整份報告",
	}
	cf := addConnFlags(cmd)
	fromFile := cmd.Flags().String("from-file", "", "改讀本機 health_report.json（離線重播，不連線）")
	output, outFile := addOutputFlags(cmd)

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		os.Exit(runCheck(cf, *fromFile, *output, *outFile))
		return nil
	}
	return cmd
}

func runCheck(cf *connFlags, fromFile, output, outFile string) int {
	if fromFile != "" {
		b, err := os.ReadFile(fromFile)
		if err != nil {
			fmt.Fprintln(os.Stderr, "讀檔失敗:", err)
			return 11
		}
		hr, err := collector.ParseHealthReport(b)
		if err != nil {
			fmt.Fprintln(os.Stderr, "解析失敗:", err)
			return 11
		}
		meta := diagnostic.ClusterMeta{Host: "(from-file) " + fromFile, ESVersion: "unknown"}
		return emit(buildReport(meta, analyzer.FromHealthReport(hr), "check"), output, outFile)
	}

	client, host, code := buildClient(cf)
	if code != 0 {
		return code
	}
	t := loadThresholds(cf)
	hr, err := client.HealthReport()
	if err != nil {
		fmt.Fprintln(os.Stderr, "採集失敗:", err)
		return 11
	}

	results := analyzer.FromHealthReport(hr)
	if mode, e := client.IlmStatus(); e == nil {
		errs, _ := client.IlmExplain()
		results = append(results, analyzer.ILM(mode, errs))
	}
	if rows, e := client.ThreadPool(); e == nil {
		results = append(results, analyzer.RejectedRequests(rows), analyzer.TaskBacklog(rows, t))
	}
	if nodes, e := client.NodesJVMOldPool(); e == nil {
		results = append(results, analyzer.JVMPressure(nodes, t))
	}
	if brks, e := client.NodesBreakers(); e == nil {
		results = append(results, analyzer.CircuitBreaker(brks))
	}
	var cpus []collector.NodeCPU
	if c, e := client.CatNodesCPU(); e == nil {
		cpus = c
		results = append(results, analyzer.HighCPU(cpus, t), analyzer.HotSpotting(cpus, t))
	}
	if alloc, e := client.CatAllocation(); e == nil {
		results = append(results, analyzer.Unbalanced(alloc))
	}
	if counts, e := client.MappingFieldCounts(); e == nil {
		results = append(results, analyzer.MappingExplosion(counts, t))
	}
	if pipes, e := client.IngestPipelineStats(); e == nil {
		results = append(results, analyzer.IngestPipelineErrors(pipes, t))
	}
	if idx, e := client.CatIndicesHealth(); e == nil {
		results = append(results, analyzer.DataCorruption(idx))
	}
	if stopped, e := client.WatcherManuallyStopped(); e == nil {
		results = append(results, analyzer.Watcher(stopped))
	}
	if ts, e := client.Transforms(); e == nil {
		results = append(results, analyzer.Transforms(ts))
	}
	if rcs, e := client.RemoteInfo(); e == nil {
		results = append(results, analyzer.RemoteClusters(rcs))
	}
	if deps, e := client.Deprecations(); e == nil {
		results = append(results, analyzer.UpgradeDeprecations(deps))
	}
	if mon, e := client.MonitoringCollectionEnabled(); e == nil {
		results = append(results, analyzer.Monitoring(mon))
	}
	if sl, e := client.SlowlogEnabledIndices(); e == nil {
		results = append(results, analyzer.SlowLog(sl))
	}

	// --- B 類加深（見 spec-health-report.md；A 類已由 FromHealthReport 產出）---
	if ce, e := client.ClusterAllocationEnable(); e == nil {
		results = append(results, analyzer.DataAllocationBlocked(ce)) // #19
	}
	results = append(results, analyzer.IndexAllocationBlocked(indexAllocationEnables(client, hr))) // #20
	if exp, found, e := client.AllocationExplain(); e == nil {
		results = append(results, analyzer.AllocationGuidance(exp, found)) // #37
	}
	if mig, e := client.IlmMigrating(); e == nil {
		results = append(results, analyzer.IlmTierMigration(mig)) // #25
	}
	if totalNodes, e := client.ClusterNodeCounts(); e == nil {
		if masterEligible, e2 := client.MasterEligibleCount(); e2 == nil {
			results = append(results, analyzer.MasterStabilityContext(totalNodes, masterEligible)) // #30
		}
	}
	if tiers, e := client.DataTierNodeCounts(); e == nil {
		results = append(results, analyzer.DataTierAvailability(tiers)) // #24
	}
	if ops, e := client.RestoreProgress(); e == nil {
		results = append(results, analyzer.RestoreStatus(ops)) // #36
	}

	var pools []collector.WritePoolRow
	if p, e := client.WritePool(); e == nil {
		pools = p
	}

	meta := diagnostic.ClusterMeta{Name: client.ClusterName(), Host: host, ESVersion: client.Version()}
	report := buildReport(meta, results, "check")
	report.SuggestedSymptoms = suggestSymptoms(results, cpus, pools, t)
	return emit(report, output, outFile)
}

// suggestSymptoms 依 spec-diagnose-symptoms §3 的反向觸發規則，偵測到特定症狀特徵組合
// 時提示對應 diagnose --symptom；純函式、不觸發額外採集，只重用 check 已收集的資料。
func suggestSymptoms(results []diagnostic.Result, cpus []collector.NodeCPU, pools []collector.WritePoolRow, t rules.Thresholds) []diagnostic.SymptomHint {
	var hints []diagnostic.SymptomHint
	for _, r := range results {
		if r.ID == "ilm_slm_status" && r.Status == diagnostic.StatusCritical {
			hints = append(hints, diagnostic.SymptomHint{
				Symptom: "ilm-stuck",
				Reason:  "偵測到 ILM 已停止或有 index 處於 ERROR step",
			})
			break
		}
	}
	if len(cpus) > 0 && len(pools) > 0 {
		if wb := analyzer.WriteBottleneck(cpus, pools, t); wb.Status == diagnostic.StatusCritical {
			hints = append(hints, diagnostic.SymptomHint{
				Symptom: "write-bottleneck",
				Reason:  "偵測到 CPU 低 + write queue 積壓 + allocated_processors 偏低",
			})
		}
	}
	return hints
}

const maxIndexAllocationScan = 20 // 對照 spec 原定上限，避免受影響 index 過多時逐一查爆量請求

// indexAllocationEnables 對 shards_availability 診斷點名的受影響 index（上限 20 個），
// 逐一查 index.routing.allocation.enable 生效值，供 #20 使用。
func indexAllocationEnables(client *collector.Client, hr *collector.HealthReport) map[string]string {
	affected := analyzer.AffectedIndices(hr, "shards_availability")
	if len(affected) > maxIndexAllocationScan {
		affected = affected[:maxIndexAllocationScan]
	}
	enables := make(map[string]string, len(affected))
	for _, idx := range affected {
		if v, err := client.IndexAllocationEnable(idx); err == nil {
			enables[idx] = v
		}
	}
	return enables
}
