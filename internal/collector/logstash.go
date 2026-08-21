package collector

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// LogstashEvidence 是 Logstash 子採集器保存的原始證據。collector 不解讀
// pipeline 或 health report；狀態分級與趨勢語意由 analyzer 負責。
type LogstashEvidence struct {
	ID                  string
	RootCode            int
	RootBody            []byte
	HealthReportCode    int
	HealthReportBody    []byte
	NodeInfoCode        int
	NodeInfoBody        []byte
	NodeStatsCode       int
	NodeStatsBody       []byte
	HotThreadsCode      int
	HotThreadsBody      []byte
	PluginsCode         int
	PluginsBody         []byte
	PipelinesCode       int
	PipelinesBody       []byte
	PipelineSample1Code int
	PipelineSample1Body []byte
	PipelineSample2Code int
	PipelineSample2Body []byte
}

// ReadLogstashBundle 讀取 schema v2 bundle 的 logstash/<instance-id>/ 目錄。
// 沒有 logstash 目錄代表本次未要求 Logstash 採集，不是錯誤。
func ReadLogstashBundle(dir string) ([]LogstashEvidence, error) {
	root := filepath.Join(dir, "logstash")
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var out []LogstashEvidence
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		instanceDir := filepath.Join(root, entry.Name())
		codes, err := readServiceStatuses(filepath.Join(instanceDir, BundleStatusFile))
		if err != nil {
			return nil, fmt.Errorf("讀取 Logstash %s 狀態清單失敗: %w", entry.Name(), err)
		}
		ev := LogstashEvidence{
			ID:                  entry.Name(),
			RootCode:            serviceStatusOrDefault(codes, "root.json"),
			NodeInfoCode:        serviceStatusOrDefault(codes, "node_info.json"),
			NodeStatsCode:       serviceStatusOrDefault(codes, "node_stats.json"),
			HotThreadsCode:      serviceStatusOrDefault(codes, "hot_threads.txt"),
			PluginsCode:         serviceStatusOrDefault(codes, "node_plugins.json"),
			PipelinesCode:       serviceStatusOrDefault(codes, "node_pipelines.json"),
			PipelineSample1Code: serviceStatusOrDefault(codes, "pipelines_sample_1.json"),
			PipelineSample2Code: serviceStatusOrDefault(codes, "pipelines_sample_2.json"),
		}
		// health_report 是版本條件式端點。舊版採集包可能沒有這一行，
		// 以 0 保留「未採集」語意，避免被當成 HTTP 200 空回應。
		if code, ok := codes["health_report.json"]; ok {
			ev.HealthReportCode = code
		}
		read := func(name string, target *[]byte) error {
			b, readErr := readServiceOptional(filepath.Join(instanceDir, name))
			if readErr != nil {
				return fmt.Errorf("讀取 Logstash %s %s 失敗: %w", entry.Name(), name, readErr)
			}
			*target = b
			return nil
		}
		if err := read("root.json", &ev.RootBody); err != nil {
			return nil, err
		}
		if err := read("health_report.json", &ev.HealthReportBody); err != nil {
			return nil, err
		}
		if err := read("node_info.json", &ev.NodeInfoBody); err != nil {
			return nil, err
		}
		if err := read("node_stats.json", &ev.NodeStatsBody); err != nil {
			return nil, err
		}
		if err := read("hot_threads.txt", &ev.HotThreadsBody); err != nil {
			return nil, err
		}
		if err := read("node_plugins.json", &ev.PluginsBody); err != nil {
			return nil, err
		}
		if err := read("node_pipelines.json", &ev.PipelinesBody); err != nil {
			return nil, err
		}
		if err := read("pipelines_sample_1.json", &ev.PipelineSample1Body); err != nil {
			return nil, err
		}
		if err := read("pipelines_sample_2.json", &ev.PipelineSample2Body); err != nil {
			return nil, err
		}
		out = append(out, ev)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}
