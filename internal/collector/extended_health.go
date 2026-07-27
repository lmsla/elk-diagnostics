package collector

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"elk-diagnostics/internal/nodecontext"
)

// ErrFeatureUnavailable 表示 optional feature 因 license 未啟用而不可採集。這和缺少
// monitor 權限不同：前者應 skipped，後者仍必須 unknown，避免把權限缺口包裝成未使用。
var ErrFeatureUnavailable = errors.New("optional feature unavailable")

type featureUnavailableError struct {
	feature string
	cause   error
}

func (e *featureUnavailableError) Error() string {
	return fmt.Sprintf("%s 功能未由目前 license 啟用: %v", e.feature, e.cause)
}
func (e *featureUnavailableError) Unwrap() error { return ErrFeatureUnavailable }

func licenseUnavailable(body []byte) bool {
	s := strings.ToLower(string(body))
	return strings.Contains(s, "license") &&
		(strings.Contains(s, "non-compliant") || strings.Contains(s, "not available") || strings.Contains(s, "not enabled"))
}

type IndexingPressureNode struct {
	ID                          string
	Name                        string
	CombinedCoordinatingPrimary *int64
	ReplicaBytes                *int64
	AllBytes                    *int64
	LimitBytes                  *int64
}

type IndexingPressureSnapshot struct {
	Coverage nodecontext.Coverage
	Nodes    []IndexingPressureNode
}

func (c *Client) IndexingPressure() (*IndexingPressureSnapshot, error) {
	b, err := c.get(EpNodesIndexingPressure)
	if err != nil {
		return nil, err
	}
	var raw struct {
		Coverage rawCoverage `json:"_nodes"`
		Nodes    map[string]struct {
			Name     string `json:"name"`
			Pressure struct {
				Memory struct {
					Current struct {
						Combined *int64 `json:"combined_coordinating_and_primary_in_bytes"`
						Replica  *int64 `json:"replica_in_bytes"`
						All      *int64 `json:"all_in_bytes"`
					} `json:"current"`
					Limit *int64 `json:"limit_in_bytes"`
				} `json:"memory"`
			} `json:"indexing_pressure"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	out := &IndexingPressureSnapshot{Coverage: coverageOf(raw.Coverage, len(raw.Nodes))}
	for id, node := range raw.Nodes {
		out.Nodes = append(out.Nodes, IndexingPressureNode{
			ID: id, Name: node.Name,
			CombinedCoordinatingPrimary: nonNegativeInt64(node.Pressure.Memory.Current.Combined),
			ReplicaBytes:                nonNegativeInt64(node.Pressure.Memory.Current.Replica),
			AllBytes:                    nonNegativeInt64(node.Pressure.Memory.Current.All),
			LimitBytes:                  nonNegativeInt64(node.Pressure.Memory.Limit),
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

type IndexBlock struct {
	Index               string
	ReadOnly            bool
	ReadOnlyAllowDelete bool
	Read                bool
	Write               bool
	Metadata            bool
}

func (c *Client) IndexBlocks() ([]IndexBlock, error) {
	b, err := c.get(EpAllSettings)
	if err != nil {
		return nil, err
	}
	var raw map[string]struct {
		Settings map[string]json.RawMessage `json:"settings"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	out := make([]IndexBlock, 0)
	for index, entry := range raw {
		if isSystemIndex(index) {
			continue
		}
		block := IndexBlock{
			Index:               index,
			ReadOnly:            rawBool(entry.Settings["index.blocks.read_only"]),
			ReadOnlyAllowDelete: rawBool(entry.Settings["index.blocks.read_only_allow_delete"]),
			Read:                rawBool(entry.Settings["index.blocks.read"]),
			Write:               rawBool(entry.Settings["index.blocks.write"]),
			Metadata:            rawBool(entry.Settings["index.blocks.metadata"]),
		}
		if block.ReadOnly || block.ReadOnlyAllowDelete || block.Read || block.Write || block.Metadata {
			out = append(out, block)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Index < out[j].Index })
	return out, nil
}

type CCRFollower struct {
	Index               string
	GlobalCheckpointLag int64
	FatalErrors         []string
	ReadErrors          []string
}

type CCRStats struct {
	FailedFollowIndices       int64
	FailedRemoteStateRequests int64
	RecentAutoFollowErrors    []string
	Followers                 []CCRFollower
}

func (c *Client) CCRStats() (CCRStats, error) {
	b, err := c.get(EpCCRStats)
	if err != nil {
		if licenseUnavailable(b) {
			return CCRStats{}, &featureUnavailableError{feature: "CCR", cause: err}
		}
		return CCRStats{}, err
	}
	var raw struct {
		AutoFollow struct {
			FailedFollow int64           `json:"number_of_failed_follow_indices"`
			FailedState  int64           `json:"number_of_failed_remote_cluster_state_requests"`
			RecentErrors json.RawMessage `json:"recent_auto_follow_errors"`
		} `json:"auto_follow_stats"`
		Follow struct {
			Indices []struct {
				Index    string `json:"index"`
				TotalLag int64  `json:"total_global_checkpoint_lag"`
				Shards   []struct {
					LeaderCheckpoint   int64           `json:"leader_global_checkpoint"`
					FollowerCheckpoint int64           `json:"follower_global_checkpoint"`
					Fatal              json.RawMessage `json:"fatal_exception"`
					Read               json.RawMessage `json:"read_exceptions"`
				} `json:"shards"`
			} `json:"indices"`
		} `json:"follow_stats"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return CCRStats{}, err
	}
	out := CCRStats{FailedFollowIndices: raw.AutoFollow.FailedFollow, FailedRemoteStateRequests: raw.AutoFollow.FailedState}
	out.RecentAutoFollowErrors = errorReasons(raw.AutoFollow.RecentErrors)
	for _, index := range raw.Follow.Indices {
		follower := CCRFollower{Index: index.Index, GlobalCheckpointLag: index.TotalLag}
		var computedLag int64
		for _, shard := range index.Shards {
			if delta := shard.LeaderCheckpoint - shard.FollowerCheckpoint; delta > 0 {
				computedLag += delta
			}
			follower.FatalErrors = append(follower.FatalErrors, errorReasons(shard.Fatal)...)
			follower.ReadErrors = append(follower.ReadErrors, errorReasons(shard.Read)...)
		}
		if follower.GlobalCheckpointLag == 0 && computedLag > 0 {
			follower.GlobalCheckpointLag = computedLag
		}
		out.Followers = append(out.Followers, follower)
	}
	sort.Slice(out.Followers, func(i, j int) bool { return out.Followers[i].Index < out.Followers[j].Index })
	return out, nil
}

type MLJob struct {
	ID                    string
	State                 string
	AssignmentExplanation string
}

type MLDatafeed struct {
	ID                    string
	JobID                 string
	State                 string
	AssignmentExplanation string
}

func (c *Client) MLJobs() ([]MLJob, error) {
	b, err := c.get(EpMLJobStats)
	if err != nil {
		if licenseUnavailable(b) {
			return nil, &featureUnavailableError{feature: "Machine Learning", cause: err}
		}
		return nil, err
	}
	var raw struct {
		Jobs []struct {
			ID          string `json:"job_id"`
			State       string `json:"state"`
			Explanation string `json:"assignment_explanation"`
		} `json:"jobs"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	out := make([]MLJob, 0, len(raw.Jobs))
	for _, job := range raw.Jobs {
		out = append(out, MLJob{ID: job.ID, State: job.State, AssignmentExplanation: job.Explanation})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (c *Client) MLDatafeeds() ([]MLDatafeed, error) {
	b, err := c.get(EpMLDatafeedStats)
	if err != nil {
		if licenseUnavailable(b) {
			return nil, &featureUnavailableError{feature: "Machine Learning", cause: err}
		}
		return nil, err
	}
	var raw struct {
		Datafeeds []struct {
			ID          string `json:"datafeed_id"`
			State       string `json:"state"`
			Explanation string `json:"assignment_explanation"`
			Timing      struct {
				JobID string `json:"job_id"`
			} `json:"timing_stats"`
		} `json:"datafeeds"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	out := make([]MLDatafeed, 0, len(raw.Datafeeds))
	for _, feed := range raw.Datafeeds {
		out = append(out, MLDatafeed{ID: feed.ID, JobID: feed.Timing.JobID, State: feed.State, AssignmentExplanation: feed.Explanation})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

type PlannedShutdown struct {
	NodeID                     string
	Type                       string
	Reason                     string
	Status                     string
	StartedMillis              int64
	ShardMigrationStatus       string
	ShardMigrationsRemaining   int64
	ShardMigrationExplanation  string
	PersistentTasksStatus      string
	PersistentTasksExplanation string
	PluginsStatus              string
}

func (c *Client) PlannedShutdowns() ([]PlannedShutdown, error) {
	b, err := c.get(EpPlannedShutdown)
	if err != nil {
		return nil, err
	}
	var raw struct {
		Nodes []struct {
			NodeID         string `json:"node_id"`
			Type           string `json:"type"`
			Reason         string `json:"reason"`
			Status         string `json:"status"`
			Started        int64  `json:"shutdown_started_millis"`
			StartedCompat  int64  `json:"shutdown_startedmillis"`
			ShardMigration struct {
				Status      string `json:"status"`
				Remaining   int64  `json:"shard_migrations_remaining"`
				Explanation string `json:"explanation"`
			} `json:"shard_migration"`
			PersistentTasks struct {
				Status      string `json:"status"`
				Explanation string `json:"explanation"`
			} `json:"persistent_tasks"`
			Plugins struct {
				Status string `json:"status"`
			} `json:"plugins"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	out := make([]PlannedShutdown, 0, len(raw.Nodes))
	for _, node := range raw.Nodes {
		started := node.Started
		if started == 0 {
			started = node.StartedCompat
		}
		out = append(out, PlannedShutdown{
			NodeID: node.NodeID, Type: node.Type, Reason: node.Reason, Status: node.Status, StartedMillis: started,
			ShardMigrationStatus: node.ShardMigration.Status, ShardMigrationsRemaining: node.ShardMigration.Remaining, ShardMigrationExplanation: node.ShardMigration.Explanation,
			PersistentTasksStatus: node.PersistentTasks.Status, PersistentTasksExplanation: node.PersistentTasks.Explanation,
			PluginsStatus: node.Plugins.Status,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].NodeID < out[j].NodeID })
	return out, nil
}

type VotingExclusion struct {
	NodeID   string `json:"node_id"`
	NodeName string `json:"node_name"`
}

func (c *Client) VotingExclusions() ([]VotingExclusion, error) {
	b, err := c.get(EpVotingExclusions)
	if err != nil {
		return nil, err
	}
	var raw struct {
		Metadata struct {
			Coordination struct {
				Exclusions []VotingExclusion `json:"voting_config_exclusions"`
			} `json:"cluster_coordination"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	sort.Slice(raw.Metadata.Coordination.Exclusions, func(i, j int) bool {
		if raw.Metadata.Coordination.Exclusions[i].NodeName != raw.Metadata.Coordination.Exclusions[j].NodeName {
			return raw.Metadata.Coordination.Exclusions[i].NodeName < raw.Metadata.Coordination.Exclusions[j].NodeName
		}
		return raw.Metadata.Coordination.Exclusions[i].NodeID < raw.Metadata.Coordination.Exclusions[j].NodeID
	})
	return raw.Metadata.Coordination.Exclusions, nil
}

func rawBool(raw json.RawMessage) bool {
	if len(raw) == 0 || string(raw) == "null" {
		return false
	}
	var value bool
	if json.Unmarshal(raw, &value) == nil {
		return value
	}
	parsed := strings.TrimSpace(strings.ToLower(rawString(raw)))
	return parsed == "true" || parsed == "1" || parsed == "yes" || parsed == "on"
}

// errorReasons 只擷取 ES exception 的 type/reason，不保留 stack_trace 與整段 request context。
// schema 在 CCR 的 recent/read exceptions 間不同，故用遞迴 traversal 而非版本綁死的 struct。
func errorReasons(raw json.RawMessage) []string {
	if len(raw) == 0 || string(raw) == "null" || string(raw) == "[]" || string(raw) == "{}" {
		return nil
	}
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return []string{"無法解析的 exception"}
	}
	seen := map[string]bool{}
	var out []string
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case []any:
			for _, item := range x {
				walk(item)
			}
		case map[string]any:
			typeName, _ := x["type"].(string)
			reason, _ := x["reason"].(string)
			if reason != "" {
				msg := reason
				if typeName != "" {
					msg = typeName + ": " + reason
				}
				if !seen[msg] {
					seen[msg] = true
					out = append(out, msg)
				}
			}
			for key, item := range x {
				if key != "stack_trace" && key != "reason" && key != "type" {
					walk(item)
				}
			}
		}
	}
	walk(value)
	if len(out) == 0 {
		out = append(out, "exception details present")
	}
	return out
}
