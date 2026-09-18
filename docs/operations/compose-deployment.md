# Compose 生产部署

Monee 的生产部署使用两个运行容器，但保持一个浏览器同源：

- `api`：Go 模块化单体、OAuth、账本规则和 SQLite；连接内部网络和独立出网网络，以访问 Google/GitHub 的发现、令牌和身份接口。API 不加入外部代理网络，也不发布主机端口。
- `web`：Kotlin/Wasm 静态资源，并把 `/api/`、`/auth/` 转发给 `api`；通过共享的代理网络接入现有 Nginx Proxy Manager。
- `data-init`：启动前把私有 `auth.json` 复制进数据卷，校正为非 root 运行用户可读的 `0600`；它不是常驻服务。

浏览器和未来 iPhone / Android 客户端都使用 `api/openapi.yaml` 的稳定契约。移动应用不进入服务器 Compose；它们继续从 `shared/src/commonMain` 复用模型、状态和 UI，在 GitHub CI 中独立构建、签名和发布。

## 构建与发布边界

`.github/workflows/container-images.yml` 在 PR 上构建镜像并运行隔离 Compose 验证，在 `main`、版本标签或手动运行时发布：

- `ghcr.io/letrahoo/monee-api:sha-<完整提交>`
- `ghcr.io/letrahoo/monee-web:sha-<完整提交>`

生产部署必须使用同一个不可变 `sha-...` 标签，不使用 `latest`。原有 `build.yml` 继续负责 Go 测试、KMP 测试、Web 构建和桌面发行资源；容器工作流只增加可部署镜像，不替代客户端 CI。

首次发布后应将两个 GHCR package 设为 public；若保持 private，服务器使用只有 `read:packages` 的令牌登录 GHCR，不使用个人全权限令牌，也不把令牌写入 `.env`。

`bash scripts/smoke-compose.sh` 使用本地 `monee-api:smoke`、`monee-web:smoke` 镜像，创建独立网络、数据卷和临时 TLS 代理。验证证书、静态资源、路由刷新、同源 API、401/403、OAuth 跳转与安全 Cookie、API 出网、凭据权限和重启健康；还会强制重建 API 并占住旧 IP，确认 Web 无需重启即可恢复 API 和登录代理。结束后清理本次创建的资源。只使用合成 OAuth 配置，不完成真实用户登录。需要 Docker、Compose v2、OpenSSL 和 jq。

Web 使用 Docker 内置 DNS 和 Nginx 动态上游解析（开源版要求 Nginx 1.27.3 或更新），API 容器替换时最多约 5 秒后刷新地址；不要将其改回仅启动时解析的固定上游配置。

## 首次准备

目标主机需要 Docker Compose v2，并需要一个供反向代理与 Web 容器共享的网络：

```sh
docker network create monee-edge
docker network connect monee-edge nginx-app
```

创建独立发布目录，将仓库中的 `compose.yaml`、`deploy/compose.env.example` 和 `deploy/secrets/auth.example.json` 作为模板复制进去。真实文件建议布局如下：

```text
/opt/apps/monee/current/
├── compose.yaml
├── .env                 # 0600，不进 Git
└── secrets/
    └── auth.json        # 0600，不进 Git
```

`.env` 至少填写：

```dotenv
MONEE_PUBLIC_URL=https://finance.letra.xin
MONEE_IMAGE_TAG=sha-<main 上已通过 CI 的完整提交>
MONEE_EDGE_NETWORK=monee-edge
MONEE_DATA_VOLUME=monee-data
```

OAuth 应用回调地址必须与正式域名一致：

```text
https://finance.letra.xin/auth/callback/google
https://finance.letra.xin/auth/callback/github
```

`auth.json` 按 `deploy/secrets/auth.example.json` 填写。不要把 Client Secret、账号清单或数据库放进 GitHub Actions 产物或镜像。
如果密钥目录不随发布目录切换，可通过 `MONEE_AUTH_DIR` 指向主机上的绝对目录；目录和文件仍应分别限制为 `0700`、`0600`。

## 候选版本检查与切换

先拉取固定版本并检查最终配置：

```sh
docker compose --env-file .env config
docker compose --env-file .env pull
```

对现有 `monee-data` 做一致性备份并记录当前镜像标签后，再启动候选版本。API 镜像内置只读打开 SQLite、包含 WAL 已提交内容并校验完整性的备份工具；备份输出目录必须事先不存在：

```sh
mkdir -p backups
chown 65532:65532 backups
chmod 0700 backups
backup_name="application-$(date -u +%Y%m%dT%H%M%SZ)"
docker compose --env-file .env run --rm --no-deps \
  -v "$PWD/backups:/backup" \
  --entrypoint /monee-backup api \
  -source /data/application.db -out "/backup/$backup_name"
```

备份完成后检查目录内有 `database.sqlite3` 和 `manifest.json`，并把整个目录复制到独立存储。`auth.json`、`redaction.key` 和 `.env` 不包含在数据库快照中，必须通过独立的加密备份保存。随后启动候选版本：

```sh
docker compose --env-file .env up -d
docker compose --env-file .env ps
```

Nginx Proxy Manager 的上游使用共享网络内的 `monee-web:8080`，公网只开放正式域名的 80/443。不要发布 `api:4173` 或 `web:8080` 到主机公网端口。

切换后至少验证：

1. `api`、`web` 健康，`data-init` 成功退出。
2. `https://<域名>/api/v1/health` 返回 `200`。
3. 未登录访问账本接口返回 `401`，错误 Origin/Host 返回 `403`。
4. Google、GitHub 回调实际使用 HTTPS 正式域名，Cookie 带 `Secure`、`HttpOnly`、`SameSite=Lax`。
5. 登录、账本读取及一次独立测试账本操作通过；日志没有新增高优先级错误。
6. 证书主机名、有效期和自动续期正常。

## 回滚

回滚只修改 `.env` 中的 `MONEE_IMAGE_TAG` 为上一个已验证的完整提交，再执行 `docker compose --env-file .env up -d`。如果新版本包含数据库迁移，先停止服务并按对应版本的备份/恢复说明回滚数据；不要直接覆盖运行中的 SQLite 文件。

数据库恢复必须在 API 停止后进行，并先用 `/monee-backup -restore BACKUP_DIR -out NEW_DB` 恢复到一个不存在的新文件；核对校验结果后保留当前数据库，再原子切换新文件。不要把恢复目标直接指向运行中的 `/data/application.db`。

## 后续移动端扩展

Compose 部署不会阻碍移动端，但上线移动端前仍需完成：

- 为远程 API 明确版本兼容和最低客户端版本；保持整数分/十进制字符串金额语义。
- 将桌面专用浏览器回跳替换为 iOS Universal Links / Android App Links，并为移动端使用 PKCE；服务端继续验证 provider 稳定 ID。
- iOS 使用 Keychain、Android 使用 Keystore 保存短期会话，不在 App 包或 CI 中嵌入 OAuth Client Secret。
- GitHub Actions 增加 Android 测试/签名产物；iOS 构建使用 macOS runner，签名和 TestFlight 发布使用环境保护与独立 Secret。
- SQLite 仍只属于服务器；手机端通过 API 访问，离线同步与冲突解决应作为单独协议设计，不能复制数据库文件。
