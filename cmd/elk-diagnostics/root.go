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
	root.AddCommand(newCheckCmd(), newDiagnoseCmd(), newVersionCmd())
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
	return &connFlags{
		cfgPath:   fs.String("config", "config.yaml", "設定檔路徑"),
		hosts:     fs.StringSlice("host", nil, "ES base URL（可重複，或以逗號分隔，依序故障轉移）"),
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
		Hosts:    *cf.hosts,
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
		Retries:    cfg.Cluster.Retries,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "連線失敗:", err)
		return nil, "", 11
	}
	return client, cfg.Cluster.Hosts[0], 0
}

// ---- 共用 ----

func addOutputFlags(cmd *cobra.Command) (output, outFile *string) {
	output = cmd.Flags().String("output", "json", "輸出格式：json | html")
	outFile = cmd.Flags().StringP("output-file", "o", "", "輸出檔路徑（省略則印 stdout）")
	return output, outFile
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
