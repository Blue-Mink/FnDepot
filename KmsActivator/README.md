# KMS Activator

基于 vlmcsd 的 KMS 激活服务，为局域网内 Windows / Office / Windows Server 提供批量 KMS 激活服务。

## 功能特点

- 🚀 **轻量高效** — 基于 vlmcsd，仅 80KB+，资源占用极低
- 🌐 **Web 管理界面** — 一键启动/停止 vlmcsd 服务，无需 SSH
- 🔌 **自动 IP 检测** — 页面自动显示本机局域网 IP，激活命令无需手动修改
- 🪟 **支持全平台** — Windows 10/11、Office 2013-2016、Windows Server 2016-2025
- 🔑 **内置 GVLK 密钥库** — 常用产品密钥即开即用

## 安装方式

1. 下载 `KmsActivator.fpk`
2. 在 fnOS 应用中心 → 手动安装 → 选择 fpk 文件
3. 安装完成后打开应用，点击「启动」
4. 局域网内任意设备访问 Web 界面，按教程执行激活命令即可

## 使用说明

| 系统 | 管理工具 | 命令 |
|------|---------|------|
| Windows 10/11 | `slmgr` | `slmgr /ipk <GVLK>` → `slmgr /skms <NAS_IP>` → `slmgr /ato` |
| Windows Server | `slmgr` | `slmgr /ipk <GVLK>` → `slmgr /skms <NAS_IP>` → `slmgr /ato` |
| Office 2013/2016 | `ospp.vbs` | `cscript ospp.vbs /sethst:<NAS_IP>` → `cscript ospp.vbs /act` |

## 技术细节

- 工作端口：`11688`（vlmcsd 监听），自动 iptables 转发 `1688` → `11688`
- 运行模式：fnOS 第三方应用，CGI + fnOS nginx 代理
- 日志位置：`/var/apps/KmsActivator/var/info.log`

## 免责声明

页面仅供技术学习参考，请支持正版软件。
