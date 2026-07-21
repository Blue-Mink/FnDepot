# 全球电台 (GlobalRadio)

## 简介

基于 Vue 3 + Vite 的在线电台应用，支持全球电台搜索、播放、收藏、历史记录、主题切换与多国语言。

## 功能

- 电台搜索（支持中文）
- 分享电台
- 播放控制
- 睡眠定时器
- 收藏与播放历史
- 亮色/暗色主题
- 全球主流语言支持

## 原作者

- **项目**: [moli-xia/global-radio](https://github.com/moli-xia/global-radio)
- **演示站点**: https://aabb.live
- **客户端下载**: [GitHub Releases](https://github.com/moli-xia/global-radio/releases)

## 关于本 FPK 包

本包为 fnOS 应用中心格式打包版本，由 Blue-Mink 分发。

### 端口配置

| 项目 | 说明 |
|------|------|
| 默认端口 | 32678 |
| 修改方式 | 已安装应用 → 设置 → 访问端口 |
| 生效方式 | 修改后自动重建 Docker 容器 |

### Docker 部署（原始项目）

```bash
docker pull superneed/global-radio:latest
docker run -d --name global-radio --restart unless-stopped -p 8080:80 superneed/global-radio:latest
```

更多信息请参阅 [原项目 README](https://github.com/moli-xia/global-radio#readme)。
