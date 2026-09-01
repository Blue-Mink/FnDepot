# TREK - 旅行规划工具

Trek 是一款自托管的旅行规划工具，基于 [liketrek/TREK](https://github.com/liketrek/TREK) 开源项目构建。

## 功能特点

- **实时协作地图** - 在地图上规划行程，支持多人协作
- **路线优化** - 智能优化旅行路线
- **预算管理** - 跟踪旅行开销
- **行李清单** - 管理旅行物品
- **旅行日记** - 记录旅行体验
- **AI 辅助** - 集成 AI 旅行建议
- **SSO 登录** - 支持 OAuth/SSO
- **PWA 离线访问** - 移动设备离线使用
- **MCP 协议扩展** - 可扩展插件系统

## 版本信息

- 上游版本：`v4.1.1`
- FPK 版本：`4.1.1`
- 架构：`amd64`
- 默认端口：`32679`

## 安装说明

1. 在 fnOS 应用中心安装 `TREK`
2. 设置访问端口（默认 32679）
3. 安装完成后访问 `http://<NAS-IP>:32679/`
4. 使用默认凭据登录并修改密码

## 环境变量

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `ENCRYPTION_KEY` | 自动生成 | 加密密钥（首次启动自动生成） |
| `TZ` | `Asia/Shanghai` | 时区 |
| `LOG_LEVEL` | `info` | 日志级别 |
| `ADMIN_EMAIL` | `admin@trek.local` | 管理员邮箱 |
| `ADMIN_PASSWORD` | `changeme` | 管理员密码 |

## 数据目录

- `/var/apps/trek/var/data` - 应用数据
- `/var/apps/trek/var/uploads` - 用户上传文件

## 原作者

- **项目**: [liketrek/TREK](https://github.com/liketrek/TREK)
- **许可**: MIT
- **Docker镜像**: [mauriceboe/trek](https://hub.docker.com/r/mauriceboe/trek)

---

本 FPK 由 [Blue-Mink](https://github.com/Blue-Mink) 为 fnOS 平台打包。
