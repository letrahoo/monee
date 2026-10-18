# M11 S1 当前支出跨来源相似项验证（2026-10-18）

本记录对应API首片 `a3dd036`。后续客户端执行结果见[独立客户端记录](association-hints-client-2026-10-18.md)，不以旧提交CI或匿名检查替代新客户端/正常双端验收。

## 基线与交付范围

从已合并main `72f2a37` 独立开始。新增一个Go只读GET及窄DTO、固定分页/当前账本快照、OpenAPI1.8.0和合成测试；没有DDL、模型/解析器、确认合并/删除、统计抑制、付费模型或草稿PR14/15/16/17的生产依赖。公共TLS fixture/always JVM报告与发行产物过滤只是独立复用已审e387测试补丁，生产TLS/timeout和安全断言不变。

本片不是可信支付平台↔银行卡识别，也没有完成M11“只计一次”。Source、账户或native/legacy前缀均不证明经济身份；API只检索当前投影，角色始终未验证、readOnly/actions空。客户端排查入口尚未实现，真正确认/纠错/原证据关联与旧迁移重叠仍后续。

## 执行检查

- 定向Store `-race`通过；HTTP正常认证/账本成员链定向`-race`通过。
- 完整`go -C server test -race -count=1 ./...`及`go -C server vet ./...`通过。按测试审查补writer失败路径join、最终空items、native/legacy完整ID集合后再次执行完整Go race/vet，exit0。
- `--no-build-cache --rerun-tasks`实际执行共享JVM、Web生产、Mac分发：67项任务全部执行，1分49秒成功，exit0。Wasm体积/动态依赖既有警告保留，不声称性能已验收。
- 正式新二进制、全新匿名目录/生产Web包：health/schema3通过，新GET正常与坏page均先401、Web shell200；临时进程检查后停止，无共享路由变更。这不是正常登录态API或GUI验收。
- YAML/全部本地引用/closed DTO/固定理由tuple/负整数边界静态检查通过；私有测试机数据或路径未放入公开产物。

## 主要矩阵

1. 自由标签“支付宝”与“银行卡”精确匹配只生成提示，SourceRolesVerified仍false；重复查询前后所有临时应用表相同，Dashboard仍两笔合计2468分，没有自动只计一次。
2. 51条候选按完整ID集合升序，50+1+0分页且无赢家；账本版本/计数/空数组稳定。只读前后源记录、审计、原预览字节和全部表内容不变。
3. 日期、类型、金额、商户分别不同不匹配；同Source不同资金账户也不匹配。坏Unicode空白、前后空格、控制字符、version0、SQLite REAL金额均在计数/分页前排除；坏基交易与有效但无匹配区分。
4. 单纯分类备注annotation导致版本提升仍保留；金额更正后再改回相同金额仍因已知correct历史排除，损坏correct payload也不当成无更正。任何退款关系含已软删退款均保守排除。
5. 手工、native导入和synthetic legacy-style命名的当前交易均可作弱提示；锁native与legacy两个候选ID精确集合，不以trace namespace/import link升级来源真实性，三笔仍计3702分。此例不代表真实旧迁移重叠整体已验证。
6. 两个独立Store连接在Correct同时读取，允许完整更正前快照（version2、原候选version1/金额）或完整更正后快照（version3、空集合）；最终只读全表不变。它是并发一致性回归，不强制某个SQL阶段交错；writer在成功和Fatal路径都先join再关闭Store。
7. 正常_test身份owner/editor/viewer可读；撤成员、停准入和此前已取得Scoped对象仍拒绝；管理员非成员/跨账本拒绝，存在性不越界。严格query只page，非法/重复/unknown/溢出/编码/分号均422，未登录先401。
8. DTO白名单不含账户、备注、分类、外部流水号、raw、confidence、confirmed或allowedConfirm；HTTP成功/错误no-store，无新POST/confirm/merge/delete入口。

## 独立审查及过程问题

独立生产/API审查未发现具体P1/P2。一次503 schema疑点检查外层Handler后撤回，实际响应有标准code/message，不作为已修缺陷。独立测试审查提出的writer收尾、post-state空items和混合来源精确集合已补。

初次Store fixture幂等键过短被既有创建校验拒绝，新API尚未执行；补合成键后重跑通过，没有降低生产幂等校验，不称该初始失败为业务缺陷。

## 尚未覆盖

正常已登录Web/Mac受影响业务没有完成；本片也尚无客户端排查入口。正常CG截图/输入能力本轮只读复核仍不可用。未读取真实账单/数据库/凭据，没有切换共享路由、修改正式账本、模型花费、合并或部署。匿名运行检查（若列出）不等于登录态业务或GUI通过。整个M11确认、只计一次、误关联可纠正和真实旧迁移重叠仍未完成。
