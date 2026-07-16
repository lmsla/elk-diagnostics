package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

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
	output, outFile, noColor := addOutputFlags(cmd)

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		os.Exit(runCheck(cf, *fromFile, *fromBundle, *output, *outFile, *noColor))
		return nil
	}
	return cmd
}

func runCheck(cf *connFlags, fromFile, fromBundle, output, outFile string, noColor bool) int {
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
		return emit(buildReport(meta, analyzer.FromHealthReport(hr), "check"), output, outFile, noColor)
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
	isBundle := fromBundle != ""
	unknownf := func(zero diagnostic.Result, err error) diagnostic.Result {
		return unknownFrom(zero, err, isBundle)
	}

	esVersion := client.Version()
	var (
		hr            *collector.HealthReport
		results       []diagnostic.Result
		versionNotice string
	)
	switch {
	case !supportsHealthReport(esVersion):
		// ES < 8.4：無 _health_report，非「抓取失敗」而是「此環境不適用」，A 類 skipped
		// 而非 unknown（見 spec-cli §4、spec-resilience §1）。
		results = analyzer.HealthReportVersionUnsupported(esVersion)
		versionNotice = fmt.Sprintf("目標叢集 ES %s 低於 8.4，無 _health_report：A 類診斷全數略過，B/C 類結果照跑但未經此版本測試（見各項 version_warning）。", esVersion)
	default:
		var err error
		hr, err = client.HealthReport()
		if err != nil {
			// 執行期抓取失敗（連線逾時/4xx/5xx；bundle 缺檔或錯誤 body）：不中止，A 類
			// 全數以 unknown 浮出，B/C 類照常執行（見 spec-resilience §1，2026-07-16 修訂）。
			results = analyzer.HealthReportFetchFailed(fetchFailureSummary(isBundle), []string{err.Error()})
		} else {
			results = analyzer.FromHealthReport(hr)
		}
	}
	if mode, e := client.IlmStatus(); e == nil {
		errs, _ := client.IlmExplain()
		results = append(results, analyzer.ILM(mode, errs))
	} else {
		results = append(results, unknownf(analyzer.ILM("", nil), e))
	}
	if rows, e := client.ThreadPool(); e == nil {
		results = append(results, analyzer.RejectedRequests(rows), analyzer.TaskBacklog(rows, t))
	} else {
		results = append(results, unknownf(analyzer.RejectedRequests(nil), e), unknownf(analyzer.TaskBacklog(nil, t), e))
	}
	if nodes, e := client.NodesJVMOldPool(); e == nil {
		results = append(results, analyzer.JVMPressure(nodes, t))
	} else {
		results = append(results, unknownf(analyzer.JVMPressure(nil, t), e))
	}
	if brks, e := client.NodesBreakers(); e == nil {
		results = append(results, analyzer.CircuitBreaker(brks))
	} else {
		results = append(results, unknownf(analyzer.CircuitBreaker(nil), e))
	}
	var cpus []collector.NodeCPU
	if c, e := client.CatNodesCPU(); e == nil {
		cpus = c
		results = append(results, analyzer.HighCPU(cpus, t), analyzer.HotSpotting(cpus, t))
	} else {
		results = append(results, unknownf(analyzer.HighCPU(nil, t), e), unknownf(analyzer.HotSpotting(nil, t), e))
	}
	if alloc, e := client.CatAllocation(); e == nil {
		results = append(results, analyzer.Unbalanced(alloc))
	} else {
		results = append(results, unknownf(analyzer.Unbalanced(nil), e))
	}
	if counts, e := client.MappingFieldCounts(); e == nil {
		results = append(results, analyzer.MappingExplosion(counts, t))
	} else {
		results = append(results, unknownf(analyzer.MappingExplosion(nil, t), e))
	}
	if pipes, e := client.IngestPipelineStats(); e == nil {
		results = append(results, analyzer.IngestPipelineErrors(pipes, t))
	} else {
		results = append(results, unknownf(analyzer.IngestPipelineErrors(nil, t), e))
	}
	if idx, e := client.CatIndicesHealth(); e == nil {
		results = append(results, analyzer.DataCorruption(idx))
	} else {
		results = append(results, unknownf(analyzer.DataCorruption(nil), e))
	}
	if stopped, e := client.WatcherManuallyStopped(); e == nil {
		results = append(results, analyzer.Watcher(stopped))
	} else {
		results = append(results, unknownf(analyzer.Watcher(false), e))
	}
	if ts, e := client.Transforms(); e == nil {
		results = append(results, analyzer.Transforms(ts))
	} else {
		results = append(results, unknownf(analyzer.Transforms(nil), e))
	}
	if rcs, e := client.RemoteInfo(); e == nil {
		results = append(results, analyzer.RemoteClusters(rcs))
	} else {
		results = append(results, unknownf(analyzer.RemoteClusters(nil), e))
	}
	if deps, e := client.Deprecations(); e == nil {
		results = append(results, analyzer.UpgradeDeprecations(deps))
	} else {
		results = append(results, unknownf(analyzer.UpgradeDeprecations(nil), e))
	}
	if mon, e := client.MonitoringCollectionEnabled(); e == nil {
		results = append(results, analyzer.Monitoring(mon))
	} else {
		results = append(results, unknownf(analyzer.Monitoring(""), e))
	}
	if sl, e := client.SlowlogEnabledIndices(); e == nil {
		results = append(results, analyzer.SlowLog(sl))
	} else {
		results = append(results, unknownf(analyzer.SlowLog(nil), e))
	}

	// --- B 類加深（見 spec-health-report.md；A 類已由 FromHealthReport 產出）---
	if ce, e := client.ClusterAllocationEnable(); e == nil {
		results = append(results, analyzer.DataAllocationBlocked(ce)) // #19
	} else {
		results = append(results, unknownf(analyzer.DataAllocationBlocked(""), e))
	}
	results = append(results, indexAllocationBlockedResult(client, hr, isBundle)) // #20
	if exp, found, e := client.AllocationExplain(); e == nil {
		results = append(results, analyzer.AllocationGuidance(exp, found)) // #37
	} else {
		results = append(results, unknownf(analyzer.AllocationGuidance(nil, false), e))
	}
	if mig, e := client.IlmMigrating(); e == nil {
		results = append(results, analyzer.IlmTierMigration(mig)) // #25
	} else {
		results = append(results, unknownf(analyzer.IlmTierMigration(nil), e))
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
		results = append(results, unknownf(analyzer.MasterStabilityContext(0, 0), err))
	}
	if tiers, e := client.DataTierNodeCounts(); e == nil {
		results = append(results, analyzer.DataTierAvailability(tiers)) // #24
	} else {
		results = append(results, unknownf(analyzer.DataTierAvailability(nil), e))
	}
	if ops, e := client.RestoreProgress(); e == nil {
		results = append(results, analyzer.RestoreStatus(ops)) // #36
	} else {
		results = append(results, unknownf(analyzer.RestoreStatus(nil), e))
	}

	var pools []collector.WritePoolRow
	if p, e := client.WritePool(); e == nil {
		pools = p
	}

	if versionNotice != "" {
		applyVersionWarning(results, esVersion)
	}

	meta := diagnostic.ClusterMeta{Name: client.ClusterName(), Host: host, ESVersion: esVersion}
	report := buildReport(meta, results, "check")
	report.Meta.CollectedAt = client.CollectedAt()
	report.Meta.CollectScriptVersion = client.CollectScriptVersion()
	report.VersionNotice = versionNotice
	report.SuggestedSymptoms = suggestSymptoms(results, cpus, pools, t)
	return emit(report, output, outFile, noColor)
}

// applyVersionWarning 幫 B/C 類結果附上 version_warning（ES < 8.4 未經測試，見 spec-cli §4）。
// A 類（healthReportIndicators 驅動表）此時已是 skipped，不需要再疊加版本警告，
// 排除清單一律從 analyzer.HealthReportIndicatorIDs 這個單一來源導出，不另建清單。
func applyVersionWarning(results []diagnostic.Result, esVersion string) {
	skip := make(map[string]bool)
	for _, id := range analyzer.HealthReportIndicatorIDs() {
		skip[id] = true
	}
	warning := fmt.Sprintf("ES %s 低於 8.4，本項未經測試（無 _health_report 可佐證）", esVersion)
	for i := range results {
		if skip[results[i].ID] {
			continue
		}
		results[i].VersionWarning = warning
	}
}

// supportsHealthReport 判斷版本是否 >= 8.4（_health_report 的最低支援版本，見 spec-cli §4）。
// 版本字串無法解析時（如 bundle/測試環境的怪異值）保守地當作「可能支援」，讓程式照常嘗試
// 抓取 _health_report——最壞結果是抓取失敗轉 unknown，仍然不是 pass，比誤判成 skipped 安全
// （見 docs/VERIFICATION.md §1：寧可保守判定失敗，不可靜默假設）。
func supportsHealthReport(esVersion string) bool {
	major, minor, ok := parseMajorMinor(esVersion)
	if !ok {
		return true
	}
	if major != 8 {
		return major > 8
	}
	return minor >= 4
}

func parseMajorMinor(version string) (major, minor int, ok bool) {
	parts := strings.SplitN(version, ".", 3)
	if len(parts) < 2 {
		return 0, 0, false
	}
	major, err1 := strconv.Atoi(parts[0])
	minor, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	return major, minor, true
}

// fetchFailureSummary 依模式決定 unknown 結果的措辭（見 spec-resilience §3，2026-07-16 補）：
// bundle 模式沒有「抓取」這個動作，沿用連線模式的措辭會誤導使用者以為是分析端的網路問題。
func fetchFailureSummary(isBundle bool) string {
	if isBundle {
		return "bundle 缺少該端點資料，無法判定"
	}
	return "資料抓取失敗，無法判定"
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
//
// isBundle 決定 summary 措辭（見 spec-resilience §3，2026-07-16 補）：bundle 模式沒有
// 「抓取」這個動作，措辭改為「bundle 缺少該端點資料」；findings 兩種模式都保留完整錯誤
// 訊息（bundle 模式下 err 本身已含缺少的檔名，見 collector.NewFromBundle）。
func unknownFrom(zero diagnostic.Result, err error, isBundle bool) diagnostic.Result {
	zero.Status = diagnostic.StatusUnknown
	zero.Conclusion = diagnostic.ConclusionNormal
	zero.Summary = fetchFailureSummary(isBundle)
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

// indexAllocationBlockedResult 產出 #20 的完整結果：對 shards_availability 診斷點名的
// 受影響 index（上限 20 個）逐一查 index.routing.allocation.enable 生效值後交給 analyzer 判定。
//
// unprobed（該查但查不到的 index，權限不足或 bundle 模式）**必須傳給 analyzer 而非吞掉**：
// analyzer 要靠它區分「查過都正常」與「根本沒查到」，否則會把後者講成前者。
//
// health_report 本身不可用（hr 為 nil，或無 shards_availability indicator）時直接回
// unknown——受影響 index 清單根本拿不到，「無受影響 index 需檢查（shards_availability
// 目前正常）」這句 pass 只有真的讀到 shards_availability 且清單為空時才可能出現；
// 沒有資料時說「目前正常」正是 VERIFICATION.md §1.1 記載的假陰性模式（T2 讓
// health_report 失敗不再整份中止後，此路徑首次真正可達，故須在此明確擋住）。
func indexAllocationBlockedResult(client *collector.Client, hr *collector.HealthReport, isBundle bool) diagnostic.Result {
	affected, ok := analyzer.AffectedIndices(hr, "shards_availability")
	if !ok {
		res := unknownFrom(analyzer.IndexAllocationBlocked(nil, nil),
			fmt.Errorf("health_report 的 shards_availability indicator 不可用，無法取得受影響 index 清單"), isBundle)
		if isBundle {
			res.Summary = "bundle 缺少 health_report 資料，無法取得受影響 index 清單，無法判定"
		} else {
			res.Summary = "health_report 不可用，無法取得受影響 index 清單，無法判定"
		}
		return res
	}
	if len(affected) > maxIndexAllocationScan {
		affected = affected[:maxIndexAllocationScan]
	}
	enables := make(map[string]string, len(affected))
	var unprobed []string
	for _, idx := range affected {
		v, err := client.IndexAllocationEnable(idx)
		if err != nil {
			unprobed = append(unprobed, idx)
			continue
		}
		enables[idx] = v
	}
	return analyzer.IndexAllocationBlocked(enables, unprobed)
}
