// Package reporter 將 diagnostic.Report 渲染為輸出格式（見 docs/內部/規格/診斷報告規格.md）。
// 本切片實作 JSON；HTML 後續補。
package reporter

import (
	"encoding/json"

	"elk-diagnostics/internal/diagnostic"
)

// JSON 產出對外穩定契約（診斷報告規格 §4）。
func JSON(r diagnostic.Report) ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}
