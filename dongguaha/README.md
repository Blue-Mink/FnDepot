# 🏠 fnos-vm-dongguaha

![GitHub release](https://img.shields.io/github/v/release/Blue-Mink/fnos-vm-dongguaha?style=flat-square)
![Platform](https://img.shields.io/badge/platform-fnOS%20x86_64-blue?style=flat-square)
![License](https://img.shields.io/badge/license-MIT-green?style=flat-square)
![HA](https://img.shields.io/badge/Home%20Assistant-18.0-red?style=flat-square)

> 在 x86 fnOS 系统中创建冬瓜HAOS虚拟机。

---

## 📦 安装

1. 下载 [com.dongguaha.vm-18.0-fnos-amd64.fpk](https://github.com/Blue-Mink/fnos-vm-dongguaha/releases/download/v18.0/com.dongguaha.vm-18.0-fnos-amd64.fpk)
2. 在飞牛 NAS 应用中心选择「从文件安装」
3. 安装完成后，在「虚拟机」应用中启动 / 停止虚拟机
4. VNC 中查看虚拟机 IP，通过 `http://虚拟机IP:8123` 访问 Home Assistant Web UI
5. 通过 `http://虚拟机IP:8124` 访问冬瓜HAOS伴侣

---

## 📱 手机端 Home Assistant 客户端

| 平台 | 下载 | 说明 |
|------|------|------|
| Android (APK) | [下载 APK](https://github.com/Blue-Mink/fnos-vm-dongguaha/releases/download/v18.0/Home-Assistant.apk) | 本地安装包 |
| Android (Google Play) | [Google Play](https://play.google.com/store/apps/details?id=io.homeassistant.companion.android) | 官方商店 |
| iOS | [App Store](https://apps.apple.com/cn/app/home-assistant/id1099568401) | 官方商店 |

---

## ⚙️ 系统要求

| 项目 | 要求 | 备注 |
|------|------|------|
| 系统 | fnOS x86_64 | 64 位系统 |
| 依赖应用 | `trim.vm` | 必备虚拟机组件 |
| CPU | 2 核 | 推荐 2 核以上 |
| 内存 | 2 GB | 推荐 4 GB 更流畅 |
| 磁盘 | 32 GB | 建议 SSD |

---

## 🛠️ 技术栈

| 组件 | 版本/说明 |
|------|----------|
| 虚拟化 | KVM 硬件加速 |
| 操作系统 | Home Assistant OS 18.0 |
| 打包规范 | 飞牛 fnOS FPK 应用规范 |

---

## ⚠️ 注意事项

- 安装后需手动在「虚拟机」应用中启动虚拟机
- 首次启动 Home Assistant 需要完成初始化设置
- 请勿直接修改虚拟机配置文件，应通过应用中心管理
- 如遇网络问题，请检查 NAS 与虚拟机的网络配置

---

## 🔨 构建

### 环境准备

| 工具 | 版本 | 说明 |
|------|------|------|
| fnpack | 最新版 | fnOS FPK 打包工具 |
| Python | 3.8+ | 用于打包脚本 |
| tar / gzip | 系统自带 | 用于打包 app.tgz |

> **提示**：推荐在飞牛 NAS 或 Docker 容器中构建，以确保打包规范兼容。

### 构建步骤

1. **克隆仓库**

```bash
git clone https://github.com/Blue-Mink/fnos-vm-dongguaha.git
cd fnos-vm-dongguaha
```

2. **安装 fnpack**

```bash
# 在飞牛 NAS 上安装
apt update && apt install -y fnpack

# 或使用 Docker
docker run --rm -v $(pwd):/work -w /work alpine:latest sh -c "apk add --no-cache fnpack && fnpack build -d . -o dist/com.dongguaha.vm-18.0-fnos-amd64.fpk"
```

3. **修改 manifest 版本号（可选）**

如需要发布新版本，编辑 `manifest` 文件中的 `version` 字段：

```ini
version = 18.0
```

4. **执行打包**

```bash
# 确保目录结构完整
ls -la cmd/ config/ wizard/ i18n/ ICON.PNG ICON_256.PNG manifest app.tgz

# 执行打包
fnpack build -d . -o dist/com.dongguaha.vm-18.0-fnos-amd64.fpk
```

5. **验证构建产物**

```bash
# 检查文件大小（正常约 32KB）
ls -lh dist/com.dongguaha.vm-18.0-fnos-amd64.fpk

# 查看包内结构
tar -tzf dist/com.dongguaha.vm-18.0-fnos-amd64.fpk | head -20
```

### 常见问题

| 问题 | 原因 | 解决 |
|------|------|------|
| `fnpack: command not found` | fnpack 未安装 | 在飞牛 NAS 上执行 `apt install fnpack` |
| `Permission denied` | 目录权限不足 | 使用 `sudo` 或在有写权限目录下构建 |
| `checksum mismatch` | 文件被篡改 | 重新执行打包，确保 `app.tgz` 未被修改 |
| `manifest 缺失字段` | manifest 格式错误 | 对照飞牛 FPK 规范检查 manifest 文件 |

### 构建产物说明

打包完成后，`dist/` 目录下会生成：

```
dist/
└── com.dongguaha.vm-18.0-fnos-amd64.fpk  # 可直接安装的 FPK 包
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

- [Home Assistant](https://www.home-assistant.io/) — 开源智能家居平台
- [冬瓜HAOS的由来](https://bbs.hassbian.com/thread-24065-1-1.html)
- [冬瓜HAOS 镜像包](https://bbs.hassbian.com/thread-23791-1-1.html) — 镜像包下载
- [RROrg/fn-apps](https://github.com/RROrg/fn-apps/tree/main/fn-vfnOS) — 项目参考
