# 🛒 FnDepot

飞牛 fnOS 第三方应用商店 **FnDepot** 的官方客户端：应用浏览、下载、安装与管理，支持官方源与 FnDepot V1/V2 外部源，是本仓库索引规范（`fndepot.json` / `fnpack.json`）的原始定义方。

<div align="center">

[![FnDepot](https://img.shields.io/badge/FnDepot-v0.5.1-green?style=flat-square)](https://github.com/Blue-Mink/FnDepot/releases/tag/fndepot-v0.5.1)
[![Platform](https://img.shields.io/badge/Platform-fnOS%20x86__64%20%C2%B7%20ARM64-lightgrey?style=flat-square)](#)

</div>

| 项目 | 信息 |
| :--- | :--- |
| 👨‍💻 **原作者** | [EWEDL](https://club.fnnas.com/home.php?mod=space&uid=5411) |
| 📁 **原项目** | [EWEDLCM/FnDepot](https://github.com/EWEDLCM/FnDepot) |
| 📥 **安装方式** | 下载 [fndepot-0.5.1-fnos-all.fpk](https://github.com/Blue-Mink/FnDepot/releases/download/fndepot-v0.5.1/fndepot-0.5.1-fnos-all.fpk)（[sha256](fpk.sha256)）在 fnOS 应用中心手动安装 |
| 🏷️ **版本** | v0.5.1（`platform = all`，包内内置 amd64 / arm64 双服务端） |
| 🌐 **入口** | 飞牛统一网关 `/app/fndepot`（应用 unix socket 运行，不占 TCP 端口，桌面图标与应用中心「打开」直达） |
| 🔧 **依赖** | 无（不需要 Docker / 虚拟机），最低系统版本 fnOS 1.2.0401 |

## 关于这个包

- **来源**：从一台已安装该应用的飞牛设备上提取的官方安装包，**未重新打包、未修改任何字节**，SHA256 = `9d47e92977b957af8dd0ab09477d3cf3abcf7c5d00d36a23eaccfa2b09b43eb0`。
- **版本口径**：作者公开的 GitHub Release 目前最新为 [v0.4.6](https://github.com/Blue-Mink/FnDepot/releases)（更早 v0.3.2），本包是设备上更新的 0.5.1，公开渠道当时取不到，故代为分发以便直接安装。
- **归属**：程序版权归原作者 EWEDL 所有，本仓库仅作分发镜像并标注出处；应用自身的更新检查、条款与反馈以上游为准（[反馈入口](https://github.com/EWEDLCM/FnDepot/issues)）。

## 安装

1. 下载上方 FPK，确认 `sha256sum` 与上表一致
2. 应用中心 → 手动安装（`platform = all`，x86 与 ARM64 通用，无端口向导）
3. 桌面图标或应用中心「打开」进入统一网关页面
4. 首次进入同意条款后，即可在「应用源」中添加本源：`https://github.com/Blue-Mink/FnDepot`

## 已知边界

- 客户端以 `run-as: package` 非 root 运行，Web 入口依赖飞牛统一网关；未登录飞牛时直接访问网关路径会被平台拦到登录页，属平台行为。
- 包内 `ui/config` 的图标槽位写作 `images/icon_{0}.png`，而 `ui/images/` 只提供 `icon_64.png` / `icon_256.png`（缺 `icon_0.png` 兜底文件）；桌面图标由平台从 FPK 根部的 `ICON.PNG` / `ICON_256.PNG` 取用，显示正常。此现象属上游打包习惯，本仓库未代改。
- 卸载向导会让你选择「保留」或「清空」应用数据：应用数据（应用源配置、已下载 FPK、SQLite 库）在应用数据目录，选清空会一并删除。
