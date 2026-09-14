# Monee 品牌说明

品牌信息确认日期：2026-09-14。

## 名称与 Slogan

- 产品展示名称：**Monee**。
- 仓库名称：`monee`。
- Slogan：**Know Your Money. Own Your Future.**

对外展示保留 Slogan 的英文拼写、大小写和两个句号。产品界面仅在登录品牌入口展示 Slogan；账本页不重复宣传语，应用窗口与网页标题仅为 Monee。

## Logo

<img src="../assets/brand/logo.png" alt="Monee Logo：绿色 M 形图案与金色硬币" width="280" height="280" />

| 属性 | 内容 |
| --- | --- |
| 文件 | [`assets/brand/logo.png`](../assets/brand/logo.png) |
| 格式 | PNG |
| 尺寸 | 1254 × 1254 像素 |
| 来源 | 产品负责人提供的设计原图 |
| 图像要素 | 深浅绿色的 M 形图案、金色硬币、浅色圆角方形底板及阴影 |

仓库中的文件与提供的原图完全一致，未裁切、改色、重绘或重新压缩。

### 应用图标

[`assets/brand/app-icon.png`](../assets/brand/app-icon.png) 是 1254 × 1254 的 RGBA 派生图，供 Mac 安装包和 Web / Mac 页面使用。圆角底板之外的白色画布及烘焙阴影已移除，轮廓使用抗锯齿透明边缘；绿色 M、金币、奶油色底板内部的原始 RGB 像素保持不变。

派生图由 `python3 scripts/prepare-app-icon.py` 确定性生成（需要 Pillow），脚本只写入透明通道，不重绘图案。轮廓检测针对当前原图校准，源文件哈希变化时要求重新校准和目视检查。普通应用构建直接使用已提交的派生 PNG，不依赖 Python 或在线图像服务。

已核对四角透明度为 0、RGB 像素与原图一致，并检查深浅背景下的轮廓。原始 `logo.png` 继续作为设计稿保留。

## 使用约定

- README 和应用界面引用透明边缘派生图 `app-icon.png`，保持等比例缩放；原始设计稿引用 `logo.png`。
- 原始设计稿带完整背景；应用派生图保留不透明圆角底板，只有底板之外透明，不视为裸 M 标志或 SVG 源文件。
- macOS 应用图标已接入打包配置：构建时由 `scripts/generate-macos-icon.sh` 从 `app-icon.png` 等比例生成 16–1024 px 图标并封装为 ICNS，保存在 `desktopApp/build/generated/appIcon/Monee.icns`；该文件也用于 Gradle 桌面运行的 Dock 图标。透明通道随缩放保留，ICNS 和中间尺寸文件不提交。
- Web favicon、iOS 和 Android 图标后续分别适配各平台尺寸和遮罩要求，派生文件单独存放，保留原图。
- 当前未确定独立字体、精确品牌色值、深色版或单色版规范；后续 UI 设计时补充。
