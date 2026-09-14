# ai-financial 数据与功能迁移

## 结论与边界

以当前 main 的 KMP / Compose Multiplatform + Go + SQLite 为准。旧版提供历史数据、来源证据和功能设计参考；不引入 React、Python 运行时、Basic Auth、旧版浮点金额运算或线上静态快照充当正式账本。

已实现 Go 离线适配器 `server/cmd/migrate-legacy`：读取旧版 `product-snapshot.json`（schema 1.0），生成可供现有 CSV 导入页预览的消费候选、逐条处置清单和完整源快照。**生成迁移包不等于正式入账或完整功能迁移。** 不增加数据库 schema，不读写正式账本，不绕过 OAuth / 白名单，不修改旧项目或阿里云服务。

本次证据来自旧版源码、本地 SQLite 和本地部署快照；没有连接阿里云校验线上快照的新鲜度。实际条数、金额、来源账户和逐笔处置只写入仓库外的私有迁移包。旧库与快照已核对 ID、金额、方向、来源、canonical / excluded 标记；SQLite 完整性及外键检查通过。

## 功能差异与处理

| 领域 | 旧版实现 | 当前 Monee | 本次处理 / 后续 |
| --- | --- | --- | --- |
| 客户端 | React/Vite；历史静态页 | KMP 共用 Web / Mac UI | 保留当前客户端 |
| 账本写入 | Python 离线重建 SQLite，Go 只读快照 | Go 事务、整数分、平衡分录、版本/change_log | 导入复用当前 Go 规则 |
| 数据获取 | 支付宝、微信、京东、淘宝、银行 CSV/XLSX/PDF；Gmail 脚本 | 标准 UTF-8 CSV；手动补记 | 先适配既有快照；原生解析后续逐源移植到 Go |
| 账户语义 | 消费源头、支付渠道、资金账户分开；存款/借贷分类 | 单一 source + 待核实 clearing 账户 | 保留 source/account；完整维度留在源快照，不宣称资产余额 |
| 去重 | canonical 标记、规则/置信度、跨来源关联 | 文件哈希、来源身份、内容冲突、疑似重复确认 | 旧重复不再入账；全量关联保留；候选继续经过当前去重 |
| 退款及统计 | 特定来源整体排除；全退原单剔除；部分退款不冲减消费展示 | 当前只支持普通收入/支出 | 不复制旧展示总额；退款、疑似相关原单单列核实 |
| 收入/转账/还款 | 已识别多种方向，但消费页只展示子集 | 仅 income / expense 业务模型 | 收入亦暂核实，避免把内部调拨误作收入 |
| 审计 | 全量流水、去重处置、来源批次/哈希、筛选排序与时间线 | 交易明细、CSV 来源记录；月度搜索分页 | 快照完整保留；审计 UI、更多筛选是后续功能 |
| 分类/事件 | 分类覆盖、手工覆盖规则、重大事件聚合 | 收支分类汇总 | 候选沿用已存分类；事件/覆盖定义私有归档，不直接执行旧规则 |
| MCP | Basic Auth 保护的只读工具 | 尚无业务 MCP | 不移植旧权限；以后复用当前授权与查询服务 |
| 部署/权限 | 阿里云 HTTPS/Nginx、Basic Auth、只读快照容器 | 本地同源 Go、OAuth、白名单、共享本机账本 | 当前权限与运行方式保持；不切换线上部署 |

代码依据：旧版 `scripts/build_sqlite_ledger.py`、`scripts/generate_dashboard_data.py`、`backend/internal/api/api.go`、`backend/internal/audit/query.go`；当前 `server/internal/ledger`、`server/internal/auth`、`shared/src/commonMain`。旧版描述中的愿景没有当成已实现功能。

## 字段映射

| 旧字段 | 当前字段 / 留存位置 |
| --- | --- |
| transactionId | external_id = `ai-financial:<旧 ID>`；Monee 仍生成自己的 UUID |
| transactionTime | date 取业务日期；完整时间在源快照 |
| direction | 仅明确普通 expense 生成候选；其他方向核实 |
| amount | 直接解析 JSON 十进制字面量；不经过 float64、不四舍五入补救 |
| currency | 只接受 CNY |
| merchant / category | 原值；按当前服务端长度与字符限制验证 |
| source / accountName | source / account；继续采用当前待核实账户语义 |
| orderId / description / notes | note 保留，并附旧流水、rawRecord、batch ID |
| lineage / dedup | 完整保存在 source-snapshot.json；note 提供追溯索引 |
| excluded / canonical / disposition | manifest 逐条记录候选、核实、归档，不丢弃原行 |
| aggregate / analysis / 新增未知字段 | 源快照逐字节保存；不当作新账本统计结果 |

旧 ID 命名空间确保同一旧流水重复迁移可识别；它不等于未来原生账单流水号。未来原生格式导入必须处理与迁移记录的重叠，不能声称已解决全部跨格式去重。

## 候选选择与核实规则

1. 非 canonical、已 excluded、旧版明确排除来源/重复/全退原单：归档，完整保留证据。归档不是删除，也不是认定旧规则永久正确。
2. 非普通支出、未 includedInSpend、币种/金额不兼容、缺少来源证据：核实。
3. 状态、描述、备注含退款、还款、转账、充值、提现、冻结、理财等提示：核实。
4. 与任一退款具有相同商户、相同描述或订单前缀关系：保守核实。该规则会多留部分正常消费，不构成退款配对结论。
5. 其余经过当前 `normalize` 校验，生成候选；过长字段不截断、不静默改写。

候选金额是历史普通消费的保守子集，不能与旧页面总额、净支出或资产余额等同。manifest 满足 `total = candidates + review + archived`；每条旧 ID 有且仅有一个处置。

## 运行与入账

准备 Go 环境后，在项目根目录运行（参数自行指向私有目录，真实路径不写入仓库）：

```sh
go -C server run ./cmd/migrate-legacy \
  -snapshot "$LEGACY_SNAPSHOT" \
  -out "$MIGRATION_BUNDLE"
```

输出父目录须已存在，目标目录必须不存在；文件 0600、目录 0700，旧包不会被覆盖。输入最多 64 MiB，未知 schema / 行数不符 / 重复旧 ID 会失败。原快照、迁移包均不应放在 Git 或 Web 静态资源目录。

输出：

- `source-snapshot.json`：完整源快照，manifest 保存其 SHA-256。
- `manifest.json`：版本、源哈希、批次哈希、数量、整数分字符串合计、逐条 ID 与处置原因。
- `candidates-*.csv`：每批最多 1000 条且 CSV 不超过 1 MiB，为请求编码留余量；逐批通过当前 CSV 解析器验证。
- `README.txt`：用户操作与金额口径说明。

登录当前 Monee，在导入页逐批选择 CSV，预览后确认。每批提交后才预览下一批，避免版本过期。疑似重复必须核实，不能统一勾选来推进迁移。相同文件复用批次；相同旧 ID 内容变化会报冲突，不应修改 ID 绕过。当前没有应用内撤销，请在现有确认流程中核对。

此命令没有数据库或 commit 参数，不能用于免登录写入。完整源证据应与迁移文件一并保留；源快照不含全部原始文件字节，原 SQLite、原账单、手工事件定义仍需保存在私有归档中。

## 后续顺序与验收

1. 核实候选预览与重叠后，通过现有授权导入入账；记录每批结果，核对新增/跳过数量。
2. 实现版本化备份及恢复校验，再扩展 schema：来源账户/支付渠道、原始来源、退款关联、还款/转账类型；金额规则只放 Go。
3. 用合成回归数据移植旧版有价值的去重案例，明确误判与人工确认边界；不直接照搬旧浮点阈值。
4. 以共享 KMP UI 补全审计筛选、来源覆盖、时间线、重大事件；聚合由 Go 基于正式账本计算。
5. 原生格式逐源落地后，再接文件夹/邮件。邮件 OAuth 与登录 OAuth 分开，旧凭据不迁入 Git。

已增加合成测试：候选/核实/归档隔离、十进制精度拒绝、退款关联、缺失来源、批次拆分与确定性、现有 SQLite 预览/提交/重试/冲突、完整快照留存、私有权限及禁止覆盖。实际测试结果见开发交接。
