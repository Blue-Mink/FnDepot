# KMS Activator

局域网 KMS 激活服务，基于 [vlmcsd](https://github.com/SystemInflux/vlmcsd)（上游仓库已下架），为局域网内的 Windows / Windows Server / Office 提供批量激活。

默认监听 **标准端口 1688**，客户端 `slmgr /skms <NAS IP>` 就能连上，不需要写端口。

<div align="center">

[![FPK](https://img.shields.io/badge/FPK-v1.1.10-green?style=flat-square)](https://github.com/Blue-Mink/FnDepot/releases/tag/kms-activator-v1.1.10)
![Platform](https://img.shields.io/badge/Platform-x86__64-lightgrey?style=flat-square)
![Port](https://img.shields.io/badge/Port-1688-blue?style=flat-square)

</div>

## 安装与启动

1. 应用中心 → 手动安装 → 选择 `KmsActivator-1.1.10-fnos-amd64.fpk`（校验值见 [fpk.sha256](fpk.sha256)）
2. 安装完成后打开应用页面，点「启动」；页面显示「运行中」即已就绪
3. 应用中心的「停用 / 启用」与页面按钮会互相同步状态

## 客户端用法

在客户机以**管理员**身份打开 CMD 或 PowerShell：

| 客户机 | 命令 |
| :--- | :--- |
| Windows | `slmgr /ipk <GVLK>` → `slmgr /skms <NAS IP>` → `slmgr /ato` |
| Windows Server | 同上；须用 Standard / Datacenter 等批量许可（VOL）版本的 GVLK |
| Office | `cscript ospp.vbs /sethst:<NAS IP>` → `cscript ospp.vbs /act` |

应用页面内置 Windows / Server / Office 三段分步指引与 GVLK 密钥表，点一下即可复制。

## 相比 1.0.0 的改动

| 项目 | 1.0.0 | 本版 1.1.10 |
| :--- | :--- | :--- |
| 监听端口 | 11688，靠 1688→11688 转发 | **1688** 直接监听 |
| 端口转发 | iptables REDIRECT（非 root 下必然失败，页面却仍显示「运行中」，客户端连不上） | 不再使用任何 nat / iptables 规则 |
| 运行权限 | 包用户，无法绑定 1688 | 以 root 启动**只为绑定 1688**，绑定后立即降权到应用用户运行 |
| 启停 | 网页直启，绕过应用中心造成状态背离 | 优先走 `appcenter-cli`，调不动时直启并在页面如实提示 |
| 界面 | 单页浅色 | 简约版式、深色命令块、Windows Server 板块、明暗主题（自动 / 浅色 / 深色） |
| 图标 | 64px 原图，高分屏发糊 | 按上游原版构图矢量重绘，64/128/256/512 各按目标尺寸出图 |
| 署名 | 指向名下并无此项目的账号 | Blue-Mink（真实上游为 vlmcsd，见下方致谢） |

## 已知边界

- **仅支持 x86_64**。包内 `vlmcsd` 沿用上游发布的二进制（stripped，无可核验的上游 tag 与 checksum），ARM 机型需要自行编译替换。
- 只适用于 **VOL 批量许可**镜像；Retail / OEM 版本改 KMS 端口不会生效（典型报错 `0xC004F074`）。
- KMS 激活请用于自有设备的合法批量许可场景，相关许可条款由微软约束。

## 构建

源码在 [source/](source/)（manifest、`cmd/` 生命周期脚本、`config/privilege`、`app/ui/index.cgi` 页面与图标；`vlmcsd` 二进制不入库，只随 Release 的 FPK 分发）。

```bash
fnpack build -d source
```

## 📄 许可证

打包脚本与界面代码遵循 FnDepot 的 [MIT License](../LICENSE)。vlmcsd 程序本体、各版本 GVLK 密钥的版权与商标归各自权利人所有。

---

## 🙏 致谢

- [SystemInflux/vlmcsd](https://github.com/SystemInflux/vlmcsd) —— KMS 服务端本体（作者 Steve Jenkins，上游仓库现已下架）
- [kkkgo/vlmcsd](https://github.com/kkkgo/vlmcsd) —— 仍在维护的 vlmcsd 分支，可自行编译参考
- [FnDepot 应用源构建规范](https://github.com/EWEDLCM/FnDepot)
- [飞牛 fnOS](https://www.fnnas.com/)

---

<div align="center">

**如果觉得好用，顺手点个 ⭐ Star 支持一下！**

Made with ❤️ by [Blue-Mink](https://github.com/Blue-Mink)

</div>
