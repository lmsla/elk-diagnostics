package collector

import (
	"net/http"
	"testing"
)

// 真機驗證抓到的兩層 bug（見 flatSettingString 註解）：
//  1. filter_path=**.a.b.c 對 flat_settings=true 的回應永遠比對不到。
//  2. defaults 區塊混雜非字串型別（陣列/null），若整段硬解 map[string]string 會直接
//     失敗、還被吞掉，變成明明有值卻讀不到的假陰性。
//
// 這裡的 response body 刻意用未經 filter_path 縮減的真實 flat_settings 結構，且
// defaults 混入真機實測到的陣列型別值，確保回歸測試真的在驗證修好的那個路徑。
const clusterSettingsBlockedBody = `{
  "persistent": {},
  "transient": {"cluster.routing.allocation.enable": "none"},
  "defaults": {
    "cluster.routing.allocation.enable": "all",
    "network.host": ["0.0.0.0"],
    "xpack.security.user": null
  }
}`

func TestClusterAllocationEnable(t *testing.T) {
	t.Run("transient 覆寫優先於 defaults，且不被 defaults 裡的非字串值拖垮解析", func(t *testing.T) {
		c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/" {
				w.Write([]byte(versionBody))
				return
			}
			w.Write([]byte(clusterSettingsBlockedBody))
		})
		got, err := c.ClusterAllocationEnable()
		if err != nil {
			t.Fatalf("ClusterAllocationEnable() 失敗: %v", err)
		}
		if got != "none" {
			t.Errorf("got %q, want %q（應讀到 transient 層的 none，不該落回 all）", got, "none")
		}
	})
	t.Run("無任何層設定時預設 all", func(t *testing.T) {
		c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/" {
				w.Write([]byte(versionBody))
				return
			}
			w.Write([]byte(`{"persistent":{},"transient":{},"defaults":{}}`))
		})
		got, err := c.ClusterAllocationEnable()
		if err != nil {
			t.Fatalf("ClusterAllocationEnable() 失敗: %v", err)
		}
		if got != "all" {
			t.Errorf("got %q, want all", got)
		}
	})
}

const indexSettingsBlockedBody = `{
  "blocked-test": {
    "settings": {"index.routing.allocation.enable": "none"},
    "defaults": {
      "index.routing.allocation.enable": "all",
      "index.query.default_field": ["*"]
    }
  }
}`

func TestIndexAllocationEnable(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			w.Write([]byte(versionBody))
			return
		}
		w.Write([]byte(indexSettingsBlockedBody))
	})
	got, err := c.IndexAllocationEnable("blocked-test")
	if err != nil {
		t.Fatalf("IndexAllocationEnable() 失敗: %v", err)
	}
	if got != "none" {
		t.Errorf("got %q, want none", got)
	}
}

// 真機健康叢集：無未分配 shard，ES 回 400 illegal_argument_exception。
func TestAllocationExplain_NoUnassignedShard(t *testing.T) {
	b := loadFixture(t, "es8-health/allocation_explain.json")
	if !isNoUnassignedShardError(b) {
		t.Fatal("healthy fixture 應被判定為「無未分配 shard 可解釋」")
	}
}

// 真機異常叢集：有未分配 shard，deciders 帶 same_shard/NO。
func TestAllocationExplain_UnassignedShardWithDecider(t *testing.T) {
	b := loadFixture(t, "es8-unhealthy/allocation_explain.json")
	if isNoUnassignedShardError(b) {
		t.Fatal("unhealthy fixture 不應被誤判為「無未分配 shard」")
	}
	exp, found, err := parseAllocationExplain(b)
	if err != nil {
		t.Fatalf("解析失敗: %v", err)
	}
	if !found || exp == nil {
		t.Fatal("應解析出可解釋的未分配 shard")
	}
	if exp.Index != "elkdoctor-ilmerr" || exp.Shard != 0 || exp.Primary != false {
		t.Errorf("index/shard/primary 解析不符: %+v", exp)
	}
	if len(exp.Deciders) != 1 || exp.Deciders[0].Decider != "same_shard" || exp.Deciders[0].Decision != "NO" {
		t.Errorf("deciders 解析不符: %+v", exp.Deciders)
	}
}
