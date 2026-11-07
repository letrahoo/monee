# 消费关联授权 API 验证（2026-11-07）

基线45fe6be；[范围与尚未开放项](../plans/expense-pair-authorized-api.md)。仅临时目录与合成原生账单，没有正常环境升级、真实账本写入或路由切换。

## 已执行

- 默认schema5四public方法拒绝且全部原表不变。HTTP四路由匿名请求401，已登录请求503 expense_pair_not_enabled；未知body字段400。
- 隔离Store开放连接标记能力6后：预览不写、明确primary/secondary及2468→1234/-1234分；确认与同key重放receipt相同；两成员均可查到同一决策；Dashboard总数1/支出1234。撤回后历史retracted、总数2/支出2468。
- viewer可读详情，不可预览/确认/撤回；损坏成员active标记后detail失败并返回空DTO；撤去成员后优先返回ledger_denied。
- 确认与撤回的已成功key在能力改回5后拒绝重放；恢复6但撤去成员后也拒绝。另一账本成员查询原交易返回not_found。
- 真HTTP编解码/请求授权集成：合成身份仅测试创建会话，HTTP导入两份原生Alipay账单、明确保留primary预览、确认与重放、Dashboard一次统计、详情、撤回、恢复两行。直接改隔离库user_version仅为测试公共方法，未新增生产绕过。
- YAML检查：bundled Python缺PyYAML，未安装依赖；使用系统Ruby YAML解析OpenAPI成功，并确认四个新增组件存在。不是完整OpenAPI语义lint。

定向race全通过。独立只读审查未发现P1/P2，建议的跨账本、能力降级重放及撤权重放回归已补。最终完整Go race通过（ledger74.255s/httpapi9.025s），vet/diff-check通过；fresh共享测试/Web生产/Mac分发通过（1m39s，68任务全执行）。本提交远程CI需另行收集。

## 未验收

HTTP合成集成测试不是正常登录Web/Mac实际操作。当前正常程序仍拒绝6/非空5，无共享UI，无Dashboard关联badge。暂不能称用户已可使用“只计一次”。本提交没有合并或部署；schema6运行及正常双端流程继续后续。
