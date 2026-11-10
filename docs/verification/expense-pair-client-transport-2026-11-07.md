# 关联导航与共享传输层验证（2026-11-07）

基线f2b2c68；[设计与边界](../plans/expense-pair-client-transport.md)。合成临时账本及MockEngine测试，不使用正常服务、OAuth、系统凭据或正式账本。

## 服务端导航

- owner/viewer Dashboard只给当前页可见主记录的active receipt。
- 搜索未命中、页2为空、撤回后两行恢复都不残留导航；损坏关系失败关闭，不返回部分导航或明细。
- 副记录原位于raw第50条的边界测试：主记录补至有效第50条时receipt也在page1，不泄至page2/3。
- 独立只读server审查未发现P1/P2；suppression与导航在同一strict loader结果中生成，不污染持久Transaction JSON。
- 完整Go race/vet/diff通过（ledger66.624s、httpapi9.163s）。

## 共享客户端

- 窄DTO、金额字符串、receipt/detail输入输出绑定与旧Dashboard默认空数组；Transaction模型不新增派生字段。
- 每origin/user/ledger最多保留一项未知结果submission。确认→撤回或反向切面板不能替换未知请求；原始input/key必须保留到确认结果。
- 固定origin/token/CSRF/ledger的请求stamp与epoch，不二次发现后把旧请求重定向到新scope。
- commonTest覆盖字段/金额/历史版本及pending生命周期；Lead另在既有jvmTest依赖范围新增MockEngine transport测试（没有改build依赖）。
- MockEngine已覆盖丢失结果保留原key和body后成功重试、异常/不匹配receipt不清pending、origin/user变化拒发旧submission、discovery挂起后重连拒发、迟到401不清新身份，并补500/取消及迟到成功的pending保留。
- 初次定向共享测试通过（21s）；最终fresh共享全量测试/Web生产/Mac分发通过（1m31s，68任务全执行）。独立共享只读审查未发现P1/P2。编译提示两个内部构造data class未来copy可见性规则的兼容警告；当前提交用同对象身份校验拒绝伪造copy，警告不当测试失败，后续可按Kotlin升级处理。

共享列表仅加“已关联 · 支出仅计一次”状态提示。完整确认/撤回面板、两份凭证导航及schema6运行门禁尚未实现；正常schema5不会产生关联，Open6仍拒绝。编译/MockEngine/HTTP合成测试都不替代正常登录双端实际验收。未部署、合并、升级真实库或切路由。
