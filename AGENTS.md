# Monee 开发约定

- 客户端确定采用 Kotlin Multiplatform + Compose Multiplatform；服务端采用 Go。
- 先交付 Web / Mac，随后补齐 iOS / Android。UI 和客户端状态放在 `shared/src/commonMain`，原生能力放在平台层。
- 正式账本、去重、退款、还款和统计规则只在 Go 中实现；当前 `DemoData.kt` 仅为合成展示样本。
- 首版 Web / Mac 共用同一个本地 Go 服务和 SQLite 账本；保留稳定 ID、版本和删除语义以支持未来云端同步。
- 金额传输/存储使用最小货币单位整数或十进制字符串，不经由浮点金额运算。
- 真实账单、邮箱凭据、数据库、构建缓存和本机路径不得提交。
- 品牌原图以 `assets/brand/logo.png` 为准并保留不变；应用使用其透明边缘派生图 `assets/brand/app-icon.png`，构建时生成 Compose 资源与 ICNS。
- Slogan 保持 `Know Your Money. Own Your Future.`。

## 常用检查

- `./gradlew :shared:jvmTest`
- `./gradlew :desktopApp:classes :webApp:wasmJsBrowserDistribution`

## 设计依据

- 产品范围：`docs/product/mvp-discussion.md`
- 架构：`docs/architecture/mvp-proposal.md`
- KMP 验证：`docs/architecture/kmp-validation.md`
- 品牌：`docs/brand.md`
