package main

import (
	"os"
	"strings"
	"testing"

	"elk-diagnostics/internal/collector"
)

const checkedInInventory = "../../docs/交付/API清單.md"

// TestAPIInventory_CheckedInCopyIsFresh 確保 checked in 的 API 清單與端點表同步。
//
// 這份文件會交給使用者做導入／資安審查。一份與實作對不上的清單比沒有還糟——
// 使用者據以放行的範圍，跟工具實際打的 API 不一致，等於我們給了不實陳述。
func TestAPIInventory_CheckedInCopyIsFresh(t *testing.T) {
	got, err := os.ReadFile(checkedInInventory)
	if err != nil {
		t.Fatalf("讀 API清單.md 失敗（請執行 make generate）: %v", err)
	}
	if string(got) != apisMarkdown() {
		t.Errorf("docs/交付/API清單.md 已過期（端點表已變動）。請執行：\n  make generate")
	}
}

func TestAPIs_ListsEveryEndpoint(t *testing.T) {
	for _, render := range map[string]func() string{"text": apisText, "markdown": apisMarkdown} {
		out := render()
		for _, e := range collector.Endpoints {
			if !strings.Contains(out, e.Path) {
				t.Errorf("清單漏了端點: %s", e.Path)
			}
			if !strings.Contains(out, e.Purpose) {
				t.Errorf("清單漏了用途說明: %s", e.Path)
			}
		}
	}
}

// 動態端點不在 Endpoints 表中，最容易在產清單時被漏掉——漏了就是對使用者漏報。
func TestAPIs_DisclosesDynamicEndpoint(t *testing.T) {
	for name, render := range map[string]func() string{"text": apisText, "markdown": apisMarkdown} {
		out := render()
		if !strings.Contains(out, "/<index>/_settings") {
			t.Errorf("%s 未揭露動態端點，清單不完整", name)
		}
		if !strings.Contains(out, maxIndexAllocationScanStr) {
			t.Errorf("%s 未說明動態端點的查詢上限", name)
		}
	}
}

// 唯讀是對使用者的核心承諾，清單必須明講。
func TestAPIs_StatesReadOnly(t *testing.T) {
	out := apisText()
	if !strings.Contains(out, "只送出 HTTP GET") {
		t.Error("清單未聲明唯讀")
	}
	if !strings.Contains(out, "不讀取任何文件") {
		t.Error("清單未說明不讀取文件內容——這是使用者最在意的一點")
	}
	if strings.Contains(out, "POST") || strings.Contains(out, "PUT") || strings.Contains(out, "DELETE") {
		t.Error("清單出現寫入方法")
	}
}
