# 🛜 iStoreOS (istoreos)

在 x86 fnOS 上以虚拟机形式运行 [iStoreOS](https://www.istoreos.com/)（基于 OpenWrt 的路由/NAS 系统）的 FPK 应用包。安装向导选版本后全自动：下载官方镜像 → SHA256 强校验 → 网络预置 → 创建虚拟机 → 应用中心/桌面图标直达。

<div align="center">

[![iStoreOS](https://img.shields.io/badge/iStoreOS--fnos-v1.1.10-green?style=flat-square)](https://github.com/Blue-Mink/fnos-vm-istoreos/releases/tag/v1.1.10)
[![Platform](https://img.shields.io/badge/Platform-fnOS%20x86__64-lightgrey?style=flat-square)](#)

</div>

| 项目 | 信息 |
| :--- | :--- |
| 📥 **安装方式** | 下载 [com.istoreos.vm-1.1.10-fnos-amd64.fpk](https://github.com/Blue-Mink/FnDepot/releases/download/istoreos-v1.1.10/com.istoreos.vm-1.1.10-fnos-amd64.fpk) ([sha256](https://github.com/Blue-Mink/FnDepot/releases/download/istoreos-v1.1.10/com.istoreos.vm-1.1.10-fnos-amd64.fpk.sha256)) 在 fnOS 应用中心手动安装 |
| 🏷️ **包版本** | v1.1.10 |
| 📦 **可选系统版本** | iStoreOS 25.12.5（默认）/ 24.10.8 / 22.03.7（x86-64 EFI） |
| 🌐 **入口端口** | 36125（IP 寻踪固定入口，自动 302 到虚拟机管理后台） |
| 🔧 **依赖** | 飞牛「虚拟机」应用（提供 KVM/libvirt/OVMF）；主路由 DHCP 可用 |
| 📁 **原项目** | [fnos-vm-istoreos](https://github.com/Blue-Mink/fnos-vm-istoreos) |
| 🧩 **上游** | [iStoreOS](https://www.istoreos.com/)（koolcenter 团队，本包不修改系统本体） |

## 特性

- **秒回安装 + 后台进度**：安装提交立即返回，下载/预置/建机后台进行，进度可查 `cat /tmp/istoreos-install.state`，失败可在应用中心点「启动」断点续跑
- **网络预置**：安装期离线把 LAN 口改为 DHCP 客户端、关闭 iStoreOS 自带 DHCP/RA —— 融入任意现有局域网网段，不需要 192.168.100.x，也不与主路由抢 DHCP
- **IP 寻踪**：MAC/ARP → VNC 横幅 → mDNS → domifaddr → 局域网扫描多级发现，桌面图标与应用中心「打开」自动跟随虚拟机 DHCP 地址变化
- **强校验**：三版本官方镜像文件名+SHA256 固化在安装器，从 iStoreOS 官方 CDN 下载即验
- **停用/启用联动**：点停用真的关虚拟机（ACPI 优雅关机），从入口开机也会把应用中心状态扳回「运行中」
- **入口分状态 + 串口自救**：入口如实显示准备中/已关机/正在获取地址/后台启动中；LAN 没有 DHCP 或 Web 未起时，可在「网络修复」页经虚拟机串口重新获取 IP、重启网络、设静态地址或手动跳转
- **数据不丢**：升级/换版本自动把旧磁盘留档在 `/vol1/vm/backup/`，重装沿用同一网卡 MAC，不会刷掉已有配置

## 安装

1. 下载本目录发布链接（或 Releases 页）的 FPK
2. 应用中心 → 手动安装 → 向导中选版本/CPU/内存/是否随 NAS 自启
3. 等待后台完成（全新安装：25.12.5/24.10.8 约 4 分钟，22.03.7 约 11 分钟；复用已有磁盘 10~40 秒）
4. 点桌面图标或应用中心「打开」进入固定入口；虚拟机关着可在入口页一键开机，无需再去「虚拟机」应用手动操作

> 提示：官方 24.10.8 / 22.03.7 镜像 gz 尾部带杂散字节，日志中 `gunzip ... trailing garbage ignored` 属正常（SHA256 已校验，安装器自动容忍）。

## 支持的镜像版本

| 向导选项 | 官方镜像 | SHA256 |
|---|---|---|
| 25.12.5 | istoreos-25.12.5-2026091113-x86-64-squashfs-combined-efi.img.gz | `a89206e3238cc0421561e4f3d6ffb17296d700bd13d7e77fd93ed1f86afdfc30` |
| 24.10.8 | istoreos-24.10.8-2026073111-x86-64-squashfs-combined-efi.img.gz | `8825143bc8e4ae45f6f828c7e40e1c9a605ba455ce4c7abf691a4e2b3d2e005e` |
| 22.03.7 | istoreos-22.03.7-2025050912-x86-64-squashfs-combined-efi.img.gz | `bda277850a2ccb37a67e69974d6b43733057d96dfb5a70914228375879b7d2ba` |

## 源码

FPK 构建工程（fnpack 布局）见 [`source/`](source/) 子目录；构建：`fnpack build -d source目录`。
