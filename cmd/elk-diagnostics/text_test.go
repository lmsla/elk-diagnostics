package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestColorEnabled 涵蓋 診斷報告規格 §5.1 的色彩規則：僅 TTY 且無 --no-color／NO_COLOR
// 且非寫檔時才輸出 ANSI。isTTY 由呼叫端注入（見 root.go stdoutIsTTY），故這裡不需要
// 真造一個 TTY 就能測滿四種關閉條件。
func TestColorEnabled(t *testing.T) {
	cases := []struct {
		name       string
		outFile    string
		noColor    bool
		envNoColor string
		isTTY      bool
		want       bool
	}{
		{"TTY 且無任何關閉條件", "", false, "", true, true},
		{"非 TTY", "", false, "", false, false},
		{"--no-color 關閉", "", true, "", true, false},
		{"NO_COLOR 環境變數關閉", "", false, "1", true, false},
		{"寫檔一律純文字", "out.txt", false, "", true, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.envNoColor != "" {
				t.Setenv("NO_COLOR", c.envNoColor)
			} else {
				os.Unsetenv("NO_COLOR")
			}
			got := colorEnabled(c.outFile, c.noColor, c.isTTY)
			if got != c.want {
				t.Errorf("colorEnabled(%q, %v, %v) = %v, want %v", c.outFile, c.noColor, c.isTTY, got, c.want)
			}
		})
	}
}

// TestRunCheck_TextOutput_NoANSIToFile 對映驗收條件：`--output text -o out.txt` 檔案內無 ANSI。
func TestRunCheck_TextOutput_NoANSIToFile(t *testing.T) {
	bundle := fixtureDir("es9-unhealthy")
	outFile := filepath.Join(t.TempDir(), "out.txt")

	code := runCheck(newTestConnFlags(t, nil, "", ""), "", bundle, "text", outFile, false)
	t.Logf("exit_code=%d", code)

	b, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("讀輸出失敗: %v", err)
	}
	out := string(b)
	if strings.ContainsRune(out, '\x1b') {
		t.Errorf("-o 寫檔應一律純文字，got:\n%s", out)
	}
	if !strings.Contains(out, "整體狀態") {
		t.Errorf("輸出應含整體狀態列，got:\n%s", out)
	}
}

// TestRunCheck_InvalidOutputFormat 對映驗收條件：不合法的 --output foo 有清楚錯誤。
func TestRunCheck_InvalidOutputFormat(t *testing.T) {
	bundle := fixtureDir("es9-unhealthy")
	outFile := filepath.Join(t.TempDir(), "out.foo")

	code := runCheck(newTestConnFlags(t, nil, "", ""), "", bundle, "foo", outFile, false)
	if code != 10 {
		t.Errorf("exit code = %d, want 10（不支援的輸出格式屬設定錯誤）", code)
	}
	if _, err := os.Stat(outFile); err == nil {
		t.Error("不合法格式不應寫出檔案")
	}
}
