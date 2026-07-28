# spec-cli — CLI 與結束碼

**實作位置**：`cmd/`（cobra）。

## 1. 指令

| 指令 | 用途 |
|---|---|
| `elk-diagnostics check` | 全面巡檢，跑所有適用診斷，產出整份報告 |
| `elk-diagnostics diagnose --symptom <x>` | 症狀排查，跑該症狀診斷樹（見 spec-diagnose-symptoms） |
| `elk-diagnostics version` | 印工具版本 |

> `report` 不獨立成指令；輸出格式由 `--output` 控制（原骨架的 report.go 可作為 reporter 呼叫點或併入）。

## 2. Flag

**全域**：

| flag | 預設 | 說明 |
|---|---|---|
| `--config <path>` | `./config.yaml` | 設定檔 |
| `--host <url>`（可重複） | — | 覆寫 hosts |
| `--api-key` / `--username` / `--ca-cert` / `--insecure` | — | 連線（見 spec-config）；Basic Auth 未提供密碼時由終端安全詢問 |
| `--password` | — | 已棄用，僅供向下相容；正式環境不得使用 |
| `--output <fmt>` | `json` | `json` \| `html` \| `text`（text 為終端摘要，見 spec-report §5.1） |
| `-o, --output-file <path>` | stdout | 輸出檔；省略則印 stdout |
| `--rules <path>` | （內建） | 覆寫規則 YAML |
| `--timeout <sec>` | 10 | 單請求逾時 |
| `--no-color` | false | 關閉終端色彩 |
| `--log-level <lvl>` | info | error/warn/info/debug |

**check**：無額外必要 flag。
**diagnose**：`--symptom <x>`（必填）、`--index <name>`（部分症狀可縮限範圍）、`--interval <sec>`（雙取樣，用於累積計數型診斷，見 spec-performance）。

## 3. 結束碼（供排程 / CI）

| code | 意義 |
|---|---|
| `0` | 全 pass（或僅 skipped） |
| `1` | 最高為 warning |
| `2` | 存在 critical |
| `3` | 存在 unknown（有項目無法判定，且無 critical/warning） |
| `10` | 設定錯誤（缺連線資訊、設定檔解析失敗） |
| `11` | 連線/認證失敗（所有 host 不可達） |
| `20` | 工具內部錯誤 |

> code 對映 `overall_status`（見 spec-report §2）。`skipped` 不影響 code。這讓「巡檢失敗就告警」可直接靠 exit code 串排程。

## 4. 版本偵測與 fallback 行為

1. 連線後 `GET /` 取 `version.number`（bundle 模式讀 `version.json`）。
2. `>= 8.4`：採 `_health_report` 為 primary（spec-health-report）。
3. `< 8.4`：**目前不提供 raw API fallback**（2026-07-16 修訂：原規劃的全面 fallback 未實作，明文降級為待辦而非假承諾）。行為：A 類診斷全數 `skipped`（summary 註明「ES < 8.4 無 _health_report，本項不適用」）、B/C 類照跑但每條附 `version_warning`（8.4 以下未經測試）、報告頂層設 `version_notice` 全域警告。理由：假裝支援而未測試，比明說不支援更危險——這正是 VERIFICATION.md §1 的教訓。
4. 版本高於已測上限（見各診斷 `tested_versions`）：診斷照跑，報告頂層 `version_notice` 提示（不靜默）。
5. 執行期 `_health_report` 抓取失敗（版本支援但拿不到）：不中止，A 類全數 `unknown`，見 spec-resilience §1（2026-07-16 修訂）。

## 5. 範例

```bash
elk-diagnostics check --output html -o report.html --config prod.yaml
elk-diagnostics check --host https://es:9200 --api-key "$KEY"          # 輸出 JSON 到 stdout
elk-diagnostics diagnose --symptom write-bottleneck --interval 5
echo $?    # 2 = 有 critical
```

## 6. 邊界

- 互動性零：無 prompt、無確認，純一次性執行（適合排程/離線交付）。
- 不提供任何會改動叢集的 flag（無 `--fix` 之類）。

`tested_versions`: CLI 層與 ES 版本無關，不需標。
