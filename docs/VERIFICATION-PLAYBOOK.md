# 人工真機驗證入口

驗證分成兩條獨立路線。一次只選一份 Playbook，不得混跑。

| 路線 | 驗證目的 | Playbook |
|---|---|---|
| A：Live 直連 | Binary 直接連 ES 時能否抓到真實故障 | [`VERIFICATION-LIVE-PLAYBOOK.md`](./VERIFICATION-LIVE-PLAYBOOK.md) |
| B：Bundle 客戶流程 | 客戶只執行 `collect.sh`，離線分析能否抓到故障 | [`VERIFICATION-BUNDLE-PLAYBOOK.md`](./VERIFICATION-BUNDLE-PLAYBOOK.md) |
| Multi-node | ES 8 三節點 topology／awareness／runtime 驗證 | [`VERIFICATION-MULTINODE-PLAYBOOK.md`](./VERIFICATION-MULTINODE-PLAYBOOK.md) |

所有路線都使用同一份 [`fault-scenarios.sh`](../dev/phase0/fault-scenarios.sh)。
P01～P16 僅支援 single topology；M00 之後的 M 系列僅支援 multi topology，
控制器會在執行前拒絕不相容的環境。

## 執行順序

1. 先完成 ES 8 的 Live Playbook。
2. 另開 Terminal，完成 ES 8 的 Bundle Playbook。
3. 確認 ES 8 全部通過後，分別重跑 ES 9。
4. 使用獨立 ES 8 三節點環境執行 Multi-node Playbook。
5. 將驗證結論更新到 [`VERIFICATION.md`](./VERIFICATION.md)。

## 不可混淆的邊界

- Live：故障存在時由 binary 直連診斷。
- Bundle：故障存在時只准 `collect.sh` 採集；必須先復原 ES，之後才能在分析階段執行 binary。
- 兩份 Playbook 都只適用可丟棄的內部測試叢集，禁止在客戶環境造壓。
- 客戶正式健檢流程只有 `collect.sh → bundle → 我方離線分析`，見 [`spec-bundle.md`](./specs/spec-bundle.md)。
