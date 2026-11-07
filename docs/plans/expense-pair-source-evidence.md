# 关联两侧的单行来源凭证明细

基线dab74a5。补齐原先只有两侧filename/line的入口；直接在共享关联面板内展开这一行，不切换到含导入写操作的批次面板。

## 绑定与展示

复用Go已有逐请求授权的GET import detail和transaction sources。客户端以单一origin/user/ledger/epoch stamp重新验证receipt当前存在、所选source确属主或副交易，然后读取该批次；仅在import ID、line、transactionId及disposition唯一匹配后选出凭证。原normalized record的ID可以不同于linked live transaction ID，不能为显示方便改写历史快照。

两侧独立按角色展示：导入时保存的规范记录，以及该line仍保留的sourceDocument字段；无原文字段时明确未保留/已隐藏，不虚构或回退到当前交易。ImportDetail增加可选evidence DTO数组，不改变Go或数据库，不把派生结果写回Transaction。

这里只在UI展示一行。底层仍复用整批import-detail JSON接口，不是新的按行网络接口；不返回原始raw_csv或完整原文件下载。多次HTTP不代表同一SQLite快照，结果也不能作为新财务计算或写入授权凭据。

## 生命周期

UI需保留detail target/version/generation和loaded对象身份检查；换详情、刷新来源后清除行明细。中途重连或换身份即拒绝返回旧结果。读取不触及未知写入submission及其幂等key；viewer只有只读展开按钮。

完整Go、共享测试、Web/Mac构建及独立审查均须记录。正常schema5未启用/Open6仍拒绝，因此不存在正常OAuth双端实际验收证据；不得把编译或MockEngine当实际点击验证。后续仍需正常schema6运行门禁、显式离线升级入口和真实旧v5实现兼容验证。
