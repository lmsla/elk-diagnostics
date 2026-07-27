package collector

import (
	"errors"
	"testing"
)

func TestExtendedHealthCollectors(t *testing.T) {
	t.Run("indexing pressure and index blocks", func(t *testing.T) {
		c := newStaticBundleClient(t, map[string]string{
			"nodes_indexing_pressure.json": `{"_nodes":{"total":1,"successful":1,"failed":0},"nodes":{"a":{"name":"n1","indexing_pressure":{"memory":{"current":{"combined_coordinating_and_primary_in_bytes":80,"replica_in_bytes":20,"all_in_bytes":100},"limit_in_bytes":1000}}}}}`,
			"all_settings.json":            `{ ".security":{"settings":{"index.blocks.write":"true"}}, "logs":{"settings":{"index.blocks.write":"true"}}, "archive":{"settings":{"index.blocks.read_only_allow_delete":true}} }`,
		})
		pressure, err := c.IndexingPressure()
		if err != nil || !pressure.Coverage.Complete() || len(pressure.Nodes) != 1 || *pressure.Nodes[0].LimitBytes != 1000 {
			t.Fatalf("IndexingPressure() = %+v, %v", pressure, err)
		}
		blocks, err := c.IndexBlocks()
		if err != nil || len(blocks) != 2 || blocks[0].Index != "archive" || !blocks[0].ReadOnlyAllowDelete || !blocks[1].Write {
			t.Fatalf("IndexBlocks() = %+v, %v", blocks, err)
		}
	})

	t.Run("CCR parses lag and exceptions without stack trace", func(t *testing.T) {
		c := newStaticBundleClient(t, map[string]string{
			"ccr_stats.json": `{"auto_follow_stats":{"number_of_failed_follow_indices":1,"number_of_failed_remote_cluster_state_requests":2,"recent_auto_follow_errors":[{"auto_follow_exception":{"type":"security_exception","reason":"denied","stack_trace":"secret"}}]},"follow_stats":{"indices":[{"index":"copy-logs","total_global_checkpoint_lag":0,"shards":[{"leader_global_checkpoint":100,"follower_global_checkpoint":80,"fatal_exception":{"type":"illegal_state_exception","reason":"fatal"},"read_exceptions":[]}]}]}}`,
		})
		got, err := c.CCRStats()
		if err != nil || len(got.Followers) != 1 || got.Followers[0].GlobalCheckpointLag != 20 || len(got.Followers[0].FatalErrors) != 1 || len(got.RecentAutoFollowErrors) != 1 {
			t.Fatalf("CCRStats() = %+v, %v", got, err)
		}
		if got.Followers[0].FatalErrors[0] != "illegal_state_exception: fatal" {
			t.Errorf("fatal reason = %q", got.Followers[0].FatalErrors[0])
		}
	})

	t.Run("ML shutdown and voting exclusions", func(t *testing.T) {
		c := newStaticBundleClient(t, map[string]string{
			"ml_job_stats.json":      `{"count":1,"jobs":[{"job_id":"job-a","state":"failed","assignment_explanation":"no ml node"}]}`,
			"ml_datafeed_stats.json": `{"count":1,"datafeeds":[{"datafeed_id":"feed-a","state":"stopped","timing_stats":{"job_id":"job-a"}}]}`,
			"planned_shutdown.json":  `{"nodes":[{"node_id":"n1","type":"restart","reason":"patch","shutdown_startedmillis":123,"status":"STALLED","shard_migration":{"status":"STALLED","shard_migrations_remaining":2,"explanation":"blocked"},"persistent_tasks":{"status":"COMPLETE"},"plugins":{"status":"COMPLETE"}}]}`,
			"voting_exclusions.json": `{"metadata":{"cluster_coordination":{"voting_config_exclusions":[{"node_id":"n1","node_name":"master-1"}]}}}`,
		})
		jobs, err := c.MLJobs()
		if err != nil || len(jobs) != 1 || jobs[0].State != "failed" {
			t.Fatalf("MLJobs() = %+v, %v", jobs, err)
		}
		feeds, err := c.MLDatafeeds()
		if err != nil || len(feeds) != 1 || feeds[0].JobID != "job-a" {
			t.Fatalf("MLDatafeeds() = %+v, %v", feeds, err)
		}
		shutdowns, err := c.PlannedShutdowns()
		if err != nil || len(shutdowns) != 1 || shutdowns[0].StartedMillis != 123 || shutdowns[0].ShardMigrationsRemaining != 2 {
			t.Fatalf("PlannedShutdowns() = %+v, %v", shutdowns, err)
		}
		exclusions, err := c.VotingExclusions()
		if err != nil || len(exclusions) != 1 || exclusions[0].NodeName != "master-1" {
			t.Fatalf("VotingExclusions() = %+v, %v", exclusions, err)
		}
	})

	t.Run("license unavailable is not confused with missing privilege", func(t *testing.T) {
		c := newStaticBundleClient(t, map[string]string{
			"ccr_stats.json": `{"error":{"type":"security_exception","reason":"current license is non-compliant for [ccr]"},"status":403}`,
			BundleStatusFile: "version.json 200\nccr_stats.json 403\n",
		})
		if _, err := c.CCRStats(); !errors.Is(err, ErrFeatureUnavailable) {
			t.Fatalf("CCRStats() err=%v, want ErrFeatureUnavailable", err)
		}
		c = newStaticBundleClient(t, map[string]string{
			"ccr_stats.json": `{"error":{"type":"security_exception","reason":"missing monitor privilege"},"status":403}`,
			BundleStatusFile: "version.json 200\nccr_stats.json 403\n",
		})
		if _, err := c.CCRStats(); err == nil || errors.Is(err, ErrFeatureUnavailable) {
			t.Fatalf("missing privilege err=%v, must remain ordinary error", err)
		}
	})
}
