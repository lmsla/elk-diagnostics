package reporter

import (
	"strings"
	"testing"

	"elk-diagnostics/internal/nodecontext"
)

func TestHTML_NodeContext(t *testing.T) {
	r := sampleReport()
	used, total, fdOpen, fdMax := int64(512), int64(1024), int64(8), int64(100)
	cpu, osMemoryUsed, heapUsed := 25, 93, 54
	r.NodeContext = &nodecontext.Snapshot{
		StatsCoverage: nodecontext.Coverage{Available: true, Total: 1, Successful: 1, Returned: 1},
		InfoCoverage:  nodecontext.Coverage{Available: true, Total: 1, Successful: 1, Returned: 1},
		Nodes: []nodecontext.Node{{
			Name:  "node-a",
			Roles: []string{"data_hot"},
			OS: nodecontext.OS{
				CPUPercent: &cpu,
				Memory:     nodecontext.Memory{UsedBytes: &used, TotalBytes: &total, UsedPct: &osMemoryUsed},
			},
			Process:    nodecontext.Process{OpenFileDescriptors: &fdOpen, MaxFileDescriptors: &fdMax},
			Filesystem: nodecontext.Filesystem{DataPaths: []nodecontext.DataPath{{Path: "/srv/es-data"}}},
			JVM:        nodecontext.JVM{HeapUsedPct: &heapUsed},
		}},
	}
	out, err := HTML(r)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, want := range []string{
		"節點環境（Node Context）",
		"node-a",
		"data_hot",
		"/srv/es-data",
		"1/1 成功",
		"OS RAM（含 cache） 93%",
		"JVM Heap 54%",
		"不等於 JVM Heap",
		"單次高值不單獨視為記憶體壓力",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("HTML 缺少 %q", want)
		}
	}
	for _, legacy := range []string{"｜ RAM 93%", "｜ Heap 54%"} {
		if strings.Contains(s, legacy) {
			t.Errorf("HTML 不應再使用模糊標籤 %q", legacy)
		}
	}
}

func TestHTML_DiagnosticCardsHaveExplicitStatusLabels(t *testing.T) {
	out, err := HTML(sampleReport())
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, label := range []string{"PASS", "WARNING", "CRITICAL", "SKIPPED", "UNKNOWN"} {
		want := `<span class="status-label">` + label + `</span>`
		if !strings.Contains(s, want) {
			t.Errorf("HTML 診斷卡缺少明確狀態標籤 %q", label)
		}
	}
}
