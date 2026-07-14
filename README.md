# elk-diagnostics

ELK 系統快速診斷 CLI 工具。單一二進位、零依賴、可離線執行，涵蓋日常巡檢與故障排查兩種情境。

## 規格與架構

**實作的唯一依據在 [`docs/specs/`](./docs/specs/)。** 打開該目錄、從 [`README`](./docs/specs/README.md) 讀起即可動工。

採集層以 `_health_report`（ES 8.4+）優先，它沒做的才自己打 raw API。37 條診斷收斂為「1 個 health_report 解析器 + 4 類補洞模組 + 報告層」。

## 狀態

實作進度勾稽見 **[`docs/PROGRESS.md`](./docs/PROGRESS.md)**。

| 項目 | 狀態 |
|---|---|
| 規格（11 份，輸入→診斷→報告→平台） | ✅ 完成 |
| Phase 0 前置驗證（取真實 `_health_report` 驗顆粒度） | ✅ 核心已驗（部分項目待造壓補測，見 PROGRESS） |
| 程式實作（MVP + v0.2 + v0.3 + v0.4 診斷、規則引擎） | 🟡 進行中，詳見 PROGRESS（cobra 化、B 類加深、自動化測試仍待補） |

## 目錄結構

```
elk-diagnostics/
├── cmd/elk-diagnostics/        # CLI 進入點（check / diagnose / version）
├── internal/
│   ├── collector/              # raw API 連線 + 版本偵測 + _health_report 解析
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
