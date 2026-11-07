# 真实保留 v5 兼容验证（2026-11-07）

基线668ba10。[设计](../plans/retained-v5-compatibility.md)。环境macOS arm64、实际Go1.26.8、JDK17；全部新建隔离合成数据库，未复制真实账本/凭据、未切路由/部署或降低任何现有库版本。

## 实际结果

- 固定真实旧提交dab74a5、固定源码树e883e849；源码clean。真实monee、database-backup及旧原test binary均有对应embedded VCS revision/modified=false和SHA256清单。
- 完整真实旧版脚本首次通过：ledger tagged suite3.490s；两子例包括旧原ledger/auth/Restore fence，以及真实服务health5正对照→关闭→实际协调器升级empty6→释放锁→旧服务版本拒绝→不ready/discovery不变/无migrationbackup/完整逻辑schema+rows不变→旧backup archive6可验证→restore6拒绝无目标→原5备份新目标old health5正对照。
- 最终脚本在独立虚构GOENV文件（invalid overlay、plan9/mips64及无效workspace）下重跑仍通过（tagged ledger2.645s），证明GOENV=off不是只清空环境GOFLAGS。未改用户真实go env配置。
- 模拟depth1 checkout：实测外层fetch后shared clone缺固定tree；加入内层fetch固定SHA后checkout成功，tree与clean工作树精确匹配。不是远程Linux CI已通过声明。
- 缺MONEE_RETAINED_V5_DIR的tagged suite实测必失败并返回缺fixture错误，不会Skip；Bash/Python语法、workflow YAML语法及diff检查通过。
- 完整Go race/vet通过ledger79.290s、httpapi9.647s，带retained_v5的ledger vet另通过。普通全Go不包括build-tag兼容例，后者单列实际执行。
- fresh共享全量测试/Web生产/Mac分发构建1m40s、68任务全执行通过。既有Kotlin未来copy可见性与Webpack/Node警告未消失；构建不是GUI验收。

## 审查及发现修复

初次linked worktree构建未嵌入VCS，身份检查正确失败；实读所选Go实现只识别.git目录，改独立源码clone而不放宽检查。修正manifest原外层go1.22启动器版本为模块实际选中Go1.26.8。初审未见P1/P2，修复复核发现持久go env overlay P2，已加GOENV=off并完成poison配置重跑；最终独立复核确认先前P2已修复，未发现新P1/P2。

CI新增必跑retained-v5-compatibility job及manifest诊断上传，release同时依赖原build和compatibility。无缺env跳过/continue-on-error。本片未修改正常服务schema门禁。

## 未验证

active/retracted6真实旧版拒绝及正常Open6尚未覆盖；需要下一片启用严格runtime6后扩展同一夹具。没有正常OAuth Web/Mac真实点击验收。本片不改变默认5、不自动升级6、不merge/deploy，也不代表可交付目标完成。逻辑digest相同不等于所有SQLite文件字节相同；manifest不是独立签名来源证明。
