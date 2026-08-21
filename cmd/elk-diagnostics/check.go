package main

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"elk-diagnostics/internal/analyzer"
	"elk-diagnostics/internal/collector"
	"elk-diagnostics/internal/diagnostic"
	"elk-diagnostics/internal/nodecontext"
	"elk-diagnostics/internal/reporter"
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
	metricsOut := cmd.Flags().String("metrics-output", "", "另存可供長期趨勢使用的 NDJSON 觀測值")
	expectedESNodesFile := cmd.Flags().String("expected-es-nodes-file", "", "預期 ES node.name 清單（每行一個；用於找出採集前已離線節點）")

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		os.Exit(runCheckWithMetrics(cf, *fromFile, *fromBundle, *output, *outFile, *noColor, *metricsOut, *expectedESNodesFile))
		return nil
	}
	return cmd
}

func runCheck(cf *connFlags, fromFile, fromBundle, output, outFile string, noColor bool) int {
	return runCheckWithMetrics(cf, fromFile, fromBundle, output, outFile, noColor, "")
}

func runCheckWithMetrics(cf *connFlags, fromFile, fromBundle, output, outFile string, noColor bool, metricsOut string, expectedESNodesFiles ...string) int {
	expectedESNodesFile := ""
	if len(expectedESNodesFiles) > 0 {
		expectedESNodesFile = expectedESNodesFiles[0]
	}
	if fromFile != "" && fromBundle != "" {
		fmt.Fprintln(os.Stderr, "--from-file 與 --from-bundle 不可同時使用")
		return 10
	}
	if fromFile != "" {
		if expectedESNodesFile != "" {
			fmt.Fprintln(os.Stderr, "--expected-es-nodes-file 不適用於僅含 health_report 的 --from-file 模式")
			return 10
		}
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
		return emitCheck(buildReport(meta, analyzer.FromHealthReport(hr), "check"), output, outFile, noColor, metricsOut, clientLogoPath(cf))
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
	var expectedESNodes []string
	if expectedESNodesFile != "" {
		nodes, err := collector.ReadExpectedESNodes(expectedESNodesFile)
		if err != nil {
			fmt.Fprintln(os.Stderr, "讀取預期 ES 節點清單失敗:", err)
			return 10
		}
		expectedESNodes = nodes
	} else if fromBundle != "" {
		expectedESNodes = client.ExpectedESNodes()
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
		// 而非 unknown（見 命令列規格 §4、韌性規格 §1）。
		results = analyzer.HealthReportVersionUnsupported(esVersion)
		versionNotice = fmt.Sprintf("目標叢集 ES %s 低於 8.4，無 _health_report：A 類診斷全數略過，B/C 類結果照跑但未經此版本測試（見各項 version_warning）。", esVersion)
	default:
		var err error
		hr, err = client.HealthReport()
		if err != nil {
			// 執行期抓取失敗（連線逾時/4xx/5xx；bundle 缺檔或錯誤 body）：不中止，A 類
			// 全數以 unknown 浮出，B/C 類照常執行（見 韌性規格 §1，2026-07-16 修訂）。
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
	if policies, e := client.ILMPolicies(); e == nil {
		results = append(results, analyzer.ILMPolicyInventory(policies))
	} else {
		results = append(results, optionalUnknownFrom(analyzer.ILMPolicyInventory(nil), e, isBundle, "cluster read_ilm"))
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
	var nodeSnapshot *nodecontext.Snapshot
	if snapshot, e := client.NodeContextSnapshot(); e == nil {
		nodeSnapshot = snapshot
		if snapshot.StatsCoverage.Complete() {
			snapshot.MissingNodes = nodecontext.MissingExpectedNames(expectedESNodes, snapshot)
		}
		nodeResults := analyzer.NodeContextResults(snapshot, t)
		nodeResults = append(nodeResults, analyzer.ExpectedESNodeCoverage(expectedESNodes, snapshot))
		if isBundle {
			applyBundleNodeContextWording(nodeResults)
		}
		results = append(results, nodeResults...)
	} else {
		nodeResults := analyzer.NodeContextResults(nil, t)
		// 原始端點錯誤只放在 node_api_coverage；衍生檢查以 dependency unknown
		// 指向完整性根因，避免同一個 403／timeout 在報告重複六次。
		nodeResults[0] = unknownf(nodeResults[0], e)
		nodeResults = append(nodeResults, analyzer.ExpectedESNodeCoverage(expectedESNodes, nil))
		if isBundle {
			applyBundleNodeContextWording(nodeResults)
		}
		results = append(results, nodeResults...)
	}
	if brks, e := client.NodesBreakers(); e == nil {
		results = append(results, analyzer.CircuitBreaker(brks))
	} else {
		results = append(results, unknownf(analyzer.CircuitBreaker(nil), e))
	}
	if fielddata, e := client.FielddataStats(); e == nil {
		results = append(results, analyzer.FielddataMemory(fielddata))
	} else {
		results = append(results, optionalUnknownFrom(analyzer.FielddataMemory(nil), e, isBundle, "cluster monitor"))
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
	if streams, e := client.DataStreams(); e == nil {
		results = append(results, analyzer.DataStreamHealth(streams))
	} else {
		results = append(results, optionalUnknownFrom(analyzer.DataStreamHealth(nil), e, isBundle, "index view_index_metadata"))
	}
	if watcher, e := client.WatcherStats(); e == nil {
		results = append(results, analyzer.WatcherHealth(watcher))
	} else {
		results = append(results, unknownf(analyzer.WatcherHealth(collector.WatcherStats{}), e))
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

	// --- B 類加深（見 健康報告規格.md；A 類已由 FromHealthReport 產出）---
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

	// --- ES-GAP-01～06：單次 API 快照即可支持的靜態健檢（靜態健檢規格）---
	if tasks, e := client.PendingClusterTasks(); e == nil {
		results = append(results, analyzer.PendingClusterTasks(tasks, t))
	} else {
		results = append(results, unknownf(analyzer.PendingClusterTasks(nil, t), e))
	}
	if tasks, e := client.RunningTasks(); e == nil {
		results = append(results, analyzer.LongRunningTasks(tasks, t))
	} else {
		results = append(results, unknownf(analyzer.LongRunningTasks(nil, t), e))
	}
	if shards, e := client.ShardSizes(); e == nil {
		results = append(results, analyzer.ShardSizing(shards, t))
	} else {
		results = append(results, unknownf(analyzer.ShardSizing(nil, t), e))
	}
	analysisNow := snapshotReferenceTime(client.CollectedAt(), time.Now().UTC())
	slmPolicies, slmErr := client.SLMPolicies()
	if slmErr == nil {
		results = append(results, analyzer.SnapshotFreshness(slmPolicies, t, analysisNow))
	} else {
		results = append(results, unknownf(analyzer.SnapshotFreshness(nil, t, analysisNow), slmErr))
	}
	repositories, repositoryErr := client.SnapshotRepositories()
	switch {
	case repositoryErr != nil:
		results = append(results, optionalUnknownFrom(analyzer.SnapshotRepositoryReferences(nil, nil), repositoryErr, isBundle, "cluster monitor_snapshot"))
	case slmErr != nil:
		results = append(results, unknownf(analyzer.SnapshotRepositoryReferences(nil, repositories), slmErr))
	default:
		results = append(results, analyzer.SnapshotRepositoryReferences(slmPolicies, repositories))
	}
	if runtimes, e := client.NodeRuntimes(); e == nil {
		results = append(results, analyzer.NodeRuntimeConsistency(runtimes))
	} else {
		results = append(results, unknownf(analyzer.NodeRuntimeConsistency(nil), e))
	}
	if certs, e := client.TLSCertificates(); e == nil {
		results = append(results, analyzer.TLSCertificateExpiry(certs, t, analysisNow))
	} else {
		results = append(results, unknownf(analyzer.TLSCertificateExpiry(nil, t, analysisNow), e))
	}
	if license, e := client.LicenseInfo(); e == nil {
		results = append(results, analyzer.LicenseHealth(license, t, analysisNow))
	} else {
		results = append(results, unknownf(analyzer.LicenseHealth(collector.LicenseInfo{}, t, analysisNow), e))
	}
	if replicas, e := client.IndexReplicas(); e == nil {
		results = append(results, analyzer.ReplicaCoverage(replicas))
	} else {
		results = append(results, unknownf(analyzer.ReplicaCoverage(nil), e))
	}
	awareness, awarenessErr := client.AllocationAwarenessAttributes()
	if awarenessErr != nil {
		results = append(results, unknownf(analyzer.AllocationAwareness(nil, nil), awarenessErr))
	} else if len(awareness) == 0 {
		results = append(results, analyzer.AllocationAwareness(nil, nil))
	} else if topology, e := client.NodeTopology(); e == nil {
		results = append(results, analyzer.AllocationAwareness(awareness, topology))
	} else {
		results = append(results, unknownf(analyzer.AllocationAwareness(awareness, nil), e))
	}

	// --- ES-GAP-07～12：第二批單次快照健檢（擴充健檢規格）---
	if pressure, e := client.IndexingPressure(); e == nil {
		results = append(results, analyzer.IndexingPressure(pressure, t))
	} else {
		results = append(results, unknownf(analyzer.IndexingPressure(nil, t), e))
	}
	if blocks, e := client.IndexBlocks(); e == nil {
		results = append(results, analyzer.IndexReadWriteBlocks(blocks))
	} else {
		results = append(results, unknownf(analyzer.IndexReadWriteBlocks(nil), e))
	}
	if ccr, e := client.CCRStats(); e == nil {
		results = append(results, analyzer.CCRHealth(ccr, t))
	} else if errors.Is(e, collector.ErrFeatureUnavailable) {
		results = append(results, analyzer.CCRFeatureUnavailable())
	} else {
		results = append(results, unknownf(analyzer.CCRHealth(collector.CCRStats{}, t), e))
	}
	jobs, jobsErr := client.MLJobs()
	feeds, feedsErr := client.MLDatafeeds()
	switch {
	case jobsErr == nil && feedsErr == nil:
		results = append(results, analyzer.MLHealth(jobs, feeds))
	case (jobsErr == nil || errors.Is(jobsErr, collector.ErrFeatureUnavailable)) &&
		(feedsErr == nil || errors.Is(feedsErr, collector.ErrFeatureUnavailable)):
		results = append(results, analyzer.MLFeatureUnavailable())
	default:
		err := jobsErr
		if err == nil || errors.Is(err, collector.ErrFeatureUnavailable) {
			err = feedsErr
		}
		results = append(results, unknownf(analyzer.MLHealth(nil, nil), err))
	}
	if shutdowns, e := client.PlannedShutdowns(); e == nil {
		results = append(results, analyzer.PlannedShutdownHealth(shutdowns))
	} else if status, ok := collector.HTTPStatus(e); ok && (status == 403 || status == 404) {
		results = append(results, analyzer.PlannedShutdownUnavailable(status))
	} else {
		results = append(results, unknownf(analyzer.PlannedShutdownHealth(nil), e))
	}
	if exclusions, e := client.VotingExclusions(); e == nil {
		results = append(results, analyzer.VotingExclusionsHealth(exclusions))
	} else {
		results = append(results, unknownf(analyzer.VotingExclusionsHealth(nil), e))
	}

	// Kibana 是選配服務：只有採集包明確要求 kibana，或確實存在 kibana/目錄時，
	// 才加入服務診斷；ES-only bundle 的既有報告不增加空白卡片。
	if isBundle {
		kibana, e := collector.ReadKibanaBundle(fromBundle)
		if e != nil {
			results = append(results,
				kibanaReadFailure(analyzer.KibanaStatus(nil), e),
				analyzer.KibanaStats(nil),
				kibanaReadFailure(analyzer.KibanaTaskManagerHealth(nil), e),
				kibanaReadFailure(analyzer.KibanaAlertingHealth(nil), e),
			)
		} else if len(kibana) > 0 || hasService(client.CollectedServices(), "kibana") {
			results = append(results,
				analyzer.KibanaStatus(kibana),
				analyzer.KibanaStats(kibana),
				analyzer.KibanaTaskManagerHealth(kibana),
				analyzer.KibanaAlertingHealth(kibana),
			)
		}
	}

	// Logstash 是選配服務：只有採集包明確要求 logstash，或確實存在
	// logstash/目錄時才加入服務診斷；ES-only／ES+Kibana 報告不增加空白卡片。
	if isBundle {
		logstash, e := collector.ReadLogstashBundle(fromBundle)
		if e != nil {
			results = append(results,
				logstashReadFailure(analyzer.LogstashStatus(nil), e),
				logstashReadFailure(analyzer.LogstashHealthReport(nil), e),
				logstashReadFailure(analyzer.LogstashPipelineStats(nil), e),
			)
		} else if len(logstash) > 0 || hasService(client.CollectedServices(), "logstash") {
			results = append(results,
				analyzer.LogstashStatus(logstash),
				analyzer.LogstashHealthReport(logstash),
				analyzer.LogstashPipelineStats(logstash),
			)
		}
	}

	var pools []collector.WritePoolRow
	if p, e := client.WritePool(); e == nil {
		pools = p
	}

	if versionNotice != "" {
		applyVersionWarning(results, esVersion)
	}

	meta := diagnostic.ClusterMeta{Name: client.ClusterName(), UUID: client.ClusterUUID(), Host: host, ESVersion: esVersion}
	report := buildReport(meta, results, "check")
	report.Meta.CollectedAt = client.CollectedAt()
	report.Meta.CollectScriptVersion = client.CollectScriptVersion()
	report.Meta.BundleSchemaVersion = client.BundleSchemaVersion()
	report.Meta.CollectedServices = client.CollectedServices()
	report.NodeContext = nodeSnapshot
	report.VersionNotice = versionNotice
	report.SuggestedSymptoms = suggestSymptoms(results, cpus, pools, t)
	return emitCheck(report, output, outFile, noColor, metricsOut, clientLogoPath(cf))
}

func hasService(services []string, want string) bool {
	for _, service := range services {
		if service == want {
			return true
		}
	}
	return false
}

func kibanaReadFailure(zero diagnostic.Result, err error) diagnostic.Result {
	zero.Status = diagnostic.StatusUnknown
	zero.Conclusion = diagnostic.ConclusionNormal
	zero.Summary = "Kibana bundle 資料讀取失敗，無法判定"
	zero.Findings = []string{err.Error()}
	zero.Measurements = nil
	return zero
}

func logstashReadFailure(zero diagnostic.Result, err error) diagnostic.Result {
	zero.Status = diagnostic.StatusUnknown
	zero.Conclusion = diagnostic.ConclusionNormal
	zero.Summary = "Logstash bundle 資料讀取失敗，無法判定"
	zero.Findings = []string{err.Error()}
	zero.Measurements = nil
	return zero
}

func emitCheck(report diagnostic.Report, output, outFile string, noColor bool, metricsOut string, clientLogo ...string) int {
	code := emit(report, output, outFile, noColor, clientLogo...)
	if code >= 10 || metricsOut == "" {
		return code
	}
	b, err := reporter.MetricsNDJSON(report)
	if err != nil {
		fmt.Fprintln(os.Stderr, "趨勢資料輸出失敗:", err)
		return 20
	}
	if err := os.WriteFile(metricsOut, b, 0644); err != nil {
		fmt.Fprintln(os.Stderr, "趨勢資料寫檔失敗:", err)
		return 20
	}
	fmt.Fprintln(os.Stderr, "已寫入", metricsOut)
	return code
}

// snapshotReferenceTime 讓離線報告描述採集當下的狀態，而不是把一份舊 bundle
// 依分析當天重新判成「snapshot/TLS/license 已過期」。舊 bundle 沒有 manifest 或
// collected_at 格式錯誤時才退回分析時間。
func snapshotReferenceTime(collectedAt string, fallback time.Time) time.Time {
	if collectedAt != "" {
		if parsed, err := time.Parse(time.RFC3339, collectedAt); err == nil {
			return parsed.UTC()
		}
	}
	return fallback.UTC()
}

// applyBundleNodeContextWording 保持 韌性規格 §3 的離線措辭契約：Node Context
// 可能只缺部分欄位而非整個 endpoint，因此不會走 unknownFrom，但 summary 仍須明講
// 資料來源是 bundle，避免使用者誤認分析端剛剛連線抓取失敗。
func applyBundleNodeContextWording(results []diagnostic.Result) {
	for i := range results {
		if results[i].Status == diagnostic.StatusUnknown && !strings.Contains(results[i].Summary, "bundle") {
			results[i].Summary = "bundle 資料不完整：" + results[i].Summary
		}
	}
}

// applyVersionWarning 幫 B/C 類結果附上 version_warning（ES < 8.4 未經測試，見 命令列規格 §4）。
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

// supportsHealthReport 判斷版本是否 >= 8.4（_health_report 的最低支援版本，見 命令列規格 §4）。
// 版本字串無法解析時（如 bundle/測試環境的怪異值）保守地當作「可能支援」，讓程式照常嘗試
// 抓取 _health_report——最壞結果是抓取失敗轉 unknown，仍然不是 pass，比誤判成 skipped 安全
// （見 docs/內部/歷史/驗證紀錄.md §1：寧可保守判定失敗，不可靜默假設）。
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

// fetchFailureSummary 依模式決定 unknown 結果的措辭（見 韌性規格 §3，2026-07-16 補）：
// bundle 模式沒有「抓取」這個動作，沿用連線模式的措辭會誤導使用者以為是分析端的網路問題。
func fetchFailureSummary(isBundle bool) string {
	if isBundle {
		return "bundle 缺少該端點資料，無法判定"
	}
	return "資料抓取失敗，無法判定"
}

// suggestSymptoms 依 症狀診斷規格 §3 的反向觸發規則，偵測到特定症狀特徵組合
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
// （見 韌性規格 §3）。zero 是呼叫該診斷項目的 analyzer 函式、帶零值/nil 輸入
// 取得的結果，只用來拿正確的 id/title/category/docs，避免另建一份易漂移的對照表——
// 所有 analyzer 函式對零值輸入都是安全的純函式（已由零值/pass-path 測試涵蓋）。
//
// isBundle 決定 summary 措辭（見 韌性規格 §3，2026-07-16 補）：bundle 模式沒有
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
	zero.Measurements = nil // API 未成功時不得把 analyzer 的零值輸出成真實觀測值。
	return zero
}

// optionalUnknownFrom 為額外權限 API 保留 403／404 的真實語意：權限不足或端點
// 不可用都不是 ES 故障，也不能被包裝成 pass。其餘錯誤沿用共用 unknown 規則。
func optionalUnknownFrom(zero diagnostic.Result, err error, isBundle bool, privilege string) diagnostic.Result {
	res := unknownFrom(zero, err, isBundle)
	status, ok := collector.HTTPStatus(err)
	if !ok {
		return res
	}
	switch status {
	case 403:
		res.Summary = "帳號權限不足，無法判定"
		res.Recommendations = []diagnostic.Recommendation{{Desc: "若需啟用本項檢查，請為健檢角色補上最小權限：" + privilege}}
	case 404:
		res.Summary = "此版本、功能或端點不可用，無法判定"
	}
	return res
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
// 沒有資料時說「目前正常」正是 驗證狀態.md §1.1 記載的假陰性模式（T2 讓
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
