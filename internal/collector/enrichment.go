package collector

import (
	"encoding/json"
	"sort"

	"elk-diagnostics/internal/nodecontext"
)

// ILMPolicyDefinition 保留報告實際使用的 policy 結構；不攜帶任意 _meta。
type ILMPolicyDefinition struct {
	Name               string
	Version            int64
	ModifiedMillis     int64
	Phases             []ILMPolicyPhase
	UsedIndices        int
	UsedDataStreams    int
	UsedIndexTemplates int
}

type ILMPolicyPhase struct {
	Name    string
	MinAge  string
	Actions []string
}

func (c *Client) ILMPolicies() ([]ILMPolicyDefinition, error) {
	b, err := c.get(EpILMPolicies)
	if err != nil {
		return nil, err
	}
	var raw map[string]struct {
		Version        int64 `json:"version"`
		ModifiedMillis int64 `json:"modified_date_millis"`
		Policy         struct {
			Phases map[string]struct {
				MinAge  string                     `json:"min_age"`
				Actions map[string]json.RawMessage `json:"actions"`
			} `json:"phases"`
		} `json:"policy"`
		InUseBy struct {
			Indices             []string `json:"indices"`
			DataStreams         []string `json:"data_streams"`
			ComposableTemplates []string `json:"composable_templates"`
		} `json:"in_use_by"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	out := make([]ILMPolicyDefinition, 0, len(raw))
	for name, policy := range raw {
		item := ILMPolicyDefinition{
			Name: name, Version: policy.Version, ModifiedMillis: policy.ModifiedMillis,
			UsedIndices: len(policy.InUseBy.Indices), UsedDataStreams: len(policy.InUseBy.DataStreams),
			UsedIndexTemplates: len(policy.InUseBy.ComposableTemplates),
		}
		for phaseName, phase := range policy.Policy.Phases {
			actions := make([]string, 0, len(phase.Actions))
			for action := range phase.Actions {
				actions = append(actions, action)
			}
			sort.Strings(actions)
			item.Phases = append(item.Phases, ILMPolicyPhase{Name: phaseName, MinAge: phase.MinAge, Actions: actions})
		}
		sort.Slice(item.Phases, func(i, j int) bool { return item.Phases[i].Name < item.Phases[j].Name })
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

type SnapshotRepository struct {
	Name string
	Type string
}

func (c *Client) SnapshotRepositories() ([]SnapshotRepository, error) {
	b, err := c.get(EpSnapshotRepositories)
	if err != nil {
		return nil, err
	}
	var raw map[string]struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	out := make([]SnapshotRepository, 0, len(raw))
	for name, repository := range raw {
		out = append(out, SnapshotRepository{Name: name, Type: repository.Type})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

type DataStream struct {
	Name           string
	Status         string
	Template       string
	Generation     int64
	ILMPolicy      string
	ManagedBy      string
	BackingIndices int
}

func (c *Client) DataStreams() ([]DataStream, error) {
	b, err := c.get(EpDataStreams)
	if err != nil {
		return nil, err
	}
	var raw struct {
		DataStreams []struct {
			Name       string `json:"name"`
			Status     string `json:"status"`
			Template   string `json:"template"`
			Generation int64  `json:"generation"`
			ILMPolicy  string `json:"ilm_policy"`
			ManagedBy  string `json:"next_generation_managed_by"`
			Indices    []struct {
				Name      string `json:"index_name"`
				ILMPolicy string `json:"ilm_policy"`
				ManagedBy string `json:"managed_by"`
			} `json:"indices"`
		} `json:"data_streams"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	out := make([]DataStream, 0, len(raw.DataStreams))
	for _, stream := range raw.DataStreams {
		policy, managedBy := stream.ILMPolicy, stream.ManagedBy
		for _, index := range stream.Indices {
			if policy == "" && index.ILMPolicy != "" {
				policy = index.ILMPolicy
			}
			if managedBy == "" && index.ManagedBy != "" {
				managedBy = index.ManagedBy
			}
		}
		out = append(out, DataStream{
			Name: stream.Name, Status: stream.Status, Template: stream.Template,
			Generation: stream.Generation, ILMPolicy: policy, ManagedBy: managedBy,
			BackingIndices: len(stream.Indices),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

type FielddataNode struct {
	ID          string
	Name        string
	MemoryBytes int64
	Evictions   int64
}

type FielddataSnapshot struct {
	Coverage nodecontext.Coverage
	Nodes    []FielddataNode
}

func (c *Client) FielddataStats() (*FielddataSnapshot, error) {
	b, err := c.get(EpNodesFielddata)
	if err != nil {
		return nil, err
	}
	var raw struct {
		Coverage rawCoverage `json:"_nodes"`
		Nodes    map[string]struct {
			Name    string `json:"name"`
			Indices struct {
				Fielddata struct {
					MemoryBytes int64 `json:"memory_size_in_bytes"`
					Evictions   int64 `json:"evictions"`
				} `json:"fielddata"`
			} `json:"indices"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	out := &FielddataSnapshot{Coverage: coverageOf(raw.Coverage, len(raw.Nodes))}
	for id, node := range raw.Nodes {
		out.Nodes = append(out.Nodes, FielddataNode{
			ID: id, Name: node.Name, MemoryBytes: node.Indices.Fielddata.MemoryBytes,
			Evictions: node.Indices.Fielddata.Evictions,
		})
	}
	sort.Slice(out.Nodes, func(i, j int) bool {
		if out.Nodes[i].Name != out.Nodes[j].Name {
			return out.Nodes[i].Name < out.Nodes[j].Name
		}
		return out.Nodes[i].ID < out.Nodes[j].ID
	})
	return out, nil
}
