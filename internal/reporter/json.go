// Package reporter 將 diagnostic.Report 渲染為輸出格式（見 docs/specs/spec-report.md）。
// 本切片實作 JSON；HTML 後續補。
package reporter

import (
	"encoding/json"

	"elk-doctor/internal/diagnostic"
)

// JSON 產出對外穩定契約（spec-report §4）。
func JSON(r diagnostic.Report) ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}
