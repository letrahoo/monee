# 关联两侧单行凭证验证（2026-11-07）

基线dab74a5；[设计与边界](../plans/expense-pair-source-evidence.md)。仅隔离合成测试，无真实账本/凭据复制、路由切换或正常provider注入。

## 已收集

- 新增ImportDetail可选evidence字段，和Go SourceEvidence wire字段逐项核对；缺字段默认空，旧导入读者仍可解码，focused读者缺凭证则不展示。
- 4组common模型回归：原normalized ID允许不同于linked live ID；import/line/transaction/disposition错误及重复line拒绝；原文字段缺失只保留规范记录，不虚构原文；wire兼容旧响应。
- 5组MockEngine回归：主副及撤回历史均可GET读取原保存行，不动未知写入key；关系或来源变更拒读import；错误import或transaction快照拒展示；非法角色/路径拒发；import在途换scope后迟到成功或401不返回详情。
- 初次定向共享测试25s通过；完整Go race/vet/diff通过（ledger68.334s、httpapi9.335s）。最终fresh共享全量测试、Web生产与Mac分发构建通过（1m32s，68任务全执行），最终diff检查通过。既有Kotlin未来copy可见性、Webpack体积等警告仍在，未将警告当实际GUI验收。
- 独立只读审查未发现P1/P2。UI沿用detail ticket和loaded对象身份，换详情或刷新来源清空已展开行；按角色、decision、transaction和完整source对象分键，不合并两侧。

## 实际验收与局限

没有实际Compose点击、正常OAuth Web/Mac双端或窗口关闭重开验证。正常schema5仍未启用/Open6仍拒绝，不能把这些测试当正常运行验收。

UI只展示所选行，但底层仍读现有整批ImportDetail，批次Preview/SourceDocument和规范证据均会进入客户端内存；不是新的单行最小响应接口。不会下载raw_csv/原文件或导出整批。

多次HTTP逐请求授权，但不是同一个SQLite快照；这里只展示历史凭证，不做当前交易计算、替换或写入授权。原文缺失不能区分真实隐藏、旧导入未保存或空字段；normalized merchant/note等仍来自原证据，这不是完整隐私脱敏。mock缺失sourceDocument不证明真实脱敏流程。

下一步仍需正常schema6运行能力、显式离线升级和保留真实旧v5实现的兼容验证，随后才能开展可用的正常登录双端实际验收。未合并、部署或改写真实库。
