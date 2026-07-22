// Package rules 外部化 C 類診斷（health_report 無對應 indicator、需自行判定連續型
// 指標）用的可調閾值。A/B 類診斷直接轉述 _health_report 自身的 status/diagnosis，
// 不經過這裡——ES 自己已經下過判斷，沒有「閾值」這回事，見 spec-rules.md。
package rules

import (
	_ "embed"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

//go:embed default.yaml
var defaultYAML []byte

type Thresholds struct {
	Performance struct {
		JVMWarnPct   int `yaml:"jvm_warn_pct"`
		JVMCritPct   int `yaml:"jvm_crit_pct"`
		CPUWarnPct   int `yaml:"cpu_warn_pct"`
		QueueBacklog int `yaml:"queue_backlog"`
	} `yaml:"performance"`
	Data struct {
		MappingLimitDefault int `yaml:"mapping_limit_default"`
		MappingWarnFrac     int `yaml:"mapping_warn_frac"`
		IngestFailWarnPct   int `yaml:"ingest_fail_warn_pct"`
	} `yaml:"data"`
	Balance struct {
		HotspotSpreadPct int `yaml:"hotspot_spread_pct"`
	} `yaml:"balance"`
	WriteBottleneck struct {
		CPULowPct              int `yaml:"cpu_low_pct"`
		WriteQueueMin          int `yaml:"write_queue_min"`
		AllocatedProcessorsLow int `yaml:"allocated_processors_low"`
	} `yaml:"write_bottleneck"`
	NodeContext struct {
		FDWarnPct           int `yaml:"fd_warn_pct"`
		FDCritPct           int `yaml:"fd_crit_pct"`
		CgroupMemoryWarnPct int `yaml:"cgroup_memory_warn_pct"`
	} `yaml:"node_context"`
	StaticHealth struct {
		PendingTaskWarnSeconds int `yaml:"pending_task_warn_seconds"`
		PendingTaskCritSeconds int `yaml:"pending_task_crit_seconds"`
		LongTaskWarnSeconds    int `yaml:"long_task_warn_seconds"`
		ShardLargeWarnGB       int `yaml:"shard_large_warn_gb"`
		ShardSmallMaxMB        int `yaml:"shard_small_max_mb"`
		ShardSmallCountWarn    int `yaml:"shard_small_count_warn"`
		SnapshotWarnHours      int `yaml:"snapshot_warn_hours"`
		SnapshotCritHours      int `yaml:"snapshot_crit_hours"`
		ExpiryWarnDays         int `yaml:"expiry_warn_days"`
	} `yaml:"static_health"`
}

// Load 讀內建預設值；overridePath 非空時以檔案中大於 0 的欄位覆寫預設值。
// 0 或負值視為無效並沿用預設值。覆寫檔不存在或格式錯誤不會中斷程式，
// 回傳警告訊息、其餘沿用內建值——鐵律是零外部 YAML 也要能跑。
func Load(overridePath string) (Thresholds, []string) {
	var t Thresholds
	if err := yaml.Unmarshal(defaultYAML, &t); err != nil {
		panic("內嵌 rules/default.yaml 解析失敗（不應發生）：" + err.Error())
	}
	if overridePath == "" {
		return t, nil
	}

	b, err := os.ReadFile(overridePath)
	if err != nil {
		return t, []string{fmt.Sprintf("讀取覆寫規則檔失敗，改用內建預設值：%v", err)}
	}
	var o Thresholds
	if err := yaml.Unmarshal(b, &o); err != nil {
		return t, []string{fmt.Sprintf("覆寫規則檔格式錯誤，改用內建預設值：%v", err)}
	}

	mergeInt(&t.Performance.JVMWarnPct, o.Performance.JVMWarnPct)
	mergeInt(&t.Performance.JVMCritPct, o.Performance.JVMCritPct)
	mergeInt(&t.Performance.CPUWarnPct, o.Performance.CPUWarnPct)
	mergeInt(&t.Performance.QueueBacklog, o.Performance.QueueBacklog)
	mergeInt(&t.Data.MappingLimitDefault, o.Data.MappingLimitDefault)
	mergeInt(&t.Data.MappingWarnFrac, o.Data.MappingWarnFrac)
	mergeInt(&t.Data.IngestFailWarnPct, o.Data.IngestFailWarnPct)
	mergeInt(&t.Balance.HotspotSpreadPct, o.Balance.HotspotSpreadPct)
	mergeInt(&t.WriteBottleneck.CPULowPct, o.WriteBottleneck.CPULowPct)
	mergeInt(&t.WriteBottleneck.WriteQueueMin, o.WriteBottleneck.WriteQueueMin)
	mergeInt(&t.WriteBottleneck.AllocatedProcessorsLow, o.WriteBottleneck.AllocatedProcessorsLow)
	mergeInt(&t.NodeContext.FDWarnPct, o.NodeContext.FDWarnPct)
	mergeInt(&t.NodeContext.FDCritPct, o.NodeContext.FDCritPct)
	mergeInt(&t.NodeContext.CgroupMemoryWarnPct, o.NodeContext.CgroupMemoryWarnPct)
	mergeInt(&t.StaticHealth.PendingTaskWarnSeconds, o.StaticHealth.PendingTaskWarnSeconds)
	mergeInt(&t.StaticHealth.PendingTaskCritSeconds, o.StaticHealth.PendingTaskCritSeconds)
	mergeInt(&t.StaticHealth.LongTaskWarnSeconds, o.StaticHealth.LongTaskWarnSeconds)
	mergeInt(&t.StaticHealth.ShardLargeWarnGB, o.StaticHealth.ShardLargeWarnGB)
	mergeInt(&t.StaticHealth.ShardSmallMaxMB, o.StaticHealth.ShardSmallMaxMB)
	mergeInt(&t.StaticHealth.ShardSmallCountWarn, o.StaticHealth.ShardSmallCountWarn)
	mergeInt(&t.StaticHealth.SnapshotWarnHours, o.StaticHealth.SnapshotWarnHours)
	mergeInt(&t.StaticHealth.SnapshotCritHours, o.StaticHealth.SnapshotCritHours)
	mergeInt(&t.StaticHealth.ExpiryWarnDays, o.StaticHealth.ExpiryWarnDays)
	return t, nil
}

func mergeInt(base *int, override int) {
	if override > 0 {
		*base = override
	}
}
