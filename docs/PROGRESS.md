# elk-diagnostics 實作進度表

實作的勾稽清單。每完成一項，更新狀態並在 PR/commit 引用對應規格檔。
**狀態**：⬜ 未開始｜🟡 進行中｜✅ 完成｜⏭️ 略過（不適用）
**規格**：實作依據，見 [`specs/`](./specs/)。所有診斷項一律產出 `DiagnosticResult`（spec-report §1）。

---

## 0. 開工關卡（Phase 0，MVP 前必過）

| 狀態 | 項目 | 規格 |
|---|---|---|
| ✅ | 備妥多版本測試叢集（docker-compose 8.14.3 / 9.0.0） | `dev/phase0/`，fixture 已產 |
| 🟡 | 取真實 `_health_report` 輸出驗 diagnosis 顆粒度 | 已驗 shards_availability（足夠）、ilm（需補 explain）；disk/capacity/slm/repository 待各自實作時補測 |
| ✅ | 顆粒度不足項目標記改走 raw API | spec-health-report「Phase 0 實測結果」：#5 ilm 須搭 `_ilm/explain`；解析器須容忍未知 indicator（9.x 多 file_settings） |

---

## 1. 地基 / 平台層

| 狀態 | 項目 | 規格 | 位置 |
|---|---|---|---|
| 🟡 | `go mod init` + 切片 CLI（stdlib flag；cobra 待換） | spec-cli | go.mod, cmd/elk-diagnostics/main.go |
| ✅ | 設定載入（config.yaml + env + flag 優先序、預設、驗證） | spec-config | internal/config |（真機驗證：flag 路徑出報告）
| ✅ | 連線 client（認證 basic/api_key/bearer + TLS/CA/mTLS + 多 host 故障轉移；唯讀） | spec-config | collector/client.go |（真機編譯+執行通過）
| ✅ | 版本偵測 + cluster_name（GET /）；<8.4 fallback 分支待補 | spec-cli §4 | collector/client.go |
| ✅ | `DiagnosticResult` 型別（統一結果契約 + 收斂 + 結束碼） | spec-report §1 | internal/diagnostic |
| ✅ | 規則引擎：default.yaml embed + flat 閾值 + 覆寫合併（僅 C 類連續型指標，範圍縮小見 spec-rules §1） | spec-rules | rules/default.yaml, rules/rules.go |（go build 驗證：預設值/覆寫合併/覆寫檔失效不 crash 三種路徑皆通過）
| ✅ | `_health_report` 解析基座（真機驗證；容忍未知 indicator） | spec-health-report | collector/health_report.go |
| ✅ | reporter：JSON（序列化 + 收斂；真機驗證） | spec-report §2,4 | reporter/json.go |
| ✅ | reporter：離線 HTML（內嵌 CSS、零 CDN、<details> 折疊、可列印） | spec-report §5 | reporter/html.go |（真機產檔驗證：9 卡、0 外部資源）
| ✅ | 結束碼對映 overall_status | spec-cli §3 | internal/diagnostic |

> 切片狀態：程式已寫，沙箱無 Go 工具鏈未編譯；邏輯已用真實 fixture 重播驗證（green→exit0、yellow→exit1，根因/受影響 index/建議正確）。待本機 `go build` 確認編譯 + 連 8.14 實跑。

---

## 2. 診斷項目（37 條）

類別：A=讀 health_report indicator｜B=indicator+raw 加深｜C=手刻｜基座=health_report 本身。

### MVP

| 狀態 | # | 項目 | 類 | 規格 | analyzer |
|---|---|---|---|---|---|
| ✅ | 29 | `_health_report` 整合（基座） | 基座 | spec-health-report | collector |（真機驗證）
| ✅ | 1 | Red/Yellow cluster health | A | spec-health-report | cluster |（真機 8.14.3 驗證通過）
| ✅ | 2 | Unassigned shards 根因 | A | spec-health-report | cluster |（併入 shards_availability）
| ✅ | 3 | Watermark errors | A | spec-health-report | capacity |（disk indicator，green 已驗；異常情境待補測）
| ✅ | 4 | Data nodes out of disk | A | spec-health-report | capacity |（disk indicator）
| ✅ | 5 | ILM stopped / errors | A→raw | spec-health-report | management |（ilm_status+explain，真機驗證）
| ✅ | 10 | Shard capacity issues | A | spec-health-report | capacity |（shards_capacity）
| ✅ | 14 | Master/Other nodes out of disk | A | spec-health-report | capacity |（disk indicator）
| ✅ | 15 | Snapshot policy failures (SLM) | A | spec-health-report | snapshot |（slm indicator）
| ✅ | 21 | Not enough nodes for replica | A | spec-health-report | cluster |（shards_availability）
| ✅ | 22 | Shards per index exceeded | A | spec-health-report | capacity |（shards_capacity）
| ✅ | 23 | Shards per node exceeded | A | spec-health-report | capacity |（shards_capacity）
| ✅ | 26 | Broken snapshot repositories | A | spec-health-report | snapshot |（repository_integrity）
| ✅ | 6 | Rejected requests（thread pool） | C | spec-performance | performance |（真機驗證）

> MVP 診斷層真機驗證通過（8.14.3）：9 條結果、overall=warning、exit=1，編譯零錯誤。
> 待補測（非阻斷）：disk / shards_capacity / slm / repository_integrity 這次皆 green，其「非 green 時的 diagnosis 顆粒度」尚未觀察，建議用 seed 製造該類故障再驗一次。

### v0.2

| 狀態 | # | 項目 | 類 | 規格 | analyzer |
|---|---|---|---|---|---|
| ✅ | 7 | High JVM memory pressure | C | spec-performance | performance |（真機驗證；old pool 壓力）
| ✅ | 8 | Circuit breaker errors | C | spec-performance | performance |（真機驗證；tripped 累積）
| ✅ | 9 | High CPU + hot threads | C | spec-performance | performance |（真機驗證；cat nodes + hot_threads 引導）
| ✅ | 12 | Task queue backlog | C | spec-performance | performance |（真機驗證；queue 瞬時）
| ✅ | 11 | Mapping explosion | C | spec-data | data |（真機驗證；欄位數近似計數）
| ✅ | 13 | Ingest pipeline errors | C | spec-data | data |（真機驗證；failed% >10）

### v0.3

| 狀態 | # | 項目 | 類 | 規格 | analyzer |
|---|---|---|---|---|---|
| ✅ | 16 | Write bottleneck（因果鏈） | C | spec-write-bottleneck | write_bottleneck |（真機驗證；負向路徑。瓶頸觸發路徑邏輯已備、待實際故障驗）
| ✅ | 17 | Hot spotting | C | spec-performance | performance |（真機驗證；單節點正確跳過）
| ✅ | 18 | Unbalanced cluster | C | spec-performance | cluster |（真機驗證；單節點正確跳過）
| ✅ | 32 | Data corruption 偵測 | C | spec-data | data |（真機驗證；red 徵兆 + 導向查 log）
| 🟡 | 19 | Data allocation blocked | B | spec-health-report | analyzer/allocation.go |（單元測試通過；真機非阻塞情境待驗）
| 🟡 | 20 | Index allocation blocked | B | spec-health-report | analyzer/allocation.go |（單元測試通過；真機非阻塞情境待驗）
| 🟡 | 24 | Preferred data tier missing | B | spec-health-report | analyzer/data.go |（單元測試通過；設計為資訊性，不臆測缺 tier＝異常）
| 🟡 | 25 | Incomplete migration to tiers | B | spec-health-report | analyzer/management.go |（單元測試通過；單次快照只能列候選，卡住判定需重複觀測）
| 🟡 | 30 | Unstable cluster | B | spec-health-report | analyzer/cluster.go |（單元測試通過；用 master-eligible 節點數/奇偶佐證，非直接偵測選舉事件）
| 🟡 | 36 | Restore from snapshot 狀態 | B | spec-health-report | analyzer/snapshot.go |（單元測試通過；改用 recovery API 而非 spec 原列的 _snapshot/_status，見 spec-health-report.md 修正說明）
| 🟡 | 37 | Cluster allocation 引導 | B | spec-health-report | analyzer/allocation.go |（單元測試通過；真機 fixture 驗證 decider 解析正確；僅代表性抽查 1 個 shard，非規格原定的上限 20 逐一查）

> B 類 7 條皆用 httptest/synthetic 資料 + 既有真機 fixture 完成單元測試（collector 解析 + analyzer 判定），但尚未接上真實 8.14.3/9.0.0 叢集做端到端真機驗證（沙箱無可連線叢集）；下次有真機時應比照 MVP 補驗。

### v0.4

| 狀態 | # | 項目 | 類 | 規格 | analyzer |
|---|---|---|---|---|---|
| ✅ | 27 | Watcher troubleshooting | C | spec-management | management |（真機驗證；manually_stopped）
| ✅ | 28 | Transforms troubleshooting | C | spec-management | management |（真機驗證；failed state）
| ✅ | 31 | Search slow log 分析 | C | spec-performance | performance |（真機驗證；引導型，偵測+給開啟方式）
| ✅ | 33 | Monitoring troubleshooting | C | spec-management | management |（真機驗證；收集啟用狀態）
| ✅ | 34 | Upgrade deprecation warnings | C | spec-management | management |（真機驗證；critical/warning 計數）
| ✅ | 35 | Remote clusters 狀態 | C | spec-management | management |（真機驗證；connected）

---

## 3. 症狀診斷樹

| 狀態 | symptom | 規格 |
|---|---|---|
| ⬜ | `red-cluster` | spec-diagnose-symptoms |
| ✅ | `write-bottleneck` | spec-diagnose-symptoms + spec-write-bottleneck |（真機驗證；diagnose 指令 + 因果鏈）
| ⬜ | `high-heap` | spec-diagnose-symptoms |
| ⬜ | `ingest-lag` | spec-diagnose-symptoms |
| ⬜ | `ilm-stuck` | spec-diagnose-symptoms |
| ⬜ | check 反向觸發提示 | spec-diagnose-symptoms §3 |

---

## 4. 橫切（出貨前必備，規格待補）

| 狀態 | 項目 | 規格 |
|---|---|---|
| ⬜ | 多版本 golden test（錄製 response → 斷言 DiagnosticResult） | ⚠️ 待補 spec |
| ⬜ | 錯誤與韌性（逾時/重試/部分不可達 → unknown） | ⚠️ 待補 spec |
| ⬜ | 安全與非功能（唯讀保證、密鑰遮蔽、單一二進位打包 OS/arch） | ⚠️ 待補 spec |
| ⬜ | 每項實作前先讀官方文件、填 `tested_versions`（鐵律，逐項執行） | specs README |

---

## 5. 里程碑彙總

| 里程碑 | 範圍 | 完成度 |
|---|---|---|
| 規格 | 11 份 specs（輸入→診斷→報告→平台） | ✅ 完成 |
| Phase 0 | 多版本驗證 health_report 顆粒度 | ✅ 核心已驗（disk/capacity/slm/repo 待造壓補測） |
| MVP | 地基 + A 類 + #6 + JSON 報告 | ✅ 完成（真機 8.14.3） |
| v0.2 | #7,8,9,12,11,13 + 離線 HTML 報告 | ✅ 完成（真機） |
| v0.3 | #16,17,18,32 | ✅ 完成（真機） |
| v0.4 | #27,28,31,33,34,35 | ✅ 完成（真機） |
| B 類加深 | #19,20,24,25,30,36,37 | 🟡 單元測試完成，待真機端到端驗證 |
| 缺口診斷 | check 24 條 + diagnose write-bottleneck | ✅ 全數真機驗證 |
| 待辦 | cobra、其餘症狀樹、B 類真機驗證、造壓驗證、韌性/打包 | ⬜ 未開始 |
