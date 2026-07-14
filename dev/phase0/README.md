# Phase 0 — 驗證 `_health_report` 顆粒度

目的：在寫任何 analyzer 之前，用**真實 ES 輸出**回答唯一未消除的架構問題——
**`_health_report` 各 indicator 的 `diagnosis` 是否足以取代 A 類逐 API 判斷？**
順便產出測試用 fixture。需求：Docker、`curl`、`jq`。

## 步驟

```bash
cd dev/phase0
cp .env.example .env          # 視需要調整版本 tag
docker compose up -d          # 起 es8（:9208）與 es9（:9209）
# 等到 healthy（約 30–60 秒）
docker compose ps

# 1) 健康狀態下擷取
./capture.sh http://localhost:9208 es8-healthy
./capture.sh http://localhost:9209 es9-healthy

# 2) 製造異常，讓 indicator 吐出 diagnosis（重點）
./seed-unhealthy.sh http://localhost:9208
sleep 10
./capture.sh http://localhost:9208 es8-unhealthy
./seed-unhealthy.sh http://localhost:9209
sleep 10
./capture.sh http://localhost:9209 es9-unhealthy

docker compose down -v        # 收工清除
```

fixture 會存到 `dev/phase0/fixtures/<label>/`，capture 結束會印出 health_report 涵蓋率摘要。

## 驗證檢查表（人工判讀 unhealthy fixture）

逐一檢視 `fixtures/*-unhealthy/health_report.json`，對每個 A 類 indicator 回答：

| indicator | A 類項目 | 要確認 | 結論 |
|---|---|---|---|
| `shards_availability` | #1,2,21 | diagnosis 是否點出未分配根因（足以取代逐 shard allocation/explain？） | ☐ 夠 / ☐ 需 raw 加深 |
| `disk` | #3,4,14 | 是否能定位到**哪個節點**、處於哪段水位 | ☐ 夠 / ☐ 需 `_nodes/stats/fs` |
| `shards_capacity` | #10,22,23 | 是否給出當前/上限數值 | ☐ 夠 / ☐ 需 raw |
| `ilm` | #5 | 是否點出 ERROR step 與 index | ☐ 夠 / ☐ 需 `_ilm/explain` |
| `slm` | #15 | 是否反映失敗原因 | ☐ 夠 / ☐ 需 `_slm/policy` |
| `repository_integrity` | #26 | 損壞類型是否明確 | ☐ 夠 / ☐ 需 raw |

判讀結果回填到 `docs/specs/spec-health-report.md` 各條：標明 primary 真的夠，或降級為「primary 偵測 + raw 加深」。同時比對 es8 vs es9 的**欄位結構差異**，回填各條 `tested_versions` 與差異註記。

## 產出

1. `fixtures/`：餵 golden test 的真實回應（spec-report §1 的 `DiagnosticResult` 斷言用）。
2. 上表結論：確認或修正 health_report-first 架構。
3. 版本差異筆記：8.x vs 9.x 的欄位落差。

完成後，PROGRESS.md 的「Phase 0」三項即可打勾，進入第二步（一條到底的垂直切片）。

## Kibana 視覺化對照

docker-compose 內含對應版本的 Kibana，用來與 elk-diagnostics 的判斷交叉核對。

| ES | Kibana |
|---|---|
| es8 `http://localhost:9208` | kibana8 `http://localhost:5601` |
| es9 `http://localhost:9209` | kibana9 `http://localhost:5602` |

```bash
docker compose up -d es8 kibana8   # 只起 es8 + 對應 Kibana（省資源）
# Kibana 首次啟動約 1–2 分鐘，就緒後開 http://localhost:5601（security 關閉，免登入）
```

對照用法：

- **Dev Tools → Console**：手打 `GET _health_report`、`GET _cluster/health`、`GET _cat/thread_pool` 等，對照 elk-diagnostics 的 JSON 結果是否一致。
- **Stack Management → Index Management / Data Streams**：對照 shards / mapping / ILM 判斷。
- **Stack Monitoring**（若啟用）：看 JVM / CPU / thread pool 趨勢，對照 performance 群診斷。

注意：Kibana 會建立 `.kibana*` 等系統 index，之後 `check` 的 mapping / shards_capacity / cat_indices 會多出這些系統 index，屬正常，非工具誤判。

## 注意

- security 已關閉，僅限本機。連有防護的真實叢集時，用 `ES_USER/ES_PASS` 或 `ES_API_KEY` + `CA_CERT` 環境變數（見 capture.sh 開頭）。
- `seed-unhealthy.sh` 是唯一有寫入的腳本，**只對本機測試叢集執行**。
- Kibana 版本必須與其連接的 ES 完全一致（compose 已用同一版本變數綁定）。
