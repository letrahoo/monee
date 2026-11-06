# 共享目录租约验证（2026-11-07）

基线 7ec65a4；范围见[计划](../plans/shared-directory-lease.md)。全部文件与数据库使用隔离临时目录及合成资料。

## 覆盖

- 租约同目录冲突、错误目录/nil/已关闭租约拒绝、操作错误透传、重复 Close、释放后重获、锁 inode 不删除。
- 新旧 flock 双向兼容、目录别名、拒绝锁文件符号链接、回调规范目录不跟随验证后改指向的别名。
- Restore 在检查不存在备份之前拒绝无效租约；失败不产生目标，错误返回后释放新取得的锁。
- 旧协议子进程持锁时 Restore 拒绝；子进程退出后可重获租约。
- 显式 RestoreWithLease 成功且不释放调用方锁；原有 WAL 内容、拒绝覆盖、备份篡改和未来版本测试继续执行。
- 正常持租约的旧库合并/重启测试；无效租约开库不产生数据库或备份。

## 审查与修复

初次审查发现 P1：服务已持锁，旧库迁移调用新的 Restore 会重复取锁、自冲突。给旧迁移测试先加实际服务锁后稳定变红（data directory lease unavailable）；修复为主入口显式传同一 Lease、迁移用 RestoreWithLease 后通过，没有释放锁绕过。

审查还指出验证 canonical 后仍用原目录别名写入的风险。Within 现向回调传规范目录，Restore 在该目录拼目标名；添加别名改指向后仍写原已锁目录的用例。此用例验证租约回调路径，非完整目录命名空间攻击防护。

另加非支持平台 fail-closed stub；`GOOS=windows GOARCH=amd64 go build ./internal/storage ./internal/ledger ./internal/auth ./cmd/database-backup` 通过，仅证明编译，不声称 Windows 运行支持。

最终完整 Go race 全部通过（ledger 67.289s），vet/diff-check 通过；共享测试、Web 生产及 Mac 分发 fresh 构建通过（1m25s，68任务全执行）。独立只读复审确认启动重锁与目录别名问题已修复，未发现新增 P1/P2。正常双端启动/退款/关联操作未验收，构建和隔离测试不能替代真实操作。未升 schema、未部署、未改真实库或共享路由。
