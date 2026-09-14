# 登录与访问白名单

> 账号、权限和数据文件布局已更新：以 [统一账号与多账本模型](unified-accounts-ledgers.md) 为准。当前使用 `application.db`；下文涉及独立 `auth.db` / `monee.db` 和单账本的描述属于旧版。

更新：2026-09-14。Web / Mac 使用同一套 Google / GitHub 登录和后端权限检查，访问的是当前服务的同一份本地账本。白名单成员具有现有账本功能的读写权限；不提供成员间的数据隔离或只读角色。

## 用户行为

- 未登录：仅显示登录页，不读取账单、汇总、导入内容或白名单。
- 已登录但未获准：显示“无数据访问权限”、验证后的账号与稳定 ID，以及切换账号/退出入口。
- 白名单成员：可使用账本；不能读取或修改访问名单。
- 初始超管：可添加白名单、启用/停用成员并查看最近的管理记录。超管条目受保护，网页不能修改其角色或停用它。
- 账本和白名单管理页均提供“退出登录”。退出成功后撤销当前会话，清除 Web cookie / Mac 内存凭据和页面状态，返回登录首页。退出期间暂停登录状态轮询，丢弃旧响应；请求失败时显示错误并允许重试，不把未撤销的会话显示为已退出。
- 页面默认展示账号和状态；账号 ID、备注和操作记录按需展开。操作记录显示操作对象，将初始化操作人显示为“系统”；记录详情中的 UTC 时间仍明确标注时区。登录页的“数据与隐私”说明本机存储、云端备份状态和成员访问范围。
- 停用会在下一次数据请求生效，已经开始的请求不倒回。客户端每 5 秒检查登录状态，发现撤销或失效后销毁账本界面状态。退出和切换账号同样清除当前界面，不保留另一账号的账单表单或明细。

未配置 OAuth 时仍可启动服务，但登录按钮显示“暂不可用”，所有数据入口保持拒绝访问。旧 `/api/v1/session` 和共享本地 Bearer token 已移除；读取 connection.json 不再能取得数据访问权限。

## 首次配置

每个 OAuth 应用都需要 Client ID / Client Secret。凭据只放在本地 Go 服务的私有配置中，不写入 KMP、网页资源或 Git。

1. 在 [GitHub 开发者设置](https://github.com/settings/developers) 创建 GitHub App 或 OAuth App，名称可用 Monee Local，主页填写 `http://127.0.0.1:4173/`，Callback URL 填写 `http://127.0.0.1:4173/auth/callback/github`。使用精确回调，不启用通配符。GitHub App 配置 `github.appType: "github-app"`，使用 Client ID / Client Secret；App ID 和安装私钥不用于本功能。GitHub App 只需识别用户，不需要仓库权限或安装到仓库；其权限由注册设置决定，OAuth scope 不适用。OAuth App 配置 `appType: "oauth-app"`（省略时的默认值），代码只申请 `read:user`。
2. 在 [Google Auth Platform](https://console.cloud.google.com/auth/clients) 为自己的项目配置应用信息、受众及 OAuth 客户端，客户端类型选择 Web application。回调填写 `http://127.0.0.1:4173/auth/callback/google`。若应用处于 External / Testing，把准备登录的账号加入 Google 的测试用户列表；该列表与 Monee 数据白名单是两层不同限制。代码只申请 openid / email / profile。下载的客户端 JSON 应有 `web.client_id` / `web.client_secret`，Client ID 以 `.apps.googleusercontent.com` 结尾；`type: "service_account"` 且含 `private_key` 的服务账号文件不适用于个人登录。
3. 运行下列配置脚本。Client Secret 输入时不会显示，也不会进入命令行参数。已有配置按回车保留。

```sh
python3 scripts/configure-login.py
./scripts/run-local.sh
```

若未来使用自己的域名，需先部署 Go 后端和 HTTPS，并增加远程服务的 Host / Origin / Secure cookie 配置，然后将两家回调改成该域名下的 `/auth/callback/github` 与 `/auth/callback/google`。仅购买域名或配置 DNS 不会使当前本地服务支持公网登录。

如果服务已运行，先在原服务终端停止，再启动。回调端口要和服务一致；使用其他端口时需要在提供方另行登记匹配的回调。

重新构建 Mac 应用后，要完全退出原来的 Monee 进程，再打开新生成的 .app。已运行的 JVM 不会自动加载新包；若界面仍无登录入口并提示旧的连接错误，先检查是否仍运行更新前的客户端。新版首次打开应显示登录页，GitHub 授权完成后会自动进入获准账本。

macOS 默认配置为 `~/Library/Application Support/Monee/auth.json`，权限必须为 0600。服务可通过 `-auth-config` 指定其他私有配置文件，或用 `MONEE_DATA_DIR` 同时改变账本与默认配置目录。字段结构见 [auth.example.json](../../config/auth.example.json)。示例没有预置超管或凭据，不能直接作为已配置系统使用。

### 初始超管

`superadmins` 支持 GitHub 的 `subject` 数值 ID，或 Google 的 `subject` 稳定 ID / `email` 登录邮箱。GitHub 数值 ID 可从 [GitHub 用户 API](https://docs.github.com/en/rest/users/users#get-a-user) 查询；不要把显示名称当成 ID。

首次配置可指定同一位超管的两个提供方入口。初始化记录保存在 auth.db；之后更改配置文件不会自动追加超管。Google 邮箱入口在首次可信 Google 登录后绑定到 sub，后续改邮箱不转移权限。

应用内只管理普通白名单成员，不提供超管自助提升或第一位登录者自动成为管理员的机制。更换超管需要后续专门设计恢复/交接流程；不要通过删除 auth.db 来日常管理权限。

### 添加成员

- GitHub 用户名：后端调用 GitHub 用户 API 验证个人账号，并在添加时解析为数值 ID。用户名变更不转移既有权限；查询失败时不添加。也可直接添加已核实的数值 ID。
- Google 邮箱：作为待绑定邀请。首次登录必须有 Google 验证的 Gmail / Workspace 邮箱声明，再绑定 sub；未受 Google 托管的第三方邮箱应使用登录后显示的稳定 ID。邮箱一旦绑定，不会跟随同邮箱的另一账号。
- Google 账号 ID：直接匹配 Google 验证过的 sub。
- Google / GitHub 使用不同身份命名空间，不根据显示名称或相似邮箱自动合并账号。
- 已停用条目保留 ID、绑定关系与管理记录，可再次启用；重复添加返回冲突。

## 授权与会话

两种提供方均采用 Authorization Code + PKCE S256。每次登录产生独立、一次性、10 分钟有效的 state、浏览器绑定 cookie 和随机验证信息。回调需匹配提供方、state 和发起浏览器；授权码不能重放。回调处理后立即跳转到不含授权码的页面。

Google 使用 go-oidc 验证 RS256 签名、issuer、audience、有效期、nonce，并检查已提供的 azp 和 access-token hash。账号依据 sub。GitHub 使用服务端交换获得的 access token 调用 `/user`，每次登录重新取得 ID 和用户名。提供方 token 只用于这次身份验证，不持久化、不发送给客户端，也不用于读取邮件或仓库。

Web 使用 HttpOnly、SameSite=Lax 的会话 cookie，按服务 origin 区分 cookie 名。所有持 cookie 的受保护写请求额外要求 X-Monee-CSRF。会话状态读取同源返回 CSRF proof；从外部网页不能读取。当前只运行回环 HTTP，因此不设置 Secure；部署到远程 HTTPS 时需独立调整会话与来源配置，当前程序不能直接开放公网。

Mac 通过系统浏览器完成同一提供方授权。客户端先生成随机 proof，后端保存其 SHA-256 challenge；浏览器完成后，只有持有原 proof 的客户端能一次性领取本地会话。授权不会把会话 token 放入回调 URL或共享发现文件。Mac 会话只保存在进程内，退出 App 后需重新登录。

Mac 领取会话后自动恢复最小化窗口并请求切回前台，然后检查白名单权限。系统若限制窗口切换，可点击完成页的“返回 Monee”。打包的 .app 注册固定链接 `monee://auth/complete`，处理器只负责显示窗口，不接受查询参数、片段或其他目标，也不读取任何凭据或改变登录身份。Google / GitHub 控制台仍登记 Go 服务的 HTTP 回调；不把该应用链接用作提供方回调。

浏览器可能询问是否打开 Monee；直接用 Gradle 运行而未启动打包的 .app 时，自定义协议入口可能不可用。若授权期间完全退出 App，原 proof 已丢失，点击返回只能重新打开客户端，需要再次登录。完成页不根据外部 `redirectTo` 跳转，也不签发会话。

本地会话有效期 12 小时，数据库只保存 token 哈希；退出立即撤销。每个数据请求都重新读取白名单状态，不把“已登录”直接当作“有数据权限”。

Host / Origin / Fetch Metadata 防护继续生效。仅 OAuth 回调和明确的顶层页面导航允许跨站 GET；跨站数据请求与 JSON 写请求不被放行。鉴权覆盖 dashboard、template、手动写入、预览和导入确认，不只隐藏页面按钮。

## 数据与兼容性

- 原有 monee.db 的账单结构保持不变。身份、白名单、会话哈希与权限审计保存在同一私有目录下的 auth.db，独立 schema version 1。
- auth.db 不属于当前账本 change_log 同步内容；未来云端认证需专门迁移身份和授权，不能复制本地会话作为云端凭据。
- 本地文件仍依赖操作系统目录权限。OAuth 约束应用/API 访问，不隔离有权限读取数据库文件的本机操作系统用户。
- 不提供开发模式鉴权绕过。合成登录页只存在于 `_test.go` 的隔离测试服务，正常应用二进制不含该入口。

## 接口

详细字段见 [OpenAPI](../../api/openapi.yaml)。

| 接口 | 访问要求 |
| --- | --- |
| GET /api/v1/auth/me | 返回当前登录身份、是否允许、角色、登录方式状态与 CSRF proof；未登录不含身份 |
| POST /api/v1/auth/start | 发起 Web / Mac 登录，提供方必须已配置 |
| GET /auth/begin | 消费一次性登录启动 ticket，绑定浏览器后跳转提供方 |
| GET /auth/callback/{provider} | 校验并完成提供方授权 |
| POST /api/v1/auth/native/poll | Mac proof 绑定的完成查询及一次性会话领取 |
| POST /api/v1/auth/logout | 注销当前会话；Cookie 会话要求 CSRF proof |
| GET /api/v1/admin/allowlist | 仅超管可读取 |
| POST /api/v1/admin/allowlist | 仅超管可添加普通成员 |
| PATCH /api/v1/admin/allowlist/{id} | 仅超管，带条目版本启用/停用，不能修改受保护超管 |
| GET /api/v1/admin/audit | 仅超管，最近 100 条权限操作 |

## 验证范围

Go 回归测试覆盖：未登录/未授权的全部账本入口、普通成员越权、客户端伪造角色、稳定 ID 和提供方隔离、邮箱一次性绑定、即时停用、受保护超管、并发重复添加、会话过期/退出/重启保存、CSRF、state/cookie/提供方绑定、回调重放、Mac proof 和一次性领取、取消/超时，以及实际 RSA 签名的 Google token 校验和 GitHub 身份 API 解析。完成页测试检查固定返回链接、忽略外部跳转参数、不反射 token、不签发会话和禁止缓存；JVM 测试检查只接受无凭据的固定应用链接。

KMP JVM 测试、Web 生产构建与 Mac .app 构建通过。隔离浏览器使用合成提供方和合成账本，验证未授权提示、Google 超管邮箱绑定、添加成员、成员访问和停用拦截，以及服务断开时隐藏账本和显示连接错误、服务恢复后自动重新验证权限。GitHub 真实授权：Web 已由用户确认登录通过；Mac 完全退出旧进程并重新打开新版后，已实测完成系统浏览器授权、自动领取客户端会话、识别超管身份、读取本机账本并进入白名单管理页。未向正式账本写入测试数据，也未改变成员权限。Google 的真实 Web OAuth 客户端已在原开发机配置；已观察到 Mac Google 超管登录、账本读取和首次邮箱绑定记录。Web 完整 Google 登录仍需单独验收。换机配置和密钥备份边界见[异地开发指南](../operations/development.md)。

Mac 自动返回已通过真实 GitHub 授权验证，由用户确认窗口自动切回前台。构建产物的 URL scheme 注册与完成页链接已检查；备用按钮的自定义协议跳转受内置浏览器测试环境限制，尚未完成手动唤起实测，最小化窗口恢复也尚未单独实测。

Web / Mac 均已实测从白名单管理页点击“退出登录”回到登录首页。Web 退出后重新加载页面仍保持未登录，Mac 后续身份刷新也未恢复旧界面。

## 官方依据

- [Google OpenID Connect](https://developers.google.com/identity/openid-connect/openid-connect)：sub、ID token 校验和 nonce。
- [Google Web Server OAuth](https://developers.google.com/identity/protocols/oauth2/web-server)：应用配置、授权码与回环回调规则。
- [GitHub OAuth App 授权](https://docs.github.com/en/apps/oauth-apps/building-oauth-apps/authorizing-oauth-apps)：PKCE、回调与登录后身份查询。

- [GitHub App 用户授权](https://docs.github.com/en/apps/creating-github-apps/authenticating-with-a-github-app/generating-a-user-access-token-for-a-github-app)：Client ID、PKCE 与用户身份查询；GitHub App 不使用 OAuth scopes。
- [Compose 原生分发](https://kotlinlang.org/docs/multiplatform/compose-native-distribution.html)：macOS Info.plist 和深层链接注册。
- [Java Desktop](https://docs.oracle.com/en/java/javase/24/docs/api/java.desktop/java/awt/Desktop.html)：应用 URI 处理器与前台窗口请求。
