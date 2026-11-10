# M11：显式 schema4 开发依赖链

本文保留b08c9ac阶段的基线范围；后续005/schema5基础见[当前迁移与启动门禁](expense-pair-schema5-foundation.md)。004保持原文不变，但当前分支已新增独立005；下文“本片无新增迁移”仅指R13基线阶段，不是当前HEAD没有005。

## 用户已选的开发方式

用户明确选择基于PR14开发后续迁移，不等待main合并，也不自动合并任何PR、部署、切换共享路由或写正式账本。这是默认main开发方式的本任务明确例外，不是把草稿当已合并；主干仍72f2a37/schema3。

首版资金范围亦已明确：不同实际记账资金账户禁止关联，同日同额两笔支出需明确保留记录，误关联可撤回。内部账户UUID不证明真实银行身份；这些选择不自动批准未知来源/成员版本及撤回所有权政策。

## 精确依赖和本片内容

| 来源 | 本片使用 | 未声称 |
| --- | --- | --- |
| PR14 2135fe0658895b33e7d1660bdedbc098f19e0479 | base分支feat/internal-transfers，现有004/schema4、转账及原测试/UI完整保留 | 不代表PR14已合并或其正常双端已验收 |
| PR17 31066f14fb7a92c1512b549abb246fbfe5429bc8 | 选定import/native evidence冻结生产与对应Go测试，非整个PR合并 | 不导入单条决策、模型或其他界面 |
| PR20 119e8fad70eab43030ce08989f78255e1dc64203 | 单笔facts/pair假设gross纯函数及测试 | 不导入实际确认、权限或只计一次账务 |
| 公共e387测试补丁 | TLS fixture和CI报告两文件，只保留一次 | 不放宽生产TLS/timeout/安全检查 |

PR14与main对应import.go blob相同，来源冻结hunk可以按原上下文应用；这只证明文件接缝无冲突，不替代全tree兼容测试。没有暗带PR15日常工作台、PR16元数据或PR18只读客户端生产代码。

本片**只组装开发基线**，不新增migration、关联表、确认/撤回API或有效财务投影。原004保持原blob、schema仍4；后续持久化才在这条明确版本链上新增唯一后续迁移，不能另造004或让main3直接跳到005。

## 兼容验收与下一段

重新执行原转账/迁移/权限/退款更正/undo测试、原生快照冻结/严格解码/committed原件/Unicode/稀疏XLSX/竞争测试、facts/pair矩阵以及完整Go race/vet、共享测试、Web/Mac分发；不继承各来源草稿绿测。

新增临时schema4接缝测试：预览原生支出→创建转账改变ledgerVersion→刷新保留原文档/字节→提交支出→已提交回放全表不变。验证transfer两条clearing腿仍平衡且不能冒充expense facts；两笔普通支出可给hypothetical gross，但两笔普通支出金额仍合计2468，另保留转账事件（共3事件），转账不计收支金额且全表不变。

后续真实决策必须一并具备持久化关系/修订、完整effective transactions/postings/所有读取、逐请求授权/证据/版本/幂等、全部旧writer及导入undo/restore guard、确认和撤回及正常双端合成账本验收。未完成前不开放部分金融写、不把当前纯gross算术当完成。

本分支PR显式以feat/internal-transfers为base；最终进入main前仍需维护者明确依赖提交合入顺序，不能自动合并来解决依赖。正常Web/Mac实际业务未验收，不以隔离临时库/构建替代；正式环境始终不变。
