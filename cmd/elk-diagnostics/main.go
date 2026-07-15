// elk-diagnostics MVP：check（全面巡檢）與 diagnose（症狀排查）。
// connect（設定檔/TLS/認證/故障轉移）→ 採集 → 判定 → JSON / 離線 HTML → 結束碼。
package main

import "os"

const toolVersion = "0.0.4-mvp"

func main() {
	// 結束碼契約見 spec-cli §3，由各子指令的 RunE 自行 os.Exit；
	// Execute() 回傳的 error 僅代表 cobra 本身的解析/骨架錯誤（flag 未知等）。
	if err := rootCmd().Execute(); err != nil {
		os.Exit(10)
	}
}
