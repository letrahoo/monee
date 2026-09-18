<p align="center">
  <img src="assets/brand/app-icon.png" alt="Monee Logo：绿色 M 形图案与金色硬币" width="180" height="180" />
</p>

<h1 align="center">Monee</h1>

<p align="center"><strong>Know Your Money. Own Your Future.</strong></p>

Monee 是一款本地优先的个人财务助手，目标是自动整理账单、解释财务变化，只在需要判断时提醒你。当前正在逐步实现 MVP。

**换电脑或新会话接续：先读[开发交接](docs/HANDOFF.md)和[异地开发指南](docs/operations/development.md)。完整开发进度已同步到 `main`，换机直接克隆主干。**

## 产品方向

- **自动归集**：优先接收指定文件夹和邮件附件中的账单，必要时补充手动导入。
- **可信账本**：统一支付宝、微信、银行卡与京东白条流水，处理重复交易、退款和信用账户还款。
- **主动分析**：解释月度变化，提示疑似固定支出变化，集中展示需要核实的事项。
- **本地优先**：Web 与 Mac App 共用本地数据，为后续云端同步保留兼容性。
- **四端规划**：客户端使用 KMP + Compose Multiplatform，先支持 Web / Mac，随后补齐 iOS / Android；服务端使用 Golang。

## 预览版下载

[0.0.1 构建与使用说明](docs/releases/0.0.1.md) · [GitHub Releases](https://github.com/letrahoo/monee/releases)

## 项目状态

已打通 **KMP Web / Mac → Go API → 本机 SQLite**。两端读取同一份账本，支持标准 CSV 的预览与确认导入、重复流水检查、手动收入/支出、月度统计、中文搜索、分页和明细。默认打开真实空账本，确认后持久化，客户端每 5 秒刷新；现在需先通过登录与系统准入验证，并获得对应账本权限。

目前仅支持 UTF-8 标准 CSV 和普通人民币收入/支出。支付宝、微信、各银行与白条的原生文件格式、目录监听、邮件接收、退款/转账/还款、编辑撤销、完整备份恢复与云端同步尚未实现。资金账户均标为待核实，不展示资产余额。Android / iOS 入口后续补齐。

## 登录与访问权限

Web / Mac 已接入 Google / GitHub 登录框架、统一账号及账本授权。一个用户可以绑定多个登录身份，创建多个账本，通过账号 ID 邀请成员，并授予可编辑或只读权限。未登录不能访问数据；未获准账号显示“无数据访问权限”；初始超管可添加、启用或停用白名单成员。配置与实际验证范围见[登录与访问白名单](docs/architecture/login-and-access.md)。

Mac 在系统浏览器完成授权、领取客户端会话后自动回到前台；完成页也提供“返回 Monee”入口。Google / GitHub 的回调地址仍指向 Go 服务，无需改成应用链接。

首次使用前需创建提供方 OAuth 应用并填写本机私有配置；未配置时数据访问默认关闭。代码不包含公共默认超管、OAuth 密钥或开发模式登录后门。

## 运行本地应用

需要 JDK 17 和 Go（模块使用 Go 1.26，锁定工具链 1.26.8；支持工具链自动下载的 Go 可自动获取）。首次构建需要联网下载 Gradle、Kotlin/Compose 和 Go 依赖。

```sh
# 首次配置 Google / GitHub OAuth（密钥输入不回显）
python3 scripts/configure-login.py

# 启动本地 Go 服务，同时提供 Web 页面。保持此终端运行。
./scripts/run-local.sh
# 浏览器打开 http://127.0.0.1:4173/

# 另一个终端启动 Mac；读取同一个服务
./gradlew :desktopApp:run

# 检查与生成 Mac .app
go -C server test -race ./...
go -C server vet ./...
./gradlew :shared:jvmTest :desktopApp:createDistributable :webApp:wasmJsBrowserDistribution
```

Mac 数据默认保存在 `~/Library/Application Support/Monee/`，应用重建不会清空账本。可通过 `MONEE_DATA_DIR` 为服务和 Mac 指定同一个私有目录。不要把账本目录放进仓库；也不要用独立静态文件服务替代上述启动方式，Web 需要与 API 同源。停止服务后，页面会提示连接失败，并隐藏账本，直到能够重新验证访问权限。

导入页提供空模板；[合成 CSV 样本](fixtures/synthetic/standard.csv) 仅供独立测试账本使用，示例确认后也会真实保存。字段、去重规则、测试账本运行方法和接口见[本地数据链路](docs/architecture/local-data-flow.md)。

Web 需要支持 WebAssembly GC 的现代浏览器。完整中文字体随应用资源提供；首次加载体积仍需优化。Mac 键盘输入尚待实际输入法复验。当前 .app 需要先运行 Go 服务，尚未包含服务的自动启动、签名或公证。重新构建后应完全退出旧 Monee 进程，再打开新版 .app；正在运行的客户端不会自动加载新代码。

共享界面位于 `shared/src/commonMain`，平台入口为 `desktopApp` 和 `webApp`。技术边界和验证结果见 [KMP 体验验证](docs/architecture/kmp-validation.md)。

## 文档与品牌资源

- [开发交接](docs/HANDOFF.md)：方案、产品原则、完成状态、验证缺口和下一步。
- [异地开发指南](docs/operations/development.md)：获取代码、无凭据测试、独立账本与配置恢复边界。
- [Compose 生产部署](docs/operations/compose-deployment.md)：GitHub CI 镜像、服务端/Web 编排、反向代理、验证与回滚。
- [登录与访问白名单](docs/architecture/login-and-access.md)：OAuth、权限模型和实际验收。
- [品牌说明](docs/brand.md)：Logo 原图、Slogan 与使用约定。
- [Logo 原图](assets/brand/logo.png)：1254 × 1254 PNG。
- [应用图标](assets/brand/app-icon.png)：透明边缘派生图，供页面和 Mac 打包使用。
- [MVP 产品讨论稿](docs/product/mvp-discussion.md)：目标、核心流程、统计口径与实施阶段。
- [产品与代码架构提案](docs/architecture/mvp-proposal.md)：跨端方案、本地存储、导入链路与云端兼容性。
- [KMP 体验验证](docs/architecture/kmp-validation.md)：运行方式、共享边界与验证状态。
- [本地数据链路](docs/architecture/local-data-flow.md)：已实现范围、数据规则、启动与验证。
- [OpenAPI](api/openapi.yaml)：当前可调用接口与金额、分页、鉴权契约。

## License

[MIT](LICENSE)

## 旧版历史数据

已提供 [ai-financial 差异分析与迁移适配](docs/migration/ai-financial.md)：Go 生成私有消费候选 CSV、核实清单与完整源快照，再通过现有授权导入流程入账。退款、还款、转账等不强制转成普通收支；生成迁移包不代表正式账本已更新。

## 统一账号与账本

[账号、身份绑定和账本授权](docs/architecture/unified-accounts-ledgers.md)说明已实现模型、权限、旧库升级与备份恢复命令。正式数据库为私有数据目录中的 `application.db`；系统准入不等于所有账本访问权。飞书本期仅预留接口。
