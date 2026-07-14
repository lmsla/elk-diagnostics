# spec-write-bottleneck — 寫入瓶頸因果鏈（#16，C 類）

**實作位置**：`write_bottleneck.go`。專屬 `elk-diagnostics diagnose --symptom write-bottleneck`。
源自實際客戶案例（ES on K8s 寫入效能事件）。此項是**因果鏈驗證**，非單點告警。

## 目標

當「寫入變慢但 CPU 不高」時，驗證是否為 `allocated_processors` 偏低 → write thread pool 過小 → write queue 積壓的連鎖。

## 採集（唯讀，多源）

```
GET /_nodes/os                 # allocated_processors（ES 實際看到的核數）
GET /_nodes/settings           # thread_pool.write 設定
GET /_nodes/stats/thread_pool  # write pool active/queue/rejected
GET /_cat/thread_pool/write?v=true&h=node_name,size,active,queue,rejected
GET /_nodes/stats/indices      # indexing 速率與延遲
```

## 判定（因果鏈，需同時成立才觸發結論）

依序檢查，**全部成立**才判定為寫入瓶頸（❌/⚠️）：

1. CPU 使用率**偏低**（排除單純算力不足；閾值入 rules）。
2. write queue **積壓**（queue 持續>0 或逼近 queue capacity）。
3. `allocated_processors` **偏低**（常見於 K8s 未正確設定 CPU limit/request，導致 ES 只看到少數核 → write pool size 連帶過小）。

三者成立 → 結論：寫入瓶頸來自 `allocated_processors` 過低導致 write pool 過小。
部分成立 → 降為 ⚠️ 並列出已成立環節，不妄下結論。

## 反向觸發

`check` 巡檢時若偵測到「CPU 低 + write queue 積壓 + allocated_processors 偏低」的組合，主動提示使用者執行本 diagnose 路徑。

## 建議（唯讀引導）

- 修正容器 CPU limit/request，使 ES 取得正確核數；或顯式設定 `node.processors`。
- 確認 write thread pool `size` 是否被 `allocated_processors` 壓低。
- 連動 #6（rejected）、#9（hot threads）佐證。

## 限制

- `allocated_processors` 的取得欄位與 thread pool 模型跨 8.x/9.x 可能不同，須以目標版本實測。
- 單次快照看不出「持續積壓 vs 瞬時尖峰」，建議搭配 `--interval` 雙取樣。

## 官方文件

https://www.elastic.co/docs/reference/elasticsearch/configuration-reference/thread-pool-settings

`tested_versions`: []
