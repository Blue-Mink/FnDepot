# 🛒 FnDepot

飞牛 fnOS 第三方应用商店 **FnDepot** 的官方客户端：应用浏览、下载、安装与管理，支持官方源与 FnDepot V1/V2 外部源，是本仓库索引规范（`fnpack.json`）的原始定义方。

<div align="center">

[![FnDepot](https://img.shields.io/badge/FnDepot-v0.8.36-green?style=flat-square)](https://github.com/Blue-Mink/FnDepot/releases/tag/fndepot-v0.8.36)
[![Platform](https://img.shields.io/badge/Platform-fnOS%20x86__64%20%C2%B7%20ARM64-lightgrey?style=flat-square)](#)

</div>

| 项目 | 信息 |
| :--- | :--- |
| 👨‍💻 **原作者** | [EWEDL](https://club.fnnas.com/home.php?mod=space&uid=5411) |
| 📁 **原项目** | [EWEDLCM/FnDepot](https://github.com/EWEDLCM/FnDepot) |
| 📥 **安装方式** | 下载 [fndepot_v0.8.36.fpk](https://github.com/Blue-Mink/FnDepot/releases/download/fndepot-v0.8.36/fndepot_v0.8.36.fpk)（[sha256](fpk.sha256)）在 fnOS 应用中心手动安装 |
| 🏷️ **版本** | v0.8.36（`platform = all`，包内内置 amd64 / arm64 双服务端） |
| 🌐 **入口** | 飞牛统一网关 `/app/fndepot`（应用 unix socket 运行，不占 TCP 端口，桌面图标与应用中心「打开」直达） |
| 🔧 **依赖** | 无（不需要 Docker / 虚拟机），最低系统版本 fnOS 1.2.0401 |

## 本次更新（0.8.36）

本版把 0.8.32–0.8.36 五个内部构建合并为一个对外版本，按类别归拢如下：

**【系统适配】**
- 适配 fnOS 1.2.05+ 的 FPK 签名校验：未签名第三方应用安装/更新会被官方以 10900 拒绝；现安装、更新与自更新都会先读包校验状态并弹风险确认，确认后自动带上官方要求的签名信息（老系统行为不变）。

**【安装与更新】**
- 修复更新第三方应用仍报 10900：提交前必重新读取校验状态，需要确认时必定先弹确认框；后端代建任务同样加签名闸门。
- 修复「取消勾选『安装后启动』，装完仍被启动」：统一以确认页选择为准，后端代理不再硬编码 immediateStart。
- 修复「本地安装包停在解析中且无法取消」：签名确认框提到最上层（z-140），两处静默卡死分支改为放行并交回可读错误；取消即关闭安装卡。

**【界面】**
- 顶栏 60px、控件 38px、分页留白 18px（移动端 14px）；首页宽屏铺满、分类「宽屏 1 行 / 窄屏 3 行」；工具条收至 48px；移除左下角无法关闭的进度浮层。

**【其他】**
- 官方直连失败不再丢错误码；补全第三方依赖许可清单。

## 关于这个包

- **来源**：从一台已安装该应用的飞牛设备上提取的官方安装包（应用自更新下载件），**未重新打包、未修改包内任何字节**，SHA256 = `b4704b8ade8ddc442435f61504dd03f9c8e4c28cb3b36d9e84edfa379e06c2c9`（12,278,030 B）。
- **版本口径**：作者公开的 GitHub Release 仍停在 [v0.4.6](https://github.com/Blue-Mink/FnDepot/releases)（源码插件线），客户端本体通过应用内自更新分发，本包即设备自更新到的 0.8.36（2026-11 提取），公开渠道取不到，故代为分发以便直接安装。
- **归属**：程序版权归原作者 EWEDL 所有，本仓库仅作分发镜像并标注出处；应用自身的更新检查、条款与反馈以上游为准（[反馈入口](https://github.com/EWEDLCM/FnDepot/issues)）。

## 安装

1. 下载上方 FPK，确认 `sha256sum` 与上表一致
2. 应用中心 → 手动安装（`platform = all`，x86 与 ARM64 通用，无端口向导）
3. 桌面图标或应用中心「打开」进入统一网关页面
4. 首次进入同意条款后，即可在「应用源」中添加本源：`https://github.com/Blue-Mink/FnDepot`

## 已知边界

- 客户端以 `run-as: package` 非 root 运行，Web 入口依赖飞牛统一网关；未登录飞牛时直接访问网关路径会被平台拦到登录页，属平台行为。
- **图标**：本仓库树内的 `ICON.PNG` / `ICON_256.PNG` 已统一为同一张 512×512 母版（商店卡片走 `icon_url`，路径不变）。包内 `ui/images/` 仍只有 `icon_64.png` / `icon_256.png` 且两文件同为 256×256 图像（上游打包习惯），为保持「包体字节未改」未代改；桌面图标由平台取包根 `ICON.PNG` / `ICON_256.PNG`，显示正常。
- 卸载向导会让你选择「保留」或「清空」应用数据：应用数据（应用源配置、已下载 FPK、SQLite 库）在应用数据目录，选清空会一并删除。
