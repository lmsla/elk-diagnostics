package main

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"elk-diagnostics/internal/collector"
)

// Elasticsearch 固定端點與 collect-script 都由 collector.Endpoints 產生，不另手寫維護；
// 選配服務端點則在下方明確列出，避免把 Kibana／Logstash 的選配範圍誤算進 ES 固定清單。
// 一份跟實作對不上的 API 清單交給使用者端資安審查，比沒有還糟。

const apisPreamble = `本工具只送出 HTTP GET，不執行任何寫入操作。
以下為 check 會呼叫的全部端點，皆為叢集／節點層級的中繼資料。

操作路線：
  - Route A（主要）：使用者端只執行 collect.sh，將採集包交由獲准的分析端處理。
  - Route B：獲准的分析機執行 elk-diagnostics，直接連線 ES 產生報告。
  兩條路線使用相同的 Elasticsearch 診斷端點；Kibana／Logstash 只有 collect.sh 指定對應服務時才呼叫。

資料範圍：
  - 工具不讀取任何文件（document）內容
  - /_mapping 會回傳各 index 的「欄位名稱」（不含欄位值）
  - /_cat/nodes、/_nodes 會回傳節點名稱、角色與資源使用率
  - 其餘為叢集狀態與統計`

func newAPIsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "apis",
		Short: "印出本工具會呼叫的所有 ELK API（供使用者導入審查）",
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
	fmt.Fprintf(&b, "elk-diagnostics %s — ELK API 呼叫清單\n\n", toolVersion)
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
	b.WriteString(serviceEndpointNote)
	return b.String()
}

func apisMarkdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# elk-diagnostics %s — ELK API 呼叫清單\n\n", toolVersion)
	for _, line := range strings.Split(apisPreamble, "\n") {
		b.WriteString(line + "\n")
	}
	b.WriteString("\n| 方法 | 端點 | 用途 |\n|---|---|---|\n")
	for _, e := range collector.Endpoints {
		fmt.Fprintf(&b, "| GET | `%s` | %s |\n", e.Path, e.Purpose)
	}
	fmt.Fprintf(&b, "\n共 %d 個固定端點。\n", len(collector.Endpoints))
	b.WriteString(dynamicEndpointNote)
	b.WriteString(serviceEndpointMarkdown)
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

const serviceEndpointNote = `
選配服務端點（不計入上述 Elasticsearch 固定端點；只有 collect.sh 指定對應服務時才呼叫）：

  Kibana  GET /api/status                       核心服務與 plugin 健康狀態
  Kibana  GET /api/stats?extended=true&legacy=true  runtime 觀測值（僅供趨勢）
  Kibana  GET /api/task_manager/_health           Task Manager 健康狀態
  Kibana  GET /api/alerting/_health               Alerting framework 健康狀態

  Logstash GET /                         服務可用性與版本
  Logstash GET /_health_report           Logstash 健康指標（8.16.0+；舊版可能 404）
  Logstash GET /_node                    Node 設定與節點資訊
  Logstash GET /_node/stats              Node runtime／pipeline 統計
  Logstash GET /_node/hot_threads?human=true  Hot threads 原始快照
  Logstash GET /_node/stats/pipelines    Pipeline counters／queue 統計（可雙取樣）
  Logstash GET /_node/plugins            已載入 plugin 清單
  Logstash GET /_node/pipelines          Pipeline 設定摘要
`

const serviceEndpointMarkdown = `
## 選配服務 API

以下端點不計入 Elasticsearch 固定端點；只有 collect.sh --services ... 指定對應服務時才呼叫。

| 服務 | 方法 | 端點 | 用途 |
|---|---|---|---|
| Kibana | GET | /api/status | 核心服務與 plugin 健康狀態 |
| Kibana | GET | /api/stats?extended=true&legacy=true | runtime 觀測值（僅供趨勢） |
| Kibana | GET | /api/task_manager/_health | Task Manager 健康狀態 |
| Kibana | GET | /api/alerting/_health | Alerting framework 健康狀態 |
| Logstash | GET | / | 服務可用性與版本 |
| Logstash | GET | /_health_report | Health indicators（Logstash 8.16.0+；舊版可能 404） |
| Logstash | GET | /_node | Node 設定與節點資訊 |
| Logstash | GET | /_node/stats | Node runtime／pipeline 統計 |
| Logstash | GET | /_node/hot_threads?human=true | Hot threads 原始快照 |
| Logstash | GET | /_node/stats/pipelines | Pipeline counters／queue 統計（可雙取樣） |
| Logstash | GET | /_node/plugins | 已載入 plugin 清單 |
| Logstash | GET | /_node/pipelines | Pipeline 設定摘要 |
`
