# 退款摘要读取保护（2026-11-07）

基线 ddb8265；仍为 PR21/PR14 草稿依赖链，无合并或部署。

## 范围

`Refunds` 之前对 active primary 和 secondary 都返回各自可退额度，虽有写入保护，却与“关联期间需先撤回再修改”的限制不一致。现在在授权、原消费解析之后，填充摘要之前，于同一 SQL 事务调用严格关联保护：active 成员拒绝、损坏关系失败关闭；不映射 ID，不改原证据，不支持关联期间退款。撤回后恢复原始摘要及退款子 ID 导航。

## 回归

- `TestExpensePairEffectiveRefundSummaryRequiresWithdrawal`：owner/viewer 读取两成员均 locked，不能返回原交易 ID 或可退额度，零写入；撤回后两条恢复各自摘要；登记正常部分退款后，按原消费与退款子 ID 读取结果一致。
- `TestExpensePairEffectiveRefundSummaryDamagedDecisionFailsClosed`：成员标记损坏时两端 unavailable、无额度、零写入；移除成员权限后优先 ledger_denied，不透露关联状态。
- 新测试先红：两个场景都错误返回成功和 1234 可退额度；接入保护后定向测试及旧退款测试通过。

- 独立只读审查未发现 P1/P2。依建议把拒绝返回值收紧为完整空摘要比较，并将 Refunds 加入缺一/二/三表、active标记损坏、缺成员的统一零写入矩阵。
- 最终完整 Go race 全部通过（ledger 58.412s），vet/diff-check 通过。
- 共享测试、Web 生产、Mac 分发 fresh 构建通过（1m24s，68任务执行）；之后只增强 Go 测试断言，生产源码未变化。
- 远程 CI 尚待本提交推送后收集；真实双端验收未补齐。

## 未验收

正常 Open 非空关系门禁保留，尚无确认/撤回 HTTP/UI 与独占升级。上述为隔离合成 Store 测试，不是正常登录的 Web/Mac 操作验收；不改真实账本或共享路由。
