# iStoreOS for fnOS（飞牛 NAS 虚拟机应用）

在飞牛 fnOS 上以虚拟机形式运行 [iStoreOS](https://www.istoreos.com/)（基于 OpenWrt 的路由/NAS 系统）的 FPK 应用包。安装向导选版本后全自动：下载官方镜像 → 校验 → 网络预置 → 创建虚拟机 → 应用中心/桌面图标直达。

> 本仓库为 fnOS 打包适配工程，不修改 iStoreOS 本体；镜像从 iStoreOS 官方 CDN 原样下载并做 SHA256 校验。

## 当前版本

| | |
|---|---|
| 包版本 | **1.0.5** |
| FPK 下载 | [Releases](../../releases/latest) |
| FPK SHA256 | `ca270c76f85fcd217d968c6a165f75cf6c94c102dcb7c196f8019a675d4053f3` |

## 特性

- **秒回安装 + 后台进度**：安装回调秒级返回（规避 fnOS 约 190 秒回调看门狗），下载/预置/建机全部异步进行，状态机可查：`cat /tmp/istoreos-install.state`
- **三版本向导可选**：25.12.5 / 24.10.8 / 22.03.7（x86-64 EFI 官方 combined 镜像，安装时从官方 CDN 下载并强校验）
- **网络预置（A1 离线注入）**：安装期内存快照 + 一次性 qemu 起机，把 LAN 口改为 DHCP 客户端、关闭 iStoreOS 自带 DHCP/RA —— 接入任意主路由网段自动拿 IP，无需要求 192.168.100.x 网段
- **IP 寻踪入口（36125 端口）**：mac/arp → VNC 横幅 → mDNS → domifaddr → ping sweep 多级寻踪，应用中心「打开」按钮与桌面图标自动 302 到 VM 当前 IP（DHCP 地址漂移无感）
- **官方风格图标**：应用中心/桌面图标采用飞牛官方 squircle（连续曲率超椭圆）圆角曲线
- **断点续跑**：安装失败/中断后在应用中心点「启动」会按原参数自动重试；已有预置磁盘时秒级复用

## 系统要求

| 项目 | 要求 |
|---|---|
| 系统 | fnOS x86_64（需 KVM，`/dev/kvm`） |
| 虚拟化 | 飞牛「虚拟机」应用已安装（提供 libvirt/OVMF 固件/virsh） |
| 内存 | 默认 1024MB，向导可设 512MB ~ 物理内存-1.5GB |
| 磁盘 | 安装卷剩余 ≥ 2GB（qcow2 虚拟磁盘 2GB 薄分配） |
| 网络 | 主路由 DHCP 可用（VM 桥接在 NAS 网口上取 IP） |

## 安装

1. [Releases](../../releases/latest) 下载 `com.istoreos.vm-1.0.5-fnos-amd64.fpk`
2. 飞牛 应用中心 → 手动安装 → 选择 FPK
3. 向导中选择版本/CPU/内存/是否随 NAS 自启
4. 等待后台完成（首次下载约 240MB，全程约 5~8 分钟；qcow2 已存在时重装约 1 分钟）
5. 完成后首次需在「虚拟机」应用中启动 `istoreos`（或 `virsh start istoreos`），之后应用中心「打开」/桌面图标自动跳转 iStoreOS 后台

进度排查：

```bash
cat /tmp/istoreos-install.state        # queued/downloading/verifying/extracting/provisioning/defining-vm/ready/failed:xxx
tail -f /tmp/istoreos-install.log      # 状态轨迹
journalctl -u isto-install.service     # 后台 worker 详细日志
```

## 支持的 iStoreOS 版本

| 向导选项 | 官方镜像文件 | SHA256 |
|---|---|---|
| 25.12.5（默认） | istoreos-25.12.5-2026091113-x86-64-squashfs-combined-efi.img.gz | `a89206e3238cc0421561e4f3d6ffb17296d700bd13d7e77fd93ed1f86afdfc30` |
| 24.10.8 | istoreos-24.10.8-2026073111-x86-64-squashfs-combined-efi.img.gz | `8825143bc8e4ae45f6f828c7e40e1c9a605ba455ce4c7abf691a4e2b3d2e005e` |
| 22.03.7 | istoreos-22.03.7-2025050912-x86-64-squashfs-combined-efi.img.gz | `bda277850a2ccb37a67e69974d6b43733057d96dfb5a70914228375879b7d2ba` |

下载源：`https://fw.koolcenter.com/iStoreOS/x86_64_efi/`（iStoreOS 官方 CDN）

> 注：24.10.8 / 22.03.7 官方 gz 文件尾部带杂散字节，`gunzip` 会报 `trailing garbage ignored`（rc≠0）——镜像内容等价且 SHA256 校验通过，安装器已容忍，属正常现象。

## 卸载说明

- 应用中心卸载会删除虚拟机定义、qcow2 磁盘与寻踪服务，`/tmp` 下状态/日志文件除外
- 想保留磁盘换版本：先备份 `/vol1/vm/pool/istoreos.qcow2`（或在虚拟机应用里导出）

## 仓库结构

```
manifest            FPK 元数据
cmd/                平台生命周期回调（install/main/uninstall/config/upgrade）
app/bin/            后台安装 worker、离线预置脚本、IP 寻踪入口服务、DB 守护
app/ui/             桌面图标与入口配置
config/             权限与资源声明
wizard/             安装/配置向导定义
```

## 从源码构建

依赖飞牛 `fnpack` 打包工具：

```bash
fnpack build -d .
# 产出 com.istoreos.vm.fpk（文件名取 manifest 的 appname，重命名加版本号即可）
```

## 致谢与上游

- iStoreOS：https://www.istoreos.com/ （下载站 https://fw.koolcenter.com/ ）
- iStoreOS 本体版权归 koolcenter 团队所有；本项目仅为 fnOS 侧的 FPK 封装与运维脚本
