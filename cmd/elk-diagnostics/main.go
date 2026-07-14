// elk-diagnostics MVP：check（全面巡檢）與 diagnose（症狀排查）。
// connect（設定檔/TLS/認證/故障轉移）→ 採集 → 判定 → JSON / 離線 HTML → 結束碼。
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"elk-diagnostics/internal/analyzer"
	"elk-diagnostics/internal/collector"
	"elk-diagnostics/internal/config"
	"elk-diagnostics/internal/diagnostic"
	"elk-diagnostics/internal/reporter"
	"elk-diagnostics/rules"
)

const toolVersion = "0.0.4-mvp"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(10)
	}
	switch os.Args[1] {
	case "check":
		os.Exit(runCheck(os.Args[2:]))
	case "diagnose":
		os.Exit(runDiagnose(os.Args[2:]))
	case "version":
		fmt.Println("elk-diagnostics", toolVersion)
		os.Exit(0)
	default:
		usage()
		os.Exit(10)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `用法:
  elk-diagnostics check [連線flag] [--output json|html] [-o file]
  elk-diagnostics check --from-file <health_report.json>      # 離線重播（僅 health_report 類）
  elk-diagnostics diagnose --symptom <name> [連線flag] [--output json|html] [-o file]
  elk-diagnostics version

連線flag：--config / --host / --username --password / --api-key / --ca-cert / --insecure / --timeout / --rules
連線資訊優先序：flag > 環境變數(ELK_DIAGNOSTICS_*) > config.yaml > 預設
支援症狀：write-bottleneck`)
}

// ---- 連線設定（check 與 diagnose 共用）----

type connFlags struct {
	cfgPath, hostsCSV, username, password, apiKey, caCert, rulesPath *string
	insecure                                                         *bool
	timeout                                                          *int
}

func addConnFlags(fs *flag.FlagSet) *connFlags {
	return &connFlags{
		cfgPath:   fs.String("config", "config.yaml", "設定檔路徑"),
		hostsCSV:  fs.String("host", "", "ES base URL（多個以逗號分隔，依序故障轉移）"),
		username:  fs.String("username", "", "basic auth 帳號"),
		password:  fs.String("password", "", "basic auth 密碼"),
		apiKey:    fs.String("api-key", "", "API key 認證"),
		caCert:    fs.String("ca-cert", "", "自簽 CA 憑證路徑"),
		insecure:  fs.Bool("insecure", false, "略過 TLS 憑證驗證（不建議）"),
		timeout:   fs.Int("timeout", 0, "單請求逾時秒數（覆寫設定）"),
		rulesPath: fs.String("rules", "", "覆寫 C 類診斷閾值的 YAML（僅覆寫檔案中出現的欄位；A/B 類不受影響）"),
	}
}

// loadThresholds 讀內建閾值，--rules 有指定時嘗試合併覆寫；失敗僅警告，不中斷執行。
func loadThresholds(cf *connFlags) rules.Thresholds {
	t, warnings := rules.Load(*cf.rulesPath)
	for _, w := range warnings {
		fmt.Fprintln(os.Stderr, "⚠", w)
	}
	return t
}

// buildClient 回傳 (client, primaryHost, errExitCode)；errExitCode 非 0 表失敗。
func buildClient(cf *connFlags) (*collector.Client, string, int) {
	cfg, err := config.Load(*cf.cfgPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "設定錯誤:", err)
		return nil, "", 10
	}
	cfg.ApplyEnv()
	cfg.ApplyFlags(config.FlagOverrides{
		Hosts:    splitCSV(*cf.hostsCSV),
		Username: cf.username, Password: cf.password, APIKey: cf.apiKey,
		CACert: cf.caCert, Insecure: cf.insecure, Timeout: cf.timeout,
	})
	if err := cfg.Validate(); err != nil {
		fmt.Fprintln(os.Stderr, "設定錯誤:", err)
		return nil, "", 10
	}
	if cfg.Cluster.TLS.InsecureSkipVerify {
		fmt.Fprintln(os.Stderr, "⚠ 警告：已啟用 --insecure，未驗證 TLS 憑證，僅限測試環境使用")
	}
	client, err := collector.New(collector.Options{
		Hosts:      cfg.Cluster.Hosts,
		AuthType:   cfg.Cluster.Auth.Type,
		Username:   cfg.Cluster.Auth.Username,
		Password:   cfg.Cluster.Auth.Password,
		APIKey:     cfg.Cluster.Auth.APIKey,
		Token:      cfg.Cluster.Auth.Token,
		CACert:     cfg.Cluster.TLS.CACert,
		ClientCert: cfg.Cluster.TLS.ClientCert,
		ClientKey:  cfg.Cluster.TLS.ClientKey,
		Insecure:   cfg.Cluster.TLS.InsecureSkipVerify,
		Timeout:    time.Duration(cfg.Cluster.TimeoutSeconds) * time.Second,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "連線失敗:", err)
		return nil, "", 11
	}
	return client, cfg.Cluster.Hosts[0], 0
}

// ---- check ----

func runCheck(args []string) int {
	fs := flag.NewFlagSet("check", flag.ExitOnError)
	cf := addConnFlags(fs)
	fromFile := fs.String("from-file", "", "改讀本機 health_report.json（離線重播，不連線）")
	output := fs.String("output", "json", "輸出格式：json | html")
	outFile := addOutFile(fs)
	_ = fs.Parse(args)

	if *fromFile != "" {
		b, err := os.ReadFile(*fromFile)
		if err != nil {
			fmt.Fprintln(os.Stderr, "讀檔失敗:", err)
			return 11
		}
		hr, err := collector.ParseHealthReport(b)
		if err != nil {
			fmt.Fprintln(os.Stderr, "解析失敗:", err)
			return 11
		}
		meta := diagnostic.ClusterMeta{Host: "(from-file) " + *fromFile, ESVersion: "unknown"}
		return emit(buildReport(meta, analyzer.FromHealthReport(hr), "check"), *output, *outFile)
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
	if cpus, e := client.CatNodesCPU(); e == nil {
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

	meta := diagnostic.ClusterMeta{Name: client.ClusterName(), Host: host, ESVersion: client.Version()}
	return emit(buildReport(meta, results, "check"), *output, *outFile)
}

// ---- diagnose ----

func runDiagnose(args []string) int {
	fs := flag.NewFlagSet("diagnose", flag.ExitOnError)
	cf := addConnFlags(fs)
	symptom := fs.String("symptom", "", "症狀：write-bottleneck")
	output := fs.String("output", "json", "輸出格式：json | html")
	outFile := addOutFile(fs)
	_ = fs.Parse(args)

	if *symptom == "" {
		fmt.Fprintln(os.Stderr, "需提供 --symptom（目前支援：write-bottleneck）")
		return 10
	}

	client, host, code := buildClient(cf)
	if code != 0 {
		return code
	}
	t := loadThresholds(cf)
	meta := diagnostic.ClusterMeta{Name: client.ClusterName(), Host: host, ESVersion: client.Version()}

	switch *symptom {
	case "write-bottleneck":
		cpus, e1 := client.CatNodesCPU()
		pools, e2 := client.WritePool()
		if e1 != nil || e2 != nil {
			fmt.Fprintln(os.Stderr, "採集失敗")
			return 11
		}
		res := []diagnostic.Result{analyzer.WriteBottleneck(cpus, pools, t)}
		return emit(buildReport(meta, res, "diagnose:write-bottleneck"), *output, *outFile)
	default:
		fmt.Fprintln(os.Stderr, "不支援的症狀:", *symptom, "（目前支援：write-bottleneck）")
		return 10
	}
}

// ---- 共用 ----

func addOutFile(fs *flag.FlagSet) *string {
	var v string
	fs.StringVar(&v, "o", "", "輸出檔路徑（省略則印 stdout）")
	fs.StringVar(&v, "output-file", "", "輸出檔路徑（省略則印 stdout）")
	return &v
}

func buildReport(meta diagnostic.ClusterMeta, results []diagnostic.Result, mode string) diagnostic.Report {
	return diagnostic.NewReport(diagnostic.Meta{
		ToolVersion: toolVersion,
		Cluster:     meta,
		Mode:        mode,
	}, results)
}

func emit(report diagnostic.Report, format, outFile string) int {
	var (
		out []byte
		err error
	)
	switch format {
	case "html":
		out, err = reporter.HTML(report)
	default:
		out, err = reporter.JSON(report)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "輸出失敗:", err)
		return 20
	}
	if outFile != "" {
		if err := os.WriteFile(outFile, out, 0644); err != nil {
			fmt.Fprintln(os.Stderr, "寫檔失敗:", err)
			return 20
		}
		fmt.Fprintln(os.Stderr, "已寫入", outFile)
	} else {
		fmt.Println(string(out))
	}
	return report.ExitCode()
}

func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
