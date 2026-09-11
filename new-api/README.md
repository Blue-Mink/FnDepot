# 🧠 New API — 新一代大模型网关与 AI 资产管理系统

New API 是基于 [QuantumNous/new-api](https://github.com/QuantumNous/new-api) 官方 Linux 二进制构建的飞牛 fnOS 应用包。它聚合多模型供应商，统一 OpenAI 兼容接口，支持智能路由、权限管理、用量统计与成本核算。

## 功能特点

- **🔌 多模型聚合** — 统一 OpenAI 兼容接口，便于接入多个模型供应商
- **🚦 智能路由** — 支持模型分组、权重、优先级与备用策略
- **🔐 权限管理** — 支持多用户、多令牌、分组管理
- **📊 用量统计** — Token 用量跟踪与调用日志统计
- **💰 成本核算** — 按模型、用户、分组核算 API 调用成本
- **🧩 Web 管理** — 提供完整 Web 管理界面

## 版本

**v1.0.0-rc.37** — 飞牛 OS (fnOS) root 权限版（amd64 实机验证 / arm64 未实机验证）

- 基于 QuantumNous/new-api 官方 `v1.0.0-rc.37` Linux 二进制
- 本版本要点：定价表达式编辑器（旧定价模式将弃用）、任务插件流式输出与插件市场增强、渠道创建与编辑统一、通行密钥多域名支持
- 默认访问端口：`33000`
- 应用中心入口类型：`iframe`
- 运行权限：`root`
- amd64 已在 fnOS x86_64 测试机验证服务启动、端口监听与 `/api/status`；arm64 包由官方 arm64 二进制同流程构建，未在 arm 实机验证

## 安装方式

1. 下载 Release 附件：
   - amd64：[`new-api-1.0.0-rc.37-fnos-amd64-root.fpk`](https://github.com/Blue-Mink/FnDepot/releases/download/new-api-v1.0.0-rc.37/new-api-1.0.0-rc.37-fnos-amd64-root.fpk)
   - arm64：[`new-api-1.0.0-rc.37-fnos-arm64-root.fpk`](https://github.com/Blue-Mink/FnDepot/releases/download/new-api-v1.0.0-rc.37/new-api-1.0.0-rc.37-fnos-arm64-root.fpk)
2. 在飞牛 NAS 应用中心选择「从文件安装」
3. 默认端口为 `33000`
4. 安装完成后通过应用中心打开，或访问：`http://NAS_IP:33000/`

> ⚠️ 首次访问会自动初始化数据库，请按照页面提示完成初始配置。冷启动约需 10 余秒，稍候再刷新。

## 原作者

- **项目**: [QuantumNous/new-api](https://github.com/QuantumNous/new-api)
- **许可**: MIT

---

本 fpk 由 [Blue-Mink](https://github.com/Blue-Mink) 为 fnOS 平台打包。

## 校验

```text
ff20733bd0492cad86ef0bd43f49eb9ccf52242bacf8c1d042e25896beaee296  new-api-1.0.0-rc.37-fnos-amd64-root.fpk
9d0a6137e29026436af4d87c5e06e1ee920e5f7086a1533b4927ab9c21d926d8  new-api-1.0.0-rc.37-fnos-arm64-root.fpk
```
