# elk-diagnostics

ELK 系統快速診斷 CLI 工具。單一二進位、零依賴、可離線執行，涵蓋日常巡檢與故障排查兩種情境。

## 開始使用

正式操作請先閱讀 **[`docs/USER-GUIDE.md`](./docs/USER-GUIDE.md)**。內部故障注入與真機驗證另見 [`docs/VERIFICATION-PLAYBOOK.md`](./docs/VERIFICATION-PLAYBOOK.md)，不要混用。

## 規格與架構

**實作的唯一依據在 [`docs/specs/`](./docs/specs/)。** 打開該目錄、從 [`README`](./docs/specs/README.md) 讀起即可動工。

採集層以 `_health_report`（ES 8.4+）優先，它沒做的才自己打 raw API；另以 Nodes APIs 保留所有回應節點的 OS／process／filesystem／JVM context，partial response 一律浮出。

## 狀態

各文件只維護一種狀態，避免重複敘述再次漂移：

| 文件 | 唯一責任 |
|---|---|
| [`docs/PROGRESS.md`](./docs/PROGRESS.md) | 程式與交付物是否已實作 |
| [`docs/VERIFICATION.md`](./docs/VERIFICATION.md) | 真機故障觸發、正常路徑與能力邊界 |
| [`docs/ES-COVERAGE-BACKLOG.md`](./docs/ES-COVERAGE-BACKLOG.md) | ES 後續覆蓋缺口及 `implemented`／`verified` 狀態 |
| [`docs/specs/`](./docs/specs/) | 需求與判定契約，不維護目前驗證進度 |

人工真機驗證從 **[`docs/VERIFICATION-PLAYBOOK.md`](./docs/VERIFICATION-PLAYBOOK.md)** 開始。

| 項目 | 狀態 |
|---|---|
| 規格（15 份，輸入→診斷→報告→平台） | ✅ 完成 |
| 程式實作（原 37 條診斷、ES-GAP-01～12、CLI、JSON/HTML、Live/Bundle） | ✅ 完成；目前基準報告輸出 52 項結果 |
| ES 8.14.3／9.0.0 Live-Bundle 基準線 parity | ✅ 39 個固定端點、52 項 status 一致 |
| P01～P16 最新人工 ES8 Bundle Route B | 🟡 14 項完整通過；P05 條件式、P11 部分驗證 |
| ES8 三節點 M00～M09 | 🟡 Live 與人工 Bundle Route B 已完成；M05／M08 post Bundle 為延後補採，M08 timeout 修正後真機重驗仍待完成 |
| 異常分支收斂 | 🟡 ES9／跨主機多節點、真實 CPU／heap 負載、CCR／ML／維護情境仍待驗 |

## 目錄結構

```
elk-diagnostics/
├── cmd/elk-diagnostics/        # CLI 進入點（check / diagnose / version）
├── internal/
│   ├── collector/              # raw API 連線 + 版本偵測 + _health_report 解析
│   ├── nodecontext/            # 多節點 OS/process/filesystem/JVM 領域模型
│   ├── analyzer/                # cluster / capacity / data / management /
│   │                            # performance / snapshot / write_bottleneck
│   ├── config/                  # 連線設定載入（flag > env > config.yaml > 預設）
│   ├── diagnostic/              # DiagnosticResult 契約
│   └── reporter/                # json.go / html.go（HTML 離線可渲染）
├── rules/                       # 規則引擎：default.yaml（//go:embed）+ rules.go
│                                 # 僅外部化 C 類連續型指標的閾值，見 spec-rules
├── config.yaml                  # 連線設定（唯一必填類）
├── docs/specs/                  # 規格（實作依據）
└── README.md
```

## 未來方向

目前僅涵蓋 Elasticsearch。Logstash、Kibana 診斷是可能的擴展方向，但尚未排入開發日程——
兩者都沒有類似 `_health_report` 的整合式健檢端點，屬於需要另起 collector/analyzer 的獨立工作量，
會視實際案子需求評估是否啟動。

## 鐵律

只做唯讀；實作每條前先讀官方文件；`rules/default.yaml` 內嵌、零設定可跑；每條標 `tested_versions`、未測版本警告（下限 8.4）；繁中輸出。詳見 [`docs/specs/README.md`](./docs/specs/README.md)。
