# M11 纯支出会计事实校验验证（2026-10-18）

## 基线与实际增量

本文保留首片4d57251的单笔facts验证记录；之后用户已明确首版跨账户禁止关联，当前新增pair假设计算另见对应记录，不改变本首片未做政策选择的历史事实。

独立main `72f2a37` 分支，新增unexported纯Go校验与测试。没有Store/DB/API/DDL/现有写流程/UI接入、正式财务行为或权限模型变化，没有选择PR19 D1–D8。共同TLS/CI两文件与e387精确同blob，仅为验证fixture/报告基线；未复制其他草稿生产代码。

本函数只核对supplied inputs结构一致，不证明真实数据库、当前权限、同快照/target、active或现实来源角色；代码注释/计划和测试保持同一边界。

## 已实际执行

作者初次仅编译成功(no tests当时)，不记业务测试通过；随后5项顶层纯单元及父当时临时库定向race通过。父临时库3项顶层定向race通过后，按独立测试审查补输出八字段精确等于实际角色leg、Scan失败时defer Rows关闭、PRAGMA故障注入Fatal路径恢复，再执行最终完整检查：全Go race-count1/vet及diff检查通过；禁缓存且rerun的共享JVM/Web生产/Mac分发全部成功，67任务全部实际执行、1分55秒。共享35用例零失败/跳过，既有TLS安全检查保留。Wasm体积/动态依赖及Node URL既有警告不当性能验收。

独立生产源码/单元/临时库审查未发现具体P1/P2，未用review代替实跑。无授权或GUI结果从此推断。

## 关键实质断言

- 交易与分录driver int64+integer storage、canonical负金额和范围边界；拒绝MinInt64/非整数/溢出，不对任意leg求和或取负。
- 两条分录ID及account ID各自不同、交易/所属ledger一致、实际account ledger/kind正确；clearing负金额和expense正金额逐腿等于交易，而不是只sum0。
- 输入顺序无关；失败zero facts、静态错误无input回显、输入不变，输出只有八个primitive facts无policy/action/raw alias。
- 一个只读事务的_test适配器从真实临时DB读取，postings的ledger来自transaction JOIN；LEFT JOIN缺引用保留空值交校验拒绝。没有虚构postings.ledger_id或把账户显示名当资金身份。
- 真实Create→Correct金额→Annotate分类后的当前事实均通过，输出对应当前版本/金额及实际角色posting/account IDs。子退款父交易、软删交易及旧coherent输入也通过，明确不证明eligibility/active/freshness。
- 临时SQL实际注入：平衡但交易金额不等、REAL交易/分录、缺leg、两个clearing、引用其他ledger账户、缺失account；均要求错误+零输出，检查前后所有临时应用表不变。

## 未覆盖与不得宣称

没有新生产DB适配器/快照授权实现，没有新的用户可操作流程可供点击。旧页面行为未改；正常Web/Mac的同事件预览/确认/撤回/全投影仍未实现或验收，不以基础函数、临时库或构建补造通过。没有真实账单/DB/凭据、模型付费调用、共享路由修改、正式账本写入、合并或部署。
