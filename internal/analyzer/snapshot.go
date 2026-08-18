// snapshot.go：repository_integrity 的 B 類加深（#36，見 健康報告規格.md）。
package analyzer

import (
	"fmt"

	"elk-diagnostics/internal/collector"
	"elk-diagnostics/internal/diagnostic"
)

const docRestore = "https://www.elastic.co/docs/troubleshoot/elasticsearch/restore-from-snapshot"

// RestoreStatus #36：唯讀查詢進行中的 snapshot 還原進度，不執行 restore。
// 還原中屬正常操作狀態（通常是人為觸發），資訊性質；本工具只回報進度，
// 是否卡住需間隔重查比對進度是否持續推進。
func RestoreStatus(ops []collector.RestoreOperation) diagnostic.Result {
	res := diagnostic.Result{ID: "restore_status", Title: "Snapshot 還原狀態", Category: "snapshot", Source: "raw_api", Docs: []string{docRestore}}
	res.Measurements = append(res.Measurements, gauge("elasticsearch.snapshot.restore.active_shard.count", float64(len(ops)), "count", "", "", "", ""))
	if len(ops) == 0 {
		return pass(res, "無進行中的 snapshot 還原操作")
	}
	res.Status, res.Conclusion = diagnostic.StatusPass, diagnostic.ConclusionNormal
	res.Summary = fmt.Sprintf("%d 個 shard 正在從 snapshot 還原中", len(ops))
	for _, op := range ops {
		res.Findings = append(res.Findings, fmt.Sprintf("%s shard %d：stage=%s progress=%s", op.Index, op.Shard, op.Stage, op.Percent))
	}
	res.RequiresExtra, res.ExtraReason = true, "還原中不代表卡住；需間隔重查比對 progress 是否持續推進，長時間停滯才需查節點 I/O 或 repository 連通性"
	return res
}
