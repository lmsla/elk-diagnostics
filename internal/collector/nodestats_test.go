package collector

import "testing"

func TestPercentInt(t *testing.T) {
	tests := []struct {
		in   string
		want int
	}{
		{in: "89", want: 89},
		{in: "89.90", want: 89},
		{in: "0.06", want: 0},
		{in: "-", want: 0},
		{in: "-1", want: 0},
	}
	for _, test := range tests {
		if got := percentInt(test.in); got != test.want {
			t.Errorf("percentInt(%q)=%d, want %d", test.in, got, test.want)
		}
	}
}

func TestCatNodesCPUParsesStableNodeIdentity(t *testing.T) {
	calls := map[string]int{}
	c := nodeContextClient(t, map[string]string{
		EpCatNodes: `[{"id":"node-b","ip":"10.0.0.252:9300","name":"Elasticsearch02","node.role":"data","cpu":"1","load_1m":"0.1","allocated_processors":"8","heap.percent":"68","disk.used_percent":"26.05"}]`,
	}, calls)
	nodes, err := c.CatNodesCPU()
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 || nodes[0].ID != "node-b" || nodes[0].IP != "10.0.0.252" || nodes[0].Name != "Elasticsearch02" {
		t.Fatalf("node identity = %+v", nodes)
	}
	if !nodes[0].DiskKnown || nodes[0].DiskPercent != 26 {
		t.Fatalf("disk = %+v", nodes[0])
	}
}
