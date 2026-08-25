package analyzer

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"elk-diagnostics/internal/collector"
	"elk-diagnostics/internal/diagnostic"
)

const (
	docLogstashRoot      = "https://www.elastic.co/docs/api/doc/logstash/operation/operation-root"
	docLogstashHealth    = "https://www.elastic.co/docs/api/doc/logstash/operation/operation-healthreport"
	docLogstashPipelines = "https://www.elastic.co/docs/api/doc/logstash/operation/operation-nodestatspipelines"
)

// LogstashStatus 判讀 Logstash root API。Root API 的核心用途是確認服務可連線
// 與版本；若回應本身沒有 status 欄位，HTTP 2xx 仍只代表 API 可用，不推論
// pipeline 工作負載健康。
func LogstashStatus(evidence []collector.LogstashEvidence) diagnostic.Result {
	res := diagnostic.Result{ID: "logstash_status", Title: "Logstash 核心健康", Category: "service", Source: "raw_api", Docs: []string{docLogstashRoot}}
	if len(evidence) == 0 {
		return logstashUnknown(res, "bundle 未包含 Logstash 採集資料", "找不到 logstash/<instance-label>/root.json")
	}

	passCount, warningCount, criticalCount, unknownCount := 0, 0, 0, 0
	for _, ev := range evidence {
		res.Measurements = append(res.Measurements, gauge("logstash.instance.response.status", float64(ev.RootCode), "count", "service", ev.ID, ev.ID, "status"))
		if ev.RootCode < 200 || ev.RootCode >= 300 {
			unknownCount++
			res.Findings = append(res.Findings, fmt.Sprintf("%s：/ HTTP %d，無法判定 Logstash 服務狀態", ev.ID, ev.RootCode))
			continue
		}
		if len(ev.RootBody) == 0 {
			unknownCount++
			res.Findings = append(res.Findings, fmt.Sprintf("%s：缺少 root.json，無法判定 Logstash 版本", ev.ID))
			continue
		}
		doc, err := parseLogstashRoot(ev.RootBody)
		if err != nil {
			unknownCount++
			res.Findings = append(res.Findings, fmt.Sprintf("%s：root.json 格式錯誤：%v", ev.ID, err))
			continue
		}
		level := strings.TrimSpace(doc.Status)
		status := diagnostic.StatusPass
		if level != "" {
			status = logstashHealthStatus(level)
		}
		version := logstashRootVersion(doc.Version)
		if version == "" {
			version = "未提供"
		}
		res.Findings = append(res.Findings, fmt.Sprintf("%s：root API 可用，version=%s%s", ev.ID, version, logstashStatusSuffix(level)))
		switch status {
		case diagnostic.StatusPass:
			passCount++
		case diagnostic.StatusWarning:
			warningCount++
		case diagnostic.StatusCritical:
			criticalCount++
		default:
			unknownCount++
		}
	}
	res.Measurements = append(res.Measurements,
		gauge("logstash.instance.count", float64(len(evidence)), "count", "cluster", "", "Logstash", "instances"),
		gauge("logstash.instance.available.count", float64(passCount), "count", "cluster", "", "Logstash", "available"),
		gauge("logstash.instance.degraded.count", float64(warningCount), "count", "cluster", "", "Logstash", "degraded"),
		gauge("logstash.instance.unavailable.count", float64(criticalCount), "count", "cluster", "", "Logstash", "unavailable"),
		gauge("logstash.instance.unknown.count", float64(unknownCount), "count", "cluster", "", "Logstash", "unknown"),
	)
	switch {
	case criticalCount > 0:
		res.Status, res.Conclusion = diagnostic.StatusCritical, diagnostic.ConclusionConfirmed
		res.Summary = fmt.Sprintf("%d/%d 個 Logstash instance 回報嚴重狀態", criticalCount, len(evidence))
		res.Recommendations = []diagnostic.Recommendation{{Desc: "檢查 Logstash Node API、服務日誌與 pipeline 狀態"}}
	case warningCount > 0:
		res.Status, res.Conclusion = diagnostic.StatusWarning, diagnostic.ConclusionSuspected
		res.Summary = fmt.Sprintf("%d/%d 個 Logstash instance 需要注意", warningCount, len(evidence))
	case unknownCount > 0:
		return logstashUnknownWith(res, "部分 Logstash instance 無法判定", res.Findings)
	default:
		res.Status, res.Conclusion = diagnostic.StatusPass, diagnostic.ConclusionNormal
		res.Summary = fmt.Sprintf("%d 個 Logstash instance API 可用", passCount)
	}
	return res
}

// LogstashHealthReport 分析 GET /_health_report。Logstash 的 health report
// 是版本條件式端點：官方 API 自 Logstash 8.16.0 起提供；舊版的 404 以
// SKIPPED 表示不適用，不把版本差異誤報成故障。
func LogstashHealthReport(evidence []collector.LogstashEvidence) diagnostic.Result {
	res := diagnostic.Result{ID: "logstash_health_report", Title: "Logstash Health Report", Category: "service", Source: "raw_api", Docs: []string{docLogstashHealth}}
	if len(evidence) == 0 {
		return logstashEndpointSkipped(res, "未取得 Logstash health report 採集資料", "health_report.json")
	}

	passCount, warningCount, criticalCount, unknownCount, skippedCount := 0, 0, 0, 0, 0
	for _, ev := range evidence {
		res.Measurements = append(res.Measurements, gauge("logstash.health_report.response.status", float64(ev.HealthReportCode), "count", "service", ev.ID, ev.ID, "status"))
		version := logstashVersion(ev)
		if ev.HealthReportCode == 0 && len(ev.HealthReportBody) == 0 {
			skippedCount++
			res.Findings = append(res.Findings, fmt.Sprintf("%s：bundle 未包含 health_report.json；本項不適用或尚未採集", ev.ID))
			continue
		}
		if ev.HealthReportCode == 404 && version != "" && !logstashHealthSupported(version) {
			skippedCount++
			res.Findings = append(res.Findings, fmt.Sprintf("%s：Logstash %s 未支援 /_health_report（8.16.0+ 才適用）", ev.ID, version))
			continue
		}
		if ev.HealthReportCode < 200 || ev.HealthReportCode >= 300 {
			unknownCount++
			res.Findings = append(res.Findings, fmt.Sprintf("%s：/_health_report HTTP %d，無法判定 Logstash 健康狀態", ev.ID, ev.HealthReportCode))
			continue
		}
		if len(ev.HealthReportBody) == 0 {
			unknownCount++
			res.Findings = append(res.Findings, fmt.Sprintf("%s：缺少 health_report.json，無法判定 Logstash 健康狀態", ev.ID))
			continue
		}
		hr, err := collector.ParseHealthReport(ev.HealthReportBody)
		if err != nil {
			unknownCount++
			res.Findings = append(res.Findings, fmt.Sprintf("%s：health_report.json 格式錯誤：%v", ev.ID, err))
			continue
		}
		status := logstashHealthStatus(hr.Status)
		if status == diagnostic.StatusUnknown {
			unknownCount++
			res.Findings = append(res.Findings, fmt.Sprintf("%s：health report 缺少可判讀 status", ev.ID))
			continue
		}
		res.Measurements = append(res.Measurements,
			gauge("logstash.health_report.indicator.count", float64(len(hr.Indicators)), "count", "service", ev.ID, ev.ID, "indicators"),
			gauge("logstash.health_report.impact.count", float64(logstashImpactCount(hr)), "count", "service", ev.ID, ev.ID, "impacts"),
		)
		res.Findings = append(res.Findings, fmt.Sprintf("%s：status=%s，indicators=%d", ev.ID, strings.ToLower(hr.Status), len(hr.Indicators)))
		switch status {
		case diagnostic.StatusPass:
			passCount++
		case diagnostic.StatusWarning:
			warningCount++
		case diagnostic.StatusCritical:
			criticalCount++
		default:
			unknownCount++
		}
	}
	res.Measurements = append(res.Measurements,
		gauge("logstash.health_report.instance.count", float64(len(evidence)), "count", "cluster", "", "Logstash", "instances"),
		gauge("logstash.health_report.skipped.count", float64(skippedCount), "count", "cluster", "", "Logstash", "unsupported"),
	)
	switch {
	case criticalCount > 0:
		res.Status, res.Conclusion = diagnostic.StatusCritical, diagnostic.ConclusionConfirmed
		res.Summary = fmt.Sprintf("%d/%d 個 Logstash Health Report 回報 red/critical", criticalCount, len(evidence))
	case warningCount > 0:
		res.Status, res.Conclusion = diagnostic.StatusWarning, diagnostic.ConclusionSuspected
		res.Summary = fmt.Sprintf("%d/%d 個 Logstash Health Report 回報 yellow/warning", warningCount, len(evidence))
	case unknownCount > 0:
		return logstashUnknownWith(res, "部分 Logstash Health Report 無法判定", res.Findings)
	case skippedCount == len(evidence):
		return logstashEndpointSkipped(res, "目前 Logstash 版本不適用 /_health_report", "health_report.json")
	default:
		res.Status, res.Conclusion = diagnostic.StatusPass, diagnostic.ConclusionNormal
		res.Summary = fmt.Sprintf("%d 個 Logstash Health Report 正常，%d 個不適用", passCount, skippedCount)
	}
	return res
}

// LogstashPipelineStats 保存 pipeline counters 與 queue snapshot。即使有兩次
// 取樣，本卡仍標記 INFO，因為這不是完整時間序列，也不自行宣告 throughput 故障。
func LogstashPipelineStats(evidence []collector.LogstashEvidence) diagnostic.Result {
	res := diagnostic.Result{ID: "logstash_pipeline_stats", Title: "Logstash Pipeline 觀測", Category: "service", Source: "raw_api", Docs: []string{docLogstashPipelines}}
	if len(evidence) == 0 {
		return logstashEndpointSkipped(res, "未取得 Logstash pipeline stats 採集資料", "pipelines_sample_1.json")
	}

	samples, pipelineCount := 0, 0
	for _, ev := range evidence {
		for _, sample := range []struct {
			code int
			body []byte
			name string
		}{{ev.PipelineSample1Code, ev.PipelineSample1Body, "pipelines_sample_1.json"}, {ev.PipelineSample2Code, ev.PipelineSample2Body, "pipelines_sample_2.json"}} {
			if sample.code < 200 || sample.code >= 300 || len(sample.body) == 0 {
				continue
			}
			pipelines, err := parseLogstashPipelines(sample.body)
			if err != nil {
				res.Findings = append(res.Findings, fmt.Sprintf("%s：%s 格式錯誤：%v", ev.ID, sample.name, err))
				continue
			}
			samples++
			pipelineCount += len(pipelines)
			for name, pipeline := range pipelines {
				appendLogstashPipelineMeasurements(&res, ev.ID, name, pipeline)
			}
		}
	}
	if len(res.Measurements) == 0 {
		return logstashEndpointSkipped(res, "未取得可解析的 Logstash pipeline stats；未納入健康判定", "pipelines_sample_1.json")
	}
	res.Status, res.Conclusion = diagnostic.StatusInfo, diagnostic.ConclusionNormal
	res.Summary = fmt.Sprintf("已取得 %d 次 pipeline stats 取樣、%d 筆 pipeline 觀測（僅供趨勢，不作故障判定）", samples, pipelineCount)
	res.Findings = append(res.Findings, "pipeline counters 與 queue 是採集當下快照；請搭配長時間 Monitoring、Logstash log 或多次報告判讀")
	res.Measurements = append(res.Measurements,
		gauge("logstash.pipeline.sample.count", float64(samples), "count", "cluster", "", "Logstash", "samples"),
		gauge("logstash.pipeline.count", float64(pipelineCount), "count", "cluster", "", "Logstash", "observations"),
	)
	res.RequiresExtra, res.ExtraReason = true, "單次或短間隔 pipeline stats 不能單獨判定 Logstash 故障"
	return res
}

type logstashPipeline struct {
	Events struct {
		In  float64 `json:"in"`
		Out float64 `json:"out"`
	} `json:"events"`
	Queue struct {
		EventsCount float64 `json:"events_count"`
		SizeBytes   float64 `json:"size_in_bytes"`
	} `json:"queue"`
	Flow struct {
		InputThroughput struct {
			Current float64 `json:"current"`
		} `json:"input_throughput"`
		OutputThroughput struct {
			Current float64 `json:"current"`
		} `json:"output_throughput"`
	} `json:"flow"`
}

func parseLogstashPipelines(body []byte) (map[string]logstashPipeline, error) {
	var wrapper struct {
		Pipelines map[string]logstashPipeline `json:"pipelines"`
	}
	if err := json.Unmarshal(body, &wrapper); err != nil {
		return nil, err
	}
	if wrapper.Pipelines == nil {
		return map[string]logstashPipeline{}, nil
	}
	return wrapper.Pipelines, nil
}

func appendLogstashPipelineMeasurements(res *diagnostic.Result, instance, name string, pipeline logstashPipeline) {
	entity := instance + "/" + name
	res.Measurements = append(res.Measurements,
		counter("logstash.pipeline.events.in", pipeline.Events.In, "count", "pipeline", entity, name, "events.in"),
		counter("logstash.pipeline.events.out", pipeline.Events.Out, "count", "pipeline", entity, name, "events.out"),
		gauge("logstash.pipeline.queue.events", pipeline.Queue.EventsCount, "count", "pipeline", entity, name, "queue.events"),
		gauge("logstash.pipeline.queue.size", pipeline.Queue.SizeBytes, "bytes", "pipeline", entity, name, "queue.bytes"),
		gauge("logstash.pipeline.flow.input.current", pipeline.Flow.InputThroughput.Current, "count", "pipeline", entity, name, "flow.input.current"),
		gauge("logstash.pipeline.flow.output.current", pipeline.Flow.OutputThroughput.Current, "count", "pipeline", entity, name, "flow.output.current"),
	)
}

type logstashRoot struct {
	Name    string          `json:"name"`
	Status  string          `json:"status"`
	Version json.RawMessage `json:"version"`
}

func parseLogstashRoot(body []byte) (logstashRoot, error) {
	var doc logstashRoot
	if err := json.Unmarshal(body, &doc); err != nil {
		return logstashRoot{}, err
	}
	return doc, nil
}

func logstashRootVersion(raw json.RawMessage) string {
	var version string
	if json.Unmarshal(raw, &version) == nil {
		return strings.TrimSpace(version)
	}
	var nested struct {
		Number string `json:"number"`
	}
	if json.Unmarshal(raw, &nested) == nil {
		return strings.TrimSpace(nested.Number)
	}
	return ""
}

func parseLogstashVersion(body []byte) string {
	doc, err := parseLogstashRoot(body)
	if err != nil {
		return ""
	}
	return logstashRootVersion(doc.Version)
}

func logstashVersion(ev collector.LogstashEvidence) string { return parseLogstashVersion(ev.RootBody) }

func logstashHealthSupported(version string) bool {
	parts := strings.SplitN(strings.TrimSpace(version), ".", 3)
	if len(parts) < 2 {
		return false
	}
	major, err1 := strconv.Atoi(parts[0])
	minor, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return false
	}
	return major > 8 || (major == 8 && minor >= 16)
}

func logstashImpactCount(hr *collector.HealthReport) int {
	total := 0
	for _, indicator := range hr.Indicators {
		total += len(indicator.Impacts)
	}
	return total
}

func logstashHealthStatus(level string) diagnostic.Status {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "green", "ok", "available", "pass", "healthy":
		return diagnostic.StatusPass
	case "yellow", "warn", "warning", "degraded":
		return diagnostic.StatusWarning
	case "red", "error", "critical", "unavailable", "unhealthy":
		return diagnostic.StatusCritical
	default:
		return diagnostic.StatusUnknown
	}
}

func logstashStatusSuffix(level string) string {
	if strings.TrimSpace(level) == "" {
		return ""
	}
	return fmt.Sprintf("，status=%s", strings.ToLower(strings.TrimSpace(level)))
}

func logstashEndpointSkipped(res diagnostic.Result, summary, file string) diagnostic.Result {
	res.Status, res.Conclusion = diagnostic.StatusSkipped, diagnostic.ConclusionNormal
	res.Summary = summary
	if len(res.Findings) == 0 {
		res.Findings = []string{fmt.Sprintf("未找到或不適用 %s；本次未納入健康判定", file)}
	}
	return res
}

func logstashUnknown(res diagnostic.Result, summary, finding string) diagnostic.Result {
	return logstashUnknownWith(res, summary, []string{finding})
}

func logstashUnknownWith(res diagnostic.Result, summary string, findings []string) diagnostic.Result {
	res.Status, res.Conclusion, res.Summary = diagnostic.StatusUnknown, diagnostic.ConclusionNormal, summary
	res.Findings, res.Recommendations, res.Measurements = findings, nil, nil
	return res
}
