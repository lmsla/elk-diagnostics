package collector

import "testing"

func newStaticBundleClient(t *testing.T, files map[string]string) *Client {
	t.Helper()
	files["version.json"] = versionBody
	c, err := NewFromBundle(writeBundle(t, files))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestStaticHealthCollectors(t *testing.T) {
	t.Run("pending and running tasks", func(t *testing.T) {
		c := newStaticBundleClient(t, map[string]string{
			"cluster_pending_tasks.json": `{"tasks":[{"priority":"HIGH","source":"put-mapping","time_in_queue_millis":9000,"executing":false},{"priority":"NORMAL","source":"ilm","time_in_queue_millis":1000,"executing":true}]}`,
			"running_tasks.json":         `{"tasks":{"n1:7":{"node":"n1","type":"transport","action":"indices:data/write/reindex","description":"reindex logs","running_time_in_nanos":6000000000,"cancellable":true}}}`,
		})
		pending, err := c.PendingClusterTasks()
		if err != nil || len(pending) != 2 || pending[0].Source != "put-mapping" {
			t.Fatalf("PendingClusterTasks() = %+v, %v", pending, err)
		}
		running, err := c.RunningTasks()
		if err != nil || len(running) != 1 || running[0].ID != "n1:7" || running[0].Action != "indices:data/write/reindex" {
			t.Fatalf("RunningTasks() = %+v, %v", running, err)
		}
	})

	t.Run("shard sizing excludes system but keeps data stream", func(t *testing.T) {
		c := newStaticBundleClient(t, map[string]string{
			"cat_shards_sizing.json": `[
              {"index":".security-7","shard":"0","prirep":"p","state":"STARTED","node":"n1","store":"123","docs":"1"},
              {"index":".ds-logs-app-2026.07.22-000001","shard":"0","prirep":"p","state":"STARTED","node":"n1","store":"456","docs":"2"},
              {"index":"logs","shard":"0","prirep":"r","state":"STARTED","node":"n2","store":"789","docs":"3"}
            ]`,
		})
		got, err := c.ShardSizes()
		if err != nil || len(got) != 2 {
			t.Fatalf("ShardSizes() = %+v, %v", got, err)
		}
		if got[0].Index != ".ds-logs-app-2026.07.22-000001" || !got[0].Primary || got[0].StoreBytes != 456 {
			t.Errorf("data stream shard = %+v", got[0])
		}
	})

	t.Run("SLM policy timestamps", func(t *testing.T) {
		c := newStaticBundleClient(t, map[string]string{
			"slm_policies.json": `{"daily":{"modified_date_millis":1000,"next_execution_millis":4000,"last_success":{"snapshot_name":"snap-1","time":"2026-07-22T01:02:03.456Z"},"last_failure":{"snapshot_name":"snap-0","time":1500},"stats":{"snapshots_taken":2,"snapshots_failed":1}}}`,
		})
		got, err := c.SLMPolicies()
		if err != nil || len(got) != 1 || got[0].LastSuccessMillis != 1784682123456 || got[0].LastFailureMillis != 1500 || got[0].SnapshotsFailed != 1 {
			t.Fatalf("SLMPolicies() = %+v, %v", got, err)
		}
	})

	t.Run("node runtime coverage and stable sorting", func(t *testing.T) {
		c := newStaticBundleClient(t, map[string]string{
			"nodes_runtime.json": `{"_nodes":{"total":2,"successful":2,"failed":0},"nodes":{"b":{"name":"node-b","roles":["data_hot"],"version":"8.14.3","build_hash":"bbb","jvm":{"version":"21","vm_version":"21","mem":{"heap_init_in_bytes":1024,"heap_max_in_bytes":2048}},"plugins":[{"name":"analysis-icu","version":"8.14.3"}]},"a":{"name":"node-a","roles":["data_hot"],"version":"8.14.3","build_hash":"bbb","jvm":{"version":"21","vm_version":"21","mem":{"heap_init_in_bytes":1024,"heap_max_in_bytes":2048}},"plugins":[]}}}`,
		})
		got, err := c.NodeRuntimes()
		if err != nil || !got.Coverage.Complete() || len(got.Nodes) != 2 || got.Nodes[0].Name != "node-a" {
			t.Fatalf("NodeRuntimes() = %+v, %v", got, err)
		}
	})

	t.Run("TLS license replicas and topology", func(t *testing.T) {
		c := newStaticBundleClient(t, map[string]string{
			"ssl_certificates.json": `[{"path":"http.p12","alias":"node","subject_dn":"CN=node","issuer":"CN=ca","expiry":"2030-01-01T00:00:00.000Z","has_private_key":true}]`,
			"license.json":          `{"license":{"status":"active","type":"trial","issued_to":"lab","expiry_date_in_millis":2000}}`,
			"all_settings.json":     `{".kibana":{"settings":{"index.number_of_replicas":"0"}},"logs":{"settings":{"index.number_of_replicas":"0","index.auto_expand_replicas":"0-all"}}}`,
			"nodes_topology.json":   `{"_nodes":{"total":2,"successful":2,"failed":0},"nodes":{"a":{"name":"n1","roles":["data_hot"],"attributes":{"zone":"a"}},"b":{"name":"n2","roles":["data_hot"],"attributes":{"zone":"b"}}}}`,
			"cluster_settings.json": `{"persistent":{"cluster.routing.allocation.awareness.attributes":"zone"},"transient":{},"defaults":{}}`,
		})
		certs, err := c.TLSCertificates()
		if err != nil || len(certs) != 1 || !certs[0].HasPrivateKey {
			t.Fatalf("TLSCertificates() = %+v, %v", certs, err)
		}
		license, err := c.LicenseInfo()
		if err != nil || license.Type != "trial" || license.ExpiryMillis != 2000 {
			t.Fatalf("LicenseInfo() = %+v, %v", license, err)
		}
		replicas, err := c.IndexReplicas()
		if err != nil || len(replicas) != 1 || replicas[0].Index != "logs" || replicas[0].Replicas != 0 {
			t.Fatalf("IndexReplicas() = %+v, %v", replicas, err)
		}
		nodes, err := c.NodeTopology()
		if err != nil || !nodes.Coverage.Complete() || len(nodes.Nodes) != 2 || nodes.Nodes[0].Attributes["zone"] != "a" {
			t.Fatalf("NodeTopology() = %+v, %v", nodes, err)
		}
		attrs, err := c.AllocationAwarenessAttributes()
		if err != nil || len(attrs) != 1 || attrs[0] != "zone" {
			t.Fatalf("AllocationAwarenessAttributes() = %+v, %v", attrs, err)
		}
	})
}
