<p align="center">
  <img src="ICON_256.PNG" width="92" alt="iStoreOS for fnOS" /><br/>
  <b>iStoreOS for fnOS</b><br/>
  在飞牛 NAS 上把 iStoreOS 装成一台虚拟机，装完只管用一个入口访问<br/><br/>
  <a href="../../releases/latest"><img src="https://img.shields.io/github/v/release/Blue-Mink/fnos-vm-istoreos?label=FPK&color=1f6feb" alt="release"/></a>
  <img src="https://img.shields.io/badge/iStoreOS-25.12.5%20%7C%2024.10.8%20%7C%2022.03.7-2da44e" alt="istoreos versions"/>
  <img src="https://img.shields.io/badge/fnOS-x86__64%20%C2%B7%20KVM-6f42c1" alt="platform"/>
  <a href="https://www.istoreos.com/"><img src="https://img.shields.io/badge/upstream-iStoreOS-00a58a" alt="upstream"/></a>
</p>

<p align="center">
  <img src="docs/entry-states.png" width="880" alt="固定入口的三种状态：已关机、正在启动、网络修复"/><br/>
  <sub>固定入口 <code>:36125</code> 的三种状态 —— 已关机可一键开机 · 启动过程说清卡在哪 · 拿不到 IP 时经串口自救</sub>
</p>

向导里选好版本、CPU、内存就全自动跑完：**下载官方镜像 → SHA256 校验 → 网络预置 → 建虚拟机 → 桌面图标直达**。
镜像由 iStoreOS 官方 CDN 原样获取，本仓库只做 fnOS 侧封装，不改 iStoreOS 本体。

---

## 🚀 快速开始

| # | 做什么 | 说明 |
|---|---|---|
| 1 | [下载 FPK](../../releases/latest) | 当前 `1.1.10` · SHA256 `5993af3490f5bcd15d7b59a7bfd955472df69899963a8b623359f6faed00123b` |
| 2 | 应用中心 → 手动安装 | 向导选 iStoreOS 版本 / CPU / 内存 / 是否随 NAS 自启 |
| 3 | 等后台跑完 | 提交后**立即返回**；全新安装 25.12.5、24.10.8 约 4 分钟，22.03.7 约 11 分钟，磁盘已存在约 10~40 秒 |
| 4 | 打开 `http://<NAS 地址>:36125/` | 应用中心「打开」和桌面图标都走这里，自动跟随虚拟机 IP 变化 |

> **要求**：fnOS x86_64 + 已装飞牛「虚拟机」应用（需 `/dev/kvm`）· 安装卷剩余 ≥ 2GB · 主路由 DHCP 可用
> **卡住了**：`cat /tmp/istoreos-install.state`（`queued → downloading → verifying → extracting → provisioning → defining-vm → ready`）

## ✨ 会用到的几件事

| 能力 | 说明 |
|---|---|
| **固定入口** | MAC/ARP → mDNS → domifaddr → 局域网扫描 → HTTP 指纹多级寻踪，DHCP 地址漂移无感 |
| **状态透明** | 分「未开机 / 正在启动 / 还在获取地址 / Web 未就绪 / 已就绪」显示，开关机态每次都跟虚拟机实际状态核对 |
| **串口网络修复** | 拿不到 IPv4 时在入口页经 libvirt 串口重新获取 IP、设静态地址、重启网络、手动指定跳转地址，**不依赖虚拟机有网络** |
| **停用/启用联动** | 停用即 ACPI 优雅关机；在入口页点开机也会把应用中心状态同步回来 |
| **数据不丢** | 磁盘常驻 `/vol1/vm/pool/istoreos.qcow2`，重装原样复用；改选别的版本先把旧盘整块留档到 `/vol1/vm/backup`；重装沿用网卡 MAC，路由器绑定不失效 |
| **秒回安装** | 规避 fnOS 约 190 秒的安装回调看门狗；中断后点「启动」按原参数续跑 |

## 📦 支持的镜像

| 向导选项 | 官方文件（[`fw.koolcenter.com/iStoreOS/x86_64_efi/`](https://fw.koolcenter.com/iStoreOS/x86_64_efi/)） | SHA256 |
|---|---|---|
| **25.12.5**（默认） | `istoreos-25.12.5-2026091113-…-combined-efi.img.gz` | `a89206e3238c…` |
| 24.10.8 | `istoreos-24.10.8-2026073111-…-combined-efi.img.gz` | `8825143bc8e4…` |
| 22.03.7 | `istoreos-22.03.7-2025050912-…-combined-efi.img.gz` | `bda277850a2c…` |

> 24.10.8 / 22.03.7 官方 gz 尾部带杂散字节，`gunzip` 会报 `trailing garbage ignored` —— 内容等价、校验通过，安装器已容忍。

## 🧹 卸载

应用中心卸载只删虚拟机定义与寻踪服务，**qcow2 磁盘和你的配置不会被删**，重装即复用。
要彻底清掉系统数据：先停用应用，再手动删 `/vol1/vm/pool/istoreos.qcow2`（建议先备份一份）。

## 🔧 从源码构建

```bash
fnpack build -d .        # 产出 com.istoreos.vm.fpk，文件名补上版本号即可发布
```

`manifest` 元数据 · `cmd/` 生命周期回调 · `app/bin/` 安装 worker、离线预置、寻踪入口服务 · `wizard/` 向导定义

---

<p align="center"><sub>iStoreOS 本体版权归 koolcenter 团队所有 · <a href="https://www.istoreos.com/">istoreos.com</a></sub></p>
