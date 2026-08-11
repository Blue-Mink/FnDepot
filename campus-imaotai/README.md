# i茅台自动预约助手

基于 [oddfar/campus-imaotai](https://github.com/oddfar/campus-imaotai)（MIT）的 i茅台 App 自动预约助手。每日自动预约申购、自动 i茅台预约，自带 Web 管理界面，支持 Docker 一键部署到飞牛 fnOS。

## 功能特点

- 🎯 每日自动预约申购 **i茅台** 商品
- 📅 定时任务自动执行，无需人工值守
- 🌐 自带 Web 管理后台（账号/密码/验证码登录）
- 🐳 四容器架构：Java 后端 + MySQL + Redis + Nginx
- 🧩 安装向导可自定义端口

## 版本信息

| 项目 | 信息 |
| :--- | :--- |
| 🏷️ **版本** | v1.0.14 |
| 👨‍💻 **原作者** | [oddfar](https://github.com/oddfar) |
| 📁 **原项目** | [campus-imaotai](https://github.com/oddfar/campus-imaotai) |
| 📥 **安装方式** | 下载 `campus-imaotai-v1.0.14.fpk` 在 fnOS 应用中心手动安装 |

## 默认端口

| 服务 | 端口 |
| :--- | :--- |
| 前端 Web | 32681 |
| 后端 API | 32682 |

> ⚠️ 首次安装 MySQL 初始化（导入 16 张表）约需 10 分钟左右，应用显示 `starting` 属正常现象，等待 MySQL 转为 healthy 后启动即成功。

## v1.0.14 更新说明

- 🔧 修复验证码报错：升级至 Ubuntu 版 JDK 并启用 `java.awt.headless`，登录页验证码正常显示
- 🖼️ 更换 i茅台官方图标（App Store 提取，iOS 风格圆角）
- 🛠️ 修复 Nginx 容器健康检查（IPv4 显式地址），四容器全部 healthy