# M11 schema5持久化基础验证（2026-10-18）

## 精确范围

接续PR21/b08c9ac显式PR14/schema4基线，005新增三表、同账本外键/唯一/值域/JSON对象约束，SchemaVersion5与Open/startup拒绝关联数据的中间保护。004与原API/HTTP/共享生产UI未改，无Store决策writer、确认撤回或有效财务投影。

## 实际测试归属

作者730编译/纯金额回归0、731真实空5 Open及当时已有前缀race0（4.724秒），不当作之后新增完整矩阵已覆盖。父734迁移/fence前缀race0（6.414秒）。独立约束作者735运行被其子会话workspace-write策略在Go缓存open阶段拒绝，setup failed且测试未执行；未升级、改GOCACHE或重试，不记为断言红或通过。

主会话按原定职责在已授权运行上下文执行完整Go race-count1/vet/diff并成功（738，ledger39.676秒），包含三份新测试；这是主会话独立全量结果，不记作735重试或子代理自己通过。随后修清preflight消息并补强完整schema对象签名/no-repair与future_schema精确断言，最终743全Go race-count1/vet/diff再次成功（ledger35.663秒），以此最终运行覆盖新增断言。fresh禁缓存rerun共享/Web生产/Mac分发成功（739 exit0）；实际JVM报告44测试零失败/跳过。不沿用旧提交绿，也不把旧日志时间当本轮构建时长。

独立源码/迁移/fence/约束只读审查未见具体P1/P2。review未代替实跑，JSON内无typed accountFK、不证明exact-two或当前权限、startup非runtime混版本防护等边界均明确。

## 关键实质断言

- 实际4库含原生commit、普通消费/退款、转账→Open5后所有旧应用表/财务与来源值不变；迁移前备份恢复到新路径仍为4且原表完整，空5重新Open无变化。
- 旧expense_pair对象是preflight拒绝且零写，不伪称midDDL；坏旧posting FK则在DDL+user_version之后FK校验失败，事务回滚仍4、无新对象、旧数据不变。
- 同账本root/tx复合FK及actor引用FK、role/status/operation/version/contract/active及REAL1.5、invalid/object JSON、组role/version唯一、active成员唯一与inactive历史可并存，SQL约束失败后回滚比较全部表。
- schema5逐表root/member/revision单独非空、完整retracted inactive历史和unknown contract均拒绝，静态错误/不返回Store/未遗留阻塞写锁（连接关闭由源码审查确认）；缺三表、view替身、缺部分index、额外列/trigger也拒。逐表非空与结构拒绝、旧对象预检及FK失败前后均比较user_version和排序sqlite_master对象签名，直接锁定不自动修复/篡改结构。
- 未来版本拒绝fixture按当前max+1调整；原PR14旧库备份恢复仍锁实际3，不因备份目标标签v5而弱化原版本断言。普通转账/来源冻结/pair接缝在空5中完整重跑。

## 未完成

尚无实际可提交决策、来源资格策略、统一有效账簿、旧writer guards或确认撤回，更没有正常Web/Mac业务验收。拒绝任何关联数据是此中间版本的能力边界，未来需独占升级及完整功能同时移除，不能解释为已实现关系语义或运行时外部SQL安全。没有正式账本/凭据/共享路由操作、收费模型、merge或deploy。
