package reporter

import (
	"strings"
	"testing"

	"elk-diagnostics/internal/nodecontext"
)

func TestHTML_NodeContext(t *testing.T) {
	r := sampleReport()
	used, total, fdOpen, fdMax := int64(512), int64(1024), int64(8), int64(100)
	cpu := 25
	r.NodeContext = &nodecontext.Snapshot{
		StatsCoverage: nodecontext.Coverage{Available: true, Total: 1, Successful: 1, Returned: 1},
		InfoCoverage:  nodecontext.Coverage{Available: true, Total: 1, Successful: 1, Returned: 1},
		Nodes:         []nodecontext.Node{{Name: "node-a", Roles: []string{"data_hot"}, OS: nodecontext.OS{CPUPercent: &cpu, Memory: nodecontext.Memory{UsedBytes: &used, TotalBytes: &total}}, Process: nodecontext.Process{OpenFileDescriptors: &fdOpen, MaxFileDescriptors: &fdMax}, Filesystem: nodecontext.Filesystem{DataPaths: []nodecontext.DataPath{{Path: "/srv/es-data"}}}}},
	}
	out, err := HTML(r)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, want := range []string{"節點環境（Node Context）", "node-a", "data_hot", "/srv/es-data", "1/1 成功"} {
		if !strings.Contains(s, want) {
			t.Errorf("HTML 缺少 %q", want)
		}
	}
}
