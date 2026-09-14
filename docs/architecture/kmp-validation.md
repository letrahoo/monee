# KMP 体验验证

更新：2026-09-14。Web / Mac 已接入同一个本地 Go 服务，默认不再使用演示数据。最新联调与运行方式见[本地数据链路](local-data-flow.md)。下方“首轮界面验证记录”保留最初合成数据阶段的观察。

## 已确定的边界

- 共享 Kotlin / Compose UI、展示模型、金额格式化和筛选交互。
- Web 编译为 Kotlin/Wasm；Mac 使用 Kotlin/JVM + Compose Desktop。两端使用各自运行时，无 WebView 容器。
- Go 已负责账本、导入与统计，Web / Mac 通过 Ktor 共用同一个 SQLite 本地账本。
- 当前只有 Web/Mac 工程入口；Android/iOS 需补充平台配置与真机验证。

## 验证工程

| 模块 | 内容 |
| --- | --- |
| `shared` | 品牌界面、统计卡片、分类条形图、月份切换、中文搜索、账单详情、窄窗口布局 |
| `desktopApp` | JVM 窗口及 macOS 原生分发配置 |
| `webApp` | Wasm 浏览器入口与静态页面 |
| `assets/brand` | 原始 Logo 与透明边缘应用图标，构建时将应用图标复制到共享资源目录 |
| `assets/fonts` | Noto Sans SC 中文字体与 OFL 许可证，随资源分发 |

固定版本：Kotlin 2.4.10、Compose Multiplatform 1.11.1、Gradle 8.14.4、JDK 17。Kotlin/Compose 组合参考 [Kotlin 官方模板](https://github.com/Kotlin/KMP-App-Template/tree/6b6d09e4f3f5e845116422d59b658791ed6149c6)；Gradle 分发校验值写入 wrapper 配置。

Gradle wrapper 来自该模板，仅使用通用启动脚本及 wrapper JAR，其 Apache 2.0 许可证保留在 `gradle/wrapper/LICENSE`。业务界面由本项目编写，字体独立遵循 `assets/fonts/OFL.txt`。

## 运行与检查

```sh
./gradlew :desktopApp:run
./scripts/run-local.sh # 先运行服务并提供同源 Web，再启动 Mac
./gradlew :shared:jvmTest :desktopApp:classes :webApp:wasmJsBrowserDistribution
# 在 macOS 生成可直接启动的 .app（尚未签名、公证）
./gradlew :desktopApp:createDistributable
```

本地服务持续运行，浏览器访问 http://127.0.0.1:4173/。桌面任务打开窗口，并从私有连接文件发现同一服务。独立 Web 开发服务器不包含账本服务。首次构建需要下载依赖，JDK 通过 `JAVA_HOME` 或开发工具选择；不在仓库固定本机 JDK 路径。

当前共享测试覆盖分位精度、大于 JavaScript 安全整数的金额显示与 JSON 字符串解析，以及 `Long.MIN_VALUE` 的符号和精度；月份、搜索与账本规则在 Go 中测试。

## 首轮界面验证记录（接入 API 之前）

2026-09-14，在 Apple Silicon Mac、Temurin JDK 17.0.10 和 Codex 内置浏览器上验证；结果只覆盖本次环境。

| 检查 | 结果 |
| --- | --- |
| `:shared:jvmTest` | 2 项测试通过，0 失败；未在 Wasm 测试运行器重复执行 |
| `:desktopApp:createDistributable` | 通过；生成并启动 `desktopApp/build/compose/binaries/main/app/Monee.app` |
| `:webApp:wasmJsBrowserDistribution` | 通过；产物位于 `webApp/build/dist/wasmJs/productionExecutable` |
| Web / Mac 品牌与中文显示 | Logo、Slogan、中文、金额均可见 |
| Mac 安装包图标 | 使用透明边缘派生图；Info.plist 指向 Monee.icns，包内文件与生成文件一致，透明角与深浅背景轮廓已检查；原图哈希不变 |
| Web / Mac 月份切换 | 9 月净支出 ¥782.50，切换 8 月后为 ¥1,299.00；明细同步变化 |
| Web 中文搜索与清除 | 输入“微信”后 8 月明细只保留“朋友聚餐”；清除后恢复 3 笔 |
| Web / Mac 账单详情 | 行内展开与收起通过；Web 收起后继续切月、搜索和展开说明，状态正常 |
| Web 响应式布局 | 桌面与 390px 窄窗口已目视检查；窄屏卡片堆叠、明细省略独立日期列 |
| Web 控制台 | 本轮交互未发现 error；不代表所有浏览器兼容性已通过 |
| Mac 键盘输入 | 自动化输入未进入搜索框，粘贴等待超时；原因未定位，需实际键盘/输入法人工复验，不能记为通过 |

验证时发现 Compose Web 弹窗关闭后，无障碍树停止更新，现象与上游 [CMP-10623](https://youtrack.jetbrains.com/issue/CMP-10623) 一致。当前详情与说明改为主界面内展开，复验后连续交互正常。这里只绕开了触发条件，不代表修复了框架本身，也未完成完整屏幕阅读器验收。

Web 构建仍给出体积告警：Skiko Wasm 约 8.25 MiB、应用 Wasm 约 2.49 MiB、JS 约 333 KiB，另含完整中文字体和 Logo。以上是构建文件大小，不是压缩后的网络传输量或首屏耗时。

本轮结论：可以继续沿 KMP 方向建设 Web / Mac 共享客户端。已验证基础显示和部分交互；原生输入法、Web 加载性能与后续真实账本仍有独立验收工作。

## 当前限制与下一步

- 当前默认读取真实本地账本，标准 CSV 导入、手动收支、月度统计已接通，原有演示数据类已移除。接口与持久化验收见[数据链路记录](local-data-flow.md)；目录/邮件、完整账单适配和云端同步尚未实现。
- 中文完整字体约 17 MB，先确保任意中文输入的显示；后续验证字体子集、缓存和加载反馈。字体用于技术验证，不代表最终品牌字体。
- 需要继续实测中文输入法组合输入、键盘焦点、屏幕阅读器、首屏时间与大列表性能；代码编译通过不能替代这些结果。
- macOS 分发图标已由品牌原图生成并接入；签名、公证、安装与升级仍属于后续桌面交付阶段。
- Web/Mac 共用同一 Go 服务已联调验证：Web 导入和补记后 Mac 自动刷新，读取同样的账单和统计。当前 Mac 不自动启动 Go 服务。
- 本轮生产构建的应用 Wasm 约 3.12 MiB、Skiko 8.25 MiB、JS 340 KiB，另有完整中文字体约 17 MB。Webpack 仍有体积告警和 Ktor 动态依赖告警；本轮 Web 运行未出现控制台 error，不等于所有平台兼容性完成。

## 参考

- [官方 KMP 项目结构](https://kotlinlang.org/docs/multiplatform/compose-multiplatform-create-first-app.html)
- [Compose 版本兼容性](https://kotlinlang.org/docs/multiplatform/compose-compatibility-and-versioning.html)
- [Web 资源与字体](https://kotlinlang.org/docs/multiplatform/compose-web-resources.html)
