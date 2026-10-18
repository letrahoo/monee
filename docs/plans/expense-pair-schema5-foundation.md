# M11：受保护的 schema5 持久化基础

本文保留392ae91阶段范围；后续[私有确认/撤回事务核心](expense-pair-private-lifecycle.md)已新增，但没有正常入口或有效财务投影，本文启动门禁仍保留。下文“无Store方法”指原基础阶段，不否认后续未导出的metadata方法存在。

## 范围与版本链

按用户已确定的显式PR14/schema4开发链，从b08c9ac基线增加唯一005迁移与SchemaVersion5，004原文不变。不从main3直接跳5、不修改现有交易/来源/分录，也不新增确认/撤回Store方法、HTTP或UI入口。

此阶段只建立类型化持久化约束及**Open/startup能力fence**，不是已可用关联功能。旧主干/schema4程序的未来版本拒绝逻辑仍不能被绕过；实际部署、数据迁移或降级不由本PR自动执行。用户D1开发方式/D4同账户已定，其余证据、成员版本与撤回所有权政策不据绑定列自动批准。

## 三张表及约束

- `expense_pair_decisions`：ledger/id复合主键，status active或retracted，正整数version、contract_version1、created_by引用app_users、非空创建/更新时间。
- `expense_pair_members`：ledger/decision/transaction复合主键，role primary或secondary且组内role唯一，正整数transaction_version，facts/evidence为TEXT且有效JSON object，active为整数0或1。同ledger复合外键指向root与transactions；部分唯一索引禁止一笔交易同时成为多个active成员，inactive历史可保留。
- `expense_pair_revisions`：ledger/decision/revision_id复合主键，组内version唯一，operation confirm或withdraw，actor引用app_users、非空reason/time、contract_version1及object snapshot。

整数列同时检查SQLite storage type，不能因REAL1.5满足大于0就通过。JSON只检查语法/对象shape，**不保证内部账户FK、payload字段版本、来源真实性或业务资格**。没有typed account列；actor FK也不证明当前准入/成员权限。DDL不保证恰好两个成员、root状态与member active联动、会计效果或撤回所有权；未来Store必须在同事务完整校验。

## 原子迁移和备份

migrateExpensePairSchema固定连接、要求实际user_version4且FK已开启；一个事务执行005、user_version5及foreign_key_check，任错回滚。既有表不重建，不关闭FK，不改原ID/fingerprint/source_records/postings/版本/审计。Open正常升级前由现有版本化备份机制保存原schema4快照，恢复只到新目标；SQLite完整性/FK检查不代表关联业务语义已验证。

旧版本号却已有expense_pair对象时，在迁移/备份前拒绝，不覆盖或猜测修复未知结构。既存对象预检拒绝不是“DDL执行后回滚”；后者由真实坏FK导致执行DDL后校验失败的单独用例验证。

## 暂时禁止打开关联数据

Open到schema5后，在返回Store前用只读事务确认：真实schema版本5；三张真实表和部分唯一索引与同一embedded005定义匹配；除允许的SQLite自动索引外，无缺表/列/index、view替身或未知附着trigger。随后检查三表**任一非空即拒绝**，包括合法active、retracted/inactive历史、孤立member/revision及损坏状态。错误固定为association_foundation_unavailable，不回显SQL、JSON或路径；失败关闭连接，不返回可用Store。

这是保守的**启动兼容门禁，不是关系语义validator或运行时外部SQL/跨版本并发writer防护**。已打开旧进程后再由外部写入不在本阶段支持范围。未来解除门禁必须安排独占升级，并与完整effective交易/分录/全部读取、旧writer与批次undo/restore保护、授权/证据/幂等/CAS及确认+撤回同一完整交付，不能先单独放开写或仅移除fence。

## 验证与下一步

迁移保留原生证据、支出/退款/转账及全部旧表值，预迁移备份可恢复实际4，空5可重复打开；现有未来版本fixture使用SchemaVersion+1、迁移前备份仍锁原版本。真实约束矩阵、逐表非空/历史/损坏/缺schema拒绝、DDL后FK失败回滚均在独立临时库验证。完整Go/shared/Web/Mac重新执行，正常业务不以临时库和构建代替。

没有新增用户可操作关联路径，也未读写正式账本、真实凭据或共享路由；不自动merge/deploy。后续才实现完整决策生命周期及正常双端受控账本验收，M11整体不标完成。
