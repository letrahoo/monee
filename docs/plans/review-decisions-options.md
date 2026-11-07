# M-46 核实闭环接续选项（尚未领取实施）

核对基线：main `72f2a37`、M-09 草稿 PR #14/schema4、工作台草稿 PR #15/schema3；均不因本计划而合并或部署。M-46 依赖 M-02/04/06/07 已有代码，技术上普通收支不必依赖 M-09，但持久化方式和产品边界须先确定。

## 必须修正的原有假设

- 待核实 ID 已有 `importId:line`，原事实来自 preview.sourceDocument.records；必须唯一匹配完整 Record、原 ContentHash、parser/version，缺证据/重复 line 失败闭合。展示 reason 会被多次覆盖，不能用它判断退款/转账/还款是否能作为普通收支。
- 重复预览可覆盖未提交 preview_json 并重建普通行 ID。存在单条决策后必须冻结原始证据，协调未来整批 Commit，不覆盖单条结果或重账。
- source_records 只支持 added/duplicate，不能存 ignored；旧队列“有关联即隐藏”不能作为可撤回状态。队列、历史实时计数、批次 undo/restore 必须统一有效状态投影，原 pending/预览仍保留为当时快照。
- 撤回只可删除本决策创建、未被修订/退款/其他来源共享的交易；duplicate 撤回不能删主交易。批次 restore 不得复活先由单条撤回的记录，撤销来源不得重新入账。

## 存储方案

### A：零 DDL 事件先行

从已合入 main 独立分支，以 change_log.review_item 版本化事件存 pending/booked/ignored 及所有权、来源 hash、交易关联和操作；复用现有账本幂等与 source_records。事务内集中状态投影、账本版本 CAS 与授权。可避开另一个004，不依赖 PR15 UI。

代价：缺少数据库级状态唯一性/版本/FK约束，事件扫描需评估；承载状态的事件不得裁剪。旧 schema3 程序会忽略新语义，不能混用或只降级二进制，必须以匹配数据快照回滚。没有这些运维前提时不能采用。

### B：类型化状态表

review_items/review_decisions 及索引/约束更清楚，但必须协调迁移顺序：等 M-09 经授权集成后使用实际下一版本（若届时schema4则005）；或明确批准依赖固定 M-09 提交的 stacked PR。不能现在盲目新增另一个004或跳过004造005。

## 最小 API 和状态方向

- GET review-queue 默认 pending，可显式 state；detail 给版本、来源 hash、有效交易、决策与 allowedActions/blockReasons。
- POST item/preview：confirm/ignore/restore/withdraw，只计算规范提案、delta、版本、候选和阻塞，不写入。
- POST item/decisions + 原 Idempotency-Key：事务内先 authorize，再查原成功请求；新操作才重验源 hash、条目/账本/交易版本、重复/相似与依赖；正式交易/分录/来源关联/审计/版本/幂等一次提交。
- 所有金额是整数分/十进制字符串。日期、金额、支付标识不能因用户将 type 改成 expense 而绕过结构化特殊类型限制。
- ignore 需理由；restore 建议只从 ignored 回 pending，不自动入账。unknown response 客户端保留原 body/key，权限被撤销后成功 key 也拒绝重放。
- 失败/撤销批次禁止财务写。是否支持未整批提交且全待核实批次，必须明确；只支持 committed 会使 all-pending 无法处理，不可称完整闭环。

## 开始写入前需确定

1. A 的事件状态/旧程序限制是否可接受，还是 B 等迁移顺序确定。
2. all-pending 与 failed 批次单条策略，是否允许日期/金额修订（保守建议锁定来源字段，仅分类/备注）。
3. ignore/restore/duplicate-withdraw 与共享依赖的产品语义及理由必填规则。

可先做结构化来源 eligibility 的只读切片；不因此暗中实现写入或宣布 M-46 完成。

## 验证矩阵（仅计划）

原文/内容hash逐字不变；未知/损坏事件失败闭合；特殊类型即使reason被覆盖仍阻塞；严格精度/币种/日期；ordinary added/duplicate/similar；批次前后提交/撤销/恢复互操作；退款/修订/共享依赖保护；同key并发/丢响应/重启/异body冲突；各事务阶段故障原子回滚；多连接CAS；逐请求角色/准入/账本隔离和权限重放；备份恢复与匹配版本回退；月/年/分类仅计一次；所有 Go/共享检查、Web/Mac构建、正常OAuth后双端真实操作、独立审查与CI。

本计划未创建新迁移、未改变账务、未访问真实账单、未运行本任务测试；Notion M-46 仍已规划。
