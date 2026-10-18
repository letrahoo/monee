# M11：政策中立的支出分录事实校验

## 范围

本文描述4d57251开始的政策中立单笔facts层；本分支另新增[用户限定范围下的pair假设毛额投影](association-expense-pair-projection.md)。二者仍未接入业务入口，不将hypothetical计算当写许可。

本片从已合并main `72f2a37` 独立实现纯Go内部函数，作为后续决策层可用的结构校验基础。**单笔facts本身不是PR19推荐方案的政策批准，不实现可提交的决策planner/effects、同事件确认、统计扣减或撤回；新增pair假设计算遵循用户另已选定的D4范围，但不批准其余未决条件。** 不隐式依赖PR18客户端、PR17源冻结或PR14/schema4；不新增DB/Store/API/UI/DDL入口，不修改现有insert/Correct/Refund规则。

函数只证明调用方**给定输入内部一致**，不能证明数据确实存在于数据库、来自当前授权请求、属于同一读取快照、仍然active或对应客户端选中target。未来Store必须在自己的授权事务内按请求目标取可信tx/posting/account，不得接收客户端自报这些facts后据此授权。普通来源标签、记账账户UUID均不证明现实银行/钱包身份。

## 最小输入与输出

输入仅含期望ledger、交易ledger/ID/version/kind/currency/canonical signed minor及其SQLite storage class/driver raw value，以及分录的ID/所属交易ledger+ID/实际account ID和账户ledger+kind/币种/storage class/raw金额。

main的postings没有ledger_id列；分录的TransactionLedgerID必须由其transactions JOIN上下文取得，AccountLedgerID/Kind来自同一个实际被引用account。不能从Source、Account或分类显示名补这些值。

输入刻意不含Date、RefundOf、Source、账户名称、分类文字、备注、deletedAt、退款/更正历史或来源原文。因此成功不证明完整交易schema、活跃性、无更正/无退款、来源普通性、freshness、同一经济事件或关联eligibility。

输出只有ledger/tx ID、版本、有符号整数金额、fund posting/account ID与expense posting/account ID八个值；无slice/map/raw引用、动作、允许确认、选primary、delta、角色或置信度。失败返回零值facts与固定类型/错误码/文案，不回显输入。

## 结构规则

1. 期望和给定ledger相同；各不透明ID是canonical非空UTF-8且无控制字符，不做trim修复或角色解释；version为正，kind仅expense、currency仅CNY。其他合法交易类型不属于本函数范围，不等于它们在系统中非法。
2. 交易storage class必须integer、raw动态类型必须int64，金额在-1000000000000..-1，字符串必须与FormatInt完全一致。拒绝REAL、文本、blob、nil、命名int、额外精度/非canonical/溢出。
3. 恰好两条、不同posting IDs、不同account IDs；各posting属于同一交易/账本，实际引用account也属于该账本，币种一致。
4. 一条clearing金额等于交易负值，另一条expense金额等于该已限范围负值的相反数。不能只测sum0：平衡但金额不等交易也无效。先限交易范围后才取负，不对任意leg求和或取负，避免MinInt64/overflow。
5. 顺序无关，无副作用；不猜account名、不查refund/correct历史。合法更正/分类注释后的当前两分录只要一致，应通过；退款父交易、软删交易、旧coherent快照也可能通过，因为这些资格/新鲜度事实不在输入中。

## 验证与后续

纯单元矩阵覆盖边界、raw/storage类型、数量/ID/role/ledger/金额、零值失败、静态错误、输入不变与输出字段。临时库测试适配器在单只读事务中实际JOIN并读取typeof/raw值，验证Create/Correct/Annotate当前结构和实际破坏场景；它只在_test.go，不是生产授权适配器。

尽管新helper未接用户入口，仍执行全Go race/vet、共享测试、Web生产和Mac分发。共同TLS/CI两文件明确复用已审e387测试补丁，不引入其生产功能，也不放宽TLS/timeout安全断言。

后续关联服务仍须自行完成[PR19决策合同](https://github.com/letrahoo/monee/pull/19)中的产品/迁移/统一财务投影/全写入口guard与正常双端验收。本函数成功不能作为写权限、preview许可或确认token；当前没有新增可点击/调用的业务路径，M11整体与真实关联流程仍未实现或验收，不自动合并部署。
