#!/usr/bin/env bash
# 製造非 green 狀態，讓 _health_report 的 shards_availability / ilm 等 indicator
# 產生真實 diagnosis —— 這才是 Phase 0 要驗的重點（健康叢集看不到 diagnosis）。
#
# 用法: ./seed-unhealthy.sh <base_url>
# 範例: ./seed-unhealthy.sh http://localhost:9208
#
# 唯一寫入操作，僅對本機測試叢集使用，勿對正式環境執行。

set -uo pipefail
BASE="${1:?需提供 base_url}"
CURL=(curl -sS --max-time 15 -H 'Content-Type: application/json')

echo "== 製造未分配 replica（單節點要求 5 replica → 必 yellow + 未分配）=="
"${CURL[@]}" -X PUT "${BASE}/elkdoctor-unhealthy" -d '{
  "settings": { "number_of_shards": 1, "number_of_replicas": 5 }
}'; echo

echo "== 套用一個必然失敗的 ILM policy（對 2 shard index 要求 shrink 到 4）=="
"${CURL[@]}" -X PUT "${BASE}/_ilm/policy/elkdoctor-bad" -d '{
  "policy": { "phases": { "warm": { "min_age": "0ms",
    "actions": { "shrink": { "number_of_shards": 4 } } } } }
}'; echo
"${CURL[@]}" -X PUT "${BASE}/elkdoctor-ilmerr" -d '{
  "settings": { "number_of_shards": 2, "index.lifecycle.name": "elkdoctor-bad" }
}'; echo

echo
echo "已植入。等數秒讓 ILM 進入 ERROR 後，再跑 capture.sh 觀察 health_report 的"
echo "shards_availability / ilm indicator 是否帶出可用的 diagnosis。"
echo
echo "清理: curl -XDELETE ${BASE}/elkdoctor-unhealthy ${BASE}/elkdoctor-ilmerr ; curl -XDELETE ${BASE}/_ilm/policy/elkdoctor-bad"
