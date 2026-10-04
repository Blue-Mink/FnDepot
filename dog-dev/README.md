# Dog-dev for fnOS

Dog-dev 是飞牛 fnOS 的 GitHub / Docker 加速应用（基于 [GitHub++](https://github.com/MisiteQ/github-plus-plus) 深度定制）：智能测速自动选择最优加速通道，Docker 拉取加速依赖 kspeeder 应用（自动探测其安装与运行状态，端口跟随 kspeeder 自身配置），支持镜像反代、DNS 优选 hosts 加速、HTTPS 网页加速与 Docker 镜像拉取加速，内置 iOS 风格 Web 控制台。默认不影响 NAS 与局域网其他设备的正常联网。

本目录提供 Dog-dev 源码快照；FPK 安装包在 [Release](https://github.com/Blue-Mink/FnDepot/releases/tag/dog-dev-v1.2.6) 中。

## 功能特点

- GitHub 加速：内置 64 个加速源（含 xget 路径抓取型），智能测速自动选最优通道，失败自动换源
- Docker 拉取加速：依赖 kspeeder 应用（自动探测安装 / 版本 / 运行状态，端口跟随 kspeeder 配置），未运行时自动回落内置社区镜像池，官方源兜底
- DNS 优选 hosts 加速、HTTPS 网页加速、一键应用到系统
- 系统代理 + 本地 CA 联动：git / ssh 的 HTTPS 也能全速加速；卸载自动移除 CA
- iOS 风格 Web 控制台：深浅主题、上游测速、KSpeeder 状态卡片、日志查看

## 版本信息

| 项目 | 信息 |
| :--- | :--- |
| 应用名称 | Dog-dev |
| fnOS 包名 | `dog-dev` |
| 当前版本 | `1.2.6` |
| 平台 | fnOS x86_64 / ARM64 |
| 代理端口 | `37710` |
| 控制台端口 | `37717` |
| 原作者 | [MisiteQ](https://github.com/MisiteQ) |
| 原项目 | [github-plus-plus](https://github.com/MisiteQ/github-plus-plus) |
| 发布者 | [Blue-Mink](https://github.com/Blue-Mink) |

## 安装方式

1. 下载 Release 中的 `dog-dev-1.2.6-fnos-amd64.fpk`（x86）或 `dog-dev-1.2.6-fnos-arm64.fpk`（arm64）
2. 在 fnOS 应用中心选择“手动安装”
3. 打开控制台：`http://<fnOS-IP>:37717`（默认账号 admin / admin123）
4. 要开 Docker 加速，请先安装 [kspeeder](https://github.com/Blue-Mink/FnDepot/releases/tag/v0.8.2) 应用

也可以在飞牛 FnDepot 中添加本源后搜索「Dog-dev」安装。

## v1.2.6 更新内容

- 品牌与署名更新：控制台标题去掉「加速器」后缀；关于页改为 Dog-dev 介绍（原作者 MisiteQ，发布者 Blue-Mink）；应用中心发布者（distributor）改为 Blue-Mink

## 注意事项

- 应用以 root 运行（DNS 优选需要写 `/etc/hosts`）。
- 默认不影响 NAS 与局域网其他设备的正常联网。
- 要让 git / ssh 的 HTTPS 走加速，需在控制台开启「系统代理」（会联动把本地 CA 装入系统信任）。
- Docker 拉取加速依赖 [kspeeder](https://github.com/Blue-Mink/FnDepot/releases/tag/v0.8.2) 应用；未安装时 Docker 走内置社区镜像池。
