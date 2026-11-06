# 验证：同一笔消费有效统计与旧写入保护（R16）

续跑日期核对：2026-11-07 CST。文件名沿用断线前草稿日期，不代表实际验证日期。

范围见[计划](../plans/expense-pair-effective-guards.md)。基线 `7850983`（PR21 草稿，base `feat/internal-transfers`）。全部使用隔离测试库和合成资料，未接触正常环境或真实账本。

## 新增测试（`server/internal/ledger/expense_pair_effective_test.go`）

| 用例 | 断言 |
|---|---|
| CountsOnceAndRestoresAfterWithdraw | 确认前 2 笔/24.68；确认后 1 笔/12.34，分类与版本一致，仅保留显式 primary；按商户搜索不暴露 secondary；viewer 看到同一结果；撤回后恢复 2 笔与原明细；原财务/来源行未改 |
| FailsClosedOnDamagedMetadata | 成员 facts、孤立成员、根版本、修订链损坏时 Dashboard 返回 `expense_pair_unavailable`，不回退原始总额 |
| DriftedMemberFailsClosed | 绕过保护直接改 secondary 后 Dashboard 拒绝 |
| GuardsOldWriters | primary/secondary 的备注分类、更正预览、更正、退款均 `expense_pair_locked` 且零写入；手动新增照常并独立计入；撤回后可编辑、可退款 |
| BlocksWholeImportUndo | 撤销预览阻塞 1、可撤销 0；执行拒绝且零写入；撤回后可正常撤销 |
| BlocksDuplicateSourceLinkCommit | 再次导入同一流水（重复）时整批拒绝；撤回后正常跳过入账 |

原 `TestExpensePairLifecycleStoreConfirmWithdrawReplayAndHistory` 的“不抑制”断言改为“当前 active 新组只计一次”。

变异检查：临时去掉成员保护与 secondary 抑制后，3 个用例变红；还原后通过。

## 检查结果

- `go -C server test -race -count=1 ./...`、`go vet ./...`、`git diff --check`：通过（ledger 58.7s）。
- `./gradlew --no-build-cache --rerun-tasks :shared:jvmTest :webApp:wasmJsBrowserDistribution :desktopApp:createDistributable`：BUILD SUCCESSFUL，1m28s，68 任务全部执行。

## 未完成

- 正常 Open 门禁保留，无 HTTP/UI，正常双端无法也未做关联验收；正常环境统计仍为原始两笔。
- 初次独立审查发现两个 P2：缺失三表被错误当作旧 schema；旧写仅信任成员 active 标记。已修复为仅明确 schema4 且无关联残留允许零表、写事务先严格加载完整关联数据；更正预览也改为单一读事务。缺表测试先红（Dashboard 错误返回成功），修复后绿。
- 新增缺一/二/三表、active=0、缺成员的矩阵，分别覆盖 Dashboard、备注、更正预览/提交、退款、重复导入、撤销/恢复，要求 unavailable 且零写入；新增混合 Commit 前序插入回滚、混合批次整批 Undo 阻塞及撤回后 Undo/Restore。定向测试通过。
- 修复后再次完整验证：Go race 全部通过（ledger 66.013s），vet/diff-check 通过；共享测试、Web 生产及 Mac 分发 fresh 构建通过（1m19s，68 任务执行）。独立只读复审确认两个 P2 均修复，未发现新增 P1/P2。
- 本轮远程 CI 尚待提交后收集，不继承基线的六项绿灯。
