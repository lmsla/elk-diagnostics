# spec-rules — 規則引擎（閾值外部化）

**實作位置**：`rules/default.yaml`（`//go:embed` 內嵌）＋ `internal/rules/rules.go`（載入/合併/求值）。

## 1. 職責邊界（關鍵）

YAML **只外部化「數值閾值、嚴重度、訊息」**，讓不同客戶環境可調參而不改程式碼。
**判斷邏輯本身留在 Go**——複雜因果（如 #16 write-bottleneck 三條件連鎖、allocation explain 的 decider 分類）不放進 YAML，YAML 不是程式語言。

界線：能用「某指標 比較 某閾值 → 給某嚴重度」表達的，放 YAML；需要多步推理的，Go 寫死、YAML 僅供其引用的閾值。

## 2. default.yaml schema

```yaml
version: 1
rules:
  - id: cluster_health            # 對映 DiagnosticResult.id（一一對應）
    category: cluster
    params:                       # 可調閾值，供 conditions 引用
      explain_limit: 20
    conditions:                   # 依序求值，第一個命中者決定 status
      - when: "status == 'red'"
        severity: critical        # → DiagnosticResult.status / conclusion
        message: "叢集為 RED：存在未分配的 primary shard，部分資料不可用"
      - when: "status == 'yellow'"
        severity: warning
        message: "叢集為 YELLOW：replica 未分配，冗餘下降"
    remediation: "執行 unassigned shards 診斷取得逐 shard 根因"
    docs: "https://www.elastic.co/docs/troubleshoot/elasticsearch/red-yellow-cluster-status"
    tested_versions: []

  - id: watermark_errors
    category: capacity
    params:
      low_pct: 85
      high_pct: 90
      flood_stage_pct: 95
    conditions:
      - when: "node.disk_used_percent >= flood_stage_pct"
        severity: critical
        message: "節點磁碟達 flood-stage，index 將被設為唯讀"
      - when: "node.disk_used_percent >= high_pct"
        severity: warning
        message: "節點磁碟達 high watermark，shard 正被搬離"
      - when: "node.disk_used_percent >= low_pct"
        severity: warning
        message: "節點磁碟達 low watermark"
    remediation: "擴磁碟 / 刪無用 index / 套用 ILM"
    docs: "https://www.elastic.co/docs/troubleshoot/elasticsearch/fix-watermark-errors"
    tested_versions: []
```

`severity` → `DiagnosticResult`：`info`→pass/normal、`warning`→warning/suspected、`critical`→critical/confirmed。
無 condition 命中 → `status=pass`。

## 3. 條件運算式語法（最小 DSL）

支援且僅支援：

- 比較：`==` `!=` `>` `>=` `<` `<=`
- 布林：`&&` `||`、括號
- 運算元：左為**變數**（見 §4 命名空間）或 `params.*`，右為常數或 `params.*`
- 字串以單引號；數字裸寫

不支援函式、迴圈、算術運算（保持可預測、可靜態驗證）。超出此範圍的判斷一律由 Go 完成。

## 4. 變數命名空間

每條診斷向規則引擎曝露一組**具名指標**，conditions 只能引用這些。各指標定義於對應診斷規格，例如：

| 診斷 | 曝露變數（範例） |
|---|---|
| cluster_health | `status`、`unassigned_shards`、`unassigned_primary` |
| watermark_errors | `node.disk_used_percent`（逐節點求值） |
| rejected_requests | `pool.name`、`pool.rejected`、`pool.completed`、`sampling.delta_rejected` |
| jvm_pressure | `node.heap_used_percent` |

`node.*` / `pool.*` 前綴表示**逐節點 / 逐 pool 求值**，引擎對集合中每個元素套用條件，產生多筆 finding。

## 5. 覆寫與合併（`--rules custom.yaml`）

- 以 `id` 為鍵合併到內建 default。
- 外部只需寫**要改的欄位**（如某客戶把 `flood_stage_pct` 調為 90）；未提供欄位 fallback 內建預設。
- 外部 YAML 不完整、含未知 id、或語法錯 → **不 crash**：跳過該條並於報告 meta 記一筆警告，其餘照常。
- 內建 default 必須能獨立運行（鐵律：零外部 YAML 可跑）。

## 6. 邊界

- YAML 改不了「判斷邏輯」，只改數字/訊息/嚴重度。想改邏輯＝改 Go ＋改規格書。
- `version: 1` 為 schema 版本，未來不相容變更時遞增。

`tested_versions`: 引擎本身與 ES 版本無關；各 rule 的 `tested_versions` 沿用對應診斷。
