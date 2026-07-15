package main

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"elk-diagnostics/internal/collector"
)

// apis 與 collect-script 都由 collector.Endpoints 產生，不另手寫維護——手寫的清單
// 會跟實作漂移，而一份跟實作對不上的 API 清單交給客戶資安審查，比沒有還糟。

const apisPreamble = `本工具只送出 HTTP GET，不執行任何寫入操作（見 docs/specs 鐵律 1）。
以下為 check 會呼叫的全部端點，皆為叢集／節點層級的中繼資料。

資料範圍：
  - 工具不讀取任何文件（document）內容
  - /_mapping 會回傳各 index 的「欄位名稱」（不含欄位值）
  - /_cat/nodes、/_nodes 會回傳節點名稱、角色與資源使用率
  - 其餘為叢集狀態與統計`

func newAPIsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "apis",
		Short: "印出本工具會呼叫的所有 Elasticsearch API（供客戶導入審查）",
		Long:  "印出 check 會呼叫的全部唯讀端點與用途。內容由程式內的端點清單產生，與實際呼叫一致。",
	}
	format := cmd.Flags().String("output", "text", "輸出格式：text | markdown")

	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		switch *format {
		case "text":
			fmt.Print(apisText())
		case "markdown":
			fmt.Print(apisMarkdown())
		default:
			fmt.Fprintln(os.Stderr, "不支援的格式:", *format, "（支援：text | markdown）")
			os.Exit(10)
		}
		return nil
	}
	return cmd
}

func apisText() string {
	var b strings.Builder
	fmt.Fprintf(&b, "elk-diagnostics %s — Elasticsearch API 呼叫清單\n\n", toolVersion)
	b.WriteString(apisPreamble)
	b.WriteString("\n\n")

	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "方法\t端點\t用途")
	fmt.Fprintln(tw, "----\t----\t----")
	for _, e := range collector.Endpoints {
		fmt.Fprintf(tw, "GET\t%s\t%s\n", e.Path, e.Purpose)
	}
	tw.Flush()

	fmt.Fprintf(&b, "\n共 %d 個固定端點。\n", len(collector.Endpoints))
	b.WriteString(dynamicEndpointNote)
	return b.String()
}

func apisMarkdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# elk-diagnostics %s — Elasticsearch API 呼叫清單\n\n", toolVersion)
	for _, line := range strings.Split(apisPreamble, "\n") {
		b.WriteString(line + "\n")
	}
	b.WriteString("\n| 方法 | 端點 | 用途 |\n|---|---|---|\n")
	for _, e := range collector.Endpoints {
		fmt.Fprintf(&b, "| GET | `%s` | %s |\n", e.Path, e.Purpose)
	}
	fmt.Fprintf(&b, "\n共 %d 個固定端點。\n", len(collector.Endpoints))
	b.WriteString(dynamicEndpointNote)
	return b.String()
}

// dynamicEndpointNote 必須一起印：少了它，這份清單就是不完整的，
// 交給資安審查等同漏報。
const dynamicEndpointNote = `
另有 1 個動態端點：

  GET /<index>/_settings?include_defaults=true&flat_settings=true

  僅在 health_report 點名有受影響 index 時才會查詢，最多 ` + maxIndexAllocationScanStr + ` 個 index。
  叢集健康時完全不會呼叫。
`
