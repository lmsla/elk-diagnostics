package collector

import (
	"bufio"
	"net/http"
	"os"
	"strconv"
	"strings"
)

// readServiceStatuses 讀取子服務目錄的 _status.txt。採集器會保留 000、4xx
// 等實際回應，分析端才能區分「未取得」與「端點不適用／權限不足」。
func readServiceStatuses(path string) (map[string]int, error) {
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

func readServiceOptional(path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	return b, err
}

func serviceStatusOrDefault(statuses map[string]int, file string) int {
	if code, ok := statuses[file]; ok {
		return code
	}
	return http.StatusOK
}
