# 🏪 fnOS Apps（fnos-apps-store）

[conversun/fnos-store](https://github.com/conversun/fnos-store) 出品的 fnOS 第三方应用中心原版：一键安装、更新、卸载 [conversun/fnos-apps](https://github.com/conversun/fnos-apps)（749★，115+ 款自托管应用）目录里的应用。本仓库收录的是**上游官方 Release 原包，未重打包、字节未改**。

<div align="center">

[![fnOS Apps](https://img.shields.io/badge/fnOS--Apps-v1.9.6-green?style=flat-square)](https://github.com/conversun/fnos-apps/releases/tag/fnos-apps-store%2Fv1.9.6)
[![Platform](https://img.shields.io/badge/Platform-fnOS%20x86__64%20%C2%B7%20ARM64-lightgrey?style=flat-square)](#)

</div>

| 项目 | 信息 |
| :--- | :--- |
| 👨‍💻 **原作者** | [conversun](https://github.com/conversun) |
| 📁 **原项目** | [fnos-store](https://github.com/conversun/fnos-store)（应用目录 [fnos-apps](https://github.com/conversun/fnos-apps)，GPL-3.0） |
| 📥 **安装方式（x86）** | 下载 [fnos-apps-store_1.9.6_x86.fpk](https://github.com/Blue-Mink/FnDepot/releases/download/fnos-apps-store-v1.9.6/fnos-apps-store_1.9.6_x86.fpk)（[sha256](https://github.com/Blue-Mink/FnDepot/releases/download/fnos-apps-store-v1.9.6/fnos-apps-store_1.9.6_x86.fpk.sha256)）在 fnOS 应用中心手动安装 |
| 📥 **安装方式（ARM）** | 下载 [fnos-apps-store_1.9.6_arm.fpk](https://github.com/Blue-Mink/FnDepot/releases/download/fnos-apps-store-v1.9.6/fnos-apps-store_1.9.6_arm.fpk)（[sha256](https://github.com/Blue-Mink/FnDepot/releases/download/fnos-apps-store-v1.9.6/fnos-apps-store_1.9.6_arm.fpk.sha256)）在 fnOS 应用中心手动安装 |
| 🏷️ **版本** | v1.9.6（`appname = fnos-apps-store`，`display_name = fnOS Apps`） |
| 🌐 **入口端口** | 8011（应用中心「打开」与桌面图标直达） |
| 🔧 **运行身份** | `run-as: root`（需要权限读写应用中心目录） |

## 校验

```
46d20f4c01c26a76ec77826c7ed7c8f348b489ccea7ca419c5e8a9a0007383c9  fnos-apps-store_1.9.6_x86.fpk
8c91a3b217635ced84b6e04db86ebc5aa9e20767951edfefb394c39a48a62509  fnos-apps-store_1.9.6_arm.fpk
```

上游官方发布页：<https://github.com/conversun/fnos-apps/releases/tag/fnos-apps-store%2Fv1.9.6>（本仓库资产与其逐字节一致）

## ⚠️ 与 New Store 的关系（务必先读）

- 本目录收录的是**上游原版**；[new-store/](new-store/README.md) 收录的是本仓库维护的 [New Store](https://github.com/Blue-Mink/New-Store) 分支（基于同一项目继续开发）。
- **两者的 fnOS 应用标识 `appname` 都是 `fnos-apps-store`**，也就是说：
  - 同一台 NAS 上**只能装一个**——装另一个等于替换（覆盖同名应用的数据目录与 systemd 服务）；
  - 同一个应用源（本仓库 `fndepot.json` / `fnpack.json`）里**也只能有一个 `fnos-apps-store` 条目**（V1 索引的键就是 appname）。因此本目录只提供手动安装包，**不进索引**；索引中的 `fnos-apps-store` 条目指向 New Store 分支。
- 想从 New Store 换回原版：先在应用中心卸载 New Store（按需选择是否保留数据），再手动装本目录的 FPK。

## 特性（上游原版）

- 浏览 / 搜索 conversun/fnos-apps 目录内的应用，一键安装、更新、卸载
- 走应用中心官方安装流程，安装进度可查
- 单 Go 二进制 + 内嵌前端，不依赖 Docker / 虚拟机

## 归属

程序与图标版权归原作者 conversun 所有；应用目录 `conversun/fnos-apps` 采用 GPL-3.0，`conversun/fnos-store` 仓库本身未附许可证文件。本仓库仅代为分发官方安装包并标注出处，更新与反馈请走上游仓库。
