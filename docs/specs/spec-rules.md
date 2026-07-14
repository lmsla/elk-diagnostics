# spec-rules — 閾值外部化（僅 C 類）

**實作位置**：`rules/default.yaml`（`//go:embed`）＋ `rules/rules.go`（載入/合併）。

> 放在頂層 `rules/` 而非 `internal/rules/`：Go 的 `//go:embed` 不允許用 `..` 往上層目錄取檔，
> embed 檔案必須跟寫 `//go:embed` 指令的 `.go` 檔同目錄（或其子目錄）。`default.yaml` 需要放在
> repo 根目錄讓人一眼找到，載入程式碼只能跟著放同一層。

## 1. 為什麼只有 C 類，且不是條件 DSL

37 條診斷分兩種性質：

- **A/B 類**（讀 `_health_report` 的 `status`/`diagnosis`，見 spec-health-report）：ES 自己已經
  對這些指標下過判斷（red/yellow/green、tripped、stopped…），是既定事實的轉述，**不是「數值 vs
  閾值」的問題**。這類診斷完全不經過 `rules/`，`--rules` 對它們無效。
- **C 類**（`_health_report` 無對應 indicator，自己打 raw API 手刻判定）：其中一部分是連續型指標
  （JVM 壓力、CPU、mapping 欄位數這種），本質上沒有天然斷點——連 Elastic 官方文件都只給
  「建議值」而非硬性規格上限，閾值放多少是風險容忍度的判斷，因環境而異，所以**這部分**才需要
  外部化讓不同客戶可調。
- C 類裡另一部分是布林/狀態型（watcher 是否手動停止、transform 是否 failed、remote cluster 是否
  connected……見 spec-management），跟 A/B 類一樣是有/無的判斷，同樣不需要閾值。

範圍只落在 C 類裡的**連續型指標**，數量少（目前 4 個檔案、約 11 個數字），且每個都是「單一數值
比較」，不需要條件邏輯、不需要 AND/OR、不需要多步推理（write-bottleneck 的三條件因果鏈本身仍是
Go 寫死的判斷邏輯，只有鏈中用到的三個數字閾值外部化）。**因此不做條件 DSL 求值引擎**——那是為了
「可能需要任意邏輯組合」而設計的，但實際需求只是「調幾個數字」，用 flat key-value YAML 加一個
merge 函式就足夠，不需要解析器、不需要語法驗證、不需要求值器。

## 2. default.yaml schema（flat key-value）

```yaml
performance:
  jvm_warn_pct: 85
  jvm_crit_pct: 95
  cpu_warn_pct: 85
  queue_backlog: 50
data:
  mapping_limit_default: 1000
  mapping_warn_frac: 80
  ingest_fail_warn_pct: 10
balance:
  hotspot_spread_pct: 30
write_bottleneck:
  cpu_low_pct: 50
  write_queue_min: 1
  allocated_processors_low: 2
```

每個分類對應一個 analyzer 檔案（`performance.go` / `data.go` / `balance.go` /
`write_bottleneck.go`），欄位名稱直接對應程式內原本的常數。新增 C 類連續型指標時，在對應分類下
加一個欄位，並在 `rules.Thresholds` struct 與 `Load()` 的合併清單各加一行。

## 3. 覆寫與合併（`--rules custom.yaml`）

- 只需寫**要改的欄位**；未提供的欄位沿用內建預設值。合併判斷是「override 值不為 0 才覆寫」——
  這裡每個閾值合理範圍內都不該是 0，因此以 0 代表「未提供」，不需要用 pointer 欄位分辨。
- 覆寫檔不存在、讀取失敗、或 YAML 格式錯誤 → **不 crash**：印一行警告到 stderr、其餘沿用內建
  預設值繼續執行（鐵律：零外部 YAML 也要能跑，覆寫失敗不該讓整次診斷失敗）。
- 覆寫檔含未知欄位（YAML 沒有的 key）→ `yaml.Unmarshal` 直接忽略，不視為錯誤。

## 4. 邊界

- 這份設定檔改不了「判斷邏輯」，只改數字。想改邏輯＝改 Go 程式碼＋改對應診斷規格。
- A/B 類與 C 類裡的布林/狀態型診斷不受 `--rules` 影響，即使檔案裡寫了對應欄位也不會被讀取
  （schema 裡根本沒有這些欄位）。
- 未來若新增診斷確實需要「多指標組合判斷」（不是單一數值比較），才重新評估是否要做條件 DSL；
  在此之前不預先建這個能力。

`tested_versions`：本機制與 ES 版本無關；各診斷本身的 `tested_versions` 沿用對應規格。
