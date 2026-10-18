# M46 S2 原生预览证据保护验证（2026-10-18）

## 版本与范围

本切片从main `72f2a37` 独立开始，不隐式依赖PR14/schema4、PR15工作台或PR16元数据接口。仅修改现有Store预览的事务/来源保护，新增内部guard与合成测试；没有DDL、parser/模型/HTTP生产接口/OpenAPI/客户端业务动作变化，没有确认/忽略/撤回、状态A/B或M46完整闭环。

原文件包络首次仍保留；被保护的是未提交重复预览的来源快照及候选分配，不声称现行相同输入会随机丢原bytes。真规则漂移或坏旧快照返回固定409，后续显式迁移/重评尚未实现。

## 已执行与继续执行的检查

- Store定向和完整ledger race测试通过，覆盖保留原source/首次原因/filename，更新当前LedgerVersion后整批Commit成功。
- HTTP定向race通过，覆盖正常owner/editor、viewer拒绝、撤成员、停准入、先前取得的Scoped editor仍复核准入、系统管理员非成员、跨账本同输入独立。
- 第一轮完整Go race/vet通过；按独立审查补强竞争财务、已提交后合法修订正对照与固定409响应后，再次完整Go race/vet通过，exit 0。
- 共享JVM/Web初次构建成功，但部分任务来自缓存，未以它作为新实际执行证明；随后用`--no-build-cache --rerun-tasks`真实执行共享测试、Web生产与Mac分发构建，68项任务全部执行、2分7秒成功，exit 0；Wasm体积/动态依赖既有警告保留，不称性能已验收。
- 正式S2新二进制、全新匿名目录与生产Web包：health/schema3通过，未登录原生preview与历史读取401、Web shell200；检查后停止独立临时服务，未切换共享入口。这不是正常已登录业务或GUI验收。
- 公共测试基线补丁仅复用独立验证的双loopback TLS fixture受控executor/阶段cause与always JVM报告、release发行产物过滤。两文件与已验证e387测试补丁完全一致；没有引入S1生产依赖或放宽生产TLS/超时/安全断言。

## 数据不变量与关键矩阵

- 每请求在同一写事务内验证Go准入/当前成员角色，所有版本、lookup、去重、失败保存与更新走tx；单连接池中不调用s.db版本helper造成自等。
- 旧/新完整Document、Raw值、Encoding/Issues、identifier scope、line/ordinal、pending分配与财务候选投影不漂移；动态ID/version/status/当前duplicate目标及计数允许刷新。
- 坏/缺旧Doc、未知或重复结构字段、Unicode等价结构别名、null/错类型/尾随载荷、未知错误、伪动态错误、原包络不一致均409；拒绝前后所有临时应用表内容相同，错误code/message固定、不回显载荷。
- source.Issues错误继续阻塞，合法空Records/header失败可重复读取，动态去重冲突可刷新清除；不冻结整个Errors数组或清掉来源问题。
- committed/undone/restored优先返回原快照，新的解析失败不把它伪装成新批。坏生命周期事件/version/state及缺原提交来源失败闭合；原candidate按冻结的source_records投影校验，不使用当前已更正交易值。
- 合法金额更正、分类/备注修订及关联退款后的同批committed重放返回首次Rows/Doc、不动当前数据；另一未提交批次duplicate保留原金额，只关联既有主交易；软删除不复活、退款不重新分配。
- 两个独立Store连接竞争preview/commit，允许受控SQLite busy回滚而不继续旧snapshot写；最终有效6笔、毛额/净支出7404分、收入/退款0、版本6及6个批次/来源关联，并锁已提交再次读取全表不变。
- 标准CSV既有全部回归，以及微信CSV/XLSX、真实ZIP稀疏单元格与合法未知Raw列正例。

## 独立审查及真实修正

1. committed分支初版只做JSON/ID检查，若干缺Doc/坏parser/格式/账户/hash案例实际返回nil错误。补回归先失败后修复，现先完整校验，再将每条candidate与同账本、同批、同行的提交时source_records.normalized_json严格匹配；不从live transactions取替代值。
2. Raw是大小写敏感map，不是struct字段。初版全局SimpleFold会误拒合法extra/EXTRA、scope/ſcope；改typed walker，仅struct键Unicode等价去重，map精确去重，未知原列两值保留。
3. 原WeChat XLSX parser允许空cell完全省略，初版guard强求Raw键会误拒当前合法输出。仅对wechat-xlsx按已知parser语义只读视空，不回填Raw；CSV仍要求必要列，完整Document比较继续阻止静默修复。

三个P2经独立源码重审可关闭，未发现新增具体P1/P2；该结论不是双端实际验收。

## 过程问题记录

初版私有guard helper与父测试 helper重名，已仅重命名生产私有函数解决。父测试曾使用未定义的错误helper名，编译失败后改用标准errors.As；并发编译又曾读到typed walker改签名中间态，待作者冻结后统一重新测试。均保留实际失败原因，不将它们写成业务测试通过或错误兼容反例已实际运行。

## 尚未验收

- 正常Web会话此前在鉴权/范围前置停止；未为本S2绕登录、复制会话、切身份或直接写入测试端点。
- Mac正常图形能力此前不可用，本S2未宣称真实Mac操作，构建不能补齐。
- 测试只用t.TempDir与_test.go合成身份；没有真实账单/正式账本、收费模型、ECS或生产部署。未自动合并PR；M46整体仍进行中。
