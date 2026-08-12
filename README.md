# elk-diagnostics

Elasticsearch 唯讀健康檢查工具：可直接連線分析，也可先由客戶執行 `collect.sh`，再在分析端離線產生報告。

## 開始使用

- 客戶操作：[`docs/交付/使用手冊.md`](./docs/交付/使用手冊.md)
- 客戶 API 審查：[`docs/交付/API清單.md`](./docs/交付/API清單.md)
- 內部人工驗證：[`docs/內部/驗證入口.md`](./docs/內部/驗證入口.md)
- 目前驗證狀態：[`docs/內部/驗證狀態.md`](./docs/內部/驗證狀態.md)
- 工程規格：[`docs/內部/規格/規格總覽.md`](./docs/內部/規格/規格總覽.md)

## 交付內容

```text
elk-diagnostics       # 分析端 binary
collect.sh            # 客戶端可審閱的 POSIX shell 採集腳本
docs/交付/            # 客戶操作手冊與 API 清單
```

`make dist` 會產生 binary、`collect.sh`、API 清單、客戶手冊與 SBOM；客戶端不必執行 binary。

## 能力邊界

- 只使用 Elasticsearch API；不透過 SSH 執行主機健檢或自動修復。
- ES 8.4+ 可使用完整 `_health_report`；部分單次快照指標仍需 Monitoring／時間序列佐證。
- `implemented` 不等於 `verified`；異常分支的真機狀態以內部驗證狀態為準。
- Bundle 可能含 index、node、host 與 mapping 欄位名稱，外傳前需審閱；`--redact` 尚未完成。

## 開發驗證

```bash
make build
go test ./...
go vet ./...
```

測試環境與故障注入只在 [`docs/內部/`](./docs/內部/) 與 `dev/phase0/`，禁止用於客戶或共用叢集。
