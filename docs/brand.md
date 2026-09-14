# Monee 品牌说明

品牌信息确认日期：2026-09-14。

## 名称与 Slogan

- 产品展示名称：**Monee**。
- 仓库名称：`monee`。
- Slogan：**Know Your Money. Own Your Future.**

对外展示保留 Slogan 的英文拼写、大小写和两个句号。

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

## 使用约定

- README、产品介绍和界面展示优先引用这一份品牌资源，保持等比例缩放。
- 当前资源是带背景的完整栅格图，不视为透明底标志或 SVG 源文件。
- macOS 应用图标已接入打包配置：构建时由 `scripts/generate-macos-icon.sh` 使用系统 `sips` / `iconutil`，等比例生成 16–1024 px 图标并封装为 ICNS，保存在 `desktopApp/build/generated/appIcon/Monee.icns`；该文件也用于 Gradle 桌面运行的 Dock 图标。原图不变，派生文件不提交。
- Web favicon、iOS 和 Android 图标后续分别适配各平台尺寸和遮罩要求，派生文件单独存放，保留原图。
- 当前未确定独立字体、精确品牌色值、深色版或单色版规范；后续 UI 设计时补充。
