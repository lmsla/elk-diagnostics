package main

import (
	_ "embed"
	"fmt"
	"os"
	"strings"
	"text/template"

	"github.com/spf13/cobra"

	"elk-diagnostics/internal/collector"
)

//go:embed collect.sh.tmpl
var collectTmpl string

func newCollectScriptCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "collect-script",
		Short: "產生客戶環境用的採集腳本（純 curl，不需安裝本工具）",
		Long: `印出一份 POSIX sh 採集腳本到 stdout。

腳本在客戶環境執行，只用 curl 送出唯讀 GET 並把原始回應存成 bundle 目錄；
之後在自己的機器上以 check --from-bundle 分析。客戶不需要執行本二進位檔。

  elk-diagnostics collect-script > collect.sh
  # 交付 collect.sh 給客戶或在跳板機執行`,
	}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		s, err := renderCollectScript()
		if err != nil {
			fmt.Fprintln(os.Stderr, "產生採集腳本失敗:", err)
			os.Exit(20)
		}
		fmt.Print(s)
		return nil
	}
	return cmd
}

func renderCollectScript() (string, error) {
	// 端點會被單引號包進 sh，含單引號的 path 會直接破壞腳本結構（且可能構成注入）。
	// 目前沒有這種端點，但這裡擋住，避免日後新增時無聲產出壞掉的腳本。
	for _, e := range collector.Endpoints {
		if strings.ContainsAny(e.Path, "'\n") || strings.ContainsAny(e.File, "'\n/") {
			return "", fmt.Errorf("端點含無法安全嵌入 sh 的字元: %q → %q", e.Path, e.File)
		}
	}

	t, err := template.New("collect").Parse(collectTmpl)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	err = t.Execute(&b, struct {
		ToolVersion string
		Total       int
		StatusFile  string
		Endpoints   []collector.Endpoint
	}{
		ToolVersion: toolVersion,
		Total:       len(collector.Endpoints),
		StatusFile:  collector.BundleStatusFile,
		Endpoints:   collector.Endpoints,
	})
	if err != nil {
		return "", err
	}
	return b.String(), nil
}
