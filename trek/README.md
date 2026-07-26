# TREK — 自托管旅行规划工具

TREK 是一款功能全面的自托管旅行规划应用，基于 [liketrek/TREK](https://github.com/liketrek/TREK) 构建。以实时协作地图为核心，集行程规划、路线优化、预算管理、行李清单、旅行日记于一体。

## 功能特点

- **🗺️ 交互地图** — 集成 Leaflet/Mapbox GL 地图，支持 3D 建筑、路线可视化、地点聚类
- **📋 拖拽规划器** — 按天规划行程，支持跨日移动、路线优化
- **💰 费用管理** — Splitwise 风格的费用分摊，多币种支持
- **🎒 行李清单** — 支持模板、人员分配、重量追踪
- **📝 旅行日记** — 杂志风格的旅程记录，支持照片、地图、心情
- **👥 实时协作** — WebSocket 实时同步，多人协作编辑
- **🔐 SSO 登录** — 支持 Google/Apple/Authentik/Keycloak 等 OIDC 提供商
- **📱 PWA 支持** — 可安装到桌面/手机，离线访问
- **🤖 AI / MCP** — 内置 MCP 服务器，AI 助手可自动创建行程
- **🌐 20 种语言** — 支持中文、英文、日文等多语言界面

## 版本

**v3.4.1** — 飞牛 OS (fnOS) 适配版

- 基于 Docker 镜像 `mauriceboe/trek:latest`
- 支持安装时自定义访问端口
- 支持应用设置中修改端口
- 🔧 已修复 HTTP 下 Cookie Secure 导致登录失败（`COOKIE_SECURE=false`）
- 🔑 预制默认管理员账号：`admin@trek.local` / `Admin12345`

## 安装方式

在 飞牛FnDepot 直接添加本源，客户端中搜索「TREK」即可安装

> ⚠️ 首次登录请使用默认账号 `admin@trek.local` / `Admin12345`，登录后请立即修改密码。

## 原作者

- **项目**: [liketrek/TREK](https://github.com/liketrek/TREK)
- **许可**: AGPL v3

---

本 fpk 由 [Blue-Mink](https://github.com/Blue-Mink) 为 fnOS 平台打包