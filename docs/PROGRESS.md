# elk-diagnostics 實作進度表

實作的勾稽清單。每完成一項，更新狀態並在 PR/commit 引用對應規格檔。
**狀態**：⬜ 未開始｜🟡 進行中｜✅ 完成｜⏭️ 略過（不適用）
**規格**：實作依據，見 [`specs/`](./specs/)。所有診斷項一律產出 `DiagnosticResult`（spec-report §1）。

後續 Elasticsearch 覆蓋缺口與優先級以 [`ES-COVERAGE-BACKLOG.md`](./ES-COVERAGE-BACKLOG.md) 為單一追蹤來源；本檔不複製其狀態。
本檔只回答「是否已實作」。表格內保留的日期與真機結果僅供追溯；目前驗證等級一律以
[`VERIFICATION.md`](./VERIFICATION.md) 為準。

> ⚠️ **本文的 ✅ 只代表「已實作、跑得起來」，不代表「已證明會抓到問題」。**
> 兩者是不同的事——2026-07-15 真機驗證找出 4 條結構上永遠回報綠燈的診斷，它們在本文長期標著 ✅。
> **正確性追蹤請看 [`VERIFICATION.md`](./VERIFICATION.md)**，該文只認「刻意造壓並確認正確報出異常」為驗證通過。

---

## 0. 開工關卡（Phase 0，MVP 前必過）

| 狀態 | 項目 | 規格 |
|---|---|---|
| ✅ | 備妥多版本測試叢集（docker-compose 8.14.3 / 9.0.0） | `dev/phase0/`，fixture 已產 |
| ✅ | 取真實 `_health_report` 輸出驗 diagnosis 顆粒度 | 已驗 shards_availability（足夠）、ilm（需補 explain）；**2026-07-15 造壓驗證補齊 disk/shards_capacity/repository_integrity**（見 §4「造壓驗證」記錄），顆粒度皆足夠、diagnosis/action/help_url/affected_resources 完整。slm 未能在本次手動測試中重現（見 §4 說明），列為已知殘留缺口 |
| ✅ | 顆粒度不足項目標記改走 raw API | spec-health-report「Phase 0 架構決策紀錄」：#5 ilm 須搭 `_ilm/explain`；解析器須容忍未知 indicator（9.x 多 file_settings） |

---

## 1. 地基 / 平台層

| 狀態 | 項目 | 規格 | 位置 |
|---|---|---|---|
| ✅ | `go mod init` + 切片 CLI（cobra；main.go/root.go/check.go/diagnose.go/version.go） | spec-cli | go.mod, cmd/elk-diagnostics/ |
| ✅ | 設定載入（config.yaml + env + flag 優先序、預設、驗證） | spec-config | internal/config |（真機驗證：flag 路徑出報告）
| ✅ | 連線 client（認證 basic/api_key/bearer + TLS/CA/mTLS + 多 host 故障轉移；唯讀） | spec-config | collector/client.go |（真機編譯+執行通過）
| ✅ | 版本偵測 + cluster_name（GET /）；<8.4 明文不提供 raw API fallback（A 類 skipped＋B/C 附 version_warning＋頂層 version_notice，2026-07-16） | spec-cli §4 | collector/client.go, cmd/elk-diagnostics/check.go |
| ✅ | `DiagnosticResult` 型別（統一結果契約 + 收斂 + 結束碼） | spec-report §1 | internal/diagnostic |
| ✅ | 規則引擎：default.yaml embed + flat 閾值 + 覆寫合併（僅 C 類連續型指標，範圍縮小見 spec-rules §1） | spec-rules | rules/default.yaml, rules/rules.go |（go build 驗證：預設值/覆寫合併/覆寫檔失效不 crash 三種路徑皆通過）
| ✅ | `_health_report` 解析基座（真機驗證；容忍未知 indicator） | spec-health-report | collector/health_report.go |
| ✅ | reporter：JSON（序列化 + 收斂；真機驗證） | spec-report §2,4 | reporter/json.go |
| ✅ | reporter：離線 HTML（內嵌 CSS、零 CDN、<details> 折疊、可列印） | spec-report §5 | reporter/html.go |（真機產檔驗證：9 卡、0 外部資源）
| ✅ | 結束碼對映 overall_status | spec-cli §3 | internal/diagnostic |

> 平台層已完成實作；編譯、真機與故障觸發結果不在本檔重複維護，見
> [`VERIFICATION.md`](./VERIFICATION.md)。

---

## 2. 診斷項目（37 條）

類別：A=讀 health_report indicator｜B=indicator+raw 加深｜C=手刻｜基座=health_report 本身。

### MVP

| 狀態 | # | 項目 | 類 | 規格 | analyzer |
|---|---|---|---|---|---|
| ✅ | 29 | `_health_report` 整合（基座） | 基座 | spec-health-report | collector |（真機驗證）
| ✅ | 1 | Red/Yellow cluster health | A | spec-health-report | cluster |（真機 8.14.3 驗證通過）
| ✅ | 2 | Unassigned shards 根因 | A | spec-health-report | cluster |（併入 shards_availability）
| ✅ | 3 | Watermark errors | A | spec-health-report | capacity |（已實作；驗證等級見 VERIFICATION）
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

> MVP 診斷層已完成實作。disk、shards_capacity、repository_integrity 的異常路徑後續已觸發驗證；
> SLM indicator 仍有未解條件。現況與證據統一見 VERIFICATION §3。

### v0.2

| 狀態 | # | 項目 | 類 | 規格 | analyzer |
|---|---|---|---|---|---|
| ✅ | 7 | High JVM memory pressure | C | spec-performance | performance |（真機驗證；old pool 壓力）
| ✅ | 8 | Circuit breaker errors | C | spec-performance | performance |（真機驗證；tripped 累積）
| ✅ | 9 | High CPU + hot threads | C | spec-performance | performance |（真機驗證；cat nodes + hot_threads 引導）
| ✅ | 12 | Task queue backlog | C | spec-performance | performance |（真機驗證；queue 瞬時）
| ✅ | 11 | Mapping explosion | C | spec-data | data |（真機驗證；欄位數近似計數。**2026-07-15 真機驗證抓到真 bug**：`_mapping` 未排除系統/內部 index，全新零資料的 8.14.3/9.0.0 叢集因 Kibana 自建的 `.internal.alerts-*` 上千欄位被誤判 CRITICAL；第一版修正用「排除所有 `.` 開頭」過於粗暴——data stream 的 backing index 也是 `.` 開頭（如 `.ds-logs-app-*`），客戶用 `logs-*-*`/Fleet 送資料幾乎都走 data stream，會把真實客戶資料一併濾掉，是比原本誤報更嚴重的漏判；改為只排除「`.` 開頭且非 `.ds-` 開頭」（`isSystemIndex`），真機建了真實 data stream 兩個方向都驗證過：系統 index 正確排除、data stream 塞 1017 欄位正確判定 critical。`internal/collector/data_test.go` 補回歸測試）
| ✅ | 13 | Ingest pipeline errors | C | spec-data | data |（真機驗證；failed% >10）

### v0.3

| 狀態 | # | 項目 | 類 | 規格 | analyzer |
|---|---|---|---|---|---|
| ✅ | 16 | Write bottleneck（因果鏈） | C | spec-write-bottleneck | write_bottleneck |（真機驗證；負向路徑。瓶頸觸發路徑邏輯已備、待實際故障驗）
| ✅ | 17 | Hot spotting | C | spec-performance | performance |（真機驗證；單節點正確跳過）
| ✅ | 18 | Unbalanced cluster | C | spec-performance | cluster |（真機驗證；單節點正確跳過）
| ✅ | 32 | Data corruption 偵測 | C | spec-data | data |（真機驗證；red 徵兆 + 導向查 log。2026-07-15 隨 #11 同批修正：`_cat/indices` 也排除系統 index，避免系統 index 短暫 red 被誤判客戶資料毀損）
| ✅ | 19 | Data allocation blocked | B | spec-health-report | analyzer/allocation.go |（**2026-07-15 造壓驗證抓到真 bug**：`filter_path`+`flat_settings` 語意衝突，本條長期不論實際設定為何都只回報「無封鎖」——先前記錄的「真機驗證正確判定 pass」正是這個 bug，不是正確判定。已修正並以 `allocation.enable=none` 造壓確認會正確報 critical，見 [VERIFICATION.md](./VERIFICATION.md) §3.1）
| ✅ | 20 | Index allocation blocked | B | spec-health-report | analyzer/allocation.go |（已實作；blocked 分支後續已觸發驗證，見 VERIFICATION §3.1）
| ✅ | 24 | Preferred data tier missing | B | spec-health-report | analyzer/data.go |（單元測試通過；設計為資訊性，不臆測缺 tier＝異常；2026-07-15 真機驗證：單節點叢集正確判定所有 tier 皆有節點）
| ✅ | 25 | Incomplete migration to tiers | B | spec-health-report | analyzer/management.go |（單元測試通過；單次快照只能列候選，卡住判定需重複觀測；2026-07-15 真機驗證：正確判定無遷移中 index）
| ✅ | 30 | Unstable cluster | B | spec-health-report | analyzer/cluster.go |（單元測試通過；用 master-eligible 節點數/奇偶佐證，非直接偵測選舉事件；2026-07-15 真機驗證：單節點叢集正確判定「僅 1 個 master-eligible，單點故障」為 warning）
| ✅ | 36 | Restore from snapshot 狀態 | B | spec-health-report | analyzer/snapshot.go |（單元測試通過；改用 recovery API 而非 spec 原列的 _snapshot/_status，見 spec-health-report.md 修正說明；2026-07-15 真機驗證：正確判定無進行中還原）
| ✅ | 37 | Cluster allocation 引導 | B | spec-health-report | analyzer/allocation.go |（單元測試通過；真機 fixture 驗證 decider 解析正確；僅代表性抽查 1 個 shard，非規格原定的上限 20 逐一查；2026-07-15 真機驗證：正確判定無未分配 shard 可解釋）

> 2026-07-15 的首次真機結果已被後續 2026-07-22／27 驗證取代；本檔不再保留當時的
> 「待補」快照。歷史缺陷、觸發證據與目前未驗項目統一見 VERIFICATION。

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
| ✅ | `red-cluster` | spec-diagnose-symptoms |（已實作；驗證等級見 VERIFICATION §4）
| ✅ | `write-bottleneck` | spec-diagnose-symptoms + spec-write-bottleneck |（已實作；驗證等級見 VERIFICATION §4）
| ✅ | `high-heap` | spec-diagnose-symptoms |（已實作；驗證等級見 VERIFICATION §4）
| ✅ | `ingest-lag` | spec-diagnose-symptoms |（已實作；驗證等級見 VERIFICATION §4）
| ✅ | `ilm-stuck` | spec-diagnose-symptoms |（已實作；驗證等級見 VERIFICATION §4）
| ✅ | check 反向觸發提示 | spec-diagnose-symptoms §3 |（兩個提示分支均已實作；驗證等級見 VERIFICATION §4）

---

## 4. 橫切（出貨前必備，規格待補）

| 狀態 | 項目 | 規格 |
|---|---|---|
| ✅ | C 類 analyzer（performance/balance/write_bottleneck/data/management）與規則引擎合併邏輯自動化測試 | 見各 `*_test.go`、`rules/rules_test.go` |
| 🟡 | 多版本 golden test（錄製 response → 斷言 DiagnosticResult） | `cmd/elk-diagnostics/golden_test.go`＋`dev/phase0/golden/`；對 es8-health/es8-unhealthy/es9-healthy/es9-unhealthy 4 組 Phase 0 錄製檔各跑一次完整 `check`，比對整份報告。覆蓋率受限於 Phase 0 當時錄製的端點（allocation-enable/index-settings/mapping/recovery/write-thread-pool/monitoring-setting/slowlog-setting/ilm-explain 等較新端點未錄製，對應診斷在測試中如同真機缺權限被 check 容錯跳過），已在檔頭註解與 PROGRESS 誠實記錄，非缺陷。已用刻意注入的欄位改名驗證測試真的會抓到回歸。`-update` 旗標更新 golden 檔。 |
| ✅ | 錯誤與韌性（逾時/重試/部分不可達 → unknown） | [spec-resilience.md](./specs/spec-resilience.md)；`collector/client.go`（重試：暫時性錯誤/5xx 重試、4xx 不重試，`config.yaml` 的 `cluster.retries` 首次真正接上）、`check.go`/`diagnose.go`（個別 raw API 失敗一律轉 `unknown` 結果，不再靜默消失，見 `unknownFrom`）。golden test 已隨此變更更新（原本消失的 7 項失敗現正確顯示為 unknown）；collector 層新增 4 個重試行為單元測試（含「4xx 不重試」「5xx 重試後仍失敗」情境）。host 故障轉移刻意不擴大到個別請求層級，理由見 spec §4。 |
| ✅ | `_health_report` 抓取失敗不再整份中止（2026-07-16 修訂） | spec-resilience §1/§3；`analyzer.HealthReportFetchFailed`/`HealthReportVersionUnsupported`（A 類清單單一來源導出自 `healthReportIndicators`，見 `analyzer.HealthReportIndicatorIDs`）、`check.go`（`supportsHealthReport`/`applyVersionWarning`） | 連線模式逾時/4xx/5xx、bundle 缺檔或錯誤 body：不中止，A 類全數 `unknown`，B/C 類照常執行，overall/exit code 依既有公式收斂（通常 exit 3）。ES < 8.4：A 類全數 `skipped`，B/C 附 `version_warning`，報告頂層新增 `version_notice`（JSON 欄位 + HTML 頁首黃條）。單元測試：`cmd/elk-diagnostics/resilience_test.go`（bundle 缺檔／404 錯誤 body／版本偽造／連線模式 500 四種情境 + B/C 類結果刪檔前後一致性比對）。仍會 `exit 11` 的只剩初始連線/認證失敗、bundle 目錄不存在/不可讀/缺 `version.json`。 |
| ✅ | bundle 模式 unknown 措辭與連線模式區分（2026-07-16） | spec-resilience §3；`check.go` 的 `fetchFailureSummary`（`unknownFrom` 與 A 類抓取失敗共用同一措辭函式，避免兩處各自維護一份文案） | bundle 模式：「bundle 缺少該端點資料，無法判定」；連線模式維持「資料抓取失敗，無法判定」；`findings` 兩種模式皆保留完整錯誤訊息（bundle 模式含檔名）。單元測試：`TestUnknownFromBundleWording`、`TestCheck_BundleUnknownWording`。 |
| ✅ | 安全與非功能（唯讀保證、密鑰遮蔽、單一二進位打包 OS/arch） | `cmd/elk-diagnostics/security_test.go`：`TestCheckIsReadOnly`（鎖住 collector 只送 GET，防未來誤加寫入方法）、`TestCheckDoesNotLeakSecretsOnSuccess`/`OnConnectFailure`（鎖住密碼明文與 Basic auth base64 編碼不出現在報告輸出或 stderr，已用刻意注入的洩漏驗證測試真的會抓到）。`Makefile` `dist` 目標擴充為 `linux/amd64` + `linux/arm64`（涵蓋 AWS Graviton 等 ARM 機型），各自產出 SHA256 checksum，已實測交叉編譯成功。 |
| 🟡 | 造壓驗證（disk/shards_capacity/slm/repository_integrity 異常情境、症狀樹「成立」路徑） | **2026-07-15**，在真機 es8=8.14.3 上刻意製造故障，逐一驗證。**發現並修正兩個真 bug**（皆非本次新增，是既有程式碼的既有缺陷，靠造壓才浮現）：<br>1. `#11 mapping_explosion`/`#32 data_corruption` 排除系統 index 時誤把 data stream 的 `.ds-*` backing index 也排除掉，會漏掉客戶用 data stream 送的真實資料（見上方 commit 記錄，已修正為只排除「`.` 開頭且非 `.ds-` 開頭」）。<br>2. `#19 data_allocation_blocked`/`#20 index_allocation_blocked`/`#31 search_slow_log`/`#33 monitoring` 共用的 `filter_path=**.a.b.c` 寫法對 `flat_settings=true` 回應永遠比對不到（兩種語意衝突），且 `defaults` 區塊混雜陣列/null 型別、原本整段硬解 `map[string]string` 會直接失敗又被吞掉——兩層問題疊加讓這 4 條診斷長期回報「無異常」，不論實際設定為何。已改用 `flatSettingString`（`json.RawMessage` 延遲解析）修正，`internal/collector/{allocation,slowlog,management}_test.go` 補齊回歸測試（含真機實測到的陣列/null 型別 defaults 值）。<br>**驗證通過**：disk（水位）red→紅、shards_capacity red→紅、repository_integrity（真的用 `rm -rf` 破壞底層資料夾＋寫入觸發偵測）yellow→黃、ILM 停用/ERROR step→critical、cluster 層級 allocation 封鎖→critical，且 `check` 反向觸發提示在真的 ILM ERROR 時正確跳出 `ilm-stuck`。**未能重現**：`slm`（#15）indicator——`_slm/policy/_execute` 手動觸發、累積 4 次真實失敗，indicator 仍是 green；ES 似乎把「repo 本身已損壞」的失敗歸類到 `repository_integrity`（正確跳出黃燈）而非 `slm`，觸發 `slm` 本身變色的確切條件本次未測出，列為已知缺口。write-bottleneck 因果鏈的「真的觸發」路徑（需低 CPU+真實 write queue 積壓+低 allocated_processors）在單節點閒置 dev 容器上無法可靠重現，需要真實負載工具（如 esrally）才能驗，本次未做。全程測試皆已復原叢集至乾淨基準狀態。 |
| ⬜ | 每項實作前先讀官方文件、填 `tested_versions`（鐵律，逐項執行） | specs README |

---

## 5. 里程碑彙總

| 里程碑 | 範圍 | 完成度 |
|---|---|---|
| 規格 | 15 份 specs（輸入→診斷→報告→平台） | ✅ 完成 |
| Phase 0 | 多版本驗證 health_report 顆粒度 | ✅ 核心已驗；disk/shards_capacity/repository_integrity 已造壓補測（2026-07-15），slm 未能重現（見 §4） |
| MVP | 地基 + A 類 + #6 + JSON 報告 | ✅ 完成（真機 8.14.3） |
| v0.2 | #7,8,9,12,11,13 + 離線 HTML 報告 | ✅ 完成（真機） |
| v0.3 | #16,17,18,32 | ✅ 完成（真機） |
| v0.4 | #27,28,31,33,34,35 | ✅ 完成（真機） |
| B 類加深 | #19,20,24,25,30,36,37 | ✅ 全數實作；各分支驗證等級見 VERIFICATION |
| 缺口診斷 | check 24 條 + diagnose write-bottleneck | ✅ 全數實作；異常分支驗證等級見 VERIFICATION |
| 症狀樹擴充 | red-cluster、high-heap、ingest-lag、ilm-stuck | ✅ 全數實作；觸發驗證等級見 VERIFICATION §4 |
| CLI 框架遷移 | stdlib flag → cobra（子指令 -h、--host 可重複、-o/--output-file shorthand） | ✅ 完成（exit code 契約以手動起本機二進位驗證：check --from-file、diagnose 缺 symptom=10、連線失敗=11） |
| 錯誤與韌性 | 逾時/重試接上、部分不可達→unknown | ✅ 完成，見 spec-resilience.md |
| 多版本 golden test | es8/es9 × healthy/unhealthy 4 組 | 🟡 完成，覆蓋率受限於 Phase 0 錄製範圍（已誠實記錄） |
| 安全與非功能 | 唯讀保證、密鑰遮蔽測試、multi-arch 打包 | ✅ 完成 |
| 採集/判斷分離 | 端點表單一事實來源 + `--from-bundle` 離線分析 | ✅ 39 個固定端點、52 項結果已完成 ES 8.14.3／9.0.0 Live-Bundle status parity；P01～P16 兩版本皆完成故障、採集、離線分析與復原。見 [spec-bundle.md](./specs/spec-bundle.md) 與 [VERIFICATION.md](./VERIFICATION.md) §3.6。**交付邊界**：客戶只執行可讀的 curl 腳本，binary 留在分析端 |
| 客戶交付透明層 | 採集腳本 `collect.sh` + API 清單（由端點表產生） | ✅ 完成。`apis`（text/markdown，供資安審查）與 `collect-script`（POSIX sh，純 curl）兩個子指令，皆由 `collector.Endpoints` 產生。`collect.sh`（repo 根目錄）與 `docs/api-inventory.md` 皆 checked in（`make generate` 更新，測試擋過期，故新增端點時 API 呼叫面的變動會出現在 diff），`make dist` 一併產出交付包（二進位＋collect.sh＋api-inventory.md，各附 SHA256）。真機驗證：用 dash 跑產出的腳本採集 es8，離線分析結果與直連逐條一致。腳本經 sh/dash/bash `-n` 語法檢查，並鎖住唯讀、認證不上命令列、必記 HTTP 狀態碼 |
| bundle 遮罩 | `--redact`：index/node/host 名稱 | ⬜ 未開始，見 spec-bundle §5.2 |
| `--output text` 終端摘要 | check/diagnose 皆支援；非 pass 逐項、pass/skipped 壓縮彙總、`--no-color`/`NO_COLOR`/寫檔一律純文字 | ✅ 完成，見 spec-report §5.1、reporter/text.go。不合法 `--output` 值現在回報清楚錯誤（exit 10）而非靜默退回 json |
| bundle 採集時間可追溯 | `collect.sh` 開始時寫 `_manifest.json`（collect_script_version/collected_at UTC/host/endpoints_total）；`--from-bundle` 讀出後帶進報告 meta（JSON 新增 2 個 omitempty 欄位）、HTML 頁首顯示；無 manifest 的舊 bundle 欄位省略、HTML 註明「舊版採集腳本」，不猜測 | ✅ 完成，見 spec-bundle §4.2、internal/collector/client.go、cmd/elk-diagnostics/collect.sh.tmpl |
| 多節點 Node Context | Nodes Stats／Info coverage + 所有回應節點的 OS/process/filesystem/JVM context；swap、FD、有限 cgroup memory 快照診斷 | 🟡 ES8 M00 完整 `3/3`、M03 heap drift、M08 Stats/Info partial `3/4` 已驗；ES9 多節點與跨主機仍待驗，見 spec-node-context |
| ES 單次快照覆蓋 ES-GAP-01～06 | task 壅塞、shard sizing、snapshot RPO、runtime drift、TLS／License、HA 結構 | 🟡 Collector／Analyzer／Live-Bundle 共用流程已完成；ES-GAP-04、06 已由 ES8 M 系列驗證，其他權限不足與異常分支仍待專用情境，見 ES-COVERAGE-BACKLOG |
| ES 單次快照覆蓋 ES-GAP-07～12 | indexing pressure、index block、restart／memory lock、CCR、ML、planned shutdown／voting exclusion | 🟡 實作與自動化測試完成；ES8／ES9 Live-Bundle 基準線、P16 block、ES8 restart 已真機驗。高壓、真實 CCR／ML 與維護中狀態仍待專用環境，見 ES-COVERAGE-BACKLOG |
| SBOM | `make dist` 用 CycloneDX（`cyclonedx-gomod` 版本 pin 死於 Makefile）產出 `dist/sbom.cdx.json`（module + 全部相依版本），納入既有 SHA256 清單 | ✅ 完成，見 spec-bundle §2、Makefile。導入審查清單（討論總結.md §8）最後一個缺口補齊 |
| 2026-07-15 真機驗證 | 本機 Docker es8=8.14.3/es9=9.0.0 對 check 全量 + 5 條症狀樹 | ✅ 完成；意外抓到並修正 #11/#32 系統 index 誤報 bug（見 §2） |
| 2026-07-15 造壓驗證 | disk/shards_capacity/repository_integrity/ILM/allocation 封鎖異常情境 | ✅ 完成；抓到並修正 2 個真 bug（#19/#20/#31/#33 的 filter_path+flat_settings 解析、#11/#32 的 data stream 誤排除），見 §4 |
| 2026-07-22 腳本化重驗 | ES8／ES9 × Live／Bundle × P01～P16；39 端點、52 項結果 | ✅ 故障斷言與復原流程通過；這是腳本驗收，不取代各診斷的觸發驗證等級 |
| 2026-07-27 人工 ES8 Bundle Route B | P00＋P01～P16，逐案採集、復原、HTML 與截圖 | 🟡 14 項完整通過；P05 多因子為條件式通過，P11 Basic License 為部分驗證，見 VERIFICATION §3.7 |
| 2026-07-28 ES8 三節點 M00～M01 | 3 master／3 data／3 zone、hot/warm、跨 zone replica、awareness 缺失 | ✅ Live／Bundle／restore 後 parity 通過；ES-GAP-06 第一階段升為 verified，見 VERIFICATION §3.8 |
| 2026-07-28～30 ES8 三節點 M02～M09 | tier migration、runtime drift、磁碟 hotspot、undesired shards、偶數 master、partial Nodes API、0 replica | 🟡 Live 與人工 Bundle Route B 均已完成；M02～M09 fault Bundle、HTML 與截圖齊全。M05／M08 post Bundle 為延後補採，M08 timeout 修正後真機重驗仍待完成，見 VERIFICATION §3.9 |
| 待辦 | slm indicator 觸發條件（本次造壓未重現）、write-bottleneck 因果鏈真實負載驗證（需 esrally 等造壓工具） | ⬜ 未開始 |
