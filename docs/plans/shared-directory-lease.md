# 共享目录租约：独占升级前置协议

本增量不升级 schema，不开放关联入口，也不迁移真实库。schema5 非空关系启动门禁继续保留。

## 已实现的范围

- `internal/dirlease` 在 macOS/Linux 复用 `service.lock` + 非阻塞 flock，与旧正常服务互斥。提供显式 Lease、Within 和幂等 Close；操作进行时 Close 等待；锁文件绝不删除。
- 目录规范化，拒绝锁符号链接和非普通文件，校验锁文件 inode。Within 回调收到已验证的规范目录；恢复写入使用该目录加目标文件名，而不是继续跟随调用方目录别名。
- 正常服务从开库前到关闭两个 Store 后持有一个 Lease，显式传给启动协调器。旧库恢复使用已有 Lease，不嵌套重新取锁。
- `storage.Restore` 在读取备份或创建新目标之前获得目标目录租约；`RestoreWithLease` 供已有租约的协调器使用。保留 O_EXCL、校验、失败清理及仅新目标策略。在线 Snapshot 仍为只读例外，不强制取得租约。
- 非 macOS/Linux 提供失败关闭 stub：保留只读工具和依赖包的编译能力，租约/恢复不在这些平台放行。

## 边界

- 租约只协调遵守相同协议的进程。它不强制所有 ledger.Open/auth.Open 调用持锁，不约束任意 SQLite 客户端、数据库硬链接/跨目录别名或旧无锁工具。
- 需要可信、稳定的数据目录命名空间。规范目录自身被恶意 rename/替换不在此实现保障内；没有声称使用 directory-fd/openat 防御。
- 启动协调器仅在入口校验租约；调用方须保留租约直至返回的 stores 关闭，不得并发关闭。Within 的回调不能重入 Within/Close。
- 未来升级仍须停机关闭所有旧连接。user_version 提升不会使旧连接失效，WAL 的排他事务不能替代这一要求。已用[隔离反例与固定版本拒绝测试](../verification/upgrade-boundary-counterexamples-2026-11-07.md)直接验证租约和版本号不会吊销已开 SQLite 连接。

## 后续最小增量

先设计只在隔离合成库调用的显式升级协调：持租约→精确 v5/空关系结构校验→同一独占范围内验证备份→事务内重验并升级→失败回滚。不能只提高 SchemaVersion 常量使正常 Open 自动升级；只有完整消费者、旧写保护、确认/撤回生命周期和升级协议同时具备，才能考虑解除门禁及开放 API/UI。
