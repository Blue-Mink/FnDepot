# 敲门knock (fn-knock)

## 简介

**敲门knock** 是一款针对飞牛OS的安全防护软件，内置了防火墙控制和反代安全。面向 NAS、软路由与家庭服务器的多平台高性能安全网关。

采用 **Rust + Go 双核心架构**，集成反向代理、身份认证、WAF、SSL、DDNS 与内网穿透。

## 功能特点

- 🔒 **安全认证网关** — 零信任架构，多维身份认证
- 🛡️ **内置 WAF** — 一键防护常见 Web 攻击
- 🔄 **反向代理** — 高性能路由，即开即用
- 🌐 **DDNS 动态域名** — 支持多种 DDNS 服务商
- 🔑 **SSL 证书管理** — 自动申请与续期
- 🚇 **内网穿透** — 安全隧道访问内网服务

## 原始项目

- **作者**: [kci-lnk](https://github.com/kci-lnk)
- **源码**: https://github.com/kci-lnk/fn-knock-turborepo
- **官网**: https://www.fnknock.cn

## 版本信息

- **当前版本**: 2.4.14（同步上游 2026-09-14 发布）
- **架构支持**: amd64, arm64
- **包来源**: 上游官方 fnOS FPK 原包（SHA256 与官方 SHA256SUMS 一致）
- **更新说明**（含 2.4.13）:
  - 映射管理支持批量编辑标题、域名与目标地址，批量操作工具栏布局与溢出菜单优化
  - 网页终端新增便携资源状态栏，悬停或触摸可查看磁盘使用详情
  - 控制台新增在线用户 IP 详情分页，流量卡片显示与交互优化
  - 完善 fnOS 证书同步：支持受管证书的创建、更新与安全删除，新增冲突检测与异常恢复
  - Cloudflare Tunnel 新增专用回源入口，可正确识别真实访客 IP；cloudflared 更新至 2026.9.1
  - 修复 Android 网页终端软键盘无法输入、认证页子路径资源预加载、高级认证策略异常时的主机恢复逻辑

## 安装与更新

在飞牛 FnDepot 客户端中搜索「敲门knock」即可安装。

本仓库 Release 提供与上游一致的官方包与源码快照：
- [`fn-knock-2.4.14-fnos-amd64.fpk`](https://github.com/Blue-Mink/FnDepot/releases/download/v2.4.14/fn-knock-2.4.14-fnos-amd64.fpk)
- [`fn-knock-2.4.14-fnos-arm64.fpk`](https://github.com/Blue-Mink/FnDepot/releases/download/v2.4.14/fn-knock-2.4.14-fnos-arm64.fpk)
- [`fn-knock-source-v2.4.14.zip`](https://github.com/Blue-Mink/FnDepot/releases/download/v2.4.14/fn-knock-source-v2.4.14.zip)（上游 v2.4.14 源码快照）

> 注意：从 v2.0.10 起，fn-knock 已内置应用内更新机制，建议在应用内开启自动更新检查。

## 更多信息

请访问 [fnknock.cn](https://www.fnknock.cn) 或查看 [官方文档](https://docs.fnknock.cn)。


## 校验

```text
a70dad75f97fef58c1989fae7d30d39505c7cfa4596e7734aa47f8e8a663c72d  fn-knock-2.4.14-fnos-amd64.fpk
9d577fa28d7cf9c65ffbc48b5a4b2a2bdd0aeec335e98bb8630c45d63cae3a8a  fn-knock-2.4.14-fnos-arm64.fpk
f7e3fd307f80bf60c55037a05bce65b5d0fbeb7ebf3a91d28bc38bec249a97b8  fn-knock-source-v2.4.14.zip
```
