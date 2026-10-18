# M46 S1：待核实来源证据的只读元数据

## 边界

从已合并main基线独立开发，不依赖未合并的转账/schema4或工作台分支。S1只提供一个按当前权限读取保留来源证据的GET，不提供确认、忽略、恢复、撤回等动作。不选择M46写入状态存储A/B；没有DDL、源文档/预览重写、账务或决策状态变更。M46整体仍未完成。

新接口：`GET /api/v1/imports/{id}/review-items/{line}/metadata`。调用者须每次通过Go登录/统一账号准入及账本成员验证，viewer可以读取；系统管理员不是默认账本成员。line须规范正整数1–1000000，无查询参数。其他账本/不存在导入或目标pending行返回404；存在但损坏/缺失证据以unknown与稳定blocker失败闭合，不回显坏载荷。

## 不授予写入权限

响应固定`contractVersion:1`、`readOnly:true`、`actions:[]`。`ordinary_candidate`仅为来源分类，不是确认入账资格。特殊类型为`special`，不完整或矛盾证据为`unknown`。任何分类均不绕过去重、当前版本、来源有效性和未来确认策略。旧review queue的接口、SQL与计数不变。

依据完整保留的原Record与Raw，而不是可变reason或normalized expense/category；没有特殊关键词绝不能证明普通。退款/转账/还款的原类型证据不能被后续金额/日期错误覆盖。旧快照缺证据不重解析/回填，不用默认值当普通。

## 同一快照与版本区别

Store在同一个只读事务内验证权限，按ledger/import取完整preview，核对唯一pending行、唯一原Record、正ordinal/无重复、支持的native格式/parser/version、来源账户/支付流水scope及Raw/Fields一致性。批次状态及source链接读取同一事务；failed/undone/link阻塞，坏状态事件unknown，不将缺事件一概当committed。

Document parserVersion与导入envelope parser_version是不同版本；Document原文件hash与imports envelope hash也是不同引用，不能比较相等或声称重新验证了原文件完整性。SourceRef仅说明保留快照的引用。S1不扩展缺SourceDocument且不在native队列的标准CSV。

## 数据与隐私

只返回约定的来源facts、引用和稳定blocker code/path，不返回整个raw文件、原支付流水/商家订单号、令牌、损坏JSON或任意原文。来源账户与来源事实仍是授权成员可读的私有证据，不是脱敏/模型安全数据。全部合成测试身份限定在`_test.go`。

## 验证计划（未运行前不勾通过）

- 原生支付宝/微信CSV/XLSX、普通候选与原退款/转账/还款、多重错误和同商户退款风险。
- 缺失/重复/冲突快照、金额日期/币种/流水scope、未知parser/version/hash、旧证据。
- preview/failed/committed/undone/关联来源与损坏状态事件，稳定重复GET。
- owner/editor/viewer、未登录、停准入、移除成员、非成员、管理员非成员、跨账本隔离。
- 完整保留preview/raw字节、账本版本、财务与队列结果不变；无写入路由。
- 完整Go race/vet、共享JVM、Web生产、Mac分发与独立审查。真实运行按正常OAuth/权限及可用工具执行，无法完成的双端操作单列未验，不以构建/测试提供方代替。

OpenAPI基于main作增量；若另一个未合并分支先发布同minor版本，合并前需统一版本编号和契约，不能借版本相同暗示实现相同。
