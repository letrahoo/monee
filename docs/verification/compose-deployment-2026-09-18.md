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

## 尚未完成

- 当前 Mac 没有 Docker。将未提交候选源码上传到阿里云隔离目录的操作被安全策略阻止，需用户明确授权；因此尚未实际构建镜像、解析 Compose 或完成双容器网络联调。
- GitHub PR 和容器工作流尚未运行，GHCR 尚无本提交镜像。
- 未修改阿里云生产目录、容器、网络、Nginx Proxy Manager、DNS 或证书；没有正式域名切换。
- Google/GitHub 正式回调、真实登录、测试账本操作、证书续期和生产日志仍须在候选镜像通过 CI 后验收。
- 现有 ECS 有两个历史失败 systemd 单元；本轮只读观察，未处理，且未发现与 Monee 候选相关的运行实例。

## 验收结论

代码和非容器回归已通过，任务保持“进行中”。只有在容器镜像构建、Compose 隔离联调和 PR CI 完成后才可转为“待验收”；只有生产切换和约定的真实流程验收完成后才可标记“已完成”。
