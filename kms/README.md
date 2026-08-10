# KMS Activator

基于 [vlmcsd](https://github.com/Wind4/vlmcsd) 的 KMS 激活服务 fnOS FPK 应用。

## 功能

- KMS 激活服务（Windows / Office 全系列 VOL 版本）
- Web 管理界面（fnOS CGI 代理）
- 服务状态监控与启停控制
- GVLK 密钥库（Windows / Office / Server 三分类）
- 激活命令一键复制

## 安装

在飞牛应用中心 → 应用源管理 → 添加 `https://github.com/Blue-Mink/FnDepot` → 搜索 KMS Activator 安装。

## 访问

安装后桌面会出现 **KMS Activator** 图标，点击即可打开 Web 管理界面。

KMS 客户端配置：`slmgr /skms <NAS_IP>`

## 技术架构

```
用户访问 :16880 → fnOS nginx CGI 代理 → index.cgi
                                         ↓
                            vlmcsd 监听 11688（非特权端口）
                                         ↓
                            iptables 转发 1688 → 11688
```

## 注意事项

- 需要 root 权限（iptables 转发）
- vlmcsd 使用端口 11688（非特权），通过 iptables 转发 1688 端口
- 首次启动需等待约 2 秒让 vlmcsd 就绪

## 原项目

- [vlmcsd](https://github.com/Wind4/vlmcsd) - KMS 模拟服务器
- 作者：jianyun8023

## License

MIT
