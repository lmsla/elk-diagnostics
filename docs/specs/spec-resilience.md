# spec-resilience — 錯誤與韌性（逾時／重試／部分不可達）

**實作位置**：`internal/collector/client.go`（逾時＋重試）；`cmd/elk-diagnostics/check.go`、`diagnose.go`（部分不可達 → `unknown`）。

本工具是一次性唯讀健檢，不是常駐服務；韌性設計以「盡量交出一份可信的部分報告」為目標，而非重試到天長地久或整個中斷。

## 1. 分層失敗語意

| 失敗層級 | 例子 | 行為 |
|---|---|---|
| 連線/初始握手失敗（`GET /`，見 spec-cli §4） | 所有 `--host` 都連不上、認證錯誤 | 整個指令中止，`exit 11`（見 spec-cli §3）。無法連線就無法產出任何有意義的報告 |
| `_health_report` 抓取失敗 | 已連線，但 `_health_report` 逾時/中斷 | `check`／需要 `_health_report` 的症狀樹中止，`exit 11`。這是 A 類的地基，沒有它多數判定無法進行 |
| 個別 raw API 加深呼叫失敗 | 某台節點逾時、`_transform/_stats` 403、單一 B 類端點 404 | **不中止**、**不靜默省略**。該診斷項目仍出現在報告中，`status=unknown`，`summary` 註明抓取失敗原因（見 §3） |

第三層是本次補強的重點：舊行為是 `if x, e := client.Foo(); e == nil { results = append(...) }`——失敗時該診斷項目直接從報告消失，不算 pass 也不算 unknown，形同無痕跡遺失。這違反 spec-report §1/§2「`unknown`：資料抓取失敗，不可當 pass」的既有規則，且比「當 pass」更糟：連「有失敗」這件事本身都沒被記錄。修正後一律浮出為 `unknown` 結果。

## 2. 逾時與重試（`internal/collector/client.go`）

- **逾時**：`--timeout`（預設 10 秒，見 spec-config），套用在 `http.Client.Timeout`，涵蓋單一請求的 connect+讀body 全程。
- **重試**：`config.yaml` 的 `cluster.retries`（預設 2，即最多共 3 次嘗試）。只重試**可能是暫時性**的失敗：
  - 網路層錯誤（連線被拒、逾時、連線中斷等 `err != nil`）
  - HTTP 5xx
- **不重試**（視為永久失敗，直接返回）：
  - HTTP 4xx——含 ES 用 400 表示「無未分配 shard 可解釋」這類語意化錯誤（見 `AllocationExplain`），以及 401/403 權限問題：重試不會讓權限變好，只會拖慢一次性健檢的執行時間
- 重試間隔為固定短延遲（見程式內 `retryDelay`），不做指數退避——健檢工具追求「盡快交卷」，多次重試的總延遲需保持在使用者可接受範圍內。

## 3. 部分不可達 → `unknown`（`check.go` / `diagnose.go`）

每個 raw API 加深呼叫失敗時，改為呼叫 `unknownFrom(既有 analyzer 函式(零值), err)`：藉由呼叫該診斷項目本身的 analyzer 函式（帶零值/nil 輸入）取得正確的 `id`/`title`/`category`/`docs`（避免另建一份重複、易漂移的對照表），再覆寫 `status=unknown`、`conclusion=normal`、`summary`、`findings=[錯誤訊息]`。

- **範圍**：check 的所有非 health_report 加深診斷（thread pool／JVM／breaker／CPU／balance／mapping／ingest／data corruption／watcher／transforms／remote clusters／deprecations／monitoring／slow log／B 類的 #19/#25/#30/#24/#36/#37）。
- **不在此範圍**（維持既有的「盡力而為、內部再退化」設計，見程式註解，非本次調整標的）：
  - `#20 IndexAllocationBlocked` 逐 index 查 `allocation.enable`：單一 index 探測失敗只影響該 index 的統計，非整條診斷失敗，維持既有「盡力蒐集、無法探測的略過」邏輯。
- **不影響 overall_status 收斂規則**：沿用 spec-report §2 既有公式（`critical > warning > unknown > pass`），本次只是讓原本「消失的失敗」正確參與這個既有公式，不新增規則。

## 4. Host 故障轉移的範圍（刻意不擴大）

`--host` 可填多台，依序故障轉移，但**僅用於初始連線 `GET /`**（見 spec-cli §4）。個別 API 呼叫失敗時**不**切換到下一台 host 重試：

- 多數正式部署的 `--host` 清單背後是同一個 endpoint 的負載平衡（LB/VIP），不是互相獨立、資料不同步的叢集；請求層級切換節點的實際效益低。
- 請求層級故障轉移需要每個 collector 方法都感知多 host、且要處理「換host後前面已成功的請求要不要重打」等一致性問題，複雜度遠高於效益。
- 個別請求失敗已经有 §2 的重試機制對付暫時性問題；重試後仍失敗就如實回報 `unknown`，交由使用者判斷（見 §1 表格），比在背後偷換節點更誠實。

## 5. 邊界

- 本規格只處理「資料抓不到」，不處理「抓到但語意不明」的情況（如未知 indicator，已由 spec-health-report 的解析器容錯規則涵蓋）。
- 重試/逾時參數是全域設定，不支援個別診斷項目自訂逾時。

`tested_versions`：本規格與 ES 版本無關（純 CLI 層行為），不需標。
