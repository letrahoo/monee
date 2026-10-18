# M11：同账户双支出的假设毛支出投影

## 已确定与未确定

用户已通过明确选项确定首版：**不同记账账户先不允许关联**；先限同一记账账户、同日同额两笔支出，明确选择保留记录，误关联须可撤回。这是已有功能授权下资金影响边界的细化，不是重新批准功能。

本片只把该边界实现为unexported、未激活的纯Go pair毛额计算，接续本分支4d57251的会计facts基础。**不是可提交决策、财务写入计划或确认token。** 没有Store/DB/API/UI/DDL接入，不改变当前账本、分录或统计。真实确认和撤回仍须同增量实现，不能将本函数算术当作已支持撤回。

用户这次选择不表示其余D1/D2/D8迁移顺序、未知来源可否参与、成员版本与撤回所有权等条件获批。PR19的历史方案保持其原文快照；当前已确定的范围以此记录和Notion任务为准。

## 纯输入与检查

输入为期望ledger、调用方提供的非负ledgerVersion、两笔成员（各含现有最小accounting输入、Date、Merchant），以及明确的primaryTransactionID。没有Source、账户显示名称、角色或证据元数据。

1. 分别复用单笔facts检查：expense/CNY、canonical int64负金额[-1e12,-1]、ledger/tx/account引用、严格完整clearing/expense两腿。任一失败，pair零值和原静态错误返回。
2. 两笔交易ID不同，跨成员的四个posting ID不得重复；同一个expense account ID跨两笔可以合法共享。
3. 实际FundAccountID必须相同，不比较显示名称或将名称映射成身份。相同内部ID也不证明现实银行/钱包真实性。
4. 两笔有符号整数金额完全相同；日期严格ISO且真实存在、1900..2200并完全相同；商户canonical非空UTF-8、无控制字符/首尾空白、最多200rune且完全相同，不模糊或大小写折叠。
5. primary必须显式exact命中一方；不默认选base、第一条、较早记录、来源或score。反序输入不改变选定primary和结果；选定记录的分类账户引用原样保留，不被升格为可信源件。

## 输出仅是假设pair毛额

输出为supplied ledger/version、primary/secondary两份窄facts、Date/Merchant，以及PairGrossBeforeMinor、PairGrossAfterMinor、PairGrossDeltaMinor。两笔各1234分的假设毛支出：2468→1234，delta=-1234。不是全账本总额、净支出、退款后结果、资金余额或实际有效账簿。

金额先经facts限幅，取负后的magnitude最大1e12，再乘2最大2e12，在int64内；无浮点、任意posting求和或MinInt64取负。失败输出零值/固定错误文案，无输入回显、输入修改或slice/map/raw引用别名。

supplied版本只保留绑定，不证明freshness、当前权限、目标由授权请求选中、同一数据库快照、active状态、无退款/更正或来源普通性。同日同额同账户也不证明同一消费。退款父交易、已删除或旧coherent输入仍可能给出假设gross；未来writer必须在授权一致快照中重新检查全部证据、依赖和版本，不能接受客户端自报facts授权。

## 测试与退出条件

纯单元检查主次选择、反序、ID碰撞、cross-ledger/fund、日期/商户边界及2e12金额、静态错误/零值失败/输入不变和输出形状。临时库_test适配器在一个只读事务读取两成员和ledgerVersion，检查实际category/fund引用、实际Dashboard两笔仍计2468且所有表不变、不同实际fund拒绝。退款/删除与旧快照用例锁定“仅假设毛额，不提供eligibility/freshness”。

本片执行全Go race/vet和fresh共享/Web/Mac构建；正常Web/Mac业务仍无新可操作入口，不伪造运行验收。不自动合并、部署或修改正式账本。后续完整决策服务仍须统一effective transactions/postings/所有读取，逐请求授权/证据校验、幂等CAS、全部旧写与导入undo/restore guard、可追溯撤回及正常双端验收。
