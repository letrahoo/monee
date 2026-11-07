# 消费关联授权 API（尚未启用正常能力）

本增量接通 Go/HTTP 操作合同，不提高生产 SchemaVersion，不允许正常 Open 打开版本6，不解除 schema5 非空关联门禁。因此现有正常环境的这四条路由会在授权后返回503 expense_pair_not_enabled，而不是执行关联。

## 路由

- POST /api/v1/expense-pairs/preview：当前 owner/editor，显式 primary/secondary ID、交易版本和账本版本，同事务授权、能力6检查、严格历史加载、事实/实际账户/同日同额/原生证据检查，返回两笔局部毛支出 before/after/delta 字符串。无写入、不预留版本、不证明同一经济事件；不返回原始凭证。
- POST /api/v1/expense-pairs：同事务当前权限+能力检查后复用原确认核心；Idempotency-Key 绑定 actor/ledger/op/input，授权先于重放。调用者必须明确选主记录及填写理由。确认仍重验所有事实/证据，不信任预览结果。
- GET /api/v1/transactions/{id}/expense-pairs：viewer可读，先核对当前成员及交易账本归属，再严格加载关联历史，返回按decision ID排序的当前修订receipt和读取时账本版本。receipt的ledgerVersion是该修订发生时版本；详情顶层版本才是当前版本。包含已撤回历史，不返回冻结facts/evidence/envelope。
- POST /api/v1/expense-pairs/{id}/withdraw：当前owner/editor，路径绑定decision ID、body给账本/decision版本和reason，复用撤回核心。历史创建者撤权不会给他重放特权；任何当前具写权限成员可纠错。恢复两条原始消费参与统计，不自动撤销导入。

## 安全与限制

公共Store命令在与授权/写入相同SQL事务中检查能力6，且在幂等重放之前；没有从schema5绕过的public开关。保留旧未导出方法供已存在的隔离生命周期测试使用。HTTP依赖统一的ledgerForRequest、外层准入/同源/CSRF校验及严格JSON字段解码。缺能力或持久关系损坏映射503，版本/重放冲突409；viewer写操作403。

DTO只有ID、版本、状态和整数金额字符串，原始来源继续由既有受授权的交易来源/导入详情路径读取。来源/历史读模型不做有效财务suppression；当前根本没有postings/账户余额/导出HTTP接口，不能据此声称已实现未来余额报表。

共享UI、Dashboard主记录关联标识/来源导航、schema6可运行门禁、明确升级入口以及真实旧v5兼容fixture仍未交付。不要仅提高SchemaVersion常量就开启此路由；后续须完成确认→一次统计→两份凭证详情→撤回完整竖切与正常OAuth双端验收。
