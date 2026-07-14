package collector

import "encoding/json"

// IlmStatus 取 GET /_ilm/status 的 operation_mode（RUNNING / STOPPING / STOPPED）。
func (c *Client) IlmStatus() (string, error) {
	b, err := c.get("/_ilm/status")
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
	b, err := c.get("/_all/_ilm/explain?only_errors=true&only_managed=true")
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
