package main

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"elk-diagnostics/internal/collector"
	"elk-diagnostics/internal/config"
	"elk-diagnostics/internal/diagnostic"
	"elk-diagnostics/internal/reporter"
	"elk-diagnostics/rules"
)

func rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "elk-diagnostics",
		Short: "Elasticsearch 唯讀健檢工具",
		Long: `elk-diagnostics 對 Elasticsearch 叢集進行唯讀健檢與症狀排查。

連線資訊優先序：flag > 環境變數(ELK_DIAGNOSTICS_*) > config.yaml > 預設`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(newCheckCmd(), newDiagnoseCmd(), newAPIsCmd(), newCollectScriptCmd(), newVersionCmd())
	return root
}

// ---- 連線設定（check 與 diagnose 共用）----

type connFlags struct {
	cfgPath, username, password, apiKey, caCert, rulesPath *string
	hosts                                                  *[]string
	insecure                                               *bool
	timeout                                                *int
}

// addConnFlags 掛在 check/diagnose 各自的 local flag（非 persistent），故 version 不受影響。
func addConnFlags(cmd *cobra.Command) *connFlags {
	fs := cmd.Flags()
	cf := &connFlags{
		cfgPath:   fs.String("config", "config.yaml", "設定檔路徑"),
		hosts:     fs.StringSlice("host", nil, "ES base URL（可重複，或以逗號分隔，依序故障轉移）"),
		username:  fs.String("username", "", "basic auth 帳號"),
		password:  fs.String("password", "", "basic auth 密碼（已棄用；請改用互動輸入）"),
		apiKey:    fs.String("api-key", "", "API key 認證"),
		caCert:    fs.String("ca-cert", "", "自簽 CA 憑證路徑"),
		insecure:  fs.Bool("insecure", false, "略過 TLS 憑證驗證（不建議）"),
		timeout:   fs.Int("timeout", 0, "單請求逾時秒數（覆寫設定）"),
		rulesPath: fs.String("rules", "", "覆寫 C 類診斷閾值的 YAML（僅覆寫檔案中出現的欄位；A/B 類不受影響）"),
	}
	_ = fs.MarkDeprecated("password", "命令列密碼可能進入 shell history 或程序清單；請省略並由終端安全輸入")
	return cf
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
		Hosts:    *cf.hosts,
		Username: cf.username, Password: cf.password, APIKey: cf.apiKey,
		CACert: cf.caCert, Insecure: cf.insecure, Timeout: cf.timeout,
	})
	if err := ensureBasicAuthPassword(&cfg); err != nil {
		fmt.Fprintln(os.Stderr, "設定錯誤:", err)
		return nil, "", 10
	}
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
		Retries:    cfg.Cluster.Retries,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "連線失敗:", err)
		return nil, "", 11
	}
	return client, cfg.Cluster.Hosts[0], 0
}

// ---- 共用 ----

func addOutputFlags(cmd *cobra.Command) (output, outFile *string, noColor *bool) {
	output = cmd.Flags().String("output", "json", "輸出格式：json | html | text")
	outFile = cmd.Flags().StringP("output-file", "o", "", "輸出檔路徑（省略則印 stdout）")
	noColor = cmd.Flags().Bool("no-color", false, "關閉終端色彩（僅影響 --output text）")
	return output, outFile, noColor
}

func buildReport(meta diagnostic.ClusterMeta, results []diagnostic.Result, mode string) diagnostic.Report {
	return diagnostic.NewReport(diagnostic.Meta{
		ToolVersion: toolVersion,
		Cluster:     meta,
		Mode:        mode,
	}, results)
}

func emit(report diagnostic.Report, format, outFile string, noColor bool) int {
	var (
		out []byte
		err error
	)
	switch format {
	case "json":
		out, err = reporter.JSON(report)
	case "html":
		out, err = reporter.HTML(report)
	case "text":
		out = reporter.Text(report, colorEnabled(outFile, noColor, stdoutIsTTY()))
	default:
		fmt.Fprintf(os.Stderr, "不支援的輸出格式: %q（支援 json | html | text）\n", format)
		return 10
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
		fmt.Print(string(out))
		if format != "text" {
			fmt.Println()
		}
	}
	return report.ExitCode()
}

// colorEnabled 依 診斷報告規格 §5.1：僅在 stdout 為 TTY、無 --no-color、無 NO_COLOR
// 環境變數、且非寫檔時輸出 ANSI。抽成純函式（isTTY 由呼叫端注入）方便測試，不必真造 TTY。
func colorEnabled(outFile string, noColor, isTTY bool) bool {
	if outFile != "" || noColor || os.Getenv("NO_COLOR") != "" {
		return false
	}
	return isTTY
}

func stdoutIsTTY() bool {
	fi, err := os.Stdout.Stat()
	return err == nil && (fi.Mode()&os.ModeCharDevice) != 0
}
