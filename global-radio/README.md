# 📻 全球电台（GlobalRadio）— fnOS 应用包

![demo-w](https://raw.githubusercontent.com/moli-xia/global-radio/main/demo-w.png)
![demo-b](https://raw.githubusercontent.com/moli-xia/global-radio/main/demo-b.png)

> 本项目是 [**moli-xia/global-radio**](https://github.com/moli-xia/global-radio) 的 **fnOS 应用中心打包版本（fpk）**，在飞牛 NAS 应用中心一键安装全球电台，支持自定义访问端口。
>
> 🎯 **原作者项目**：[moli-xia/global-radio](https://github.com/moli-xia/global-radio)
> 📦 **原作者 Release**：[各平台客户端下载](https://github.com/moli-xia/global-radio/releases)

## 当前版本

| | |
|---|---|
| 包版本 | **v1.2.3** |
| FPK | [global-radio-1.2.3-fnos-amd64.fpk](https://github.com/Blue-Mink/FnDepot/releases/download/global-radio-v1.2.3/global-radio-1.2.3-fnos-amd64.fpk) |
| SHA256 | `1c96679ed74a4959907b7379558cd6c62af1d8ba1456b69fef5973f3c484286e` |
| 运行方式 | Docker（`superneed/global-radio:latest`，跟随上游滚动） |
| 平台 | fnOS x86_64 |

## v1.2.3 更新：端口设置闭环修复

此前版本在应用设置里改端口后，只有容器端口跟着变，桌面图标与「打开」按钮仍指向旧端口，且设置页不回显真实端口。v1.2.3 参照 fnOS Docker 应用定式重构了生命周期脚本：

- 保存端口后：容器重建 + `.env`/桌面入口配置/AppCenter 数据库全链路同步，**桌面图标与「打开」按钮自动跟随新端口**
- 设置页**回显当前真实端口**（不再固定显示默认值）
- 端口持久化到应用配置目录：升级/重装不再丢失自定义端口，也不会因环境变量缺失起不来
- 应用中心「启动/停止」按钮真实作用到容器（不再出现"显示已停止但容器还在跑"的假状态）
- 平台标注修正为 x86_64（原误标 all）

> 升级方式：直接「手动安装」覆盖即可，端口与电台收藏数据（应用数据目录）保留。

## 功能特性

- 📻 **全球电台搜索**（支持中文关键词）
- 🔗 **分享电台** 给好友
- ▶️ **播放控制**（播放/暂停/下一首）
- ⏱️ **睡眠定时器**
- ❤️ **收藏与播放历史**
- 🌓 **亮色/暗色主题切换**
- 🌍 **全球主流语言支持**
- 📱 **安卓 / iPhone / PC 客户端**（[原作者 Release](https://github.com/moli-xia/global-radio/releases/tag/clients)）

## 安装

1. 下载上方 FPK（校验文件同目录 `fpk.sha256`）
2. 飞牛 应用中心 → 手动安装 → 选择 FPK
3. 向导中设置访问端口（默认 32678），安装完成即自动启动
4. 浏览器访问 `http://NAS_IP:端口`，或点桌面/应用中心图标直达

## 端口说明

- 默认端口 `32678`，安装向导与应用设置均可修改，**保存后立即生效**
- 修改端口无需重装；桌面图标、「打开」按钮、容器映射、设置页回显自动保持一致

## 源码

FPK 构建工程见 [`source/`](source/) 子目录（fnpack 布局，`fnpack build -d source目录` 可复现构建）。

## 演示与上游

- **在线体验**：[https://aabb.live](https://aabb.live)（原作者托管）
- **一键脚本**：已加入 [kejilion.sh](https://kejilion.sh)
- 电台应用本体版权归 [moli-xia](https://github.com/moli-xia) 所有，本仓库仅为 fnOS 打包适配
