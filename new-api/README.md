# 🧠 New API — 新一代大模型网关与AI资产管理系统

New API 是新一代大模型网关与AI资产管理系统，基于 [QuantumNous/new-api](https://github.com/QuantumNous/new-api) 构建。聚合 40+ 模型供应商，统一 OpenAI 兼容接口，支持智能路由、权限管理、用量统计与成本核算。

## 功能特点

- **🔌 多模型聚合** — 聚合 40+ 模型供应商，统一 OpenAI 兼容接口
- **🚦 智能路由** — 支持权重、优先级、备用等多策略路由分发
- **🔐 权限管理** — 多用户、多令牌、分组管理，细粒度权限控制
- **📊 用量统计** — 完整的 Token 用量跟踪与可视化统计
- **💰 成本核算** — 按模型、用户、分组核算 API 调用成本
- **⚡ 速率限制** — 支持用户级、令牌级、IP 级速率限制
- **🔁 自动重试** — 请求失败自动重试与故障转移

## 版本

**v1.0.0-rc.22** — 飞牛 OS (fnOS) 适配版

- 基于 Docker 镜像 `calciumion/new-api:latest`
- 支持安装时自定义访问端口（默认 33000）
- 支持应用设置中修改端口
- 自动清理容器与镜像（卸载时）

## 安装方式

1. 下载 [new-api-v1.0.0-rc.22.fpk](https://github.com/Blue-Mink/FnDepot/releases/download/v1.0.0-rc.22/new-api-v1.0.0-rc.22.fpk)
2. 在飞牛 NAS 应用中心选择「从文件安装」
3. 安装向导中设置访问端口（默认 33000）
4. 安装完成后通过 `http://NAS_IP:33000` 访问

> ⚠️ 首次访问会自动初始化数据库并生成管理员账号，请按照页面提示完成初始配置。

## 原作者

- **项目**: [QuantumNous/new-api](https://github.com/QuantumNous/new-api)
- **许可**: MIT

---

本 fpk 由 [Blue-Mink](https://github.com/Blue-Mink) 为 fnOS 平台打包
