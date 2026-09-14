# Monee 开发约定

- 客户端确定采用 Kotlin Multiplatform + Compose Multiplatform；服务端采用 Go。
- 先交付 Web / Mac，随后补齐 iOS / Android。UI 和客户端状态放在 `shared/src/commonMain`，原生能力放在平台层。
- 正式账本、去重、退款、还款和统计规则只在 Go 中实现。默认页面读取真实本地 API，不回退到演示数据；合成样本放在 `fixtures/synthetic`。
- 首版 Web / Mac 共用同一个本地 Go 服务和 SQLite 账本；保留稳定 ID、版本和删除语义以支持未来云端同步。
- 金额传输/存储使用最小货币单位整数或十进制字符串，不经由浮点金额运算。
- 真实账单、邮箱凭据、数据库、构建缓存和本机路径不得提交。
- 品牌原图以 `assets/brand/logo.png` 为准并保留不变；应用使用其透明边缘派生图 `assets/brand/app-icon.png`，构建时生成 Compose 资源与 ICNS。
- Slogan 保持 `Know Your Money. Own Your Future.`。

- 所有账本与管理接口必须由 Go 验证登录和白名单；不能恢复共享本地 token、首个登录者自动成为超管或生产鉴权绕过。
- Google 绑定 sub、GitHub 绑定数值 ID；用户名/邮箱只能经验证后关联。OAuth 密钥和超管私有配置不能进入 Git。
- 合成身份提供方只能在 `_test.go` 中用于隔离测试，不得注入正常服务入口。

## 常用检查

- `./gradlew :shared:jvmTest`
- `./gradlew :desktopApp:createDistributable :webApp:wasmJsBrowserDistribution`
- `go -C server test -race ./...` 与 `go -C server vet ./...`
- `./scripts/run-local.sh` 启动同源 Web / API；不要用独立静态服务冒充完整应用。

## 设计依据

- 产品范围：`docs/product/mvp-discussion.md`
- 架构：`docs/architecture/mvp-proposal.md`
- KMP 验证：`docs/architecture/kmp-validation.md`
- 当前数据链路与接口：`docs/architecture/local-data-flow.md`、`api/openapi.yaml`
- 登录与授权：`docs/architecture/login-and-access.md`
- 品牌：`docs/brand.md`
