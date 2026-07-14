# spec-management — 管理子系統（C 類缺口）

**實作位置**：`management.go`。`_health_report` 無對應 indicator，自己打 raw API。各項皆唯讀；對應功能未啟用時輸出「未啟用/不適用」，非錯誤。

---

### #27 Watcher troubleshooting

- **目標**：偵測 Watcher 服務與 watch 執行狀態。
- **採集**：`GET /_watcher/stats`。
- **判定**：watcher 停用或 watch 執行失敗計數越閾值→⚠️；未使用 Watcher→標「不適用」。
- **官方文件**：https://www.elastic.co/docs/troubleshoot/elasticsearch/watcher-troubleshooting
- `tested_versions`: []

### #28 Transforms troubleshooting

- **目標**：偵測 transform 失敗/停滯。
- **採集**：`GET /_transform/_stats`，取 `state` 與 `stats`。
- **判定**：`state=failed` 或長時間無進度→⚠️/❌；未使用→「不適用」。
- **官方文件**：https://www.elastic.co/docs/troubleshoot/elasticsearch/transform-troubleshooting
- `tested_versions`: []

### #33 Monitoring troubleshooting

- **目標**：偵測 stack monitoring 收集設定是否正常。
- **採集**：`GET /_cluster/settings?include_defaults=true&filter_path=*.xpack.monitoring*`。
- **判定**：monitoring 收集關閉或設定異常→⚠️（資訊性）。
- **官方文件**：https://www.elastic.co/docs/troubleshoot/elasticsearch/monitoring-troubleshooting
- `tested_versions`: []

### #34 Upgrade deprecation warnings

- **目標**：升版前偵測 deprecation。
- **採集**：`GET /_migration/deprecations`。
- **判定**：有 `critical` 級 deprecation→❌（升版前須處理）；`warning`→⚠️。
- **建議**：依各 deprecation 的 `details`/`url` 處理。
- **官方文件**：https://www.elastic.co/docs/troubleshoot/elasticsearch/troubleshooting-upgrades
- `tested_versions`: []

### #35 Remote clusters 狀態

- **目標**：偵測 cross-cluster 連線狀態。
- **採集**：`GET /_remote/info`，取各遠端 `connected`。
- **判定**：設定的遠端 `connected=false`→⚠️/❌；無遠端→「不適用」。
- **官方文件**：https://www.elastic.co/docs/troubleshoot/elasticsearch/remote-clusters
- `tested_versions`: []
