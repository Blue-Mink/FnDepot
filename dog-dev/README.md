# 🐶 Dog-dev — fnOS 应用包

飞牛 fnOS 的 GitHub / Docker 加速应用（基于 [GitHub++](https://github.com/MisiteQ/github-plus-plus) 深度定制）：智能测速自动选最优通道，支持 GitHub 反代加速、DNS 优选 hosts、HTTPS 网页加速与 Docker 镜像拉取加速，内置 iOS 风格 Web 控制台。默认不影响正常联网。

## 当前版本

| | |
|---|---|
| 包版本 | **v1.2.6** |
| FPK | [amd64](https://github.com/Blue-Mink/FnDepot/releases/download/dog-dev-v1.2.6/dog-dev-1.2.6-fnos-amd64.fpk) / [arm64](https://github.com/Blue-Mink/FnDepot/releases/download/dog-dev-v1.2.6/dog-dev-1.2.6-fnos-arm64.fpk) |
| SHA256 | 同目录 `fpk.sha256` |
| 默认端口 | 代理 37710 / 控制台 37717 |
| 原项目 | [MisiteQ/github-plus-plus](https://github.com/MisiteQ/github-plus-plus) |

## 功能

- GitHub 加速：内置 64 个加速源，智能测速、失败自动换源
- Docker 拉取加速：依赖 kspeeder 应用（自动探测安装与运行状态，端口跟随其配置），未运行时回落内置社区镜像池
- DNS 优选 hosts、HTTPS 网页加速、一键应用到系统
- 系统代理 + 本地 CA 联动：git / ssh 的 HTTPS 也能全速加速

## 安装

1. 下载对应架构 FPK（校验文件同目录 `fpk.sha256`）
2. fnOS 应用中心 → 手动安装
3. 打开控制台 `http://<fnOS-IP>:37717`（默认 admin / admin123）；Docker 加速需先安装 [kspeeder](https://github.com/Blue-Mink/FnDepot/releases/tag/v0.8.2)

## 源码

- [`source/`](source/) — [Blue-Mink/dog-dev](https://github.com/Blue-Mink/dog-dev) 源码快照（与 v1.2.6 tag 树一致）
- 应用本体基于 GitHub++ 深度定制，版权归 [MisiteQ](https://github.com/MisiteQ) 所有
