# 有效统计边界补验（2026-11-07）

基线：6e8aadc。此次仅增加隔离合成测试和文档，不改变生产规则、HTTP/UI 或启动门禁。

## 新增用例

- `TestExpensePairEffectiveSearchAndPagination`：确认前给 primary/secondary 分别添加唯一备注，并插入 PageSize−1 条较新日期记录和一条较旧记录；确定 secondary 位于原始第50条、primary 第51条、较旧记录第52条。确认后 secondary 专属搜索命中 0，primary 专属命中 1；三个页面分别有 PageSize、1、0 条，跨页无重复、没有 secondary、主记录不遗漏，统计与版本一致，读取不改证据表。撤回后第二页恢复两条、secondary 搜索恢复命中。
- `TestExpensePairEffectiveIndependentReaderConcurrentSmoke`：在确认前通过正常 Open 打开第二连接，分别让 Dashboard 与确认/撤回同时起跑。读取必须与同一个版本的总额、分类、条数、明细一致，只允许完整前态或后态。每次操作后再检查已确定的状态。此为机会性并发烟测及提交后可见性检查；start channel 不保证 SQL 级重叠，不能作为跨查询快照隔离的确定性证明。不绕过非空关系启动门禁。
- `TestExpensePairEffectiveSchema4Boundary`：干净 schema4 零表允许；同名 view、关联审计残留、schema5 零表、未来版本零表必须拒绝。

## 执行证据

- 初次独立审查指出旧分页样本只能验证尾页隐藏，已改为副记录占满页槽位并断言主记录补位；并发用例已改名烟测，明确不保证 SQL 交错。独立复审未发现 P1/P2。
- 最终三项定向测试 `go test -race -count=10` 通过（24.061s）。
- 最终完整 `go test -race -count=1 ./...` 通过（ledger 56.942s），`go vet ./...` 和 `git diff --check` 通过。
- 本轮共享测试、Web 生产、Mac 分发 fresh 构建通过（1m19s，68 任务全执行）；之后仅调整 Go 测试与文档，生产及客户端源码未改。
- 基线 6e8aadc 的远程 CI 仍运行中，不据此宣称六项通过；本轮提交也需独立收集远程结果。

## 不变边界

无真实双端操作验收；无 HTTP/UI 关联入口及独占升级；退款摘要读取策略尚待补齐；不合并、不部署、不写真实账本。自动检查不替代真实验收。
