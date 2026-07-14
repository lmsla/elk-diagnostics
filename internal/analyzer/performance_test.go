package analyzer

import (
	"testing"

	"elk-diagnostics/internal/collector"
	"elk-diagnostics/internal/diagnostic"
	"elk-diagnostics/rules"
)

func testThresholds() rules.Thresholds {
	t, _ := rules.Load("")
	return t
}

func TestRejectedRequests(t *testing.T) {
	t.Run("無拒絕", func(t *testing.T) {
		res := RejectedRequests([]collector.ThreadPoolRow{{Node: "n1", Name: "search", Rejected: 0}})
		if res.Status != diagnostic.StatusPass {
			t.Errorf("Status = %q, want pass", res.Status)
		}
	})
	t.Run("search pool 有拒絕", func(t *testing.T) {
		res := RejectedRequests([]collector.ThreadPoolRow{{Node: "n1", Name: "search", Rejected: 5, Completed: 100}})
		if res.Status != diagnostic.StatusWarning {
			t.Errorf("Status = %q, want warning", res.Status)
		}
		if !res.RequiresExtra {
			t.Error("rejected 為累積值，應要求雙取樣確認")
		}
	})
	t.Run("非受監控 pool 有拒絕不算", func(t *testing.T) {
		res := RejectedRequests([]collector.ThreadPoolRow{{Node: "n1", Name: "get", Rejected: 5}})
		if res.Status != diagnostic.StatusPass {
			t.Errorf("Status = %q, want pass（get 不在 watchPools）", res.Status)
		}
	})
}

func TestJVMPressure(t *testing.T) {
	th := testThresholds()
	t.Run("低於警戒", func(t *testing.T) {
		res := JVMPressure([]collector.NodeJVM{{Name: "n1", PressurePct: 50}}, th)
		if res.Status != diagnostic.StatusPass {
			t.Errorf("Status = %q, want pass", res.Status)
		}
	})
	t.Run("達 warn 未達 crit", func(t *testing.T) {
		res := JVMPressure([]collector.NodeJVM{{Name: "n1", PressurePct: 90}}, th)
		if res.Status != diagnostic.StatusWarning {
			t.Errorf("Status = %q, want warning", res.Status)
		}
		if res.Conclusion != diagnostic.ConclusionSuspected {
			t.Errorf("Conclusion = %q, want suspected", res.Conclusion)
		}
	})
	t.Run("達 crit", func(t *testing.T) {
		res := JVMPressure([]collector.NodeJVM{{Name: "n1", PressurePct: 96}}, th)
		if res.Status != diagnostic.StatusCritical {
			t.Errorf("Status = %q, want critical", res.Status)
		}
		if res.Conclusion != diagnostic.ConclusionConfirmed {
			t.Errorf("Conclusion = %q, want confirmed", res.Conclusion)
		}
	})
}

func TestCircuitBreaker(t *testing.T) {
	t.Run("無跳閘", func(t *testing.T) {
		res := CircuitBreaker([]collector.NodeBreaker{{Node: "n1", Breaker: "request", Tripped: 0}})
		if res.Status != diagnostic.StatusPass {
			t.Errorf("Status = %q, want pass", res.Status)
		}
	})
	t.Run("曾跳閘", func(t *testing.T) {
		res := CircuitBreaker([]collector.NodeBreaker{{Node: "n1", Breaker: "request", Tripped: 3}})
		if res.Status != diagnostic.StatusWarning {
			t.Errorf("Status = %q, want warning", res.Status)
		}
		if !res.RequiresExtra {
			t.Error("tripped 為累積值，應要求額外確認")
		}
	})
}

func TestHighCPU(t *testing.T) {
	th := testThresholds()
	t.Run("低於門檻", func(t *testing.T) {
		res := HighCPU([]collector.NodeCPU{{Name: "n1", CPU: 40}}, th)
		if res.Status != diagnostic.StatusPass {
			t.Errorf("Status = %q, want pass", res.Status)
		}
	})
	t.Run("超過門檻", func(t *testing.T) {
		res := HighCPU([]collector.NodeCPU{{Name: "n1", CPU: 90}}, th)
		if res.Status != diagnostic.StatusWarning {
			t.Errorf("Status = %q, want warning", res.Status)
		}
		if !res.RequiresExtra {
			t.Error("cpu 為瞬時值，應要求額外確認")
		}
	})
}

func TestTaskBacklog(t *testing.T) {
	th := testThresholds()
	t.Run("無積壓", func(t *testing.T) {
		res := TaskBacklog([]collector.ThreadPoolRow{{Node: "n1", Name: "write", Queue: 0}}, th)
		if res.Status != diagnostic.StatusPass {
			t.Errorf("Status = %q, want pass", res.Status)
		}
	})
	t.Run("積壓超過門檻", func(t *testing.T) {
		res := TaskBacklog([]collector.ThreadPoolRow{{Node: "n1", Name: "write", Queue: 100}}, th)
		if res.Status != diagnostic.StatusWarning {
			t.Errorf("Status = %q, want warning", res.Status)
		}
	})
}

func TestSlowLog(t *testing.T) {
	t.Run("已開啟", func(t *testing.T) {
		res := SlowLog([]string{"idx1", "idx2"})
		if res.Status != diagnostic.StatusPass {
			t.Errorf("Status = %q, want pass", res.Status)
		}
		if res.RequiresExtra {
			t.Error("已開啟時不應標 RequiresExtra")
		}
	})
	t.Run("未開啟", func(t *testing.T) {
		res := SlowLog(nil)
		if res.Status != diagnostic.StatusPass {
			t.Errorf("Status = %q, want pass（未開啟非錯誤）", res.Status)
		}
		if !res.RequiresExtra {
			t.Error("未開啟時應提示需先開啟才能回溯")
		}
		if len(res.Recommendations) == 0 {
			t.Error("未開啟時應給出開啟方式")
		}
	})
}
