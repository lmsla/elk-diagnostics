# spec-bundle — 採集與判斷分離（離線 bundle 模式）

**實作位置**：`internal/collector/endpoints.go`（端點表）、`internal/collector/client.go`（`NewFromBundle`）、`cmd/elk-diagnostics/check.go`（`--from-bundle`）。

## 1. 要解決的問題

不是技術問題，是**交付問題**：工具寫得再正確，客戶只要覺得導入麻煩、直接拒絕，價值就歸零。

政府／金融客戶對「在正式環境執行一個未知二進位檔」的預設反應是走導入審查——弱掃、SBOM、資安簽核。為了一次健檢付這個成本，客戶多半會直接說不。

關鍵觀察是：`check` 做的兩件事，風險屬性完全不同。

| | 採集 | 判斷 |
|---|---|---|
| 做什麼 | 打 39 個固定唯讀 GET，存成 JSON | 讀 JSON、套規則、產報告 |
| 需要碰客戶網路 | 是 | **否** |
| 客戶看不看得懂 | 一眼就懂（就是 curl） | 看不懂 |
| 需要什麼 | **只要 curl** | Go runtime |

既然「判斷」根本不需要碰客戶環境，就不該逼客戶為它做導入審查。

## 2. 架構

```
客戶環境                                  自己的機器
─────────                                ─────────
collect.sh（純 curl，逐行可讀）
     │
     └──→  bundle/*.json  ────────────→  elk-diagnostics check --from-bundle
                                                 │
                                                 └──→ 報告（與連線模式完全相同）
```

交付流程與對應指令：

| 步驟 | 在哪 | 指令 |
|---|---|---|
| 1. 提交 API 清單給客戶資安審查 | — | [`docs/api-inventory.md`](../api-inventory.md)，或 `elk-diagnostics apis --output markdown` |
| 2. 交付採集腳本 | — | repo 根目錄的 [`collect.sh`](../../collect.sh)，或 `elk-diagnostics collect-script > collect.sh` |
| 3. 採集（客戶可自行審閱腳本後執行） | 客戶環境 | `./collect.sh -h https://es:9200 -u elastic` |
| 4. 帶回 bundle、離線分析 | 自己的機器 | `elk-diagnostics check --from-bundle <目錄>` |

`apis` 與 `collect-script` 皆由 `collector.Endpoints` 產生，不手寫維護——**一份與實作對不上的 API 清單交給客戶資安審查，比沒有還糟**。

兩份交付物皆 checked in（`collect.sh` 於 repo 根目錄、`docs/api-inventory.md` 於 docs），方便直接 review／diff／交付，沿用 golden 檔的模式：**產生檔 checked in，另以測試擋過期**。端點表或範本變動後須執行 `make generate`，否則 `TestCollectScript_CheckedInCopyIsFresh` / `TestAPIInventory_CheckedInCopyIsFresh` 會失敗。

checked in 而非只留在 `dist/`（gitignore）的理由：新增端點時，diff 會直接顯示「對客戶叢集的 API 呼叫面變了」——那是該進 code review 的訊號，不該只在打包時一閃而過。

`make dist` 會把兩者連同二進位檔一併收進 `dist/`，各附 SHA256；同時以 CycloneDX 產出本 module 的 SBOM（`dist/sbom.cdx.json`，記錄本工具與全部相依套件版本，供客戶資安做已知漏洞比對），是導入審查清單的最後一項。

**客戶從頭到尾沒有執行過本工具的二進位檔。** 導入審查的對象從「未知執行檔」降級為「一份文字檔」。

對照 `討論總結.md` §11 的流程（客戶環境取最小必要輸出 → 人工檢查 → 分析），本模式即是把「分析」那一格接上既有實作；同時也滿足 §13.3 的方案 C——客戶若連腳本都不想跑，可直接拿端點清單自行在 Kibana Dev Tools 查。

## 3. 設計約束：判斷邏輯只能有一份

**不得用 shell 重寫診斷邏輯。** 理由：

1. **jq 不是可假設的相依**。RHEL/CentOS minimal 預設沒有 jq，而那正是政府／金融的機型；要客戶裝 jq＝回到同一個導入審查問題。純 bash 啃 JSON 則是另一種災難。
2. **兩份邏輯必然漂移**。2026-07-15 已證明：Go 寫的、有註解、有單元測試，仍藏了 6 個 bug、其中 4 條永遠回報綠燈（見 [VERIFICATION.md](../VERIFICATION.md) §1）。同樣邏輯用 bash 重寫不會更好。
3. C 類診斷（JVM old pool 壓力、mapping 遞迴數欄位、hot spotting 取中位數）在 bash 裡不該存在。

因此實作上**只換傳輸層**：`Client.fetch` 是可抽換的欄位，連線模式走 HTTP、bundle 模式走讀檔，其上的 `get()`（重試、錯誤語意）與所有 analyzer 完全共用。bundle 與連線模式的差別僅止於 bytes 從哪來。

> 歷史真機驗證（2026-07-15，es8=8.14.3 健康叢集）：當時 bundle 模式與連線模式 31 條診斷判定逐條一致。2026-07-22 已用現行 39 端點在 ES 8.14.3／9.0.0 重驗 52 項結果，Live／Bundle status 完全一致，並完成 P01～P16，見 [`VERIFICATION.md`](../VERIFICATION.md) §3.6。

## 4. Bundle 格式

一個目錄，內含依 `collector.Endpoints` 命名的 JSON 檔，每個檔案是對應端點的**原始回應**（不加工、不重排），外加一份狀態清單：

```
bundle/
├── _manifest.json            # 採集中繼資料（見 §4.2，2026-07-16 新增）
├── _status.txt               # 每行 "<檔名> <HTTP 狀態碼>"
├── _errors.log               # curl 層級的錯誤（連線失敗等）
├── version.json              # GET /
├── health_report.json        # GET /_health_report
├── cluster_settings.json     # GET /_cluster/settings?include_defaults=true&flat_settings=true
├── ...
└── cat_thread_pool_write.json
```

### 4.1 `_status.txt` 為何是必要的

採集腳本**不使用 `curl -f`**，一律保留 body 並記下真實狀態碼。少了這份清單，就無法區分「正常回應」與「ES 的錯誤訊息」，而兩者都是 JSON：

- **語意化的 4xx 必須保留**：叢集健康時 `allocation/explain` 回 **400**，那個錯誤 body 正是「沒有未分配 shard」的答案。若腳本用 `-f` 丟棄它，#37 會在每個健康叢集上變成 unknown。
- **真正的 4xx 不能被當成資料**：若 `_watcher/stats` 因權限不足回 **403**，錯誤 body 被當成 200 解析後會得到零值 → 診斷回報「Watcher 運作中」。**實測確認過此假綠燈**：

  | | `_watcher/stats` 回 403 時 |
  |---|---|
  | 有 `_status.txt` | `unknown`「資料抓取失敗，無法判定」✅ |
  | 無 `_status.txt` | `pass`「Watcher 運作中」❌ 假綠燈 |

有了狀態碼，bundle 模式的 `get()` 對 4xx/5xx 的處理與連線模式完全相同，兩者行為一致。

`_status.txt` 不存在時一律視為 200，以相容於直接拿 `dev/phase0/fixtures/<cluster>/` 當 bundle 的用法；`AllocationExplain` 因此額外自行辨識錯誤 body（見 collector 內註解）。

### 4.2 `_manifest.json`：採集時間必須可追溯（2026-07-16 新增）

報告的 `generated_at` 是**分析**時間；政府／金融的健檢報告必須寫得出「資料**取自**何時」。bundle 可能在採集數天後才被分析，兩個時點不可混同。採集腳本於開始時寫入：

```json
{
  "collect_script_version": "0.0.5",
  "collected_at": "2026-07-16T02:10:00Z",
  "host": "https://es.example.local:9200",
  "endpoints_total": 33
}
```

- `collected_at` 一律 UTC（`date -u +%Y-%m-%dT%H:%M:%SZ`），取採集**開始**時間。
- `host` 只記 base URL，**絕不含帳密**（認證本來就走環境變數）。host 名稱屬於 bundle 既有的識別資訊範疇（§5.2），未來 `--redact` 一併處理。
- 產生方式與其他交付物相同：改 `collect-script` 範本 → `make generate` 同步 checked-in 的 `collect.sh`，過期測試會擋漂移。
- 分析端：`--from-bundle` 時把 `collected_at`／`collect_script_version` 帶進報告 meta（JSON 欄位只增，向後相容）；manifest 不存在（舊 bundle、fixture 直接當 bundle 用）時欄位省略，HTML 頁首註明「bundle 未含採集時間（舊版採集腳本）」，**不得**拿檔案 mtime 或目錄名猜測。

端點 → 檔名的對照由 `collector.Endpoints` 單一定義，同時供 bundle 讀取、golden test 回放、與後續的採集腳本產生使用。`dev/phase0/fixtures/<cluster>/` 即是此格式，故既有 fixture 可直接當 bundle 使用。

**collector 各方法一律使用 `endpoints.go` 的常數，不寫字面字串**——否則端點表與實際呼叫會各自演化，讓表本身變成新一輪靜默錯誤的來源。golden test 原本自帶一份端點對照副本，正是它讓 filter_path bug 逃過測試。

## 5. 已知限制

### 5.1 動態端點無法涵蓋 → #20 判 unknown

`GET /<index>/_settings`（#20 用）要查哪些 index，得先看 `health_report` 點名誰受影響，採集當下無法預知，故不在端點表中。

bundle 模式下 #20 因此判定為 **unknown 而非 pass**——查不到就說查不到。這是刻意的：把「沒查到封鎖」講成「正常」正是 2026-07-15 那批 bug 的共同模式。

> 附帶修正：此規則同時修好連線模式的一個潛在假陰性——原本若逐 index 探測因權限不足而失敗，#20 會回報「無受影響 index 需檢查（shards_availability 目前正常）」，即使 `shards_availability` 明明點名了受影響 index。舊的 golden 檔就把這句假話當成 unhealthy fixture 的預期輸出。

### 5.2 bundle 內容尚未遮罩（**未實作**）

bundle 是原始 ES 回應，包含：

- index 名稱（可能透露業務資訊，如 `transactions-taipei-branch-2026`）
- node 名稱、IP、hostname
- OS 版本、data path、mount 與 filesystem device 名稱（Node Context）
- **`_mapping` 的欄位名稱**（可能是 `national_id`、`account_balance` 這類敏感命名）

**本工具從不讀取文件內容**（全部端點皆為 `_cluster/*`、`_cat/*`、`_nodes/*`、`_mapping`），故 `討論總結.md` §14「不讀取客戶文件內容」結構上成立。但上述識別資訊確實存在，**bundle 離開客戶環境前需人工檢視**，且必須主動向客戶說明，不能略過。

`--redact`（遮罩 index／node／host 名稱）為待辦，見 [VERIFICATION.md](../VERIFICATION.md)。

### 5.3 現場拿不到即時報告

需把 bundle 帶回分析。對「健檢」屬可接受；救火情境若客戶允許直接執行二進位檔，仍可用連線模式——兩種模式並存，邏輯共用。

## 6. 邊界

- bundle 模式不改變任何判定規則、閾值或收斂邏輯（spec-report §2 照舊）。
- 缺檔一律轉 `unknown`（spec-resilience §1/§3 的既有規則），絕不因缺資料而回報 pass。
- 採集腳本不做任何判斷，只負責取得 bytes；所有判斷留在 analyzer。

`tested_versions`：bundle 層與 ES 版本無關（純檔案讀取）；各診斷沿用其原本標記。
