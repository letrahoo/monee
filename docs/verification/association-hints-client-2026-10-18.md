# M11 S2 只读相似支出客户端验证（2026-10-18）

## 基线及范围

在API首片 `a3dd036` 的PR18同分支接续。该API提交六项远端CI已成功：Build39105567031（Linux/Mac ARM/Intel）及Compose/images39105567041，release跳过。该结果只属于API旧提交，不沿用作当前客户端增量的远端成功。

新增共享DTO/严格契约校验、UI独立状态机、详情中按需只读查询与分页/刷新/关闭。LedgerApi增加完整测试DI、不可变origin凭据快照与旧读失效护栏，并对auth/me迟到覆盖、退出开始和旧连接失败做必要安全约束。没有Go/DDL/parser/财务动作改变，不提供身份、银行角色或只计一次证明。

## 执行与审查

- 初次作者compile未配置JAVA_HOME、找不到Java Runtime，属环境设置失败；使用JDK17后compileJvm/compileTestJvm通过，只证明编译，不称业务测试通过。
- 父首次目标测试14项通过，完整Go race-count1/vet通过。独立审查随后发现discovery阶段旧读可跨reconnect被重新绑定新origin；不能把先前绿测当全部边界已覆盖。
- 原反例实际红：hold discoverA，重连B且保持相同user UUID，再放行旧discover；旧调用错误地成功返回，预期拒绝得到error=null。不是token混发或真实网络；用MockEngine完整DI隔离复现。
- 最小修复在associationCandidates首次suspend前捕获epoch，取得stamp后比较；不符返回association_scope_changed，未发GET。原红测纳入全部17项目标用例，补丁后实际重跑通过。独立审查确认闭合，未发现新增具体P1/P2。
- 随后补“已完成同origin reconnect后的新调用可正常查询”正例；最终源码完整Go race-count1/vet再次通过。禁缓存且rerun所有共享测试/Web生产/Mac分发，68任务全部实际执行、1分49秒成功；共享JVM合计53用例（新增11个真实API与7个controller用例）零失败/跳过，TLS既有安全测试保留。Wasm体积/动态依赖与Node url.parse既有警告未作为性能验收通过。

## 自动用例证据边界

状态机覆盖latest generation、迟到success/401完整state不变、close/cancel/retry、有效50+1分页替换不追加、ledger/base version改变清页、403清页、空结果与错误区分。协议覆盖required字段、readOnly/roles/actions/basis/page/count/tuple/reasons/重复项/非法金额；无服务端原响应回显或确认按钮。

真实LedgerApi经MockEngine验证带当前ledger header的GET、注入的session读取、退出/同origin重连/跨origin延迟响应、origin与token不可混用、过期auth/me不复活A用户、旧IOException不重置B连接（锁实际注入旗标）、403与取消后重试、畸形响应严格association_invalid、路径分隔符/控制字符/坏page在发送前拒绝。discover用例不是只有网络handler延迟；它真正延迟连接发现且两端user UUID相同，专门锁已复现P2。

所有会话load/save/clear函数均测试替换、MockEngine仅synthetic example.test，不读真实Vault/Keychain、凭据、数据库或账单，不向第三方发请求。结构化协程用10秒超时，旧async结果显式收集；原始失败XML仅留本机忽略目录，不入Git。

## 正常Web/Mac实操：未验

已存在客户端入口不等于实际操作通过。正常登录态的展开/结果/多页、取消/重试、scope切换、权限失败与前后财务不变尚未双端验证；既有受控Web会话/正常Mac截图输入条件不足仍单列。不启动注入身份的正常入口、不借MockEngine或匿名服务补齐验收。无共享路由变更、正式账本试写、模型花费、合并或部署。M11整体确认/纠错/真实迁移重叠仍未完成。
