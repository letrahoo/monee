# 关联导航与共享客户端传输层

基线 f2b2c68；正常schema5能力门禁仍关闭，Open6仍拒绝。此片不执行真实库升级，也不交付完整确认/撤回UI。

## Dashboard导航

Dashboard新增顶层expensePairs数组。它只包含当前页可见主记录的active receipt，顺序随交易列表；搜索未命中、其他页及撤回后均不带该条关系。普通空关系成功响应是[]。同一授权read tx只严格加载一次关系，同时得出secondary suppression和primary导航，损坏仍失败关闭。

receipt不进入Transaction结构，避免改变transaction/source/evidence/idempotency持久JSON。receipt内ledgerVersion为确认修订时的历史版本；客户端写入时必须用当前Dashboard或detail顶层版本，不能错把历史版本当当前CAS。

共享列表根据receipt的primaryId显示“已关联 · 支出仅计一次”。这只是状态提示；两份来源导航及确认/撤回操作面板仍须继续接入，不能把徽标当完整产品验收。

## 共享DTO与API

共享KMP层接收窄DTO、整数金额字符串及默认空导航数组，兼容旧服务没有expensePairs的响应。确认与撤回必须保留完整origin/user/ledger作用域和幂等submission，未知结果、取消或异常响应不得悄悄生成新key。重连发现新服务或登录身份改变后，旧submission不能被发送至新scope。读请求及迟到401也不能修改新scope状态。

具体实现/测试结果在[验证记录](../verification/expense-pair-client-transport-2026-11-07.md)中按实际完成项登记。

## 后续

下一竖切需共享界面显式选两条交易和保留主记录→预览影响→确认→主记录两份来源/详情→撤回。作用域和账本版本改变、失连/撤权/迟到请求都要失效处理。正常schema6运行门禁、真实旧v5兼容fixture及正常OAuth双端验收仍是前置，不应直接改版本常量开启功能。
