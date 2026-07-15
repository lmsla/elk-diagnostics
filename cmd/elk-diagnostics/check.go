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
	fromFile := cmd.Flags().String("from-file", "", "改讀本機單一 health_report.json（僅 A 類；完整離線分析請用 --from-bundle）")
	fromBundle := cmd.Flags().String("from-bundle", "", "改讀採集腳本產出的 bundle 目錄（完整離線分析，全程不連線）")
	output, outFile := addOutputFlags(cmd)

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		os.Exit(runCheck(cf, *fromFile, *fromBundle, *output, *outFile))
		return nil
	}
	return cmd
}

func runCheck(cf *connFlags, fromFile, fromBundle, output, outFile string) int {
	if fromFile != "" && fromBundle != "" {
		fmt.Fprintln(os.Stderr, "--from-file 與 --from-bundle 不可同時使用")
		return 10
	}
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

	// bundle 模式與連線模式共用底下完整的診斷流程——差別只在 client 的資料來源，
	// 判斷邏輯一份，不會分岔（見 collector.NewFromBundle）。
	var (
		client *collector.Client
		host   string
	)
	if fromBundle != "" {
		c, err := collector.NewFromBundle(fromBundle)
		if err != nil {
			fmt.Fprintln(os.Stderr, "讀取 bundle 失敗:", err)
			return 11
		}
		client, host = c, "(bundle) "+fromBundle
	} else {
		c, h, code := buildClient(cf)
		if code != 0 {
			return code
		}
		client, host = c, h
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
	} else {
		results = append(results, unknownFrom(analyzer.ILM("", nil), e))
	}
	if rows, e := client.ThreadPool(); e == nil {
		results = append(results, analyzer.RejectedRequests(rows), analyzer.TaskBacklog(rows, t))
	} else {
		results = append(results, unknownFrom(analyzer.RejectedRequests(nil), e), unknownFrom(analyzer.TaskBacklog(nil, t), e))
	}
	if nodes, e := client.NodesJVMOldPool(); e == nil {
		results = append(results, analyzer.JVMPressure(nodes, t))
	} else {
		results = append(results, unknownFrom(analyzer.JVMPressure(nil, t), e))
	}
	if brks, e := client.NodesBreakers(); e == nil {
		results = append(results, analyzer.CircuitBreaker(brks))
	} else {
		results = append(results, unknownFrom(analyzer.CircuitBreaker(nil), e))
	}
	var cpus []collector.NodeCPU
	if c, e := client.CatNodesCPU(); e == nil {
		cpus = c
		results = append(results, analyzer.HighCPU(cpus, t), analyzer.HotSpotting(cpus, t))
	} else {
		results = append(results, unknownFrom(analyzer.HighCPU(nil, t), e), unknownFrom(analyzer.HotSpotting(nil, t), e))
	}
	if alloc, e := client.CatAllocation(); e == nil {
		results = append(results, analyzer.Unbalanced(alloc))
	} else {
		results = append(results, unknownFrom(analyzer.Unbalanced(nil), e))
	}
	if counts, e := client.MappingFieldCounts(); e == nil {
		results = append(results, analyzer.MappingExplosion(counts, t))
	} else {
		results = append(results, unknownFrom(analyzer.MappingExplosion(nil, t), e))
	}
	if pipes, e := client.IngestPipelineStats(); e == nil {
		results = append(results, analyzer.IngestPipelineErrors(pipes, t))
	} else {
		results = append(results, unknownFrom(analyzer.IngestPipelineErrors(nil, t), e))
	}
	if idx, e := client.CatIndicesHealth(); e == nil {
		results = append(results, analyzer.DataCorruption(idx))
	} else {
		results = append(results, unknownFrom(analyzer.DataCorruption(nil), e))
	}
	if stopped, e := client.WatcherManuallyStopped(); e == nil {
		results = append(results, analyzer.Watcher(stopped))
	} else {
		results = append(results, unknownFrom(analyzer.Watcher(false), e))
	}
	if ts, e := client.Transforms(); e == nil {
		results = append(results, analyzer.Transforms(ts))
	} else {
		results = append(results, unknownFrom(analyzer.Transforms(nil), e))
	}
	if rcs, e := client.RemoteInfo(); e == nil {
		results = append(results, analyzer.RemoteClusters(rcs))
	} else {
		results = append(results, unknownFrom(analyzer.RemoteClusters(nil), e))
	}
	if deps, e := client.Deprecations(); e == nil {
		results = append(results, analyzer.UpgradeDeprecations(deps))
	} else {
		results = append(results, unknownFrom(analyzer.UpgradeDeprecations(nil), e))
	}
	if mon, e := client.MonitoringCollectionEnabled(); e == nil {
		results = append(results, analyzer.Monitoring(mon))
	} else {
		results = append(results, unknownFrom(analyzer.Monitoring(""), e))
	}
	if sl, e := client.SlowlogEnabledIndices(); e == nil {
		results = append(results, analyzer.SlowLog(sl))
	} else {
		results = append(results, unknownFrom(analyzer.SlowLog(nil), e))
	}

	// --- B 類加深（見 spec-health-report.md；A 類已由 FromHealthReport 產出）---
	if ce, e := client.ClusterAllocationEnable(); e == nil {
		results = append(results, analyzer.DataAllocationBlocked(ce)) // #19
	} else {
		results = append(results, unknownFrom(analyzer.DataAllocationBlocked(""), e))
	}
	enables, unprobed := indexAllocationEnables(client, hr)
	results = append(results, analyzer.IndexAllocationBlocked(enables, unprobed)) // #20
	if exp, found, e := client.AllocationExplain(); e == nil {
		results = append(results, analyzer.AllocationGuidance(exp, found)) // #37
	} else {
		results = append(results, unknownFrom(analyzer.AllocationGuidance(nil, false), e))
	}
	if mig, e := client.IlmMigrating(); e == nil {
		results = append(results, analyzer.IlmTierMigration(mig)) // #25
	} else {
		results = append(results, unknownFrom(analyzer.IlmTierMigration(nil), e))
	}
	totalNodes, e1 := client.ClusterNodeCounts()
	masterEligible, e2 := client.MasterEligibleCount()
	if e1 == nil && e2 == nil {
		results = append(results, analyzer.MasterStabilityContext(totalNodes, masterEligible)) // #30
	} else {
		err := e1
		if err == nil {
			err = e2
		}
		results = append(results, unknownFrom(analyzer.MasterStabilityContext(0, 0), err))
	}
	if tiers, e := client.DataTierNodeCounts(); e == nil {
		results = append(results, analyzer.DataTierAvailability(tiers)) // #24
	} else {
		results = append(results, unknownFrom(analyzer.DataTierAvailability(nil), e))
	}
	if ops, e := client.RestoreProgress(); e == nil {
		results = append(results, analyzer.RestoreStatus(ops)) // #36
	} else {
		results = append(results, unknownFrom(analyzer.RestoreStatus(nil), e))
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

// unknownFrom 將收集失敗轉為報告中可見的 unknown 結果，而非讓該診斷項目靜默消失
// （見 spec-resilience §3）。zero 是呼叫該診斷項目的 analyzer 函式、帶零值/nil 輸入
// 取得的結果，只用來拿正確的 id/title/category/docs，避免另建一份易漂移的對照表——
// 所有 analyzer 函式對零值輸入都是安全的純函式（已由零值/pass-path 測試涵蓋）。
func unknownFrom(zero diagnostic.Result, err error) diagnostic.Result {
	zero.Status = diagnostic.StatusUnknown
	zero.Conclusion = diagnostic.ConclusionNormal
	zero.Summary = "資料抓取失敗，無法判定"
	zero.Findings = []string{err.Error()}
	zero.RootCauses = nil
	zero.Recommendations = nil
	zero.RequiresExtra = false
	zero.ExtraReason = ""
	return zero
}

const (
	maxIndexAllocationScan = 20 // 對照 spec 原定上限，避免受影響 index 過多時逐一查爆量請求
	// maxIndexAllocationScanStr 供 apis 清單引用，與上者同源避免文件與實作對不上。
	maxIndexAllocationScanStr = "20"
)

// indexAllocationEnables 對 shards_availability 診斷點名的受影響 index（上限 20 個），
// 逐一查 index.routing.allocation.enable 生效值，供 #20 使用。
//
// unprobed 回傳「該查但查不到」的 index（權限不足、或 bundle 模式）。**必須回傳而非
// 吞掉**：analyzer 要靠它區分「查過都正常」與「根本沒查到」，否則會把後者講成前者。
func indexAllocationEnables(client *collector.Client, hr *collector.HealthReport) (enables map[string]string, unprobed []string) {
	affected := analyzer.AffectedIndices(hr, "shards_availability")
	if len(affected) > maxIndexAllocationScan {
		affected = affected[:maxIndexAllocationScan]
	}
	enables = make(map[string]string, len(affected))
	for _, idx := range affected {
		v, err := client.IndexAllocationEnable(idx)
		if err != nil {
			unprobed = append(unprobed, idx)
			continue
		}
		enables[idx] = v
	}
	return enables, unprobed
}
