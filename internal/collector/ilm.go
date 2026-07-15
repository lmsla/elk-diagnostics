package collector

import "encoding/json"

// IlmStatus 取 GET /_ilm/status 的 operation_mode（RUNNING / STOPPING / STOPPED）。
func (c *Client) IlmStatus() (string, error) {
	b, err := c.get(EpIlmStatus)
	if err != nil {
		return "", err
	}
	var r struct {
		OperationMode string `json:"operation_mode"`
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return "", err
	}
	return r.OperationMode, nil
}

type IlmError struct {
	Index      string
	FailedStep string
	Reason     string
}

// IlmExplain 取處於 ERROR step 的 index（health_report ilm indicator 會延遲，故須直接問 explain）。
func (c *Client) IlmExplain() ([]IlmError, error) {
	b, err := c.get(EpIlmExplainErrors)
	if err != nil {
		return nil, err
	}
	var r struct {
		Indices map[string]struct {
			Step       string `json:"step"`
			FailedStep string `json:"failed_step"`
			StepInfo   struct {
				Type   string `json:"type"`
				Reason string `json:"reason"`
			} `json:"step_info"`
		} `json:"indices"`
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, err
	}
	var out []IlmError
	for name, idx := range r.Indices {
		if idx.Step == "ERROR" {
			reason := idx.StepInfo.Reason
			if reason == "" {
				reason = idx.StepInfo.Type
			}
			out = append(out, IlmError{Index: name, FailedStep: idx.FailedStep, Reason: reason})
		}
	}
	return out, nil
}

// IlmMigration：受管理 index 目前正處於 tier 遷移（action=migrate）。
type IlmMigration struct {
	Index string
	Phase string
	Step  string
}

// IlmMigrating 取目前 action=migrate 的受管理 index（tier 遷移候選）。#25 用。
// 是否「卡住」本質是時間序列問題：單次快照只能列候選名單，需重複執行比對同一批
// index 是否長時間停在同一 step 才能確認，不在此臆測。
func (c *Client) IlmMigrating() ([]IlmMigration, error) {
	b, err := c.get(EpIlmExplainManaged)
	if err != nil {
		return nil, err
	}
	var r struct {
		Indices map[string]struct {
			Managed bool   `json:"managed"`
			Phase   string `json:"phase"`
			Action  string `json:"action"`
			Step    string `json:"step"`
		} `json:"indices"`
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, err
	}
	var out []IlmMigration
	for name, idx := range r.Indices {
		if idx.Managed && idx.Action == "migrate" {
			out = append(out, IlmMigration{Index: name, Phase: idx.Phase, Step: idx.Step})
		}
	}
	return out, nil
}
