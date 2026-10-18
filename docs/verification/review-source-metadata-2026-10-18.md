# M46 S1 来源元数据验证（2026-10-18）

## 对象与边界

- 独立分支 `feat/review-source-metadata`，从已合并main `72f2a37` 开始；不含未合并转账/schema4或工作台分支。
- 只新增来源证据metadata GET、Store只读快照与纯评估器、HTTP测试及OpenAPI。旧队列筛选/计数、parser、Preview、数据库迁移、客户端均不变。
- 始终 `contractVersion:1`、`readOnly:true`、`actions:[]`。`ordinary_candidate`不是确认权限；没有确认/忽略/恢复/撤回、去重或M46写入存储A/B实现。M46整体未完成。

## 已执行检查

| 检查 | 实际结果 |
|---|---|
| Store定向 | 真实临时库native支付宝/微信CSV/XLSX、来源/批状态/权限/无副作用用例通过；定向race通过 |
| HTTP定向 | 规范line/拒绝query、owner/editor/viewer、停准入/撤成员、管理员非成员、跨账本、重复读稳定、不回显原文件/原流水号通过 |
| 完整Go | `go -C server test -race ./...` 与 `go -C server vet ./...`通过；Unicode修正后全套再次通过 |
| 共享与Web | `:shared:jvmTest :webApp:wasmJsBrowserDistribution`通过；初次新worktree完整执行，修正Go后重核对应任务up-to-date |
| Mac分发 | `:desktopApp:createDistributable`通过；Unicode修正后Go服务重新打包并再次成功，不当作GUI实际验收 |
| OpenAPI | 系统YAML解析、所有本地ref及新GET-only/actions空契约检查通过；不是独立完整OpenAPI规范认证 |
| 正式进程匿名检查 | 官方新二进制/生产Web包、全新匿名目录、回环origin：health正常/schema3；metadata和旧queue匿名401，非法line/query也先401，Web shell200。检查后服务停止；未改共享入口 |
| 独立审查 | API契约无阻断问题；backend发现Unicode字段名等价重复P2，修正及回归后独立复核无新增P1/P2 |

## 来源与失败闭合覆盖

- 原native状态/业务类型、原Fields/Raw与支付流水scope，而非队列reason、normalized expense或类别；特殊来源与金额/日期错误同时保留blocker。
- 同商户退款风险、普通正向保守规则、未知业务类型；金额零/负数/三位精度/溢出/科学计数、日期/币种缺失、字段长度/控制字符。
- 缺/错Document、Raw、来源账户、hash/parser/version、支付流水、仅商家单号或scope错误；重复pending/line/ordinal/identifiers，Fields/Raw/预览行冲突。
- preview/all-pending/failed/committed/undone/restore/source关联，损坏最新undo事件与payload版本；来源关联不当作已入账，缺事件不默认committed。
- Document parserVersion与envelope版本分别验证，原文件hash与envelopehash不比较相等；只作为保留引用，不声称原始bytes完整性重新验证。非法hash/parser值不原样回显，负数引用值发布0并保持blocker。
- 重复GET前后所有临时库表内容、保留preview字节、版本、队列及财务汇总保持；纯函数输入不改。HTTP路径没有新增写操作，POST遵循现有未知路由404/405语义。

上述为自动隔离测试证据，不是正常OAuth后的人工/图形操作。合成身份仅在`_test.go`，没有注入正常服务。

## 独立审查问题与实际修复

初版以`strings.ToLower`去重JSON字段，不等价Go `encoding/json` 的Unicode simple-fold。完整合法fixture加 `sourceAccount`/`ſourceAccount` 后，旧检测能漏过并错误返回ordinary_candidate；事件state别名同理。新增反例先真实失败，再改为逐rune遍历完整`unicode.SimpleFold`等价循环取规范值，拒绝语义重复而不重写原JSON。long-s、Kelvin、ASCII大小写及final sigma反例全部通过。map中fold等价的不同原列也会被保守拒绝，这是已知保守限制，不据此放宽来源资格。

同时补充匿名/撤权加非法参数仍先鉴权、错误响应no-store与错误body文档。没有以放宽ordinary判据修补测试。

## 验证过程中的非产品问题

- HTTP首次测试预设POST必405，但现有SPA/未知路径回退实际404；按既有队列测试语义接受404或405，不存在新增财务写路由。
- 验证机没有PyYAML，未安装依赖，改用已有系统YAML解析器检查；不作为产品失败。
- Web编译存在既有Wasm体积/动态依赖警告，构建exit0；不称性能指标已验收。

## 必须继续保留的未验

- 本轮没有正常已登录Web/Mac的S1读取实际操作；正常进程匿名401和临时库身份测试不能替代它。
- 新接口没有客户端动作入口，不将旧分支界面/截图或人工金额读出当作此分支验收。
- 当前正常Mac图形能力不可用，未申请/修改系统授权，未绕过或继续延期输入法诊断。
- 仍需提交/PR当前HEAD CI及正常权限环境下相应运行证据，未完成项在PR/看板持续明确。没有合并main或部署正式环境。
