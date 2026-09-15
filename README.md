<div align="center">

![FnDepot](https://img.shields.io/badge/FnDepot-应用源-blue?style=for-the-badge)
![fnOS](https://img.shields.io/badge/fnOS-NAS-green?style=for-the-badge)
![License](https://img.shields.io/badge/License-MIT-yellow?style=for-the-badge)

# 🚀 FnDepot 第三方应用源

**由 [Blue-Mink](https://github.com/Blue-Mink) 维护的飞牛 fnOS 第三方应用源**

[![GitHub stars](https://img.shields.io/github/stars/Blue-Mink/FnDepot?style=social)](https://github.com/Blue-Mink/FnDepot)
[![GitHub forks](https://img.shields.io/github/forks/Blue-Mink/FnDepot?style=social)](https://github.com/Blue-Mink/FnDepot)
[![GitHub issues](https://img.shields.io/github/issues/Blue-Mink/FnDepot)](https://github.com/Blue-Mink/FnDepot/issues)

[🔗 添加本源](https://github.com/Blue-Mink/FnDepot) · [📖 使用文档](https://github.com/EWEDLCM/FnDepot) · [🐛 问题反馈](https://github.com/Blue-Mink/FnDepot/issues)

</div>

---

## 📦 应用列表

### 🔐 敲门 knock (fn-knock)

面向 NAS、软路由与家庭服务器的多平台高性能安全网关，采用 Rust + Go 双核心架构，集成反向代理、身份认证、WAF、SSL、DDNS 与内网穿透。

<div align="center">

[![fn-knock](https://img.shields.io/badge/fn--knock-v2.4.12-orange?style=flat-square)](https://github.com/Blue-Mink/FnDepot)
[![Platform](https://img.shields.io/badge/Platform-x86__64%20%7C%20ARM64-lightgrey?style=flat-square)](#)

</div>

| 项目 | 信息 |
| :--- | :--- |
| 👨‍💻 **原作者** | [kci-lnk](https://github.com/kci-lnk) |
| 📁 **原项目** | [fn-knock-turborepo](https://github.com/kci-lnk/fn-knock-turborepo) |
| 🌐 **官网** | https://fnknock.cn |
| 📥 **安装方式** | 在飞牛 FnDepot 直接添加本源，客户端中搜索「敲门knock」即可安装 |
| 🏷️ **版本** | v2.4.12（同步原作者最新官方 FPK，含源码快照） |

---

### 📻 全球电台 (global-radio)

基于 Vue 3 + Vite 的在线电台应用，支持全球电台搜索、播放、收藏、历史记录、主题切换与多国语言。内置电台目录/封面图 NAS 缓存加速，支持端口直连与飞牛统一网关双入口。

<div align="center">

[![global-radio](https://img.shields.io/badge/global--radio-v1.3.0-purple?style=flat-square)](https://github.com/Blue-Mink/FnDepot)
[![Platform](https://img.shields.io/badge/Platform-fnOS%20x86__64-lightgrey?style=flat-square)](#)

</div>

| 项目 | 信息 |
| :--- | :--- |
| 👨‍💻 **原作者** | [moli-xia](https://github.com/moli-xia) |
| 📁 **原项目** | [global-radio](https://github.com/moli-xia/global-radio) |
| 📥 **安装方式** | 在飞牛 FnDepot 直接添加本源，客户端中搜索「全球电台」即可安装 |
| 🏷️ **版本** | v1.3.0（统一网关入口 + 目录/封面 NAS 缓存加速 + 高清图标；端口直连与网关双入口） |

---

### 🔑 KMS Activator

基于 vlmcsd 的 KMS 激活服务，为局域网内 Windows / Office / Windows Server 提供批量 KMS 激活服务。

<div align="center">

[![KMS](https://img.shields.io/badge/KMS--Activator-v1.0.0-red?style=flat-square)](https://github.com/Blue-Mink/FnDepot)
[![Platform](https://img.shields.io/badge/Platform-x86__64-lightgrey?style=flat-square)](#)

</div>

**✨ 功能特点**
- 🌐 Web 管理界面一键启停
- 🔍 自动 IP 检测
- 🔑 内置 GVLK 密钥库
- 💻 支持 Windows 10/11 / Office 2013-2016 / Windows Server 2016-2025

| 项目 | 信息 |
| :--- | :--- |
| 📥 **安装方式** | 下载 [KmsActivator.fpk](https://github.com/Blue-Mink/FnDepot/releases/download/v1.0.0/KmsActivator.fpk) 在 fnOS 应用中心手动安装 |
| 📖 **详细说明** | [kms-activator/README.md](kms-activator/README.md) |
| 🏷️ **版本** | v1.0.0 |

---

### 🗺️ TREK

TREK 是一款自托管旅行规划工具，基于 [liketrek/TREK](https://github.com/liketrek/TREK) 构建。此版本为 Docker Web FPK，已在 fnOS 备用机完成安装、HTTP 登录、应用中心启停、端口修改与卸载重装链路验证。

<div align="center">

[![TREK](https://img.shields.io/badge/TREK-v4.1.1-blue?style=flat-square)](https://github.com/Blue-Mink/FnDepot)
[![Platform](https://img.shields.io/badge/Platform-fnOS%20x86__64-lightgrey?style=flat-square)](#)

</div>

| 项目 | 信息 |
| :--- | :--- |
| 👨‍💻 **原作者** | [liketrek](https://github.com/liketrek) |
| 📁 **原项目** | [liketrek/TREK](https://github.com/liketrek/TREK) |
| 📥 **安装方式** | 下载 [com.trek.app-4.1.1-fnos-amd64.fpk](https://github.com/Blue-Mink/FnDepot/releases/download/trek-v4.1.1/com.trek.app-4.1.1-fnos-amd64.fpk) 在 fnOS 应用中心手动安装，或在飞牛 FnDepot 中添加本源后搜索「TREK」安装 |
| 🏷️ **版本** | v4.1.1 |
| 🌐 **默认端口** | 32679（HTTP） |
| 🔑 **默认账号** | admin@trek.local / Admin12345（登录后请立即修改密码） |
| ✅ **验证状态** | 已验证安装、HTTP 登录、应用中心启动/停止传递 Docker、端口 32679↔32680 配置链路 |
| 📖 **详细说明** | [trek/README.md](trek/README.md) |

---

### 🏠 冬瓜HAOS (dongguaha)

在 x86 fnOS 系统中创建冬瓜HAOS虚拟机，KVM 硬件加速原生性能。内置「IP 寻踪」与桌面双图标（冬瓜HAOS 管理后台 / Home Assistant Web UI），固定入口一键直达、自动跟随虚拟机 IP 变化；管理后台自带去横幅反代，面板里的 HA 登录页 / TTYD 按钮也会自动指向虚拟机真实地址。安装秒回、后台推进并可断点续跑，入口页可一键开机、拿不到 IP 时能经串口自救。首启 Web 页面需 15~40 分钟就绪属正常。

<div align="center">

[![冬瓜HAOS](https://img.shields.io/badge/冬瓜HAOS-v18.2.7-green?style=flat-square)](https://github.com/Blue-Mink/fnos-vm-dongguaha/releases/tag/v18.2.7)
[![Platform](https://img.shields.io/badge/Platform-fnOS%20x86__64-lightgrey?style=flat-square)](#)

</div>

| 项目 | 信息 |
| :--- | :--- |
| 👨‍💻 **原作者** | [Blue-Mink](https://github.com/Blue-Mink) |
| 📁 **原项目** | [fnos-vm-dongguaha](https://github.com/Blue-Mink/fnos-vm-dongguaha) |
| 📥 **安装方式** | 下载 [com.dongguaha.vm-18.2.7-fnos-amd64.fpk](https://github.com/Blue-Mink/FnDepot/releases/download/v18.2.7/com.dongguaha.vm-18.2.7-fnos-amd64.fpk) ([sha256](https://github.com/Blue-Mink/FnDepot/releases/download/v18.2.7/com.dongguaha.vm-18.2.7-fnos-amd64.fpk.sha256)) 在 fnOS 应用中心手动安装 |
| 🏷️ **版本** | v18.2.7 |
| 📦 **可选系统版本** | 冬瓜HAOS 18.2 / 18.1 / 18.0 / 17.3.1（官方 CDN，换版本自动留档旧磁盘） |
| 🌐 **入口端口** | 36123（寻踪跳转）· 36124（管理后台反代） |
| 📱 **手机端** | [Android APK](https://github.com/Blue-Mink/fnos-vm-dongguaha/releases/download/v18.2/Home-Assistant.apk) · [Google Play](https://play.google.com/store/apps/details?id=io.homeassistant.companion.android) · [iOS](https://apps.apple.com/cn/app/home-assistant/id1099568401) |

---

### 🎨 呆呆面板 (daidai-panel)

基于青龙面板二次开发的轻量级定时脚本管理面板。支持 Python、Node.js、Shell、TypeScript、Go 等多语言脚本，内置 18 种消息推送渠道、订阅管理、环境变量、依赖管理、Open API 等功能。一些脚本 https://www.xiaoxin03.top/

<div align="center">

[![daidai-panel](https://img.shields.io/badge/daidai--panel-v3.2.6-red?style=flat-square)](https://github.com/Blue-Mink/FnDepot)
[![Platform](https://img.shields.io/badge/Platform-x86__64-lightgrey?style=flat-square)](#)

</div>

| 项目 | 信息 |
| :--- | :--- |
| 👨‍💻 **原作者** | [linzixuanzz](https://github.com/linzixuanzz) |
| 📁 **原项目** | [daidai-panel](https://github.com/linzixuanzz/daidai-panel) |
| 📥 **安装方式** | 下载 [daidai-panel-v3.2.6.fpk](https://github.com/Blue-Mink/FnDepot/releases/download/v3.2.6/daidai-panel-v3.2.6.fpk) 在 fnOS 应用中心手动安装 |
| 🏷️ **版本** | v3.2.6（同步原作者 2026-09-08 最新版） |
| 🌐 **脚本资源** | https://www.xiaoxin03.top/ |

---

### 🧠 New API (new-api)

新一代大模型网关与 AI 资产管理系统，聚合多模型供应商，提供 OpenAI 兼容接口、智能路由、权限管理、用量统计与成本核算。

<div align="center">

[![new-api](https://img.shields.io/badge/new--api-v1.0.0--rc.37-blue?style=flat-square)](https://github.com/Blue-Mink/FnDepot)
[![Platform](https://img.shields.io/badge/Platform-x86__64%20%7C%20ARM64-lightgrey?style=flat-square)](#)

</div>

| 项目 | 信息 |
| :--- | :--- |
| 👨‍💻 **原作者** | [QuantumNous](https://github.com/QuantumNous) |
| 📁 **原项目** | [new-api](https://github.com/QuantumNous/new-api) |
| 📥 **安装方式** | 在飞牛 FnDepot 添加本源，或下载 Release 中的 `.fpk` 手动安装 |
| 🏷️ **版本** | v1.0.0-rc.37 |
| 🌐 **默认端口** | 33000 |
---

### 🌩️ DockFlare

自托管 Cloudflare Tunnel 管理平台——给 Docker 容器打上 `dockflare.*` 标签，自动创建 DNS 记录、隧道 ingress 规则与 Zero Trust Access 策略，一站式管理 Cloudflare Tunnel / DNS / Access / Workers / R2 / Email Routing。

<div align="center">

[![DockFlare](https://img.shields.io/badge/DockFlare-v3.1.3-orange?style=flat-square)](https://github.com/Blue-Mink/FnDepot)
[![Platform](https://img.shields.io/badge/Platform-x86__64%20%7C%20ARM64-lightgrey?style=flat-square)](#)

</div>

| 项目 | 信息 |
| :--- | :--- |
| 👨‍💻 **原作者** | [ChrispyBacon-dev](https://github.com/ChrispyBacon-dev) |
| 📁 **原项目** | [DockFlare](https://github.com/ChrispyBacon-dev/DockFlare) |
| 📥 **安装方式** | 下载 [dockflare-v3.1.3-fnos-all.fpk](https://github.com/Blue-Mink/FnDepot/releases/download/v3.1.3/dockflare-v3.1.3-fnos-all.fpk) 在 fnOS 应用中心手动安装，安装时可选端口（默认32671） |
| 🏷️ **版本** | v3.1.3（同步原作者最新版） |

---

### 🚀 KSpeeder

Docker 镜像加速管理工具，支持多源镜像缓存、加速规则管理、流量统计与自动清理，帮助提升容器镜像拉取速度。基于 kspeeder/docker_kspeeder 项目打包，适配飞牛 fnOS 应用中心。

<div align="center">

[![KSpeeder](https://img.shields.io/badge/KSpeeder-v0.8.0-blue?style=flat-square)](https://github.com/Blue-Mink/FnDepot)
[![Platform](https://img.shields.io/badge/Platform-x86__64-lightgrey?style=flat-square)](#)

</div>

| 项目 | 信息 |
| :--- | :--- |
| 👨‍💻 **原作者** | [kspeeder](https://github.com/kspeeder) |
| 📁 **原项目** | [docker_kspeeder](https://github.com/kspeeder/docker_kspeeder) |
| 📥 **安装方式** | 下载 [kspeeder-0.8.0-fnos-amd64.fpk](https://github.com/Blue-Mink/FnDepot/releases/download/v0.8.0/kspeeder-0.8.0-fnos-amd64.fpk) 在 fnOS 应用中心手动安装，安装/应用设置中可修改管理端口与镜像代理端口 |
| 🏷️ **版本** | v0.8.0 |
| 🌐 **默认端口** | 管理界面 5003 / 镜像代理 5443 |
| 📖 **详细说明** | [kspeeder/README.md](kspeeder/README.md) |

---

### 🛜 iStoreOS (istoreos)

在 x86 fnOS 上以虚拟机运行 [iStoreOS](https://www.istoreos.com/)（基于 OpenWrt）：向导三选一官方版本（25.12.5/24.10.8/22.03.7），下载即 SHA256 强校验；自动网络预置融入现有局域网（DHCP 客户端、不与主路由抢 DHCP）；内置「IP 寻踪」固定入口（36125）与桌面图标自动跟随虚拟机 IP 变化；安装秒回、后台进度可查、失败可续跑；与应用中心停用/启用真正联动，无 IP 时还能经虚拟机串口自救。

<div align="center">

[![iStoreOS](https://img.shields.io/badge/istoreos--fnos-v1.1.10-green?style=flat-square)](https://github.com/Blue-Mink/fnos-vm-istoreos/releases/tag/v1.1.10)
[![Platform](https://img.shields.io/badge/Platform-fnOS%20x86__64-lightgrey?style=flat-square)](#)

</div>

| 项目 | 信息 |
| :--- | :--- |
| 👨‍💻 **打包维护** | [Blue-Mink](https://github.com/Blue-Mink) |
| 📁 **原项目** | [fnos-vm-istoreos](https://github.com/Blue-Mink/fnos-vm-istoreos) · [iStoreOS 上游](https://www.istoreos.com/) |
| 📥 **安装方式** | 下载 [com.istoreos.vm-1.1.10-fnos-amd64.fpk](https://github.com/Blue-Mink/FnDepot/releases/download/istoreos-v1.1.10/com.istoreos.vm-1.1.10-fnos-amd64.fpk) ([sha256](https://github.com/Blue-Mink/FnDepot/releases/download/istoreos-v1.1.10/com.istoreos.vm-1.1.10-fnos-amd64.fpk.sha256)) 在 fnOS 应用中心手动安装 |
| 🏷️ **版本** | v1.1.10（可选 iStoreOS 25.12.5 / 24.10.8 / 22.03.7） |
| 🌐 **入口端口** | 36125（IP 寻踪入口，自动跳转虚拟机管理后台） |
| 📖 **详细说明** | [istoreos/README.md](istoreos/README.md) |

---

## 📄 许可证

本项目遵循 [MIT License](LICENSE) 许可证。

---

## 🙏 致谢

- [FnDepot 应用源构建规范 V1.1.1](https://github.com/EWEDLCM/FnDepot)
- [飞牛 fnOS](https://www.fnnas.com/)

---

<div align="center">

**⭐ 如果这个仓库对你有帮助，请给个 Star！** ⭐

Made with ❤️ by [Blue-Mink](https://github.com/Blue-Mink)

</div>

