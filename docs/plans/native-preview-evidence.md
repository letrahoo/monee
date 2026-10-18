# M46 S2：原生预览证据冻结与漂移保护

## 范围与真实风险

从已合并main `72f2a37` 独立开始，不依赖未合并PR14/schema4、PR15工作台或PR16元数据接口。没有新DDL、parser改动、决策状态、确认/忽略/撤回动作，也不选择M46状态存储A/B。

现有原生预览会对同账本、同包络hash的未提交批次重新计算并覆盖preview。当前固定parser/规则下，相同bytes/account基本确定；真实防护对象是未来parser/gate/normalizer升级，或损坏旧快照被重复预览静默修复/重解释。首次原raw包络仍保留，不能称原文件bytes丢失，也不把它夸大成客户端能任意提交preview JSON。

包络hash包含格式、来源账户与原内容，保证内容/账户隔离，但不代表业务规则版本。Document原文件hash/parser版本与导入包络hash/version分别处理，不比较两hash相等或声称原文件完整性重新验证。

## 保护规则

- Store单个写事务中复核当前准入/成员写权限，所有lookup/version/去重/失败保存/写回使用同tx，避免单连接池自锁与跨快照授权。
- 已提交批次优先返回首次来源快照与真实撤销/恢复状态；新的解析错误不能把它表现成新未提交批次。损坏快照、结果或生命周期证据失败闭合，不用默认committed掩盖。
- 未提交重复预览严格读取原快照。未知结构字段、重复JSON键或Unicode fold等价结构字段名、尾随/null/错类型、缺必要证据、来源分配歧义等返回固定冲突；Raw map仅按精确键去重，合法不同原列不合并。拒绝时不回显原文或错误载荷。
- 比较完整Document（Raw原值、Fields、identifier scope/value、line/ordinal/hash/parser/Encoding/Issues）、format/sourceAccount、pending身份/日期金额商户、候选行的财务与来源身份字段。动态ID/version/status/message和计数不是来源判据；RefundOf等财务关联字段不是可忽略信息。
- 证据或候选分配漂移时409，所有应用表不变，不重解析回填、修复或覆盖旧批次。真正规则升级后的显式重评/迁移是后续流程，本切片允许保守阻塞，不能宣称升级后自动继续。
- 证据等价时保留首次filename、原Document与Pending（含首次解释）、raw/hash/parser_version/created_at；允许当前LedgerVersion、duplicate/similar/conflict状态、目标ID和计数刷新。不能冻结整个Preview，否则账本变更后将永远无法刷新版本提交。
- 源Issues产生的静态错误继续阻塞；固定文案且精确可解释的动态去重错误可重算。未知错误不能借任意conflict message被清掉。合法的无Records/空sourceAccount解析失败，与缺失旧Document的legacy快照区分。
- 更新限定账本/id/hash且committed_at为空，检查affected rows；并发升级写入失败就回滚，不重用旧权限继续写。标准CSV仍按既有流程，未获得原生Doc或新功能。

## 验证与交付边界

仅临时库与合成身份/账单测试，身份提供方限定`_test.go`。覆盖来源漂移拒绝后全表不变、重复预览刷新版本后整批Commit、金额更正后duplicate保留原候选、软删除不复活、原失败/动态冲突、已提交/撤销/恢复、权限撤销/停准入/跨账本、微信CSV/XLSX与独立连接竞争。

需要完整Go race/vet、共享JVM、Web生产、Mac分发及独立审查。共同测试/CI补丁仅复用已独立验证的双loopback TLS测试线程池/阶段诊断与always JVM报告/发行产物过滤，不引入S1生产代码或放宽生产TLS策略。

正常Web会话前置与Mac图形能力目前尚不可用；无法完成的真实双端操作保持未验，不以临时库身份或构建替代。PR仍为草稿，不自动合并或部署；M46整体未完成。
