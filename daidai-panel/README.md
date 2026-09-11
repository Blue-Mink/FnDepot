# 🎨 呆呆面板 (daidai-panel)

![GitHub release](https://img.shields.io/github/v/release/Blue-Mink/FnDepot?style=flat-square&filter=v3.2.6)
![Platform](https://img.shields.io/badge/platform-fnOS%20amd64-blue?style=flat-square)
![License](https://img.shields.io/badge/license-MIT-green?style=flat-square)
![Version](https://img.shields.io/badge/daidai--panel-v3.2.6-red?style=flat-square)

> 基于青龙面板二次开发的轻量级定时脚本管理面板。支持 Python、Node.js、Shell、TypeScript、Go 等多语言脚本，内置 18 种消息推送渠道、订阅管理、环境变量、依赖管理、Open API 等功能。一些脚本 https://www.xiaoxin03.top/

---

## 📦 安装

1. 下载 [daidai-panel-v3.2.6.fpk](https://github.com/Blue-Mink/FnDepot/releases/download/v3.2.6/daidai-panel-v3.2.6.fpk)
   （校验文件：[daidai-panel-v3.2.6.fpk.sha256](https://github.com/Blue-Mink/FnDepot/releases/download/v3.2.6/daidai-panel-v3.2.6.fpk.sha256)）
2. 在飞牛 NAS 应用中心选择「从文件安装」
3. 安装向导中设置访问端口（默认 35700）
4. 安装完成后通过 `http://NAS_IP:端口` 访问

---

## 🔄 升级到 v3.2.6

- 同步上游 [linzixuanzz/daidai-panel](https://github.com/linzixuanzz/daidai-panel) v3.2.6（2026-09-08）：
  定时规则支持「工作日 9-22 点每 10 分钟」类表达式、订阅的两个自动开关可单独设置、
  依赖管理新增系统命令行、Monaco 代码正文配色修正等
- 随包提供上游 `ddp` 命令行工具
- **端口设置闭环修复**：应用设置页修改端口后，服务监听、面板配置、桌面入口与「打开」按钮
  全部同步为新端口；设置页回显当前真实端口
- 图标改用飞牛官方圆角（squircle）曲线，与系统应用风格一致
- 平台标注修正为 x86_64（此前误标 all，ARM 设备请勿安装）

> 从 v2.x 直接「从文件安装」覆盖升级即可，数据保留；如遇异常可先卸载再安装。

---

## ✨ 功能特性

| 功能 | 说明 |
|------|------|
| 多语言脚本 | 支持 Python、Node.js、Shell、TypeScript、Go |
| 消息推送 | 内置 18 种推送渠道（企业微信、钉钉、Telegram 等） |
| 订阅管理 | 订阅远程脚本仓库，自动同步更新 |
| 环境变量 | 集中管理任务运行所需的环境变量 |
| 依赖管理 | 自动安装 Python/Node.js 依赖包，支持系统命令行 |
| Open API | 提供 RESTful API 接口，支持外部集成 |
| 定时调度 | Cron 表达式精确控制任务执行时间 |
| 在线编辑器 | Monaco Editor 代码编辑，支持语法高亮 |

---

## ⚙️ 系统要求

| 项目 | 要求 | 备注 |
|------|------|------|
| 系统 | fnOS amd64 | 64 位 x86 系统 |
| 端口 | 35700（默认） | 安装时可自定义，应用设置可随时修改 |
| 依赖 | 无 | 独立运行，无需额外依赖 |

---

## 🛠️ 技术栈

| 组件 | 版本/说明 |
|------|----------|
| 后端框架 | Go + Gin |
| 前端框架 | Vue 3 + Vite |
| 代码编辑器 | Monaco Editor |
| 数据库 | SQLite / GORM |
| 打包规范 | 飞牛 fnOS FPK 应用规范 |

---

## ⚠️ 注意事项

- 首次安装建议使用默认端口 35700，后续可在应用设置中修改
- 修改端口后服务会自动重启，短暂中断属正常现象
- 面板数据保存在应用数据目录，卸载前请注意备份脚本
