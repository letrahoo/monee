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

`sha-...` 是方便追溯提交的标签，并非不可变内容：同一提交重新构建时，基础镜像或构建环境变化仍可覆盖标签。生产部署必须分别固定 API、Web 的 `image@sha256:<64位内容摘要>`，两者取自同一次成功发布运行。工作流在各镜像 job 摘要输出完整引用，并上传 `deployment-reference-api-<运行次数>` / `deployment-reference-web-<运行次数>` 制品，记录 digest、提交、运行 ID 和重试次数；部署前核对两份记录属于同一成功运行及同一提交。不要在部署时重新解析可变标签替代已记录的 digest。原有 `build.yml` 继续负责 Go 测试、KMP 测试、Web 构建和桌面发行资源；容器工作流只增加可部署镜像，不替代客户端 CI。

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
MONEE_API_IMAGE=ghcr.io/letrahoo/monee-api@sha256:<该次发布的API摘要>
MONEE_WEB_IMAGE=ghcr.io/letrahoo/monee-web@sha256:<该次发布的Web摘要>
MONEE_EDGE_NETWORK=monee-edge
MONEE_DATA_VOLUME=monee-data
```

OAuth 应用回调地址必须与正式域名一致：

公网地址会统一小写、默认端口和 IPv6 压缩写法。国际化域名须填写 ASCII punycode（例如 `xn--bcher-kva.example`），不接受原始 Unicode 域名；IPv4 须使用标准四段十进制，不支持简写、八进制、十六进制、带区域标识或 IPv4-mapped IPv6，以免浏览器与服务端对同一地址产生不同解释。

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

对现有 `monee-data` 做一致性备份并记录当前 API/Web 完整 digest 引用后，再启动候选版本。API 镜像内置只读打开 SQLite、包含 WAL 已提交内容并校验完整性的备份工具；备份输出目录必须事先不存在：

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

应用 Web 代理的访问日志只保留请求路径和状态，不记录查询参数或 Referer；`/auth/` 和 `/api/` 的错误日志停用，避免 Nginx 在上游故障时写出 OAuth ticket、code、state 或财务搜索条件。外层 Nginx Proxy Manager 或其他 TLS 代理也必须使用不含查询参数/Referer 的访问日志，并对这两类路径禁用未脱敏错误日志，否则敏感信息仍可能进入外层日志。排障使用路径、状态码和 Go 服务的无凭据错误结果。

Compose 对 API/Web 的容器日志均设置 `json-file` 轮转：每份最多 10 MB，保留 3 份，避免访问日志持续占满承载 SQLite 的磁盘。外层代理也须单独设置日志轮转；保留份数到限后旧日志会被轮换删除，需要长期审计时应提前送到独立存储。

切换后至少验证：

1. `api`、`web` 健康，`data-init` 成功退出。
2. `https://<域名>/api/v1/health` 返回 `200`。
3. 未登录访问账本接口返回 `401`，错误 Origin/Host 返回 `403`。
4. Google、GitHub 回调实际使用 HTTPS 正式域名，Cookie 带 `Secure`、`HttpOnly`、`SameSite=Lax`。
5. 登录、账本读取及一次独立测试账本操作通过；日志没有新增高优先级错误。
6. 证书主机名、有效期和自动续期正常。

## 回滚

没有数据库迁移时，把 `.env` 的 `MONEE_API_IMAGE`、`MONEE_WEB_IMAGE` 一起恢复为上一份已验证发布记录中的 digest 引用，再执行 `docker compose --env-file .env pull` 和 `docker compose --env-file .env up -d`。不要用可能已被重建覆盖的 `sha-...` 标签代替历史 digest。

需要恢复数据库时，先明确恢复将放弃备份时间之后的账本修改，并保留当前数据用于人工核对。SQLite 的 `application.db`、`application.db-wal` 和 `application.db-shm` 属于同一组运行状态；绝不能只覆盖主文件后留下旧 WAL/SHM，否则旧页可能再次作用到恢复后的数据库。

1. 暂停会自动重启或部署该实例的任务，停止 Web 和 API，并确认没有其他容器/主机进程打开此数据卷。整个恢复过程保持停止，失败后也不要自动重启。
2. 选择已经校验的备份目录；用备份工具恢复到数据卷中一个不存在的新文件。工具会检查快照摘要和 SQLite 完整性，失败即停止。
3. 在同一个卷内创建新的保留目录，将当前主文件及存在的 WAL/SHM **全部移入该目录**。不要单独丢弃 WAL，它可能含有尚未 checkpoint 的已提交数据。
4. 确认原路径上已经没有主文件及 WAL/SHM 后，将恢复的新文件原子重命名为 `application.db`；保留目录直到恢复验收和差异核对完成。如果这一步中断，保持停机，检查保留目录与原目录，先整理完整文件组后再继续，禁止混合两组文件启动。
5. 固定与该备份模式兼容的 API/Web digest，启动并验证健康、真实登录和账本内容。恢复失败需回到原数据时，同样先停止所有读写者，把当前三文件作为另一整组保留，再完整移回最初保留的三文件。

以下示例在发布目录执行。`backup_name` 必须替换为已有且已确认的备份目录名；不会覆盖已有备份或恢复文件：

```sh
set -euo pipefail
docker compose --env-file .env stop web api
running_api="$(docker compose --env-file .env ps --quiet --status running api)"
test -z "$running_api"
backup_name=application-YYYYMMDDTHHMMSSZ
restore_id="$(date -u +%Y%m%dT%H%M%SZ)"
data_volume="$(docker compose --env-file .env config --format json | jq -er '.volumes["monee-data"].name')"
docker compose --env-file .env run --rm --no-deps \
  -v "$PWD/backups:/backup:ro" --entrypoint /monee-backup api \
  -restore "/backup/$backup_name" -out "/data/application.restore-$restore_id.db"

# Only run after the preceding restore completed successfully, with no writers.
docker run --rm --network none --read-only --user 65532:65532 \
  --cap-drop ALL --security-opt no-new-privileges \
  --mount "type=volume,src=$data_volume,dst=/data" \
  --env RESTORE_ID="$restore_id" alpine:3.22 sh -ec '
    restored="/data/application.restore-$RESTORE_ID.db"
    retained="/data/pre-restore-$RESTORE_ID"
    test -s "$restored"
    test ! -e "$retained"
    mkdir -m 700 "$retained"
    for suffix in "" -wal -shm; do
      file="/data/application.db$suffix"
      if test -e "$file"; then mv "$file" "$retained/"; fi
    done
    test ! -e /data/application.db
    test ! -e /data/application.db-wal
    test ! -e /data/application.db-shm
    chmod 600 "$restored"
    mv "$restored" /data/application.db
  '
```

以上两步之间或搬移过程中失败时，保留所有现存文件并人工检查，不执行启动命令；不能通过重试覆盖保留目录来“清理”失败现场。`auth.json`、`redaction.key` 和数据目录锁文件不参与上述数据库替换。数据库恢复完成后再按上一段固定镜像、启动和验收。

## 后续移动端扩展

Compose 部署不会阻碍移动端，但上线移动端前仍需完成：

- 为远程 API 明确版本兼容和最低客户端版本；保持整数分/十进制字符串金额语义。
- 将桌面专用浏览器回跳替换为 iOS Universal Links / Android App Links，并为移动端使用 PKCE；服务端继续验证 provider 稳定 ID。
- iOS 使用 Keychain、Android 使用 Keystore 保存短期会话，不在 App 包或 CI 中嵌入 OAuth Client Secret。
- GitHub Actions 增加 Android 测试/签名产物；iOS 构建使用 macOS runner，签名和 TestFlight 发布使用环境保护与独立 Secret。
- SQLite 仍只属于服务器；手机端通过 API 访问，离线同步与冲突解决应作为单独协议设计，不能复制数据库文件。
