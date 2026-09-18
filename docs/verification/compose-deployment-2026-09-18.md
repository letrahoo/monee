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

新增 `scripts/smoke-compose.sh` 与独立 CI job：合成配置、独立数据卷、一次性可信测试证书和 TLS 代理；检查启动健康、静态资源与刷新路由、同源 API、401/403、登录跳转及安全 Cookie、API 网络命名空间访问 Google/GitHub、卷权限和重启。镜像发布依赖该检查成功。新增检查执行结果待回填；合成登录跳转不等于真实 OAuth 登录验收。
