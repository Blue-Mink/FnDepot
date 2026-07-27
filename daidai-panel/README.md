# 🎨 呆呆面板 (daidai-panel)

![GitHub release](https://img.shields.io/github/v/release/Blue-Mink/FnDepot?style=flat-square&filter=v2.3.9)
![Platform](https://img.shields.io/badge/platform-fnOS%20amd64-blue?style=flat-square)
![License](https://img.shields.io/badge/license-MIT-green?style=flat-square)
![Version](https://img.shields.io/badge/daidai--panel-v2.3.9-red?style=flat-square)

> 基于青龙面板二次开发的轻量级定时脚本管理面板。支持 Python、Node.js、Shell、TypeScript、Go 等多语言脚本，内置 18 种消息推送渠道、订阅管理、环境变量、依赖管理、Open API 等功能。一些脚本 https://www.xiaoxin03.top/

---

## 📦 安装

1. 下载 [daidai-panel-v2.3.9.fpk](https://github.com/Blue-Mink/FnDepot/releases/download/v2.3.9/daidai-panel-v2.3.9.fpk)
2. 在飞牛 NAS 应用中心选择「从文件安装」
3. 安装向导中设置访问端口（默认 35700）
4. 安装完成后通过 `http://NAS_IP:端口` 访问

---

## ✨ 功能特性

| 功能 | 说明 |
|------|------|
| 多语言脚本 | 支持 Python、Node.js、Shell、TypeScript、Go |
| 消息推送 | 内置 18 种推送渠道（企业微信、钉钉、Telegram 等） |
| 订阅管理 | 订阅远程脚本仓库，自动同步更新 |
| 环境变量 | 集中管理任务运行所需的环境变量 |
| 依赖管理 | 自动安装 Python/Node.js 依赖包 |
| Open API | 提供 RESTful API 接口，支持外部集成 |
| 定时调度 | Cron 表达式精确控制任务执行时间 |
| 在线编辑器 | Monaco Editor 代码编辑，支持语法高亮 |

---

## ⚙️ 系统要求

| 项目 | 要求 | 备注 |
|------|------|------|
| 系统 | fnOS amd64 | 64 位系统 |
| 端口 | 35700（默认） | 安装时可自定义 |
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
- 修改端口后服务自动重启，桌面快捷方式端口同步更新
- 定时任务支持随机延迟功能（仅对 Cron 触发有效，不影响手动执行）
- 更多脚本资源请访问 https://www.xiaoxin03.top/

---

## 🔨 构建

### 环境准备

| 工具 | 版本 | 说明 |
|------|------|------|
| fnpack | 最新版 | fnOS FPK 打包工具 |
| tar / gzip | 系统自带 | 用于打包 app.tgz |

### 构建步骤

1. **克隆仓库**

```bash
git clone https://github.com/Blue-Mink/FnDepot.git
cd FnDepot/daidai-panel
```

2. **下载呆呆面板二进制**

从原项目 [Release](https://github.com/linzixuanzz/daidai-panel/releases) 下载对应架构的二进制文件，放入 `bin/` 目录：

```bash
# 下载 Linux amd64 版本
wget https://github.com/linzixuanzz/daidai-panel/releases/download/v2.3.9/daidai-linux-amd64
chmod +x bin/daidai-linux-amd64
```

3. **修改 manifest 版本号（可选）**

如需要发布新版本，编辑 `manifest` 文件中的 `version` 字段：

```ini
version = 2.3.9
```

4. **打包 app.tgz**

```bash
# 将 bin/ 和 ui/ 等应用文件打包
cd app && tar -czf ../app.tgz * && cd ..
```

5. **执行 FPK 打包**

```bash
fnpack build -d . -o dist/daidai-panel-v2.3.9.fpk
```

或直接使用 tar 手动打包：

```bash
tar -czf daidai-panel-v2.3.9.fpk manifest app.tgz cmd/ config/ wizard/ ICON.PNG ICON_256.PNG
```

6. **验证构建产物**

```bash
ls -lh dist/daidai-panel-v2.3.9.fpk
tar -tzf dist/daidai-panel-v2.3.9.fpk | head -20
```

### 常见问题

| 问题 | 原因 | 解决 |
|------|------|------|
| `fnpack: command not found` | fnpack 未安装 | 在飞牛 NAS 上执行 `apt install fnpack` |
| `Permission denied` | cmd 脚本未加执行权限 | 执行 `chmod +x cmd/*` |
| `wizard 验证失败` | wizard JSON 格式错误 | 使用 `"type":"text"` 而非 `"type":"number"` |
| `error code 10237` | install_init 等脚本无可执行权限 | 重新打包，确保 cmd/\* 为 755 |

### 构建产物说明

打包完成后，`dist/` 目录下会生成：

```
dist/
└── daidai-panel-v2.3.9.fpk  # 可直接安装的 FPK 包
```

可直接在飞牛 NAS 应用中心「从文件安装」测试。

---

## 📄 许可证

MIT

## 👤 作者

[Blue-Mink](https://github.com/Blue-Mink)  
https://github.com/Blue-Mink/FnDepot

---

## 🙏 鸣谢

- [linzixuanzz/daidai-panel](https://github.com/linzixuanzz/daidai-panel) — 呆呆面板原项目
- [一些脚本](https://www.xiaoxin03.top/) — 脚本资源站
- [青龙面板](https://github.com/whyour/qinglong) — 面板框架参考
