# spec-report — 診斷報告（最終產出）

**實作位置**：`reporter/json.go`、`reporter/html.go`；共用結果型別 `DiagnosticResult`（建議置於 `internal/diagnostic`，供所有 analyzer 產出、reporter 消費）。

報告是工具的最終交付物。所有 analyzer 不直接輸出文字，而是回傳統一的 `DiagnosticResult`；reporter 負責收斂、排序、渲染。**analyzer 與輸出格式解耦**——新增診斷項不需動 reporter，新增輸出格式不需動 analyzer。

---

## 1. 統一結果契約 `DiagnosticResult`

每一條診斷（不論 A/B/C 類）一律產出此結構：

| 欄位 | 型別 | 說明 |
|---|---|---|
| `id` | string | 規格 id，如 `cluster_health` |
| `title` | string | 中文標題 |
| `category` | enum | `cluster`/`capacity`/`data`/`management`/`performance`/`snapshot`/`node` |
| `status` | enum | `pass`/`warning`/`critical`/`skipped`/`unknown` |
| `conclusion` | enum | 三態：`normal`／`suspected`（疑似異常）／`confirmed`（已確認異常） |
| `summary` | string | 一句話結論 |
| `findings` | []string | 具體數據/證據（如「node-1 磁碟 96%」） |
| `root_causes` | []string | 根因假設（明示為假設非確認） |
| `recommendations` | []{cmd,desc} | 唯讀引導：建議手動執行的指令 + 說明 |
| `docs` | []string | 官方文件連結 |
| `source` | enum | `health_report`／`raw_api`／`fallback`（資料來源，供稽核） |
| `requires_extra` | bool | 是否因條件不足而無法判定 |
| `extra_reason` | string | 如「需開啟 slow log」「需雙取樣比對差值」 |
| `version_warning` | string | 目標版本落在 `tested_versions` 之外時的警告 |

`status` 語意：

- `pass` ✅：正常（`conclusion=normal`）。
- `warning` ⚠️：疑似異常，需佐證（`conclusion=suspected`）。
- `critical` ❌：已確認異常（`conclusion=confirmed`）。
- `skipped`：本次未執行（功能未啟用、ES<8.4 無 health_report、不適用），**不計入健康判定**。
- `unknown`：indicator 回 `unknown` 或資料抓取失敗，**不可當 pass**。

---

## 2. 整體狀態收斂規則

報告頂層 `overall_status` 取所有**非 skipped** 結果的最嚴重者：

```
critical 存在            → overall = critical
否則 warning 存在        → overall = warning
否則 unknown 存在        → overall = unknown_present（綠底但標示「有項目無法判定」）
否則全 pass              → overall = pass
```

- `skipped` 不影響 overall，但於摘要獨立列出數量。
- `unknown` **絕不**被當成 pass 而讓 overall 變綠；至少降為「綠中帶問號」並提示須人工檢查。

頂層另附計數摘要：`{pass, warning, critical, skipped, unknown}` 各幾項。

---

## 3. 報告通用結構（JSON / HTML 共用的邏輯模型）

```
報告
├── meta：工具版本、產生時間、目標叢集(host, cluster_name, es_version)、模式(check/diagnose)、症狀(diagnose 時)
│         bundle 模式另含：collected_at、collect_script_version（取自 bundle 的 _manifest.json，見 spec-bundle §4.2；
│         舊 bundle 無 manifest 時省略欄位，HTML 頁首註明「bundle 未含採集時間（舊版採集腳本）」。
│         採集時間與分析時間是兩個時點，報告必須能區分——政府/金融報告須註明「資料取自何時」）
├── overall_status + 計數摘要
├── version_notice：若 es_version 不在多數項目 tested_versions 內，全域警告
├── node_context：Nodes Stats／Info coverage、所有回應節點的 OS/process/filesystem/JVM 快照（check 模式且資料可得時）
├── results[]：DiagnosticResult 陣列（依 §5 排序）
└── disclaimer：固定免責聲明
```

固定免責聲明文案：

> 本工具提供診斷引導，非根因確認。結論基於單次唯讀快照與預設閾值，請結合現場日誌、時間序列監控與業務脈絡綜合判斷。工具僅執行唯讀操作，任何修復指令均需人工確認後手動執行。

---

## 4. JSON 格式（`--output json`）

機器可再處理的權威格式。範例（節錄）：

```json
{
  "meta": {
    "tool_version": "0.1.0",
    "generated_at": "2026-06-14T12:00:00Z",
    "cluster": { "name": "prod-es", "host": "https://es:9200", "es_version": "8.14.3" },
    "mode": "check"
  },
  "overall_status": "critical",
  "summary": { "pass": 12, "warning": 5, "critical": 2, "skipped": 3, "unknown": 1 },
  "version_notice": null,
	"node_context": {
	  "stats_coverage": { "available": true, "total": 3, "successful": 3, "failed": 0, "returned": 3 },
	  "info_coverage": { "available": true, "total": 3, "successful": 3, "failed": 0, "returned": 3 },
	  "nodes": [{ "id": "node-id", "name": "es-data-1", "roles": ["data_hot"], "os": {}, "process": {}, "filesystem": {}, "jvm": {} }]
	},
  "results": [
    {
      "id": "cluster_health",
      "title": "叢集健康狀態",
      "category": "cluster",
      "status": "critical",
      "conclusion": "confirmed",
      "summary": "叢集為 RED：存在未分配的 primary shard，部分資料不可用",
      "findings": ["status=red", "unassigned_shards=4", "unassigned_primary=1"],
      "root_causes": ["資料節點離開叢集", "allocation 設定阻擋"],
      "recommendations": [
        { "cmd": "GET _cat/shards?v=true&h=index,shard,prirep,state,unassigned.reason&s=state", "desc": "檢視未分配 shard" }
      ],
      "docs": ["https://www.elastic.co/docs/troubleshoot/elasticsearch/red-yellow-cluster-status"],
      "source": "health_report",
      "requires_extra": false,
      "extra_reason": null,
      "version_warning": null
    }
  ],
  "disclaimer": "本工具提供診斷引導，非根因確認…"
}
```

JSON 為穩定契約：欄位只增不改名；新增診斷項只是多一個 `results[]` 元素。

---

## 5. HTML 格式（`--output html`，離線可渲染）

**硬性要求**：單一 .html 檔、CSS 全內嵌、**不引用任何外部 CDN / 字型 / JS**（政府金融離線環境）。可含少量內嵌 JS 做折疊，但無網路依賴。需可列印（A4 友善）。

版面由上而下：

1. **頁首**：工具版本、產生時間、目標叢集(name/host/es_version)、模式。
2. **總狀態橫幅**：大色塊顯示 overall_status（✅綠 / ⚠️琥珀 / ❌紅 / 灰=unknown_present），右側計數摘要。
3. **版本警告區**（若有）：es_version 超出 tested_versions 的全域提示。
4. **Node Context 區塊**（資料可得時）：Stats／Info coverage、每個節點的資源摘要與可折疊 raw context；I/O、GC、CPU throttling 明示為累積值。
5. **分類區塊**：依 category 分節（叢集 / 容量 / 資料 / 管理 / 效能 / 快照 / 節點環境），節內逐項卡片：
   - 卡片標頭：狀態 badge（色彩＋圖示＋明確文字 `PASS`／`WARNING`／`CRITICAL`／`SKIPPED`／`UNKNOWN`）、`title`、`summary`、來源標記（health_report / raw / fallback）；不得只依賴色彩或圖示表達狀態。
   - 可折疊明細：findings、root_causes、recommendations（指令以等寬框、可複製）、docs 連結、version_warning。
6. **特殊項目區**：`skipped` 與 `requires_extra` 項目集中列於獨立區塊（見 §6），不混入正常判定。
7. **頁尾**：免責聲明。

色彩對應固定：pass=綠、warning=琥珀、critical=紅、skipped=灰、unknown=深灰加問號。

---

## 5.1 純文字格式（`--output text`，2026-07-16 新增）

**用途**：顧問在客戶跳板機／終端上**立即判讀**，不依賴瀏覽器。text 不是交付物——交付仍用 html（給人）與 json（給機器）；text 也**不是穩定契約**，格式可隨版本調整，任何機器處理一律走 json。

版面由上而下（全部走 stdout，或 `-o` 指定的檔案）：

```
elk-diagnostics 0.0.5 ｜ docker-cluster（ES 8.14.3）｜ check ｜ 2026-07-16T02:10:00Z
整體狀態：⚠ 注意     ✅ 20  ⚠ 3  ❌ 0  ⏭ 0  ❓ 8

❌ / ⚠ / ❓ 逐項（每項兩行）：
  ⚠ 叢集健康 / 未分配 shard — This cluster has 7 unavailable replica shards.
     └ Searches might be slower... [elkdoctor-ilmerr, elkdoctor-unhealthy]
  ❓ 叢集層級 shard 分配封鎖 — bundle 缺少該端點資料，無法判定
     └ bundle 缺少 cluster_settings.json（採集腳本未執行此項或該端點當時失敗）

✅ 通過（20）：叢集健康、Master 穩定性、磁碟容量、…（僅列標題、頓號分隔、自動換行）
⏭ 略過（2）：Watcher（未使用）、…

（version_notice 若有，緊接整體狀態列之後，黃色整行）
（免責聲明固定最後一行，暗色）
```

規則：

- **非 pass 項目**（critical → warning → unknown 排序）逐項列出：狀態符號 + `title` + `summary` 一行，`findings` 第一條縮排一行（其餘省略，明細請看 html/json）。
- **pass 與 skipped 壓縮成彙總行**，只列標題——現場判讀要的是「哪裡有事」，通過清單掃一眼確認涵蓋面即可。
- **色彩**：pass=綠、warning=黃、critical=紅、unknown=青、skipped=暗灰。僅在 stdout 為 TTY 時輸出 ANSI；`--no-color` 或環境變數 `NO_COLOR` 存在時、或 `-o` 寫檔時，一律純文字。
- 不用表格框線、不依賴等寬對齊（終端寬度不可控），縮排用空格。
- exit code 規則與其他格式完全相同（spec-cli §3）。

---

## 6. 特殊項目呈現規則

| 情況 | status | 呈現方式 |
|---|---|---|
| 功能未啟用（無 Watcher/Transform/remote） | `skipped` | 「不適用」灰標，不計入健康 |
| ES < 8.4 無 health_report | `skipped` 或走 fallback | 標示「已改用 raw API」或「略過」 |
| 需額外條件（slow log 未開、需雙取樣） | `warning` + `requires_extra=true` | 顯示 `extra_reason` 與「如何取得完整判定」，**不臆測結論** |
| 累積計數器（thread pool/breaker 等） | `warning` | summary 註明「累積值，需間隔取樣比對差值」 |
| 版本超出 tested_versions | 原 status + `version_warning` | 卡片加版本警告，並降低結論信心措辭 |
| 資料抓取失敗 | `unknown` | 標示無法判定，不當 pass |

---

## 7. check vs diagnose 報告差異

| | `check`（巡檢） | `diagnose --symptom <x>`（排查） |
|---|---|---|
| 範圍 | 所有適用項目 | 該症狀診斷樹涉及的項目 + 因果鏈（如 write-bottleneck） |
| 結構 | 完整：總狀態 + 分類區塊 + 全項明細 | 聚焦：頁首先給「症狀結論」，再列支撐該結論的逐環節證據 |
| 排序 | 依 category 分組，組內 critical→warning→pass→skipped | 依因果鏈順序（如 CPU→queue→allocated_processors） |
| overall_status | 叢集總判定 | 該症狀是否成立（confirmed/suspected/否定） |

兩者共用 §1 結果契約與 §3–4 結構，僅範圍、排序、頂層敘述不同。

---

## 8. 邊界與限制

- 報告只反映**單次執行當下**；時間序列類（master 穩定性、漸進劣化）一律標「需持續觀察」。
- HTML 不得因離線而缺樣式：CSS 必須內嵌，圖示用 Unicode/純 CSS，不用外部圖檔。
- JSON 為對外契約，版本演進採向後相容（欄位只增不改名/不刪）。

`tested_versions`: 報告層與 ES 版本無關（純組裝），不需標。
