# spec-data — 資料與索引（C 類缺口）

**實作位置**：`data.go`。`_health_report` 無對應 indicator，自己打 raw API。

---

### #11 Mapping explosion

- **目標**：偵測 mapping 欄位數爆炸（拖垮 heap 與叢集狀態）。
- **採集**：`GET <index>/_mapping`，統計欄位數；`GET <index>/_settings` 取 `index.mapping.total_fields.limit`。
- **判定**：欄位數逼近或超過 `total_fields.limit`（比例入 rules，預設 80%）→⚠️；已報錯→❌。
- **建議**：關閉動態 mapping、用 `flattened` 型別、拆 index、調 limit（治標）。
- **限制**：大型 mapping 抓取成本高，須限制掃描 index 範圍。
- **官方文件**：https://www.elastic.co/docs/troubleshoot/elasticsearch/mapping-explosion
- `tested_versions`: []

### #13 Ingest pipeline errors

- **目標**：偵測 ingest pipeline 處理失敗。
- **採集**：`GET /_nodes/stats/ingest`，取各 pipeline `failed` 計數（累積值）。
- **判定**：`failed>0`→⚠️（累積，需差值佐證）；持續成長→❌。
- **建議**：定位失敗 pipeline 與 processor；檢查 `on_failure` 設計。
- **官方文件**：https://www.elastic.co/docs/troubleshoot/elasticsearch/troubleshoot-ingest-pipelines
- `tested_versions`: []

### #32 Data corruption 偵測

- **目標**：偵測 index 異常狀態（corruption 徵兆）。
- **採集**：`GET _cat/indices?h=index,health,status`。
- **判定**：`status=close` 非預期、或 health `red` 且非分配問題→⚠️ 並提示。**checksum 級驗證屬 ES 內部機制，工具不做**，僅偵測狀態異常。
- **建議**：對疑似 index 查 allocation/explain 與節點日誌；必要時 restore from snapshot。
- **限制**：工具無法確認真正 corruption，只能標徵兆。
- **官方文件**：https://www.elastic.co/docs/troubleshoot/elasticsearch/corruption-troubleshooting
- `tested_versions`: []
