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
| 包版本 | **v1.3.0** |
| FPK | [global-radio-1.3.0-fnos-amd64.fpk](https://github.com/Blue-Mink/FnDepot/releases/download/global-radio-v1.3.0/global-radio-1.3.0-fnos-amd64.fpk) |
| SHA256 | `9fbe42cfc7bfb415ed9a6984fde2d78946e58eba7a02acd4a0db2536d4c388eb` |
| 运行方式 | Docker（`superneed/global-radio:latest`，跟随上游滚动） |
| 平台 | fnOS x86_64 |

## v1.3.0 更新：统一网关接入 + 流量加速 + 高清图标

自 v1.2.3 以来的版本链（1.2.4 → 1.3.0）合并要点：

- 🌐 **飞牛统一网关入口**（v1.3.0 新增）：安装后可经 `http://NAS_IP:5666/app/global-radio/` 访问，与端口直连双入口并存；登录飞牛即可使用，配合 fn-knock 域名可从外安全访问，**NAS 代理缓存加速在网关入口同样生效**
- ⚡ **电台目录与封面图 NAS 代理缓存加速**（v1.2.7 / v1.2.9）：radio-browser API 中转 10 分钟缓存、封面图中转 30 天磁盘缓存（源头带 no-cache 响应头也强制收编），实测封面二访约 **0.5ms**、目录 API 命中约 1.4ms；死源负缓存不再反复请求；内网 SSRF 拦截；直播流保持客户端直连
- 🖼️ **图标高清重制**（v1.2.5）：1024 + 8× 超采样重绘 + 官方 squircle 圆角
- 🐛 **修复**：桌面入口模板括号错位导致点图标跳飞牛登录页（v1.2.4）；控制台端口编辑器与入口显示不同步（v1.2.8，四处同步：`ui/config`、AppCenter 库、桌面入口、trim_sac）
- 端口闭环保持 v1.2.3 定式：保存即生效，容器/桌面图标/打开按钮/设置页回显自动一致，改端口网关 sidecar 自动跟随

> 升级方式：直接「手动安装」覆盖即可，端口与电台收藏数据（应用数据目录）保留。桌面图标有 7 天缓存，升级后强刷（Ctrl+Shift+R）或退出桌面重进可见新图标。

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

## 端口与访问入口

- 默认端口 `32678`，安装向导与应用设置均可修改，**保存后立即生效**
- 修改端口无需重装；桌面图标、「打开」按钮、容器映射、设置页回显自动保持一致
- **统一网关入口**：`http://NAS_IP:5666/app/global-radio/`（应用启动后自动注册，随端口变更自动跟随），与端口直连功能、加速完全等价

## 源码与客户端

- [`upstream/`](upstream/) — 上游 [moli-xia/global-radio](https://github.com/moli-xia/global-radio) **源码快照**（main 分支 `ef0e82cf8`，2026-01-26，134 文件；上游仓库未附 LICENSE 文件，版权归 [moli-xia](https://github.com/moli-xia) 所有，此处仅为镜像备份）
- [`source/`](source/) — 本 FPK 打包工程（fnpack 布局，`fnpack build -d source目录` 可复现构建）
- **各平台客户端**（搬运自上游 `clients` release，SHA256 与上游 digest 逐一核验一致）：
  | 平台 | 文件 | SHA256 |
  |---|---|---|
  | Android | [GlobleRadio-v0.0.1.apk](https://github.com/Blue-Mink/FnDepot/releases/download/global-radio-v1.3.0/GlobleRadio-v0.0.1.apk) | `f4c588bc1de64c6e6e5c43e030001bd58a37b8a05de32f12086a64e4c1d7e5c3` |
  | iOS | [GlobleRadio-0.0.1.ipa](https://github.com/Blue-Mink/FnDepot/releases/download/global-radio-v1.3.0/GlobleRadio-0.0.1.ipa) | `83ee337e6913a237ad89237cf4b698fcbb6bac335e6de0689af6b0e9e9da5210` |
  | Windows | [GlobleRadio_0.0.1_x64-setup.exe](https://github.com/Blue-Mink/FnDepot/releases/download/global-radio-v1.3.0/GlobleRadio_0.0.1_x64-setup.exe) | `1f5bf5c8cf9756030eda38f9fa30c48c87a4a3d9d9b0d013699e8a2fd80e7896` |

## 演示与上游

- **在线体验**：[https://aabb.live](https://aabb.live)（原作者托管）
- **一键脚本**：已加入 [kejilion.sh](https://kejilion.sh)
- 电台应用本体版权归 [moli-xia](https://github.com/moli-xia) 所有，本仓库仅为 fnOS 打包适配
