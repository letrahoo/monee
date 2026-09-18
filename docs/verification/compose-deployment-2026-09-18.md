# M-38 HTTPS 容器化生产部署验证（2026-09-18）

分支 `feat/compose-deployment`，基线为 main `f0b06a4`。本轮只建立可部署、可回滚的候选链路，不代表已经切换生产域名或完成正式 OAuth 验收。

## 实现范围

- API 使用 Go 静态二进制和 distroless 非 root 运行镜像；同时携带已实现的一致性快照/离线恢复工具。
- Web 使用 JDK 17 构建 Kotlin/Wasm 生产资源，由非特权 Nginx 提供静态文件并同源代理 `/api/`、`/auth/`。
- Compose 使用内部 API 网络、外部代理网络、私有数据卷、只读根文件系统、能力裁剪、健康检查和一次性凭据初始化容器。
- 服务端支持显式 HTTPS 公网 Origin，继续校验 Host、Origin 和 Fetch Metadata；生产登录流及会话 Cookie 使用 `Secure`、`HttpOnly`、`SameSite=Lax`。
- GitHub Actions 在 PR 构建并冒烟检查两个镜像，在 main、版本标签或手动运行时发布带完整提交 SHA 的 GHCR 镜像。
- 部署文档记录密钥边界、固定镜像标签、数据库与密钥分别备份、候选切换和回滚。

## 已完成验证

- `go -C server test -race ./...` 与 `go -C server vet ./...` 通过。
- `:shared:jvmTest`、`:webApp:wasmJsBrowserDistribution`、`:desktopApp:createDistributable` 在 Temurin 17 下通过。
- `git diff --check` 通过。
- 本机临时服务以 `https://finance.letra.xin` 作为公网 Origin 启动；正确 Host/Origin 的健康检查返回 200，错误 Origin 返回 403，未登录账本请求返回 401。
- 合成 GitHub OAuth 启动流返回正式 HTTPS 回调地址；绑定 Cookie 实测包含 `Secure`、`HttpOnly`、`SameSite=Lax`，没有使用真实密钥或向提供方完成授权。
- 阿里云 ECS 仅做只读预检：Docker 25.0.0、Compose 2.24.1、Buildx 0.12.1 可用，现有 Nginx Proxy Manager 和其他容器未变更。
- PR #12 首轮 API 镜像构建及冒烟通过；Web 镜像首次构建发现 Kotlin/Wasm 使用的 Node 25 缺少 `libatomic.so.1`，已在构建阶段显式补充最小 `libatomic1` 依赖并重新触发 CI。
- Web 镜像第二轮构建已通过，独立冒烟因没有 Compose 提供的 `api` DNS 名称而退出；已为单镜像检查加入占位解析并保留失败日志。正式 Compose 仍使用内部网络服务发现，不使用该占位配置。
- PR #12 最新提交 `a54dde5` 的 Linux、Mac ARM64、Mac AMD64、API 镜像和 Web 镜像五项 CI 均通过。两个镜像在 PR 中实际装载运行；API 检查覆盖健康、错误 Origin 403、未登录 401，Web 检查覆盖首页和 HSTS 响应头。

## 尚未完成

- 前轮 Mac 没有 Docker，候选源码上传阿里云被安全策略阻止；镜像构建已由 GitHub 完成，完整 Compose 联调尚未完成。2026-09-19 本机已检测到 Colima Docker，新增隔离 Compose CI 可在 GitHub runner 上完成联调，无需修改生产主机。
- PR 验证阶段不发布镜像；只有合并 main、版本标签或手动运行工作流后才会生成 GHCR 镜像。因此当前没有可用于生产切换的 `sha-a54dde5...` 发布物。
- 未修改阿里云生产目录、容器、网络、Nginx Proxy Manager、DNS 或证书；没有正式域名切换。
- Google/GitHub 正式回调、真实登录、测试账本操作、证书续期和生产日志仍须在候选镜像通过 CI 后验收。
- 现有 ECS 有两个历史失败 systemd 单元；本轮只读观察，未处理，且未发现与 Monee 候选相关的运行实例。

## 验收结论

代码、非容器回归、镜像构建、单镜像运行冒烟和 PR CI 已通过，任务可转为“待验收”。完整 Compose 隔离联调、正式发布物、生产切换及约定的真实 OAuth/证书流程仍未完成，不能标记“已完成”。

## 2026-09-19 接续

复核发现 `api` 仅接入 `internal: true` 网络，无法访问 OAuth 发现/令牌/身份端点。已保留隔离的 API/Web 通道，给 API 增加独立出网网络，继续不发布主机端口、不加入外部代理网络。任务恢复“进行中”。

新增 `scripts/smoke-compose.sh` 与独立 CI job：合成配置、独立数据卷、一次性可信测试证书和 TLS 代理；检查启动健康、静态资源与刷新路由、同源 API、401/403、登录跳转及安全 Cookie、API 网络命名空间访问 Google/GitHub、卷权限和重启。镜像发布依赖该检查成功。

功能提交 `280036092c0b128b75bde1929c83f1fe567ee465` 的结果：

- [三平台构建](https://github.com/letrahoo/monee/actions/runs/35372859787)：Linux、Mac ARM64、Mac Intel 全部通过，包含 Go race/vet、共享测试、Web 生产构建及 Mac 分发构建。
- [容器构建及联调](https://github.com/letrahoo/monee/actions/runs/35372859846)：Compose、API 镜像、Web 镜像全部通过。Compose 实际运行 1 分 6 秒，可信测试 TLS、两个应用容器、初始化卷、反向代理和出网检查成功；测试资源自动清理。
- 本机同一源码再次完成 Go race/vet、Temurin 17 下共享测试和 Web/Mac 构建（部分命中缓存）。
- 通过当前 Mac 分发包内置服务，在临时目录和 `127.0.0.1:4278` 启动独立数据目录。Chrome 与当前 Mac App 均显示登录页，Google 正确显示未配置，GitHub 入口启用；公开状态为未登录。未复制会话、未写真实账本、未完成真实提供方授权。
- 第一轮 Compose job 因合成配置文件名命中全局 `auth.json` 忽略规则失败；已改成明确的 `auth.synthetic.json`，上述通过运行包含此修正。

仍需真实账号登录及登录后的独立账本验收。现有 `monee.test.letra.xin` DNS 指向本机 LAN，证书校验通过，匿名请求由外层认证拦截为 401；它连接的持久化运行目录与本轮构建不同，不能把它的状态充当候选 Compose 的验收结果。任务范围明确排除自动修改现有反向代理，因此临时接入候选环境已向用户请求确认。批准后应保留当前路由和运行版本、只使用独立测试卷/账本、验证登录退出及读写，再恢复原入口；不修改已有真实数据、DNS、信任设置或阿里云生产服务。

结论：开发与可执行隔离验证通过，M-38 保持“待验收”，不能据此宣称全部功能完成。

## 2026-09-19 授权后的独立审查与候选验证

本节更新上述历史状态。用户明确授权临时切换现有测试入口、使用 GitHub Agent 审查，并在构建、测试和运行验收都通过后直接合并并领取下一项任务。

- 独立 Agent 复现 API 容器更换 IP 后 Nginx 持续 502；增加动态 DNS 解析及强制占用旧 IP 的替换回归。
- GitHub Agent 的第一轮四项问题均已修复并逐项回复：停止所有写入者后 DB/WAL/SHM 整组保留恢复；统一公网 Origin；分别固定 API/Web digest 并记录发布证据；查询参数日志保护和容器日志轮转。
- `fee2d61bec0a6a3bb560b7cc1590bb73ea43075a` 的[三平台构建](https://github.com/letrahoo/monee/actions/runs/35377000559)和[Compose/镜像 CI](https://github.com/letrahoo/monee/actions/runs/35377000532)共六项检查通过。本机完整 Go race/vet、配置引用及脚本检查通过；独立 Agent 复查无 P1/P2。
- GitHub 第二轮指出展开式 IPv6 与 Unicode IDN 仍与浏览器序列化不一致。本次追加普通 IPv6 压缩、ASCII DNS/punycode 校验和歧义地址拒绝用例；这项新修复需以其后续提交的 CI 结果单独验证，不沿用 `fee2d61` 的成功状态。
- 本机使用独立候选数据卷和原私有 OAuth 配置的受限副本。原持久化服务及真实账本保留，未复制真实账本或登录会话，没有向正式账本试写。
- 临时可信 HTTPS 测试入口实测：认证后首页、刷新路由、JS、健康和公开登录状态均为 200；账本接口未登录为 401，外层匿名访问为 401。证书校验未绕过。浏览器仍弹出外层 Basic Auth，真实 OAuth/登录后操作未完成。
- 本机 API 为当前源码的 ARM64 镜像；Web 为本 PR `1c6ad50` 的 CI 编译资源和当前 Nginx 配置组装的 ARM64 验收镜像。其资源代码没有后续客户端改动，但不是正式 AMD64 Dockerfile 产物的字节相同副本；完整双 Dockerfile 由 CI 构建验证。
- 本机额外完整 Compose 回归在下载官方 `nginx:1.29-alpine` 时两次遇到 registry EOF，未完成；独立合成容器和卷已由测试清理。不能写成本机完整 Compose 已通过，同提交 GitHub Compose 已通过。

PR 保持未合并；Notion 保持待验收。首次正式发布 digest artifact、真实登录及账本操作仍是后续门槛。没有修改阿里云环境。下一项 M-08 仅预读需求，未开始实现。

## 2026-09-19 认证入口调整与 HTTP 边界

用户明确要求移除 Monee 最外层 Basic Auth。已仅调整 Monee 的本机 LAN/Tailnet 入口，保留应用 Google/GitHub OAuth、账号准入、账本授权及安全响应头；简历/Hermes 仍需外层密码。实测无凭据首页 200、未登录账本 401、其他站点 401、可信 HTTPS 和 HTTP 308 正常。隧道不再无条件改写 Origin，只转换既有正式别名，错误来源仍为 403。现有应用会话刷新后有效，未对真实账本试写。

GitHub 第三轮指出通配监听可误配 HTTP loopback Origin。HTTP 例外现同时要求 loopback-only 监听和 loopback Origin，补两个拒绝用例；本机完整 Go race/vet 与独立复查通过。后续提交须重新完成 CI；真实候选 OAuth/账本验收继续，不能用原服务登录成功代替。
