package rules

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_Default(t *testing.T) {
	th, warnings := Load("")
	if warnings != nil {
		t.Errorf("warnings = %v, want nil", warnings)
	}
	if th.Performance.JVMWarnPct != 85 {
		t.Errorf("JVMWarnPct = %d, want 85（內建預設值）", th.Performance.JVMWarnPct)
	}
	if th.Data.MappingLimitDefault != 1000 {
		t.Errorf("MappingLimitDefault = %d, want 1000", th.Data.MappingLimitDefault)
	}
	if th.NodeContext.FDWarnPct != 80 || th.NodeContext.FDCritPct != 90 || th.NodeContext.CgroupMemoryWarnPct != 90 {
		t.Errorf("NodeContext defaults = %+v", th.NodeContext)
	}
	if th.StaticHealth.PendingTaskWarnSeconds != 30 || th.StaticHealth.ShardLargeWarnGB != 50 || th.StaticHealth.ExpiryWarnDays != 30 {
		t.Errorf("StaticHealth defaults = %+v", th.StaticHealth)
	}
}

func TestLoad_OverridePartial(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "override.yaml")
	// 只覆寫 jvm_warn_pct，其餘欄位應沿用內建預設值。
	content := "performance:\n  jvm_warn_pct: 70\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	th, warnings := Load(path)
	if warnings != nil {
		t.Errorf("warnings = %v, want nil", warnings)
	}
	if th.Performance.JVMWarnPct != 70 {
		t.Errorf("JVMWarnPct = %d, want 70（覆寫值）", th.Performance.JVMWarnPct)
	}
	if th.Performance.JVMCritPct != 95 {
		t.Errorf("JVMCritPct = %d, want 95（未覆寫，應沿用預設值）", th.Performance.JVMCritPct)
	}
	if th.Data.MappingLimitDefault != 1000 {
		t.Errorf("MappingLimitDefault = %d, want 1000（未覆寫，應沿用預設值）", th.Data.MappingLimitDefault)
	}
}

func TestLoad_OverrideNonPositiveTreatedAsUnset(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "override.yaml")
	// 顯式寫 0 等同未提供（0 為合法範圍外的哨兵值，非真實門檻）。
	content := "performance:\n  jvm_warn_pct: 0\nstatic_health:\n  expiry_warn_days: -1\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	th, _ := Load(path)
	if th.Performance.JVMWarnPct != 85 {
		t.Errorf("JVMWarnPct = %d, want 85（覆寫值為 0 應視為未提供，沿用預設值）", th.Performance.JVMWarnPct)
	}
	if th.StaticHealth.ExpiryWarnDays != 30 {
		t.Errorf("ExpiryWarnDays = %d, want 30（負值應視為無效，沿用預設值）", th.StaticHealth.ExpiryWarnDays)
	}
}

func TestLoad_OverrideFileMissing(t *testing.T) {
	th, warnings := Load("/nonexistent/path/override.yaml")
	if len(warnings) == 0 {
		t.Error("覆寫檔不存在時應回傳警告訊息")
	}
	if th.Performance.JVMWarnPct != 85 {
		t.Errorf("JVMWarnPct = %d, want 85（讀檔失敗應 fallback 回預設值，不中斷程式）", th.Performance.JVMWarnPct)
	}
}

func TestLoad_OverrideInvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "override.yaml")
	if err := os.WriteFile(path, []byte("not: [valid: yaml"), 0o644); err != nil {
		t.Fatal(err)
	}

	th, warnings := Load(path)
	if len(warnings) == 0 {
		t.Error("覆寫檔格式錯誤時應回傳警告訊息")
	}
	if th.Performance.JVMWarnPct != 85 {
		t.Errorf("JVMWarnPct = %d, want 85（格式錯誤應 fallback 回預設值，不中斷程式）", th.Performance.JVMWarnPct)
	}
}

func TestLoad_OverrideAllFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "override.yaml")
	content := `
performance:
  jvm_warn_pct: 1
  jvm_crit_pct: 2
  cpu_warn_pct: 3
  queue_backlog: 4
data:
  mapping_limit_default: 5
  mapping_warn_frac: 6
  ingest_fail_warn_pct: 7
balance:
  hotspot_spread_pct: 8
write_bottleneck:
  cpu_low_pct: 9
  write_queue_min: 10
  allocated_processors_low: 11
node_context:
  fd_warn_pct: 12
  fd_crit_pct: 13
  cgroup_memory_warn_pct: 14
static_health:
  pending_task_warn_seconds: 15
  pending_task_crit_seconds: 16
  long_task_warn_seconds: 17
  shard_large_warn_gb: 18
  shard_small_max_mb: 19
  shard_small_count_warn: 20
  snapshot_warn_hours: 21
  snapshot_crit_hours: 22
  expiry_warn_days: 23
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	th, warnings := Load(path)
	if warnings != nil {
		t.Errorf("warnings = %v, want nil", warnings)
	}
	want := Thresholds{}
	want.Performance.JVMWarnPct = 1
	want.Performance.JVMCritPct = 2
	want.Performance.CPUWarnPct = 3
	want.Performance.QueueBacklog = 4
	want.Data.MappingLimitDefault = 5
	want.Data.MappingWarnFrac = 6
	want.Data.IngestFailWarnPct = 7
	want.Balance.HotspotSpreadPct = 8
	want.WriteBottleneck.CPULowPct = 9
	want.WriteBottleneck.WriteQueueMin = 10
	want.WriteBottleneck.AllocatedProcessorsLow = 11
	want.NodeContext.FDWarnPct = 12
	want.NodeContext.FDCritPct = 13
	want.NodeContext.CgroupMemoryWarnPct = 14
	want.StaticHealth.PendingTaskWarnSeconds = 15
	want.StaticHealth.PendingTaskCritSeconds = 16
	want.StaticHealth.LongTaskWarnSeconds = 17
	want.StaticHealth.ShardLargeWarnGB = 18
	want.StaticHealth.ShardSmallMaxMB = 19
	want.StaticHealth.ShardSmallCountWarn = 20
	want.StaticHealth.SnapshotWarnHours = 21
	want.StaticHealth.SnapshotCritHours = 22
	want.StaticHealth.ExpiryWarnDays = 23
	if th != want {
		t.Errorf("th = %+v, want %+v", th, want)
	}
}
