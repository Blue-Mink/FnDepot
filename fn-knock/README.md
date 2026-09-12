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

- **当前版本**: 2.4.12（同步上游 2026-09-12 发布）
- **架构支持**: amd64, arm64
- **包来源**: 上游官方 fnOS FPK 原包（SHA256 与官方 SHA256SUMS 一致）
- **更新说明**:
  - WAF 支持重复违规请求自动拉黑（系统设置→WAF→违规自动封禁）
  - 系统更新入口移至系统设置；请求日志支持容量上限与滚动保留
  - 修复启动宽限期无上限、计划内停止 FPK 触发健康检查误报
  - 已支持应用内更新（建议使用此功能获得最新版本）

## 安装与更新

在飞牛 FnDepot 客户端中搜索「敲门knock」即可安装。

本仓库 Release 提供与上游一致的官方包与源码快照：
- [`fn-knock-2.4.12-fnos-amd64.fpk`](https://github.com/Blue-Mink/FnDepot/releases/download/v2.4.12/fn-knock-2.4.12-fnos-amd64.fpk)
- [`fn-knock-2.4.12-fnos-arm64.fpk`](https://github.com/Blue-Mink/FnDepot/releases/download/v2.4.12/fn-knock-2.4.12-fnos-arm64.fpk)
- [`fn-knock-source-v2.4.12.zip`](https://github.com/Blue-Mink/FnDepot/releases/download/v2.4.12/fn-knock-source-v2.4.12.zip)（上游 v2.4.12 源码快照）

> 注意：从 v2.0.10 起，fn-knock 已内置应用内更新机制，建议在应用内开启自动更新检查。

## 更多信息

请访问 [fnknock.cn](https://www.fnknock.cn) 或查看 [官方文档](https://docs.fnknock.cn)。


## 校验

```text
5f1f2b8a05d7d5c87bd39627bdfe8badd403cdc7711a0ae7ef54dd0b54df4120  fn-knock-2.4.12-fnos-amd64.fpk
175f2291c8060e53ad445fea045253dc4a14ddf095ea04eccff5045edea93ad1  fn-knock-2.4.12-fnos-arm64.fpk
9fcb61b7696f6873fb7b57dc491117e708118cf1eca71219020ae777b8bda500  fn-knock-source-v2.4.12.zip
```
