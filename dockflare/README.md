# 🌩️ DockFlare — 自托管 Cloudflare Tunnel 管理平台

DockFlare 是一个基于 [ChrispyBacon-dev/DockFlare](https://github.com/ChrispyBacon-dev/DockFlare) 构建的 Cloudflare Tunnel 管理平台。给 Docker 容器打上 `dockflare.*` 标签，即可自动创建 DNS 记录、隧道 ingress 规则与 Zero Trust Access 策略，一站式管理 Cloudflare Tunnel / DNS / Access / Workers / R2 / Email Routing。

## 功能特点

- **🚇 隧道管理** — 自动创建 / 更新 / 删除 Cloudflare Tunnel，管理 tunnel config 与 ingress 规则
- **🌐 DNS 管理** — 容器打标签自动创建 DNS 记录（CNAME / A 记录）
- **🔐 Zero Trust Access** — 为隧道应用配置 Access 访问策略（Access Group / Service Token / OAuth）
- **🧩 多服务支持** — Cloudflare Tunnel、DNS、Access、Workers、R2、Email Routing 一站式管理
- **📦 免手工配置** — 只需给容器添加 `dockflare.*` labels，其余自动完成

## 版本

**v3.1.3** — 飞牛 OS (fnOS) 适配版（同步原作者最新版）

- 基于 Docker 镜像 `alplat/dockflare:stable`
- 内置 redis 缓存 + docker-socket-proxy 安全代理
- 支持安装时自定义访问端口（默认 32671）
- 支持应用设置中修改端口
- 官方图标（favicon-512）

## 安装方式

1. 下载 [dockflare-v3.1.3-fnos-all.fpk](https://github.com/Blue-Mink/FnDepot/releases/download/v3.1.3/dockflare-v3.1.3-fnos-all.fpk)
2. 在飞牛 NAS 应用中心选择「从文件安装」
3. 安装向导中设置访问端口（默认 32671）
4. 安装完成后通过 `http://NAS_IP:32671` 访问

> ⚠️ 首次打开会引导填写 Cloudflare Account ID / Zone ID / API Token（需要 Tunnel、DNS、Access 写权限），完成后即可开始给容器打标签自动建隧道。

## 使用示例

给任意 Docker 容器添加 labels 后，DockFlare 会自动处理：

```yaml
labels:
  - dockflare.enable=true
  - dockflare.hostname=your-app.example.com
  - dockflare.service=http://your-container:8080
```

## 原作者

- **项目**: [ChrispyBacon-dev/DockFlare](https://github.com/ChrispyBacon-dev/DockFlare)
- **许可**: GPL-3.0

---

本 fpk 由 [Blue-Mink](https://github.com/Blue-Mink) 为 fnOS 平台打包
