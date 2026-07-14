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

func TestLoad_OverrideZeroTreatedAsUnset(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "override.yaml")
	// 顯式寫 0 等同未提供（0 為合法範圍外的哨兵值，非真實門檻）。
	content := "performance:\n  jvm_warn_pct: 0\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	th, _ := Load(path)
	if th.Performance.JVMWarnPct != 85 {
		t.Errorf("JVMWarnPct = %d, want 85（覆寫值為 0 應視為未提供，沿用預設值）", th.Performance.JVMWarnPct)
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
	if th != want {
		t.Errorf("th = %+v, want %+v", th, want)
	}
}
