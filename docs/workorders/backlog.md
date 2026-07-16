# Backlog（2026-07-16 收整）

上一輪已完成：Group A 造壓驗證（22/33 條 ✅）、health_report 失敗降級、`--output text`、
`_manifest.json`、SBOM、bundle 措辭。本檔記剩餘待辦，依優先序。

## 下一輪

| # | 項目 | 說明 | 出處 |
|---|---|---|---|
| 1 | `--redact` | bundle 含 index/node/host 名與 mapping 欄位名。做在**採集端**（collect.sh 或 bundle 後處理），客戶審的是「什麼東西離開機房」；`_manifest.json` 的 host 欄位一併處理 | spec-bundle §5.2、討論總結 §13.4 |
| 2 | 重錄 golden fixture | 從真機健康／異常兩態各錄一份完整 bundle 當 fixture，讓 golden test 覆蓋全部 24 端點（現有 fixture 缺新端點，靠 404 跳過）。collect.sh 產出即 fixture 格式，成本低 | VERIFICATION §6.2 |
| 3 | diagnose 定位決策 | 二選一：補 `--from-bundle` 支援（規格需先定「症狀樹在離線資料下哪些節點降級」），或在 README/spec 明文「diagnose 僅供可直連時使用」。決策前不再對症狀樹投資 | 2026-07-15 通盤檢視 |
| 4 | #28 findings 補 ES reason | transform failed 時把 `_stats.reason` 截斷後放入 findings，現在只有 `state=failed`，現場不可行動 | VERIFICATION §3.1 Group A 批次註記 |

## 之後（依案子需求啟動）

- **Group B 驗證**（5 條）：docker-compose 擴充 3 節點（hot spotting／unbalanced／master 穩定性／tier 遷移／角色區分磁碟）
- **Group C 驗證**（7 條）：需 esrally 等真實負載，**含招牌診斷 #16 write bottleneck**
- **#15 SLM**：查 ES 原始碼確認 `slm` indicator 變色條件（實測 4 次真實失敗仍 green）
- **ES 7.x 支援決策**：先確認客戶版本分布再決定做 fallback 或維持明文不支援（現為 A 類 skipped + 版本警告）
- 工單流程改良：改推分支開 PR，讓批次在 GitHub 留 review 紀錄
- 小項：collect.sh 的 `$HOST` 內插進 manifest JSON 未跳脫（URL 含引號會壞，機率極低）；`spec-cli` 列的 `--log-level` 未實作（決定做或刪）

## 手動驗證（使用者自行操作，2026-07-16 起）

使用者將以「造壓 → 檢測」親手走完整健檢流程。已驗過的造壓配方見
[VERIFICATION.md §5](../VERIFICATION.md)；工具端若在過程中發現缺陷，記回 VERIFICATION §1 的模式清單。
