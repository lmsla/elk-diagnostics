package analyzer

import (
	"fmt"

	"elk-diagnostics/internal/collector"
	"elk-diagnostics/internal/diagnostic"
)

const docThreadPool = "https://www.elastic.co/docs/reference/elasticsearch/configuration-reference/thread-pool-settings"

// write-bottleneck 因果鏈閾值（暫於程式內；後續規則引擎外部化）。
const (
	wbCPULow     = 50 // CPU 偏低：排除單純算力不足
	wbWriteQueue = 1  // write queue 有積壓
	wbProcLow    = 2  // allocated_processors 偏低（常見於 K8s 未正確設 CPU limit）
)

// WriteBottleneck 對映 spec #16，專屬 diagnose --symptom write-bottleneck。
// 因果鏈：CPU 低 + write queue 積壓 + allocated_processors 偏低 → write pool 過小所致。
// 三者皆成立才判定 confirmed；部分成立為 suspected；無 write 積壓則此鏈無法解釋。
func WriteBottleneck(cpus []collector.NodeCPU, pools []collector.WritePoolRow) diagnostic.Result {
	res := diagnostic.Result{
		ID:       "write_bottleneck",
		Title:    "寫入瓶頸（因果鏈）",
		Category: "performance",
		Source:   "raw_api",
		Docs:     []string{docThreadPool},
	}

	cpuByNode := map[string]collector.NodeCPU{}
	for _, c := range cpus {
		cpuByNode[c.Name] = c
	}

	var confirmed, partial []string
	anyQueue := false
	for _, p := range pools {
		c := cpuByNode[p.Node]
		cpuLow := c.CPU < wbCPULow
		queueBacklog := p.Queue >= wbWriteQueue
		procLow := c.AllocatedProcessors > 0 && c.AllocatedProcessors <= wbProcLow
		if queueBacklog {
			anyQueue = true
		}
		// 因果鏈逐環節描述
		chain := fmt.Sprintf("%s：CPU=%d%%（低?%s）, write queue=%d（積壓?%s）, allocated_processors=%d（偏低?%s）, write pool size=%d",
			p.Node, c.CPU, yn(cpuLow), p.Queue, yn(queueBacklog), c.AllocatedProcessors, yn(procLow), p.Size)
		switch {
		case cpuLow && queueBacklog && procLow:
			confirmed = append(confirmed, chain)
		case queueBacklog:
			partial = append(partial, chain)
		}
	}

	switch {
	case len(confirmed) > 0:
		res.Status, res.Conclusion = diagnostic.StatusCritical, diagnostic.ConclusionConfirmed
		res.Summary = fmt.Sprintf("%d 個節點符合寫入瓶頸因果鏈：CPU 低 + write queue 積壓 + allocated_processors 偏低", len(confirmed))
		res.Findings = append(confirmed, partial...)
		res.RootCauses = []string{"allocated_processors 偏低導致 write thread pool 過小，無法消化寫入；常見於容器未正確設定 CPU limit/request，使 ES 只看到少數核心"}
		res.Recommendations = []diagnostic.Recommendation{
			{Desc: "修正容器 CPU limit/request 使 ES 取得正確核數，或顯式設定 node.processors"},
			{Cmd: "GET _nodes/hot_threads", Desc: "佐證 write 執行緒是否飽和（連動 #9）"},
		}
	case len(partial) > 0:
		res.Status, res.Conclusion = diagnostic.StatusWarning, diagnostic.ConclusionSuspected
		res.Summary = "偵測到 write queue 積壓，但因果鏈未完全成立（非典型 allocated_processors 瓶頸）"
		res.Findings = partial
		res.RequiresExtra, res.ExtraReason = true, "queue 為瞬時值；建議以 --interval 雙取樣確認積壓是否持續，並查 _nodes/hot_threads 與 _nodes/stats/indices 佐證"
		res.Recommendations = []diagnostic.Recommendation{{Desc: "確認積壓是否持續；檢查是否為流量尖峰、昂貴寫入或 hot spotting（#17）"}}
	default:
		res.Status, res.Conclusion = diagnostic.StatusPass, diagnostic.ConclusionNormal
		if anyQueue {
			res.Summary = "write queue 有輕微活動但未達積壓門檻"
		} else {
			res.Summary = "未觀察到 write queue 積壓，此因果鏈無法解釋寫入問題"
		}
	}
	return res
}

func yn(b bool) string {
	if b {
		return "是"
	}
	return "否"
}
