# 换机开发与运行

> 账号、权限和数据文件布局已更新：以 [统一账号与多账本模型](../architecture/unified-accounts-ledgers.md) 为准。当前使用 `application.db`；下文涉及独立 `auth.db` / `monee.db` 和单账本的描述属于旧版。

更新：2026-09-14。目标是仅依赖远程仓库即可阅读、构建和测试；真实 OAuth 登录需要另行取得凭据。继续开发不要求访问原开发机、原聊天历史或任何个人数据库。

## 获取当前代码

```sh
git clone --branch main https://github.com/letrahoo/monee.git
cd monee
# 可选：使用已登录的 GitHub CLI 核对远程进度
gh pr view 3
```

接续前读 [AGENTS.md](../../AGENTS.md) 和[交接摘要](../HANDOFF.md)。当前 main 已包含 #2/#3 全部改动；已有克隆先执行 `git switch main` 和 `git pull --ff-only origin main`。如本地有未提交改动，先保留它们再切换，不强制覆盖。

## 工具与首次构建

- JDK 17；通过 JAVA_HOME 或本机工具配置，不复制旧电脑的绝对路径。
- Go 1.26.8，或支持自动下载模块所需工具链的 Go；仓库锁定 `go 1.26.0` / `toolchain go1.26.8`。
- Python 3 用于交互式登录配置，普通应用构建不需要 Pillow。
- Gradle 8.14.4 随 wrapper 锁定并校验；首次需要联网取得依赖。Kotlin 2.4.10、Compose 1.11.1、Ktor 3.5.1 等以版本目录为准。
- macOS 验证完整桌面分发；Linux 可开发 Go/Web、运行 JVM 测试，Mac .app 打包与系统行为在 macOS 验收。当前不承诺 Windows 服务运行（锁实现仅 macOS/Linux）。

```sh
# 不需要个人凭据或个人数据库
go -C server test -race ./...
go -C server vet ./...
./gradlew :shared:jvmTest :webApp:wasmJsBrowserDistribution
# macOS 追加
./gradlew :desktopApp:createDistributable
```

生成目录：`webApp/build/dist/wasmJs/productionExecutable/`、`desktopApp/build/compose/binaries/main/app/Monee.app`。这些产物由源码生成，不需要从原机拷贝。原机依赖及构建缓存仅用于加速，不属于源码交付内容；首次在新机器构建需要下载依赖。

## Mac 安装包中的本地服务

分发构建会先编译 Go 服务及 Web，并放入 App 资源目录；打包任务显式追踪两者的内容变化，防止增量构建继续使用旧服务或旧页面；不包含个人 OAuth 配置、数据库或登录态。打包后恢复 Go 可执行权限并重新进行本地 ad-hoc 签名。该签名用于本地完整性验证，不等于 Developer ID 签名或 Apple 公证。

已配置独立数据目录时，可直接启动打包 App；无需事先运行开发服务器。App 会核对 `connection.json` 与 `/api/v1/health` 的实例 ID/协议版本，复用匹配服务或启动内置服务，默认监听 `127.0.0.1:4173`。服务使用数据库目录独占锁，桌面使用独立的启动锁。身份信息只用于服务发现，所有账本请求仍须登录授权。

旧版服务缺少实例身份时，App 会拒绝复用并显示错误；由操作者确认并关闭旧服务后重试，不自动杀进程。端口被其他程序占用、配置损坏或数据目录不一致时同样停止并提示。关闭 Mac 窗口/退出 App 不终止服务，Web 和其他客户端可以继续使用；升级服务须先确认使用情况再重启。

`./gradlew :desktopApp:run` 是开发入口，仍应先通过 `./scripts/run-local.sh` 启动当前版本服务；自动启动依赖打包资源。使用自定义数据目录测试时，启动 App 的进程必须继承同一个 `MONEE_DATA_DIR`。新电脑仍须单独配置 OAuth，服务随包不等于账号配置随包。

## 独立开发账本与 OAuth

建议在新机器创建独立开发目录；不要连接、覆盖或迁移原有正式数据来运行测试。以下环境变量在服务和 Mac 的终端都要设置：

```sh
export MONEE_DATA_DIR="$HOME/.monee-development"
python3 scripts/configure-login.py
./scripts/run-local.sh
# 保持服务运行，浏览器访问 http://127.0.0.1:4173/
```

另一个终端：

```sh
export MONEE_DATA_DIR="$HOME/.monee-development"
./gradlew :desktopApp:run
```

打包应用的 URL scheme 验证应启动构建的 .app；GUI 启动不一定继承终端的 MONEE_DATA_DIR，因此必须确认它与服务使用同一数据目录。默认 macOS 为 `~/Library/Application Support/Monee/`。不要把该变量当成全局配置已经永久保存。

未配置 OAuth 也能启动与查看登录页、访问 `/api/v1/health`，但数据 API 拒绝访问。后端自动化测试使用隔离的合成提供方，不要求真实登录。完整手工操作需要配置至少一家提供方和自己的初始超管身份，不提供绕过开关。

| 提供方 | 应用设置 |
| --- | --- |
| Google | Web application；重定向 URI `http://127.0.0.1:4173/auth/callback/google`；JavaScript 来源可留空，或填 `http://127.0.0.1:4173` |
| GitHub | GitHub App 或 OAuth App；回调 `http://127.0.0.1:4173/auth/callback/github`；GitHub App 的 appType 填 github-app |

配置工具要求交互式终端，密钥输入不回显；新账本指定自己的 GitHub 数值 ID / Google 邮箱作为初始超管。详情见[登录指南](../architecture/login-and-access.md)。改变端口必须同时在提供方控制台登记实际回调；不要混用 localhost 与 127.0.0.1。

Google Web/Mac 当前共用上述服务端回调。未来移动端需单独规划 Android 包名/签名、iOS Bundle ID 和原生登录验证；手机的 127.0.0.1 不指向电脑。远程 HTTPS、移动客户端与云端部署均未实现，不能通过修改监听地址直接开放现有服务。

## 私有资料与恢复边界

| 资料 | 当前存放/用途 | 换机时怎么办 |
| --- | --- | --- |
| 源码、品牌、合成样本、版本锁、文档 | Git 分支与 PR | 克隆仓库即可 |
| auth.json | 私有数据目录，0600；OAuth 凭据及初始超管配置 | 从自己的安全备份恢复，或创建独立开发 OAuth 应用并重新配置 |
| auth.db | 白名单、身份绑定、会话哈希、权限记录 | 不在 Git；新开发库独立初始化，已有名单需要另外安全迁移 |
| monee.db | 正式账本、来源记录与同步基础字段 | 不在 Git；尚无产品级完整备份/恢复功能 |
| connection.json | 只有本地 baseUrl | 服务重建；不要从旧机器复制作为认证方式 |
| 构建缓存、应用包、临时工具 | 可再生成 | 不应成为接续前提 |

原开发机曾把 Google/GitHub 的 OAuth 配置保存到 macOS 钥匙串：条目 **Monee OAuth Backup**，账号 **local-config-v1**，已读回校验。它是一次本机加密备份，**未实现自动更新、云端同步或应用自动读取钥匙串**。该条目包含私有配置，不包含账本，也不是可从 Git 找回的备份。

原机不可用时，可以通过提供方管理控制台创建新的开发 OAuth 客户端/密钥，配置新开发账本；无须依赖旧密钥。若要保留原身份配置和真实账本，需要负责人另行通过可信密码库或加密备份交付。尚未选定异地存储目的地，未向任何云端上传私有材料。

不要把 auth.json、数据库、Google 下载文件、实际账号值或密钥放入 Git、PR、Issue、CI 日志。不要把运行中的 SQLite 单文件直接复制作为一致备份；正式迁移需要一致性快照和恢复校验。不要删除 auth.db 来重置现有超管，首次配置不会覆盖已初始化权限。

## 冒烟与常见问题

1. `./scripts/run-local.sh` 成功后，访问 `/api/v1/health`；登录页应出现已配置的提供方按钮。
2. 登录 → 账本 → 管理白名单（超管）→ 退出；退出后刷新应保持未登录。
3. 在独立开发账本导入 `fixtures/synthetic/standard.csv`，核对预览与确认；重复导入不增加交易。不要在正式账本测试。
4. Mac 登录需由同一系统浏览器完成完整跳转，不能把 provider URL 复制到另一个浏览器；浏览器绑定 cookie 会不匹配。
5. Google 要求密码、通行密钥或安全验证时由账号持有人完成；OAuth Testing 用户列表与应用白名单是两层限制。
6. 数据连接失败：检查服务存活、同源页面、数据目录与旧进程。不要恢复演示数据兜底。
7. Web Wasm 弹窗有既往无障碍更新问题，当前采用行内展开；不要未经复验恢复弹窗。
8. 重建 Mac 后完全退出旧进程再打开新包；登录只在内存中，重启后重新验证是当前设计。

验证记录和未通过项见[交接摘要](../HANDOFF.md)。本指南不把“可以在别处开发”混同为“现有个人账本已完成云同步”。
