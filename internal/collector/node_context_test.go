package collector

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

const nodeStatsFixture = `{
  "_nodes":{"total":2,"successful":2,"failed":0},
  "nodes":{
    "z-id":{"name":"node-b","ip":"10.0.0.2","roles":["data_hot","master"],
		"os":{"cpu":{"percent":42,"load_average":{"1m":1.5,"5m":1.2,"15m":0.8}},
        "mem":{"total_in_bytes":8000,"used_in_bytes":6000,"free_in_bytes":2000,"used_percent":75,"free_percent":25},
        "swap":{"total_in_bytes":1000,"used_in_bytes":100,"free_in_bytes":900},
        "cgroup":{"cpuacct":{"usage_nanos":123},"cpu":{"cfs_period_micros":100000,"cfs_quota_micros":200000,"stat":{"number_of_elapsed_periods":10,"number_of_times_throttled":2,"time_throttled_nanos":99}},"memory":{"limit_in_bytes":"1073741824","usage_in_bytes":"900000000"}}},
      "process":{"open_file_descriptors":900,"max_file_descriptors":1000,"cpu":{"percent":12,"total_in_millis":345},"mem":{"total_virtual_in_bytes":9999}},
      "fs":{"total":{"total_in_bytes":10000,"free_in_bytes":4000,"available_in_bytes":3500},"data":[{"path":"/z","mount":"/dev/z","type":"xfs","total_in_bytes":10000,"free_in_bytes":4000,"available_in_bytes":3500}],"io_stats":{"devices":[{"device_name":"zda","operations":10,"read_operations":3,"write_operations":7,"read_kilobytes":30,"write_kilobytes":70,"io_time_in_millis":5}]}},
      "jvm":{"uptime_in_millis":12345,"mem":{"heap_used_in_bytes":400,"heap_max_in_bytes":1000,"heap_used_percent":40,"pools":{"old":{"used_in_bytes":300,"max_in_bytes":1000}}},"gc":{"collectors":{"old":{"collection_count":1,"collection_time_in_millis":2},"young":{"collection_count":3,"collection_time_in_millis":4}}}}},
    "a-id":{"name":"node-a","roles":["ingest"],
      "os":{"cpu":{"percent":-1},"swap":{"total_in_bytes":0,"used_in_bytes":0},"cgroup":{"memory":{"limit_in_bytes":"9223372036854771712","usage_in_bytes":"100"}}},
      "process":{"open_file_descriptors":-1,"max_file_descriptors":-1},
      "jvm":{"mem":{"pools":{"old":{"used_in_bytes":10,"max_in_bytes":100}}}}}
  }
}`

const nodeInfoFixture = `{
  "_nodes":{"total":2,"successful":2,"failed":0},
  "nodes":{
    "z-id":{"name":"node-b","ip":"10.0.0.2","roles":["master","data_hot"],"os":{"name":"Linux","pretty_name":"Linux","version":"6.8","arch":"aarch64","available_processors":8,"allocated_processors":4},"process":{"id":22,"mlockall":true}},
    "a-id":{"name":"node-a","roles":["ingest"],"os":{"name":"Linux","pretty_name":"Linux","version":"6.8","arch":"x86_64","available_processors":4,"allocated_processors":2},"process":{"id":11,"mlockall":false}}
  }
}`

func nodeContextClient(t *testing.T, responses map[string]string, calls map[string]int) *Client {
	t.Helper()
	return &Client{fetch: func(path string) ([]byte, int, error) {
		calls[path]++
		body, ok := responses[path]
		if !ok {
			return nil, 0, fmt.Errorf("missing fixture: %s", path)
		}
		return []byte(body), http.StatusOK, nil
	}}
}

func TestNodeContextSnapshot_MultiNodeAndOptionalFields(t *testing.T) {
	calls := map[string]int{}
	c := nodeContextClient(t, map[string]string{
		EpNodesResourceStats: nodeStatsFixture,
		EpNodesResourceInfo:  nodeInfoFixture,
	}, calls)

	s, err := c.NodeContextSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if !s.StatsCoverage.Complete() || !s.InfoCoverage.Complete() || len(s.Issues) != 0 {
		t.Fatalf("coverage=%+v/%+v issues=%v", s.StatsCoverage, s.InfoCoverage, s.Issues)
	}
	if len(s.Nodes) != 2 || s.Nodes[0].Name != "node-a" || s.Nodes[1].Name != "node-b" {
		t.Fatalf("nodes 未穩定排序: %+v", s.Nodes)
	}
	a, b := s.Nodes[0], s.Nodes[1]
	if b.IP != "10.0.0.2" {
		t.Errorf("Node IP 未正確解析: %+v", b)
	}
	if a.OS.CPUPercent != nil || a.Process.OpenFileDescriptors != nil {
		t.Error("ES 的 -1 必須正規化為不可得，而不是有效負數")
	}
	if a.OS.Cgroup.Memory.LimitUnlimited == nil || !*a.OS.Cgroup.Memory.LimitUnlimited || a.OS.Cgroup.Memory.LimitBytes != nil {
		t.Errorf("超過 int64 的 cgroup limit 應視為 unlimited: %+v", a.OS.Cgroup.Memory)
	}
	if b.OS.AllocatedProcessors == nil || *b.OS.AllocatedProcessors != 4 || b.Process.MemoryLocked == nil || !*b.Process.MemoryLocked {
		t.Errorf("Nodes Info 未正確合併: %+v %+v", b.OS, b.Process)
	}
	if b.OS.Load1m == nil || *b.OS.Load1m != 1.5 {
		t.Errorf("os.cpu.load_average 未正確解析: %+v", b.OS)
	}
	if b.OS.Cgroup.Memory.LimitBytes == nil || *b.OS.Cgroup.Memory.LimitBytes != 1073741824 || b.OS.Cgroup.Memory.UsageBytes == nil || *b.OS.Cgroup.Memory.UsageBytes != 900000000 {
		t.Errorf("cgroup string number 未正確解析: %+v", b.OS.Cgroup.Memory)
	}
	if len(b.JVM.GCCollectors) != 2 || b.JVM.GCCollectors[0].Name != "old" || b.JVM.GCCollectors[1].Name != "young" {
		t.Errorf("GC collector 未排序或遺失: %+v", b.JVM.GCCollectors)
	}
}

func TestParseCgroupLimitMax(t *testing.T) {
	limit, unlimited := parseCgroupLimit([]byte(`"max"`))
	if limit != nil || unlimited == nil || !*unlimited {
		t.Errorf("max 應視為 unlimited: limit=%v unlimited=%v", limit, unlimited)
	}
}

func TestNodeContextSnapshot_InfoFailurePreservesStats(t *testing.T) {
	calls := map[string]int{}
	c := nodeContextClient(t, map[string]string{EpNodesResourceStats: nodeStatsFixture}, calls)
	s, err := c.NodeContextSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Nodes) != 2 || s.InfoCoverage.Available {
		t.Fatalf("Stats 應保留、Info coverage 應不可得: %+v", s)
	}
	if len(s.Issues) == 0 || !strings.Contains(strings.Join(s.Issues, " "), "Nodes Info") {
		t.Errorf("應浮出 Nodes Info 失敗原因: %v", s.Issues)
	}
}

func TestNodeContextSnapshot_PartialCoverage(t *testing.T) {
	partialStats := strings.Replace(nodeStatsFixture,
		`"_nodes":{"total":2,"successful":2,"failed":0}`,
		`"_nodes":{"total":3,"successful":2,"failed":1}`, 1)
	c := nodeContextClient(t, map[string]string{EpNodesResourceStats: partialStats, EpNodesResourceInfo: nodeInfoFixture}, map[string]int{})
	s, err := c.NodeContextSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if s.StatsCoverage.Complete() || len(s.Issues) == 0 {
		t.Errorf("partial response 不得視為完整: %+v issues=%v", s.StatsCoverage, s.Issues)
	}
}

func TestNodeResourceStatsFetchedOnceForJVMAndContext(t *testing.T) {
	calls := map[string]int{}
	c := nodeContextClient(t, map[string]string{EpNodesResourceStats: nodeStatsFixture, EpNodesResourceInfo: nodeInfoFixture}, calls)
	if _, err := c.NodesJVMOldPool(); err != nil {
		t.Fatal(err)
	}
	if _, err := c.NodeContextSnapshot(); err != nil {
		t.Fatal(err)
	}
	if calls[EpNodesResourceStats] != 1 {
		t.Errorf("相同 Nodes Stats 請求執行 %d 次，want 1", calls[EpNodesResourceStats])
	}
}
