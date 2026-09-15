# 🏠 冬瓜HAOS (dongguaha)

在 x86 fnOS 上以虚拟机形式运行 [冬瓜HAOS](https://bbs.hassbian.com/thread-23791-1-1.html)（Home Assistant OS 优化版）的 FPK 应用包。向导选 CPU / 内存 / 磁盘 / 系统版本后全自动：下载官方镜像 → 完整性校验 → 创建虚拟机 → 应用中心与桌面图标直达，不需要记忆虚拟机 IP。

<div align="center">

[![冬瓜HAOS](https://img.shields.io/badge/冬瓜HAOS-fnos-v18.2.7-green?style=flat-square)](https://github.com/Blue-Mink/fnos-vm-dongguaha/releases/tag/v18.2.7)
[![Platform](https://img.shields.io/badge/Platform-fnOS%20x86__64-lightgrey?style=flat-square)](#)

</div>

| 项目 | 信息 |
| :--- | :--- |
| 📥 **安装方式** | 下载 [com.dongguaha.vm-18.2.7-fnos-amd64.fpk](https://github.com/Blue-Mink/FnDepot/releases/download/v18.2.7/com.dongguaha.vm-18.2.7-fnos-amd64.fpk) ([sha256](https://github.com/Blue-Mink/FnDepot/releases/download/v18.2.7/com.dongguaha.vm-18.2.7-fnos-amd64.fpk.sha256)) 在 fnOS 应用中心手动安装 |
| 🏷️ **包版本** | v18.2.7（FPK SHA256 `d18f108c83a6b713a6aebeb121d8c86627728af6c441605cd27538ce173503d8`） |
| 📦 **可选系统版本** | 冬瓜HAOS 18.2（904MB）/ 18.1（900MB）/ 18.0（900MB）/ 17.3.1（886MB），均为官方 CDN 原包 |
| 🌐 **入口端口** | 36123（IP 寻踪跳转）· 36124（管理后台反代去横幅） |
| 🔧 **依赖** | 飞牛「虚拟机」应用（提供 KVM/libvirt/OVMF） |
| 📁 **原项目** | [fnos-vm-dongguaha](https://github.com/Blue-Mink/fnos-vm-dongguaha)（含构建说明与完整 changelog） |
| 🧩 **上游** | [冬瓜HAOS 镜像](https://bbs.hassbian.com/thread-23791-1-1.html) / [Home Assistant](https://www.home-assistant.io/)（本包不修改系统本体） |

## 特性

- **IP 寻踪六级发现链**：MAC→ARP → VNC 横幅 OCR 直读 → mDNS → virsh domifaddr → 局域网扫描 → 端口探测，DHCP 换址秒级跟随，桌面图标与应用中心「打开」始终指向当前地址
- **桌面双入口**：**冬瓜HAOS**（→ 管理后台 `:8124`，经反代去掉「浏览器版本过低」误报横幅）与 **Home Assistant**（`/ha` → `:8123`）
- **反代下按钮也直达**：后台面板里的「HA 登录页 / TTYD」按钮按当前主机名拼兄弟端口，经反代访问会落到 NAS；本包自动把这些链接改写回虚拟机真实地址
- **秒回安装 + 后台进度**：安装提交立即返回（避开平台安装回调看门狗），886~904 MB 镜像下载、完整性校验、建机在后台推进，进度可查、失败可续跑
- **入口分状态 + 串口自救**：如实显示已关机 / 正在启动 / 正在获取地址 / Home Assistant 启动中；拿不到 IP 时可在「网络修复」页经虚拟机串口重新获取地址、重启网络、设静态 IP 或手动跳转，不依赖任何网络前提
- **停用/启用联动**：点停用真的给虚拟机发 ACPI 优雅关机，从入口页开机也会把应用中心状态扳回「运行中」
- **数据不丢**：磁盘版本会被记录，换版本先把旧盘整块留档到 `/vol1/vm/backup/`；同一版本原盘复用（10~40 秒装完），重装沿用同一网卡 MAC 与 UUID

## 安装

1. 下载本目录发布链接（或 Releases 页）的 FPK
2. 应用中心 → 手动安装 → 向导中选 CPU / 内存 / 磁盘 / HAOS 版本（磁盘填 `0` = 直通本机空闲整盘）
3. 等待后台任务完成（全新安装含下载约 3~6 分钟；复用已有磁盘约 10~40 秒）
4. 点应用图标进入口页 →「启动虚拟机」一键开机，就绪后自动跳转到 Web 界面

> **首次启动较慢属正常**：Supervisor 首启要从上游拉取全套运行镜像，Web 页面可能需 **15~40 分钟**才就绪（之后每次开机约 2~10 分钟）；期间入口页会显示启动进度。
> 点「停用」后应用中心会立刻记为已停用，虚拟机真正落定还需 **45~70 秒**（HAOS 要逐个停掉核心 / Supervisor / 插件容器）。
> `/ha`、面板里的「HA 登录页 / TTYD」按钮都是浏览器**直连虚拟机 IP**，跨网段、VPN 之外或 AP 隔离时会打不开；`36123/36124` 两个入口只要到得了 NAS 就可用。

## 源码与构建

工程源码（fnpack 布局，含 `app/bin/haos-install-worker.sh` 等）见原项目 [fnos-vm-dongguaha](https://github.com/Blue-Mink/fnos-vm-dongguaha)，构建：`fnpack build -d .`；完整版本变更见其 [Releases](https://github.com/Blue-Mink/fnos-vm-dongguaha/releases)。
