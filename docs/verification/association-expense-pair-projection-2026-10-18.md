# M11 同账户pair假设毛额验证（2026-10-18）

## 变更与授权边界

接续本PR20首个facts提交4d57251，不引入其他草稿生产依赖。用户明确选择不同记账账户首版禁止关联；新纯函数只落实same internalFundAccountID/same day/amount/exact merchant/explicit primary的假设pair gross。未批准或实现其余D1/D2/D8，不提供confirm/withdraw、净额/余额/全ledger变化或权限。

## 实际执行与审查

作者680初次编译exit0但当时no tests，不记测试覆盖。681实际5顶层纯单元定向race通过（1.582秒；早报6为计数笔误，已核为5），父683含临时库定向race通过2.194秒。

独立冻结源码119行、unit212行和临时库测试审查无具体P1/P2。审查者未运行这些检查，不能混记测试归属。按独立测试审查补强Primary/Secondary完整8facts与实际B/A绑定、Date/Merchant、不同fund反例同snapshot freshA和旧/新ledgerVersion精确绑定后，父最终全Go race-count1/vet/diff通过（ledger17.254秒）。fresh禁缓存且rerun的共享/Web/Mac构建成功2分钟，68任务全部执行，35共享测试零失败/跳过；既有Wasm体积/动态依赖/Node URL警告不当性能验收。旧4d57251六CI不继承为新提交结果。

## 关键断言

- 复用两份结构facts、不同tx及四个不同posting ID，相同实际fund ID而非名称，逐分录币种/金额/引用约束保留。
- 两个不同分类账户都保留原引用；primary明确选择第二笔时，反序输入得到完全相同结果，不默认第一条或信任某来源。
- 负金额先限幅后取负乘2，边界最大before2e12、after1e12、delta-1e12；拒绝事实金额溢出、金额差、无关/缺primary、日期/月/闰日/商户Unicode和类型错误。
- 输入不变、输出仅value facts/primitive、失败zeroPair和静态错误，不泄露输入，无activation。
- 临时库双成员+ledgerVersion在同一ROTx真实JOIN读取；计算2468→1234只是hypothesis，原Dashboard仍ExpenseMinor2468/TotalCount2且所有临时应用表不变。不同实际fund账号ID拒绝，未从label推断身份。
- 退款父交易+已软删成员仍能计算假设gross，但实际Dashboard/全部表不变；旧coherent输入在DB已更正后仍可给旧gross，fresh双输入金额不同时拒绝。说明结果不是active/refund eligibility、新鲜度或正常账务命令。

## 未完成与禁止替代

没有生产DB适配器/授权、证据契约/真实银行角色、持久化decision、effective全账簿投影、旧writer/batch guards或实际撤回。新的用户可点击/调用路径不存在，正常双端同事件确认业务仍未实现/验收。不得以纯函数、临时库、构建或旧提交六绿冒充交付；无真实账单/DB凭据、模型收费、共享路由、正式账本写入、合并或部署。
