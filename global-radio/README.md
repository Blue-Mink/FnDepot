# 全球电台（GlobalRadio）— fnOS 应用包

![demo-w](https://raw.githubusercontent.com/moli-xia/global-radio/main/demo-w.png)
![demo-b](https://raw.githubusercontent.com/moli-xia/global-radio/main/demo-b.png)

> 本项目是 [**moli-xia/global-radio**](https://github.com/moli-xia/global-radio) 的 **fnOS 应用中心打包版本（fpk）**，让你可以在飞牛 NAS（fnOS）的应用中心一键安装全球电台，支持自定义访问端口。
>
> 🎯 **原作者项目**：[moli-xia/global-radio](https://github.com/moli-xia/global-radio)  
> 📦 **原作者 Release**：[各平台客户端下载](https://github.com/moli-xia/global-radio/releases)

---

## 功能概览

- 📻 **全球电台搜索**（支持中文关键词）
- 🔗 **分享电台** 给好友
- ▶️ **播放控制**（播放/暂停/下一首）
- ⏱️ **睡眠定时器**
- ❤️ **收藏与播放历史**
- 🌓 **亮色/暗色主题切换**
- 🌍 **全球主流语言支持**
- 📱 **安卓 / iPhone / PC 客户端**（[前往原作者 Release 下载](https://github.com/moli-xia/global-radio/releases/tag/clients)）

---

## 演示站点

- **在线体验**：[https://aabb.live](https://aabb.live)（原作者托管）
- **科技 lion 一键脚本**：已加入 [kejilion.sh](https://kejilion.sh)，支持一键安装并配置域名和 SSL 证书

---

## 项目结构（fpk 包）

```text
global-radio-fpk/
├── README.md                # 本说明文档
├── manifest                 # fnOS 应用清单
├── ICON.PNG                 # 应用图标（小）
├── ICON_256.PNG             # 应用图标（大）
│
├── app/                     # 应用资源（打包为 app.tgz）
│   ├── docker/
│   │   └── docker-compose.yaml   # Docker Compose 编排（端口由向导变量控制）
│   └── ui/
│       ├── config               # fnOS 桌面快捷方式配置
│       └── images/
│           └── 256.png           # 桌面图标
│
├── cmd/                     # 生命周期脚本
│   ├── install_callback     # 安装后：校验端口、写入 .env、同步桌面入口
│   ├── config_callback      # 配置变更：更新端口、同步桌面入口、重启容器
│   ├── install_init         # 安装前初始化
│   ├── config_init          # 配置变更前校验
│   ├── uninstall_callback   # 卸载后清理
│   ├── uninstall_init       # 卸载前准备
│   ├── upgrade_callback     # 升级后处理
│   ├── upgrade_init         # 升级前准备
│   └── main                 # 主入口
│
├── config/                  # 配置
│   ├── privilege            # 权限声明
│   └── resource             # 资源声明
│
├── wizard/                  # 向导表单
│   ├── install              # 安装向导（端口输入）
│   └── config               # 设置向导（端口修改）
│
└── global-radio.fpk         # 构建产物（可直接上传安装）
```

---

## 在 fnOS 上安装

### 方式一：通过应用中心手动安装（推荐）

1. 下载 `global-radio.fpk` 到本地
2. 打开 **fnOS → 应用中心 → 手动安装**
3. 选择 `global-radio.fpk` 文件上传
4. 在安装向导中设置**访问端口**（默认 `32678`）
5. 点击确认，等待安装完成
6. 通过 `http://你的NASIP:32678` 访问

### 方式二：通过应用中心自动安装（待上架）

> 等待上架至 fnOS 官方应用商店后，可直接在应用中心搜索「全球电台」一键安装。

---

## 端口配置说明

| 配置项 | 说明 |
|--------|------|
| 默认端口 | `32678`（映射到容器内 `80` 端口） |
| 校验范围 | 1 ~ 65535 |
| 修改方式 | 已安装应用 → 设置 → 访问端口 |
| 生效方式 | 修改后自动重建 Docker 容器，端口映射同步更新 |

**端口同步机制**：
- 用户在安装/设置中填写的端口 → 通过 `wizard_access_port` 变量传递
- 端口写入 `.env` 文件 → `docker compose` 自动读取 → 容器端口映射更新
- 端口同时写入 `app/ui/config` → fnOS 桌面快捷方式指向正确的宿主机端口
- 三者（`manifest.service_port`、`docker-compose` 端口映射、`app/ui/config` 入口 port）**始终一致**

---

## Docker 部署（非 fnOS 环境）

如果你没有 fnOS，也可以直接用 Docker 部署原始项目：

```bash
# 一键部署（Docker Hub）
docker pull superneed/global-radio:latest
docker run -d --name global-radio --restart unless-stopped -p 8080:80 superneed/global-radio:latest

# ARM64 设备
docker pull superneed/global-radio-arm64:latest
docker run -d --name global-radio --restart unless-stopped -p 8080:80 superneed/global-radio-arm64:latest
```

浏览器访问 `http://localhost:8080/`

> 更多部署方式（本地构建、Nginx 静态托管、Node.js 开发）请参见 [原始项目文档](https://github.com/moli-xia/global-radio#readme)。

---

## 自行构建 fpk 包

如果你修改了源码，需要重新打包：

```bash
# 1. 将 app/ 目录打包为 app.tgz
tar -czf app.tgz app/

# 2. 打包为 fpk（tar.gz 格式）
tar -czf global-radio.fpk \
  manifest \
  app.tgz \
  wizard/ \
  cmd/ \
  ICON.PNG \
  ICON_256.PNG

# 3. 在 fnOS 应用中心手动安装生成的 global-radio.fpk
```

> ⚠️ `app/` 目录必须打包为 `app.tgz` 放入 fpk，不能直接放原始目录，否则 fnOS 解压会失败。

---

## 版本历史

| 版本 | 日期 | 说明 |
|------|------|------|
| **v1.2.2** | 2026-07-22 | 修复 app/ui/config 端口写死问题；修复 fpk 打包格式（app/ → app.tgz） |
| **v1.2.0** | 2026-07-22 | 增加可配置访问端口（默认 32678）；重写安装/配置回调脚本；Docker 端口映射使用变量 |
| **v1.1.0** | - | 原始 fpk 版本，端口写死为 80 |

---

## 致谢

- 原始项目：[moli-xia/global-radio](https://github.com/moli-xia/global-radio) — 基于 Vue 3 + Vite 的在线电台应用
- 原作者 Release：[各平台客户端](https://github.com/moli-xia/global-radio/releases)
- 演示站点：[https://aabb.live](https://aabb.live)

---

## 许可

本项目遵循原始项目的开源许可协议。详情请参阅 [原始项目 License](https://github.com/moli-xia/global-radio)。
