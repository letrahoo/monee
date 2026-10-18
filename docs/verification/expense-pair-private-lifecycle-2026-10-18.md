# M11 私有决策事务核心验证（2026-10-18）

## 范围与归属

接续PR21/392ae91、显式PR14/schema4→005/schema5链。新增未导出的models/evidence/loader/lifecycle四文件，不接HTTP/UI、不动既有财务reader/writer/Open门禁/迁移；关系元数据并不减少当前财务金额。原财务、来源、账户、交易版本保持，正常Open遇到合法私有关系数据也仍拒绝。

作者只写生产文件、测试作者只写一份store测试，均未执行Go/Gradle；生产由独立只读审查，测试与全部实际执行由主会话分别归属。父另写atomic/source边界测试，所有SQL身份和资料均为独立临时库合成数据，不注入正常服务提供方。

## 实际先红后绿

- 753首次完整目标失败仅“different actual fund”反例：测试改来源wallet别名却没改CSV付款字段，两笔实际仍同fund UUID。修fixture为不同真实原付款字段，并先JOIN断言两个fund IDs不同后仍要求unsupported/全表零写；不修改生产同fund规则，也不把错误反例当金融bug。
- 独审发现文档摘要与原始bytes未绑定：754只改SourceDocument.ContentHash为另合法sha256时确认成功，真实红测证明P2。定向修复strict uppercase envelope、Format/Preview.SourceAccount绑定、canonical严格base64及原bytes摘要等于DocumentHash；不重parse或声称验证外部真伪。755该用例转绿。
- 两笔缺失付款方式原来会落同一个fallback账户，755空、`/`、`-`、保留placeholder四例均真实成功而红。按私有能力证据边界保守unsupported，不再用fallback补足原付款证据；不是额外推断现实银行身份。
- 756完整目标race通过13.937秒。追加真正Ali+WeChat CSV同实际fund/tuple正例，明确保留WeChat交易后确认再撤回、原财务/来源表不变，757全部目标race通过14.314秒。

## 关键事务断言

同事务从真实当前交易、posting/account、committed native added原件与发生集读取，不接受调用方money/facts；删/退款/财务更正/未知manual或坏来源拒绝。canonical持久JSON/contract/两个角色/active状态/连续修订与精确审计匹配，损坏时私有核心unavailable，不修复或退回raw。

当前actor非空且准入/成员写权限先于成功缓存；scope hash先比再重放，CAS及active唯一性、确认重试零写、撤回后旧confirm key不复活、旧withdraw key不改新组、不同actor/revoked/viewer重放拒绝。历史创建者撤权不重写历史，当前editor可撤回并记真实revision actor。

末阶段idempotency trigger故障使confirm/withdraw的root/member/revision/audit/ledger/key全部回滚，同失败key可重试；两个独立预先打开的连接竞争只有一个完整获胜者。原所有非元数据表保持。私有metadata写后实际Dashboard仍两笔2468，不当金融只计一次已实现；正常Open仍拒绝，锁定门禁没被提前移除。

## 全量检查与未完成

最终758全Go race-count1/vet/diff实际成功（ledger56.497秒），包括16项新顶层生命周期测试。fresh759共享/Web/Mac成功1分30秒、68任务全部执行；已保存完整本轮日志确认时间/任务数，JVM报告44测试零失败/跳过，不沿用旧计时或继承392 CI。正常登录双端affected业务未验收；当前更不存在公开确认/撤回路径。后续完整effective交易/两腿posting/所有财务读取、旧writer及导入duplicate-link/undo保护和独占升级必须同时接齐才能解除门禁/开放入口。

未知或manual/legacy来源的首版资格、成员版本/批次撤回所有权不因私有代码自动获产品批准。证据格式/内部账户ID不证明现实账户角色或同一经济事件。未读写真实账本/凭据、未调用收费模型、未切共享入口、未merge/deploy，M11保持进行中。
