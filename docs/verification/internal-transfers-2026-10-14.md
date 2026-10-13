# M-09 接续：重试、权限与并发自动回归（2026-10-14）

## 基线与交付状态

本轮接续 `feat/internal-transfers` / [草稿 PR #14](https://github.com/letrahoo/monee/pull/14)，初始提交 `3c8c364`，主干 `72f2a37`。接续时本地 main 与 fetch 后 origin/main 一致，工作区干净；PR #14 尚未合并，初始提交六项 GitHub CI 成功（release 按条件跳过）。退款 M-08 已进入主干，不代表 M-09 已完成。

本轮代码验证提交：`315eeb140953b4914f1d8aa4d212ba68f963c7ab`；后续文档提交不改变已验证代码。

本轮补强可执行的自动回归，不扩展 M-10/M-11，不合并、不部署、不修改正式账本、路由、OAuth 或系统配置，不进行收费模型调用。Mac 自动输入问题维持用户已有的延后决策。

## 增量

- `LedgerApi` 增加 internal 依赖注入构造器供隔离测试使用；公开无参构造器仍绑定原平台 HttpClient、服务发现与安全会话存取。未新增正常入口中的合成身份提供方、鉴权绕过或 TLS 放宽。
- 新增 4 项 JVM API 生命周期测试，直接调用 `LedgerApi` 经 Ktor MockEngine 序列化/发送/处理响应：IOException 丢响应、500、201 无法解码均保留原输入及 key，成功重试清理；请求开始后取消保留 pending，取消异常传播；invalid/conflict 清理，权限/未登录拒绝保留；串行账本、账号、origin 切换拒绝错误 scope，返回原 scope 恢复原请求，另一账本成功不会清理它。
- 记录并断言 POST、账本、幂等 key、body、CSRF 与合成 Bearer，使用纯内存 vault 验证退出清理。测试不连接真实服务、身份提供方或钥匙串。
- 新增 user-scoped Store 权限测试：取得 Store 后在独立 DB 连接禁用准入、停用用户、降为 viewer、移除成员。已有 key 重放与新 key 创建均须拒绝，交易/分录/账户/幂等/审计表数量及账本版本不变；恢复后原 key 返回原 ID，新 key 未被拒绝操作消耗。
- 收紧八请求、两 Store 的并发回归：仅允许明确 SQLite BUSY/LOCKED，其他错误不再静默忽略；竞争结束后所有失败请求及两 Store 重放均不增加写入数量或版本，重新打开和快照恢复后同样检查。

## 环境与结果

- macOS ARM64；Go 模块实际工具链 `go1.26.8`，Gradle `8.14.4`，Temurin `17.0.20.1+1`。JDK 放在 Git 忽略的工作区工具链目录，不修改系统安装。
- `go -C server test -race ./...` 与 `go -C server vet ./...`：最终修正测试初始化顺序后通过。
- `:shared:jvmTest`、`:webApp:wasmJsBrowserDistribution`、`:desktopApp:createDistributable`：最终全部通过（Temurin，BUILD SUCCESSFUL）。新增 TransferApiTest 4 项，0 失败/0 错误；Mac 打包任务完成代码签名校验。Web 保留 dynamic dependency 与 bundle size 警告，不作性能验收通过结论。
- 两轮独立只读审查未发现可确认的增量缺陷；其建议已补请求头/内存会话检查。审查不替代构建与实际验收。

### 已调查并处理的检查失败

1. 初次 Go/Gradle 因工作区外缓存写权限失败；经批准重跑。不将权限错误记为代码失败或通过。
2. Mac 打包使用独立 Go 缓存时依赖下载 unexpected EOF；改为复用已通过 Go 测试的依赖缓存，不改依赖版本。
3. Homebrew JDK 被 Compose 打包厂商检查拒绝；使用隔离 Temurin，未关闭该检查。
4. 新授权测试初始先 auth.Open 后 ledger.Open 导致共享 schema 初始化失败；改为与正常服务/API 测试一致，先 ledger.Open，再 auth.Open。仅修正测试 fixture，最终全量 race/vet 通过。

## 未完成与验收边界

本轮没有浏览器或 Mac 桌面交互工具，未新增真实 OAuth 后的 Web/Mac 操作验收。沿用历史已通过证据，不把自动测试标为实际界面通过，不对当前含私有数据的测试实例做故障注入。

仍需在不含真实资料的独立实例/合成账本，经正常登录补齐两端成功响应丢失后的表单关闭/页面重建重试、受限身份/降权提交/撤权/幂等权限重放、候选服务重启及相关异常与退款回归。未齐前 PR #14 保持草稿、Notion M-09 保持进行中，不领取 M-10。

MockEngine 不证明实际服务恰好一次入账，不运行 Compose 屏幕生命周期，也不覆盖在途账号/origin 切换竞态、完整凭据 origin 分桶或整页刷新/进程退出的 pending 持久化。Store 授权补测不是 HTTP 会话验收；表数量/版本不变量不代表逐行内容全量快照验证。并发测试允许锁竞争，但有限调度不保证覆盖所有交错。
