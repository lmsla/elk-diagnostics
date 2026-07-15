package collector

import "encoding/json"

// RestoreOperation：單一 shard 的 snapshot 還原進度。
type RestoreOperation struct {
	Index   string
	Shard   int
	Stage   string
	Percent string
}

// RestoreProgress 取 GET _recovery?active_only=true，篩選 type=SNAPSHOT 的 shard
// recovery（即從 snapshot 還原中的操作）。#36 唯讀查詢進度用，不執行 restore。
//
// 註：spec-health-report.md 原列 GET _snapshot/_status 作為加深來源，但該端點是
// 「建立快照」的進度，不是「還原」的進度；還原的正確查詢端點是 recovery API，
// 這裡改用 _recovery 以求技術正確。
func (c *Client) RestoreProgress() ([]RestoreOperation, error) {
	b, err := c.get(EpRecovery)
	if err != nil {
		return nil, err
	}
	var r map[string]struct {
		Shards []struct {
			ID    int    `json:"id"`
			Type  string `json:"type"`
			Stage string `json:"stage"`
			Index struct {
				Size struct {
					Percent string `json:"percent"`
				} `json:"size"`
			} `json:"index"`
		} `json:"shards"`
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, err
	}
	var out []RestoreOperation
	for idxName, idx := range r {
		for _, s := range idx.Shards {
			if s.Type == "SNAPSHOT" {
				out = append(out, RestoreOperation{Index: idxName, Shard: s.ID, Stage: s.Stage, Percent: s.Index.Size.Percent})
			}
		}
	}
	return out, nil
}
