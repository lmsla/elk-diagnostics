package reporter

import (
	"fmt"
	"strings"

	"elk-diagnostics/internal/diagnostic"
)

// Text 產出終端可讀摘要（spec-report §5.1）。
//
// 用途是顧問在客戶跳板機／終端上立即判讀，不依賴瀏覽器；交付物仍是 html（給人）
// 與 json（給機器）——text 不是穩定契約，格式可隨版本調整，任何機器處理一律走 json。
//
// color 由呼叫端依 TTY／--no-color／NO_COLOR／是否寫檔判斷後傳入（見 cmd 的
// colorEnabled），本函式本身不判斷環境，維持純函式、易測。
func Text(r diagnostic.Report, color bool) []byte {
	var b strings.Builder

	paint := func(code, s string) string {
		if !color || s == "" {
			return s
		}
		return code + s + ansiReset
	}

	cluster := r.Meta.Cluster.Name
	if cluster == "" {
		cluster = r.Meta.Cluster.Host
	}
	fmt.Fprintf(&b, "elk-diagnostics %s ｜ %s（ES %s）｜ %s ｜ %s\n",
		r.Meta.ToolVersion, cluster, r.Meta.Cluster.ESVersion, r.Meta.Mode, r.Meta.GeneratedAt)

	fmt.Fprintf(&b, "整體狀態：%s %s     %s %d  %s %d  %s %d  %s %d  %s %d\n\n",
		paint(ansiColorFor(r.OverallStatus), textSymbol(r.OverallStatus)), textStatusName(r.OverallStatus),
		paint(ansiGreen, textSymbol(diagnostic.StatusPass)), r.Summary.Pass,
		paint(ansiYellow, textSymbol(diagnostic.StatusWarning)), r.Summary.Warning,
		paint(ansiRed, textSymbol(diagnostic.StatusCritical)), r.Summary.Critical,
		paint(ansiGray, textSymbol(diagnostic.StatusSkipped)), r.Summary.Skipped,
		paint(ansiCyan, textSymbol(diagnostic.StatusUnknown)), r.Summary.Unknown,
	)

	// 非 pass 項目：critical → warning → unknown 排序，逐項兩行。
	var pass, skipped []string
	for _, order := range []diagnostic.Status{diagnostic.StatusCritical, diagnostic.StatusWarning, diagnostic.StatusUnknown} {
		for _, res := range r.Results {
			if res.Status != order {
				continue
			}
			fmt.Fprintf(&b, "  %s %s — %s\n", paint(ansiColorFor(res.Status), textSymbol(res.Status)), res.Title, res.Summary)
			if len(res.Findings) > 0 {
				fmt.Fprintf(&b, "     └ %s\n", res.Findings[0])
			}
		}
	}

	for _, res := range r.Results {
		switch res.Status {
		case diagnostic.StatusPass:
			pass = append(pass, res.Title)
		case diagnostic.StatusSkipped:
			skipped = append(skipped, res.Title)
		}
	}

	if r.Summary.Critical+r.Summary.Warning+r.Summary.Unknown > 0 {
		b.WriteString("\n")
	}

	if len(pass) > 0 {
		fmt.Fprintf(&b, "%s 通過（%d）：%s\n", paint(ansiGreen, textSymbol(diagnostic.StatusPass)), len(pass), strings.Join(pass, "、"))
	}
	if len(skipped) > 0 {
		fmt.Fprintf(&b, "%s 略過（%d）：%s\n", paint(ansiGray, textSymbol(diagnostic.StatusSkipped)), len(skipped), strings.Join(skipped, "、"))
	}

	b.WriteString("\n")
	b.WriteString(paint(ansiGray, r.Disclaimer))
	b.WriteString("\n")

	return []byte(b.String())
}

const (
	ansiReset  = "\x1b[0m"
	ansiGreen  = "\x1b[32m"
	ansiYellow = "\x1b[33m"
	ansiRed    = "\x1b[31m"
	ansiCyan   = "\x1b[36m"
	ansiGray   = "\x1b[90m"
)

// textSymbol 對映 spec-report §5.1：不用 emoji 變體符號（無 VS16），避免部分終端字寬跑版。
func textSymbol(s diagnostic.Status) string {
	switch s {
	case diagnostic.StatusPass:
		return "✅"
	case diagnostic.StatusWarning:
		return "⚠"
	case diagnostic.StatusCritical:
		return "❌"
	case diagnostic.StatusSkipped:
		return "⏭"
	default:
		return "❓"
	}
}

func ansiColorFor(s diagnostic.Status) string {
	switch s {
	case diagnostic.StatusPass:
		return ansiGreen
	case diagnostic.StatusWarning:
		return ansiYellow
	case diagnostic.StatusCritical:
		return ansiRed
	case diagnostic.StatusSkipped:
		return ansiGray
	default:
		return ansiCyan
	}
}

func textStatusName(s diagnostic.Status) string {
	switch s {
	case diagnostic.StatusPass:
		return "正常"
	case diagnostic.StatusWarning:
		return "注意"
	case diagnostic.StatusCritical:
		return "嚴重"
	case diagnostic.StatusSkipped:
		return "略過"
	default:
		return "無法判定"
	}
}
