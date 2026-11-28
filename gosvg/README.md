# GoSvg for fnOS

GoSvg 是纯 Go 实现的轻量级 SVG 子集渲染命令行工具集（零 cgo，唯一直接依赖 `golang.org/x/image`）：把 SVG 文件渲染为 PNG/JPEG，覆盖圆形国旗、Lucide 线条图标与带文字图标三类常见场景，像素级对齐 cairosvg。

本目录提供适配飞牛 fnOS 应用中心的 GoSvg FPK 打包版本。本应用为纯 CLI 工具，无 Web 界面、无常驻服务，通过 SSH 调用；后台仅保留一个轻量哨兵进程用于维持应用中心的启动/停止状态。

## 功能特点

- `gosvg-render`：把 SVG 渲染为 PNG/JPEG，可指定宽高与输出路径
- `gosvg-verify`：运行完整验证门禁（go 测试、竞态、交叉编译、fuzz、golden 基线）
- `gosvg-corpus`：批量处理 SVG 语料库
- `gosvg-iou`：计算两张图的 IoU 相似度，用于渲染对账
- 支持 SVG 元素/属性广，含渐变、mask/clipPath、marker、文字与 `vector-effect`

## 版本信息

| 项目 | 信息 |
| :--- | :--- |
| 应用名称 | GoSvg |
| fnOS 包名 | `gosvg` |
| 当前版本 | `0.5.0` |
| 平台 | fnOS x86_64 / ARM64 |
| 运行方式 | 纯 CLI（SSH 调用），无 Web 端口 |
| 原项目 | [Blue-Mink/gosvg](https://github.com/Blue-Mink/gosvg) |
| 发布者 | [Blue-Mink](https://github.com/Blue-Mink) |

## 安装方式

1. 下载 Release 中的 FPK（按架构选择）：
   - x86_64：`gosvg-0.5.0-fnos-amd64.fpk`
   - ARM64：`gosvg-0.5.0-fnos-arm64.fpk`
2. 在 fnOS 应用中心选择"手动安装"
3. 安装完成后通过 SSH 调用，命令位于 `${TRIM_APPDEST}/bin/`

## 使用示例

SSH 登录 NAS 后：

```sh
# 查看参数
/vol1/@appcenter/gosvg/bin/gosvg-render -h

# 渲染 256x256 图片
/vol1/@appcenter/gosvg/bin/gosvg-render -size 256 in.svg out.png

# 计算两图 IoU（渲染对账）
/vol1/@appcenter/gosvg/bin/gosvg-iou a.png b.png
```

更多用法见应用中心"打开"入口的文档页，或 [上游 README](https://github.com/Blue-Mink/gosvg#readme)。
