# M11 S2：相似支出的只读客户端入口

## 接入范围

沿API首片 `a3dd036` 的同一PR18功能分支接续，只显式依赖本分支已有只读接口；不依赖PR14/15/16/17的生产代码。Web/Mac共用支出详情内的“查看相似支出”入口，viewer也可查询。收起时不加载，展开按需GET；支持加载、无匹配、不支持、失败重试、固定50条分页、刷新及收起。

只显示日期、金额、商户、来源展示名。字段相似不代表同一经济事件，来源名称不是支付平台/银行卡角色证明，疑似重复仍可能计两次。页面不提供确认、合并、删除、扣减统计或任何财务动作，不复用现有financial runAction。M11完整的关联写模型、原证据保留、误关联纠正、可撤销与真实旧迁移重叠仍后续。

## 请求与页面隔离

- 查询scope包含userId/ledgerId/transactionId/transactionVersion/ledgerVersion，controller还按api实例和enabled重建。启用条件不含每5秒普通读轮询的loading：无版本变化不重置已完成页；写入、认证忙（含退出开始）或连接错误立即换空controller。
- 新查询先清旧页；页结果只替换，不跨页拼接。版本不符时清空提示，要求刷新后重查。切用户/账本/交易/版本、关闭、取消、错误及权限变化均不能留下旧结果。
- generation仅允许当前request完成/失败/取消；迟到401也不能触发新用户退出。LaunchedEffect取消与Dispose清理共同约束旧页面；没有确认token语义。
- Kotlin DTO必需字段无安全猜测默认值。严格验证contractVersion1/readOnlytrue/sourceRolesVerifiedfalse/actions空、当前projection basis、请求baseID/page、pageSize50、总数页长关系、有序唯一ID、合法负整数分金额、日期/Unicode字段、精确tuple与不同Source及固定reasons。畸形数据/未来确认动作响应不渲染为普通结果；空结果不等于没有重复。

## 连接与身份边界

LedgerApi保留正常零参构造入口；internal测试DI完整注入HttpClient/discovery/session load-save-clear，仅用于MockEngine隔离测试，不读取真实Vault、网络或登录身份。正常默认生产构造不提供模拟身份/示例入口。

请求在短会话锁内捕获origin、用户、token、CSRF与epoch，使用不可变凭据快照；网络/discovery不持锁。新只读请求在发送前、响应/错误后与解码后校验期望stamp；reconnect、退出开始/完成、origin/用户变化、登录及关闭使旧读失效。旧auth/me不得覆盖新身份，旧网络失败不得重置新连接。原有API继续使用正常鉴权/方法，不增加共享token或生产绕过。

独立审查发现的discovery竞态必须单列：旧查询若在发现A连接时挂起，再重连B且用户UUID相同，destination的重试可能将旧调用重新定向B。回归已实际先红；最小修复在只读方法首次挂起前固定epoch，取得stamp后比较，不符立即拒绝且不发GET。它不是token跨origin泄露，不扩大正常auth初始化行为。

## 验证与未验范围

纯状态机与真实LedgerApi MockEngine覆盖旧响应/旧错误、分页替换、版本变化、403、取消重试、origin/token绑定、同origin新读、退出、过期auth/me、迟到IOException、discovery阶段同UUID重连、坏契约与路径参数。所有测试资料合成，session函数完整替换，无真实凭据存储或第三方调用。

完整Go race/vet、共享测试、Web生产、Mac分发和独立审查仍必须执行，见对应验证记录。它们不等于正常双端业务实操。实际受控账本中展开/翻页/重试/切换scope的Web/Mac操作没有通过前，保持未验收，不合并或部署；不可用MockEngine、匿名进程或测试身份替代正常登录。
