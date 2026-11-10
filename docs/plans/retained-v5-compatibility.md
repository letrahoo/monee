# 保留真实 v5 实现的兼容夹具

正常schema6开放的前置，不是提升生产版本常量。本片保持SchemaVersion=5及正常Open6拒绝。

## 固定来源

固定旧实现 `dab74a5fdcfb403c47900dbb21023540517978d2`，tree `e883e849f2359262b6a6b7c9b3f15b85026e60fa`。验证入口为 `scripts/verify-retained-v5.sh`，需要Git、Go和Python3标准库；可通过PYTHON指定解释器。

脚本使用私有新临时目录，从可信本地仓库克隆独立.git目录后detached checkout固定SHA，绝不以分支tip替代。浅CI外层fetch不一定把FETCH_HEAD传入clone，故内层缺对象时再fetch同一SHA。缺对象/构建/身份检查失败即失败。

关闭GOENV持久配置、GOWORK、环境GOFLAGS及交叉架构值，避免overlay/替代workspace污染原样旧源码。工作树须clean，三个程序都显式-buildvcs=true：真实monee、真实database-backup及旧cmd/monee原始测试二进制。manifest记录固定revision/tree、实际模块选中Go工具链、三个SHA256和完整go version -m；测试再读二进制embedded module/VCS revision/modified=false并核验摘要。

manifest不是独立签名的供应链证明。私有诊断源码clone可能共享原object store，不能当自包含长期归档；编译后程序与manifest独立保存。不提交二进制、fixture DB、缓存或本机路径，不清理/覆盖用户提供目录。

## 实际进程回归

带retained_v5 build tag的ledger测试必须有完整fixture，缺环境直接Fatal、不t.Skip。普通go test不编译该tag，CI另设无条件compatibility job运行完整脚本；标签发布同时依赖此job和原build。

1. 执行固定旧源码原TestCurrentSchema5RejectsFixedSchema6，补充旧ledger/auth/Restore真实fence，不代替实际服务验证。
2. 在纯合成application.db上启动真实旧服务，确认health200/schema5和ready，再正常停进程。
3. 取得独占目录lease，经现有严格协调器实际升级空5→6，保留验证备份，关闭lease；再次Acquire/Close证明无锁泄漏。
4. 真旧服务对实际升级6返回精确旧版本错误且不ready。比对完整逻辑schema/rows digest、version6、discovery字节不变及无迁移备份；端口/锁/配置错误不能算成功拒绝。
5. 真旧backup程序可以归档6，但restore6拒绝且无目标文件；源逻辑数据不变。将原5备份恢复到全新目标，旧服务再次health5成功；绝不降低原6的版本。

子进程不继承MONEE路径/provider/token配置，仅临时HOME/XDG/TMP与PATH；所有DB及输出均隔离。主程序当前先Listen再Open，因此只断言不Serve/ready/更新discovery，不声称从未bind端口。

## 尚未覆盖

当前仅实际升级的empty6。正常Open6启用后，还须在真实升级→正常打开6路径确认active/retracted，关闭全部连接/lease后运行同一旧程序拒绝，并验证正常重开/全部ledger严格校验。不能把本片当正常schema6服务交付或正常OAuth Web/Mac操作验收。
