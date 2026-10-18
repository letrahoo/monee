# M11 S1：当前已入账支出的跨来源相似项排查

## 定位与非目标

从已合并main `72f2a37` 独立实现M-11发现层首片。M-02/M-03依赖已完成；不隐式依赖草稿PR14/schema4、PR15工作台、PR16元数据或PR17来源冻结。

本片只查询当前已入账支出的精确相似投影。它不是可靠的支付平台↔银行卡配对，不确认同一经济事件，不减少统计，不合并/删除记录，也不操作待核实来源。当前疑似重复仍可能计两次；真正只计一次、误关联纠正、证据与旧迁移重叠的可撤销写模型仍为M11后续。

`Source`、`Account`与外部ID均可由输入提供，名字包含银行/支付宝、native前缀、有无导入关联都不能证明来源角色或独立原件。响应固定`basis=current_transaction_similarity`、`sourceRolesVerified:false`、`readOnly:true`、`actions:[]`，没有confidence、confirmed、canonical、isDuplicate或allowedConfirm。

两笔同日同商户同金额也可能是独立消费；同渠道标签改名/不同钱包也会得到提示。真实银行入账日期、商户描述与支付端不同则会漏检。本片没有日期容差、商户模糊、金额绝对值跨类型或角色猜测fallback。

## 只读查询契约

GET `/api/v1/transactions/{id}/association-candidates?page=N`，每页固定50、ID升序。只允许一个canonical `page`，缺省1，范围1..1000000；重复、未知、空、溢出或非法编码参数均拒绝。先正常认证/Scoped，再解析参数；Store在一个只读事务内再次复核当前准入与成员读权限（viewer允许），读取账本版本、base及候选。同请求是一个快照，不保证跨页请求共享快照；ledgerVersion变化时调用方应重载，不能把它当未来确认许可或稳定经济身份token。

基交易不存在、已软删或跨账本返回404；有效但v1不支持的基交易返回固定invalid422，区别于正常无匹配。v1只支持当前kind=expense、CNY、SQLite整数存储的canonical有符号负分值（-1000000000000..-1）、1900..2200合法ISO日期、正版本和1..200字canonical非空商户/来源标签。它只是当前数据库类型筛选，不证明原始来源的普通经济语义；普通收入并非一定特殊，但本片暂不扩。

保守排除任何child refund关系（包括已软删退款）和已知`correct`交易操作历史，损坏更正payload也不当成无历史。未知/缺失历史不能因此被宣称已核实原件。分类/备注annotation导致版本增加不等于财务更正，仍可参与。

候选与base使用当前实际列做日期、kind、有符号金额、币种、商户完全相等，来源显示名非空且不同；排除自身、软删除、退款/特殊类型、更正与退款依赖。金额不经float/ABS；不信预存similarity hash来断定同笔。候选经过相同资格过滤后才计数/分页，避免Unicode空白与SQLite TRIM差异造成count虚高或页漏项；流式只保留当前页。

## 窄输出与权限

仅返回id/version/date/type/amountMinor/currency/merchant/sourceDisplay，item理由固定`current_projection_match`、`different_source_display`。不返回资金账户、分类、备注、原文、外部流水号、来源文档/字节、token或源关联计数。多个提示全返，不挑赢家；源角色始终未验证，namespace或import link不升级信任。

所有成功/错误no-store。所有查询限定当前账本并绑定参数；无新POST/confirm/merge/delete路径，不写交易、源快照、审计、版本、统计或任何应用表。没有DDL/parser/model/AI/cloud变更。OpenAPI独立标1.8.0，和其他草稿未来合并时协调版本，不代表生产依赖。

## 验证门槛与未覆盖

临时合成库验证精确/多候选、51条分页、每个字段差值、假来源标签、annotation与correct区分、软删/历史退款、坏金额类型/Unicode字段、跨账本/角色撤销/停准入、全表与财务版本不变，以及独立连接更正期间单次读取快照。共享客户端无新入口，仅履行后端变更的完整Go race/vet、共享测试、Web/Mac构建和独立审查。

目前未实现客户端排查页面，也未通过正常已登录Web/Mac受影响流程；既有登录/图形交互条件不足仍单列。不能以API测试、匿名进程或构建补造GUI验收；M11整体保持进行中，不自动合并或部署。
