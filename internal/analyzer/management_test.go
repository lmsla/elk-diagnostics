package analyzer

import (
	"testing"

	"elk-diagnostics/internal/collector"
	"elk-diagnostics/internal/diagnostic"
)

func TestIlmTierMigration(t *testing.T) {
	t.Run("無遷移中", func(t *testing.T) {
		res := IlmTierMigration(nil)
		if res.Status != diagnostic.StatusPass {
			t.Errorf("Status = %q, want pass", res.Status)
		}
	})
	t.Run("有遷移候選", func(t *testing.T) {
		res := IlmTierMigration([]collector.IlmMigration{
			{Index: "logs-2026.01", Phase: "warm", Step: "migrate"},
		})
		if res.Status != diagnostic.StatusWarning {
			t.Fatalf("Status = %q, want warning", res.Status)
		}
		if !res.RequiresExtra {
			t.Error("RequiresExtra 應為 true（單次快照無法確認是否卡住）")
		}
		if len(res.Findings) != 1 {
			t.Errorf("Findings 數量 = %d, want 1", len(res.Findings))
		}
	})
}

func TestILM(t *testing.T) {
	cases := []struct {
		name       string
		mode       string
		errs       []collector.IlmError
		wantStatus diagnostic.Status
	}{
		{"RUNNING 無錯誤", "RUNNING", nil, diagnostic.StatusPass},
		{"STOPPED", "STOPPED", nil, diagnostic.StatusCritical},
		{"STOPPING 過渡狀態", "STOPPING", nil, diagnostic.StatusWarning},
		{"有 ERROR step", "RUNNING", []collector.IlmError{{Index: "idx1", FailedStep: "check-rollover-ready", Reason: "policy not found"}}, diagnostic.StatusCritical},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res := ILM(c.mode, c.errs)
			if res.Status != c.wantStatus {
				t.Errorf("Status = %q, want %q", res.Status, c.wantStatus)
			}
		})
	}
}

func TestWatcher(t *testing.T) {
	t.Run("運作中", func(t *testing.T) {
		res := Watcher(false)
		if res.Status != diagnostic.StatusPass {
			t.Errorf("Status = %q, want pass", res.Status)
		}
	})
	t.Run("手動停止", func(t *testing.T) {
		res := Watcher(true)
		if res.Status != diagnostic.StatusWarning {
			t.Errorf("Status = %q, want warning", res.Status)
		}
	})
}

func TestTransforms(t *testing.T) {
	t.Run("未使用不適用", func(t *testing.T) {
		res := Transforms(nil)
		if res.Status != diagnostic.StatusPass {
			t.Errorf("Status = %q, want pass", res.Status)
		}
	})
	t.Run("皆正常", func(t *testing.T) {
		res := Transforms([]collector.Transform{{ID: "t1", State: "started"}})
		if res.Status != diagnostic.StatusPass {
			t.Errorf("Status = %q, want pass", res.Status)
		}
	})
	t.Run("有 failed", func(t *testing.T) {
		res := Transforms([]collector.Transform{{ID: "t1", State: "failed"}})
		if res.Status != diagnostic.StatusCritical {
			t.Errorf("Status = %q, want critical", res.Status)
		}
		if res.Conclusion != diagnostic.ConclusionConfirmed {
			t.Errorf("Conclusion = %q, want confirmed", res.Conclusion)
		}
	})
}

func TestRemoteClusters(t *testing.T) {
	t.Run("未設定不適用", func(t *testing.T) {
		res := RemoteClusters(nil)
		if res.Status != diagnostic.StatusPass {
			t.Errorf("Status = %q, want pass", res.Status)
		}
	})
	t.Run("皆已連線", func(t *testing.T) {
		res := RemoteClusters([]collector.RemoteCluster{{Name: "rc1", Connected: true}})
		if res.Status != diagnostic.StatusPass {
			t.Errorf("Status = %q, want pass", res.Status)
		}
	})
	t.Run("未連線", func(t *testing.T) {
		res := RemoteClusters([]collector.RemoteCluster{{Name: "rc1", Connected: false}})
		if res.Status != diagnostic.StatusWarning {
			t.Errorf("Status = %q, want warning", res.Status)
		}
		if res.Conclusion != diagnostic.ConclusionConfirmed {
			t.Errorf("Conclusion = %q, want confirmed", res.Conclusion)
		}
	})
}

func TestUpgradeDeprecations(t *testing.T) {
	t.Run("無警告", func(t *testing.T) {
		res := UpgradeDeprecations(nil)
		if res.Status != diagnostic.StatusPass {
			t.Errorf("Status = %q, want pass", res.Status)
		}
	})
	t.Run("僅 warning 等級", func(t *testing.T) {
		res := UpgradeDeprecations([]collector.Deprecation{{Level: "warning", Message: "foo"}})
		if res.Status != diagnostic.StatusWarning {
			t.Errorf("Status = %q, want warning", res.Status)
		}
	})
	t.Run("有 critical 等級", func(t *testing.T) {
		res := UpgradeDeprecations([]collector.Deprecation{
			{Level: "warning", Message: "foo"},
			{Level: "critical", Message: "bar"},
		})
		if res.Status != diagnostic.StatusCritical {
			t.Errorf("Status = %q, want critical", res.Status)
		}
	})
}

func TestMonitoring(t *testing.T) {
	t.Run("已啟用", func(t *testing.T) {
		res := Monitoring("true")
		if res.Status != diagnostic.StatusPass {
			t.Errorf("Status = %q, want pass", res.Status)
		}
	})
	t.Run("未啟用仍為 pass（資訊性）", func(t *testing.T) {
		res := Monitoring("false")
		if res.Status != diagnostic.StatusPass {
			t.Errorf("Status = %q, want pass（資訊性，不是錯誤）", res.Status)
		}
	})
}
