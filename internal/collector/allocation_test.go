package collector

import "testing"

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
