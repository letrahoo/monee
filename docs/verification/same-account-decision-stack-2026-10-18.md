# M11 显式schema4集成基线验证（2026-10-18）

## 依赖与范围

用户明确选择基于PR14/schema4开发后续迁移。当前从固定2135fe0建立隔离分支，PR base为feat/internal-transfers，不是main。选定PR17/31066f1的来源冻结2生产8测试，PR20/119e8fa的纯facts/pair2生产4测试，公共e387 TLS/CI补丁保留一次。

所有16个选定server文件逐个hash与来源精确一致；PR14原import.go与main blob相同，按选定hunk应用，无整branch合并。原004/schema/models/store/annotation/transfer/API/UI均保留原基线，不新加005、关联表、decide/revoke或有效账务。PR15/16/18生产代码未引入。

## 实跑与审查

作者706定向Go race ledger19.422s/httpapi3.770s通过（Native/WeChat/FailedNative/Accounting/Pair/Transfer）。父全量Go race-count1/vet初次709成功，补seam精度后的最终717再次全量race-count1/vet/diff成功（ledger28.805s）；不继承各来源PR绿测。

fresh禁缓存rerun共享/Web生产/Mac分发成功1分51秒，67任务全部执行；共享44测试零失败/跳过（这是PR14客户端基线，不沿用PR20的35或PR18的53）。原Wasm/Node警告不当性能验收。

独立源码审查无具体P1/P2：生产选定blob精确，ToAccount为omitempty，strictshape允许旧JSON缺该字段；这只是源码理由，不冒充实际旧原生库迁移验证。原PR14 v3→4迁移/退款/备份等测试在完整tree重跑，当前新seam覆盖新schema4来源刷新/已提交原件回放，不把两种证据混成一项。

独立测试/文档审查指出事件数口径及转账腿精度：普通两支出合计2468，转账不计收支金额但保留事件，总事件3而非2。补明确TotalCount3及转账实际from/to账户、ledger/kind/CNY与-10001/+10001，不只sum0；补强后的最终717完整Go检查已实际收取exit0，未把旧断言运行当新证据。

## 新接缝测试

真实临时schema4：原生预览→创建转账改变ledgerVersion→刷新保留原Document/字节→提交expense→committed回放全表不变。transfer不能冒充expense facts，两clearing腿保持实际账户/币种/金额；普通expense pair可计算hypothetical gross，但真实支出仍2468/收入0/事件3，全部表不变。

## 未完成

本片是显式依赖的开发基线，不是M11财务决策完成或草稿已合并。后续迁移/类型化关系/授权与证据/幂等版本/有效全账簿投影/旧writer与undo guards/真正确认撤回仍未新增。正常Web/Mac实际业务未验收，临时库、单测、构建不替代。未读取真实账本/凭据、未切共享路由、未收费模型试跑、未merge或deploy。
