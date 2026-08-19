package collector

import (
	"bufio"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// KibanaEvidence 是 Kibana 子採集器寫入的原始證據。collector 只負責讀檔，
// 健康分級與數值解讀留在 analyzer，讓採集與判讀維持分離。
type KibanaEvidence struct {
	ID              string
	StatusCode      int
	StatusBody      []byte
	StatsCode       int
	StatsBody       []byte
	TaskManagerCode int
	TaskManagerBody []byte
	AlertingCode    int
	AlertingBody    []byte
}

// ReadKibanaBundle 讀取 schema v2 bundle 的 kibana/<instance-id>/ 目錄。
// 沒有 kibana 目錄代表本次未要求 Kibana 採集，不是錯誤；目錄存在但資料缺失
// 則保留 evidence，交由 analyzer 產生 unknown/skipped，而不靜默當成正常。
func ReadKibanaBundle(dir string) ([]KibanaEvidence, error) {
	root := filepath.Join(dir, "kibana")
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var out []KibanaEvidence
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		instanceDir := filepath.Join(root, entry.Name())
		codes, err := readKibanaStatuses(filepath.Join(instanceDir, BundleStatusFile))
		if err != nil {
			return nil, fmt.Errorf("讀取 Kibana %s 狀態清單失敗: %w", entry.Name(), err)
		}
		ev := KibanaEvidence{
			ID:              entry.Name(),
			StatusCode:      http.StatusOK,
			StatsCode:       http.StatusOK,
			TaskManagerCode: http.StatusOK,
			AlertingCode:    http.StatusOK,
		}
		ev.StatusCode = statusOrDefault(codes, "status.json")
		ev.StatsCode = statusOrDefault(codes, "stats.json")
		ev.TaskManagerCode = statusOrDefault(codes, "task_manager_health.json")
		ev.AlertingCode = statusOrDefault(codes, "alerting_health.json")
		if ev.StatusBody, err = readOptional(filepath.Join(instanceDir, "status.json")); err != nil {
			return nil, fmt.Errorf("讀取 Kibana %s status.json 失敗: %w", entry.Name(), err)
		}
		if ev.StatsBody, err = readOptional(filepath.Join(instanceDir, "stats.json")); err != nil {
			return nil, fmt.Errorf("讀取 Kibana %s stats.json 失敗: %w", entry.Name(), err)
		}
		if ev.TaskManagerBody, err = readOptional(filepath.Join(instanceDir, "task_manager_health.json")); err != nil {
			return nil, fmt.Errorf("讀取 Kibana %s task_manager_health.json 失敗: %w", entry.Name(), err)
		}
		if ev.AlertingBody, err = readOptional(filepath.Join(instanceDir, "alerting_health.json")); err != nil {
			return nil, fmt.Errorf("讀取 Kibana %s alerting_health.json 失敗: %w", entry.Name(), err)
		}
		out = append(out, ev)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func readOptional(path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	return b, err
}

func readKibanaStatuses(path string) (map[string]int, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]int{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := map[string]int{}
	scanner := bufio.NewScanner(strings.NewReader(string(b)))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 2 {
			continue
		}
		code, err := strconv.Atoi(fields[1])
		if err != nil {
			continue
		}
		out[fields[0]] = code
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func statusOrDefault(statuses map[string]int, file string) int {
	if code, ok := statuses[file]; ok {
		return code
	}
	return http.StatusOK
}
