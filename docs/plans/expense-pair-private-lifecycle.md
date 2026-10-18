# M11：未开放入口的可撤回决策事务核心

## 当前阶段不是已生效的关联功能

接续PR21/schema5基础，只在ledger包新增未导出的确认、撤回和持久关系读取逻辑；不接HTTP/UI，不改旧财务查询或writer，不移除Open的“任意关系数据即拒绝”启动门禁。私有核心只由隔离Go测试调用，写入的是关系元数据；当前实际Dashboard仍显示两份毛支出。这不是“只计一次”金融效果或正常用户验收。

后续必须同时完成有效交易/完整posting对/全部读取、全部旧写入与import undo/restore保护、正常权限及双端生命周期，安排独占版本升级后才能开放入口并解除启动门禁。

## 输入与资格边界

确认输入只含账本版本、明确primary/secondary交易ID与各自版本、原因；撤回含账本/决策版本、ID与原因。金额、资金账户、分类引用和来源事实均在同一事务从数据库读取，不能接受客户端自报facts/evidence。

显式非空actor及当前准入/成员写权限必须先于幂等重放。scope hash绑定操作、ledger、actor及规范输入，成功返回前先比较hash。同key异body冲突；active确认重放零写，撤回后旧confirm key不得重新激活或假报active；withdraw同key重放仍须当前权限。

第一私有能力仅接收可锚定的committed native added原始来源；未知/manual/legacy不靠显示名猜测，保守unsupported。支持的格式与完整原件也不证明现实账户身份或同一经济事件；明确保留记录是调用者决策，不是来源角色算法认证。此限制不代表旧迁移重叠的完整验收已完成，D2/D8也不因代码存在自动获产品批准。

读取原生冻结快照、source_records和实际分录/account上下文，严格storage/driver整数与既有facts/pair条件；同实际fund、同日同额商户、不同ID、明确primary。已删除、任何退款子关系或已知财务更正历史排除。保留原始证据，当前仅分类/备注修订不能变成财务原件重解释。

## 原子生命周期与原件所有权

新确认在同事务完成交易版本重验与同快照ledger CAS、关系唯一性检查、root version1 active、两个active成员及规范facts/evidence、confirm revision、关系审计、ledger版本递增和幂等结果。撤回同事务CAS active/version1→retracted/version2、两个成员active0、withdraw revision、审计/ledger版本与幂等结果。任意晚阶段SQL失败必须全部回滚且零receipt。

不删改原transactions/postings/source_records、原ID/hash/fingerprint/normalized快照或交易版本；撤回不重新生成财务记录、回退他人版本或强制恢复批次undo资格。新的确认需新关系ID，旧retracted历史保留。

持久关系须严格验证ledger/contract/版本、恰好两个角色与成员、active状态联动、规范JSON及修订/审计谱系。损坏状态不能被修复后继续或回退原始双计。active关系重新核对当前账务及来源发生集；retracted历史不因之后合法的current修订而被重新解释。这里的检查只供私有核心使用，不能声称旧正式财务reader已经接入。

## 验证策略与未完成项

真实受控SQL身份与native提交的测试覆盖确认→撤回、权限先于缓存、hash/CAS冲突、重放/历史、来源漂移与持久JSON损坏；独立连接竞争只有一个完整获胜者，幂等末阶段失败回滚全部表并可用同key重试。原财务及证据表保持；正常Open面对合法私有关系仍拒绝，锁定门禁未被提前移除。

自动测试和Web/Mac构建必须全量重新执行，但不替代正常登录的双端操作。未读取真实账本/凭据、未调用收费模型、未切共享路由、未merge/deploy；M11仍进行中。
