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

const (
	defaultCollectMaxTimeSeconds = 30
	fanOutCollectMaxTimeSeconds  = 10
)

type collectScriptEndpoint struct {
	collector.Endpoint
	MaxTimeSeconds int
}

func newCollectScriptCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "collect-script",
		Short: "產生使用者環境用的採集腳本（純 curl，不需安裝本工具）",
		Long: `印出一份 POSIX sh 採集腳本到 stdout。

腳本在使用者環境執行，只用 curl 送出唯讀 GET 並把原始回應存成 bundle 目錄；
之後在自己的機器上以 check --from-bundle 分析。使用者不需要執行本二進位檔。

  elk-diagnostics collect-script > collect.sh
  # 交付 collect.sh 給使用者或在跳板機執行`,
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

	endpoints := make([]collectScriptEndpoint, 0, len(collector.Endpoints))
	for _, e := range collector.Endpoints {
		endpoints = append(endpoints, collectScriptEndpoint{
			Endpoint:       e,
			MaxTimeSeconds: collectMaxTimeSeconds(e.Path),
		})
	}

	t, err := template.New("collect").Parse(collectTmpl)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	err = t.Execute(&b, struct {
		ToolVersion         string
		Total               int
		SchemaVersion       int
		StatusFile          string
		ManifestFile        string
		ElasticsearchDir    string
		ExpectedESNodesFile string
		Endpoints           []collectScriptEndpoint
	}{
		ToolVersion:         toolVersion,
		Total:               len(collector.Endpoints),
		SchemaVersion:       collector.BundleSchemaVersion,
		StatusFile:          collector.BundleStatusFile,
		ManifestFile:        collector.BundleManifestFile,
		ElasticsearchDir:    collector.BundleElasticsearchDir,
		ExpectedESNodesFile: collector.BundleExpectedESNodesFile,
		Endpoints:           endpoints,
	})
	if err != nil {
		return "", err
	}
	return b.String(), nil
}

// collectMaxTimeSeconds 對節點 fan-out 端點設定較短的外層上限。
//
// 部分 Nodes API 會等待無回應節點直到逾時；若每個端點都沿用 30 秒，單一故障
// 節點會把整份 bundle 的採集時間放大。使用明確白名單，避免把其他 `/_nodes/*`
// 管理端點（例如 planned shutdown）意外納入同一策略。
func collectMaxTimeSeconds(path string) int {
	switch path {
	case collector.EpCatThreadPool,
		collector.EpNodesResourceStats,
		collector.EpNodesResourceInfo,
		collector.EpNodesBreakers,
		collector.EpCatNodes,
		collector.EpNodesIngest,
		collector.EpNodesRoles,
		collector.EpCatThreadPoolWrite,
		collector.EpRunningTasks,
		collector.EpNodesRuntime,
		collector.EpNodesTopology,
		collector.EpNodesIndexingPressure,
		collector.EpNodesFielddata:
		return fanOutCollectMaxTimeSeconds
	default:
		return defaultCollectMaxTimeSeconds
	}
}
