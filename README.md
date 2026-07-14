# elk-doctor

ELK 系統快速診斷 CLI 工具。單一二進位、零依賴、可離線執行，涵蓋日常巡檢與故障排查兩種情境。

## 規格與架構

**實作的唯一依據在 [`docs/specs/`](./docs/specs/)。** 打開該目錄、從 [`README`](./docs/specs/README.md) 讀起即可動工。

採集層以 `_health_report`（ES 8.4+）優先，它沒做的才自己打 raw API。37 條診斷收斂為「1 個 health_report 解析器 + 4 類補洞模組 + 報告層」。

## 狀態

實作進度勾稽見 **[`docs/PROGRESS.md`](./docs/PROGRESS.md)**。

| 項目 | 狀態 |
|---|---|
| 規格（11 份，輸入→診斷→報告→平台） | ✅ 完成 |
| Phase 0 前置驗證（取真實 `_health_report` 驗顆粒度） | ⬜ 動工前必做 |
| 程式實作 | ⬜ 未開始（`.go` / `go.mod` / `config.yaml` / `rules/default.yaml` 為空 placeholder） |

## 目錄結構

```
ES-diagnostics/
├── cmd/                       # root / check / diagnose / report
├── internal/
│   ├── collector/
│   │   ├── client.go          # raw API 連線 + 版本偵測 + fallback
│   │   └── health_report.go   # _health_report 解析（採集基座）
│   ├── analyzer/              # cluster / capacity / data / management /
│   │                          # performance / snapshot / write_bottleneck
│   ├── reporter/              # json.go / html.go（HTML 離線可渲染）
│   └── rules/                 # 規則引擎
├── rules/default.yaml         # //go:embed 內嵌閾值
├── config.yaml                # 連線設定（唯一必填類）
├── docs/specs/                # 規格（實作依據）
└── README.md
```

## 鐵律

只做唯讀；實作每條前先讀官方文件；`rules/default.yaml` 內嵌、零設定可跑；每條標 `tested_versions`、未測版本警告（下限 8.4）；繁中輸出。詳見 [`docs/specs/README.md`](./docs/specs/README.md)。
