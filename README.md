# elk-diagnostics

Elasticsearch 唯讀健康檢查工具：可直接連線分析，也可先由使用者執行 `collect.sh` 產生診斷資料採集包（Bundle），再在分析端離線產生報告。

## 開始使用

- 使用者操作：[`docs/交付/使用手冊.md`](./docs/交付/使用手冊.md)
- 使用者端 API 審查：[`docs/交付/API清單.md`](./docs/交付/API清單.md)
- 內部交付打包：[`docs/內部/交付打包手冊.md`](./docs/內部/交付打包手冊.md)
- 內部人工驗證：[`docs/內部/驗證入口.md`](./docs/內部/驗證入口.md)
- 目前驗證狀態：[`docs/內部/驗證狀態.md`](./docs/內部/驗證狀態.md)
- 工程規格：[`docs/內部/規格/規格總覽.md`](./docs/內部/規格/規格總覽.md)

## 交付內容

```text
elk-diagnostics       # 分析端 binary
collect.sh            # 使用者端可審閱的 POSIX shell 採集腳本
collectors/            # Route B 選配 Host／Kibana／Logstash 子採集器
docs/交付/            # 使用者操作手冊與 API 清單
```

`make dist` 會產生 binary、`collect.sh`、選配子採集器、API 清單、使用者手冊與 SBOM；使用者端不必執行 binary。

本機產物固定放在 `artifacts/bundles/`（採集包）、`artifacts/reference/`（外部比較資料）、`reports/`（報告）與 `dist/`（交付包），不堆在 repo 根目錄。

## 能力邊界

- 現行診斷分析 Elasticsearch API；Kibana bundle 可分析 `/api/status` 核心健康並保存 `/api/stats` runtime measurements。Host／Logstash 目前仍只保存原始證據。
- ES 8.4+ 可使用完整 `_health_report`；部分單次快照指標仍需 Monitoring／時間序列佐證。
- `implemented` 不等於 `verified`；異常分支的真機狀態以內部驗證狀態為準。
- 採集包中可能包含 index、node、host 與 mapping 欄位名稱，外傳前需審閱；`--redact` 尚未完成。

## 開發驗證

```bash
make build
go test ./...
go vet ./...
```

測試環境與故障注入只在 [`docs/內部/`](./docs/內部/) 與 `dev/phase0/`，禁止用於使用者環境或共用叢集。
