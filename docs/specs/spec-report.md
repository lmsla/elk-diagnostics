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
| `category` | enum | `cluster`/`capacity`/`data`/`management`/`performance`/`snapshot` |
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
├── overall_status + 計數摘要
├── version_notice：若 es_version 不在多數項目 tested_versions 內，全域警告
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
4. **分類區塊**：依 category 分節（叢集 / 容量 / 資料 / 管理 / 效能 / 快照），節內逐項卡片：
   - 卡片標頭：狀態 badge（色+✅/⚠️/❌）、`title`、`summary`、來源標記（health_report / raw / fallback）。
   - 可折疊明細：findings、root_causes、recommendations（指令以等寬框、可複製）、docs 連結、version_warning。
5. **特殊項目區**：`skipped` 與 `requires_extra` 項目集中列於獨立區塊（見 §6），不混入正常判定。
6. **頁尾**：免責聲明。

色彩對應固定：pass=綠、warning=琥珀、critical=紅、skipped=灰、unknown=深灰加問號。

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
