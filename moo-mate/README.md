# Moo Mate

Moo 的伴侣应用：把第三方 GitHub 仓库里的 FPK 应用变成 Moo 可安装的应用源，免去手工上传 GitHub 的繁琐流程。

## 简介

Moo Mate 是一个轻量的本地伴生服务。粘贴一个第三方 GitHub 仓库（或 Release / 目录）链接，它会自动探测其中打包的飞牛（fnOS）应用，生成符合 Moo 源协议的 `moo.json` 并在本机托管，一键把该源加入同机的 Moo——不必手工编辑 JSON，也不必自己维护一个 GitHub 仓库。

与 Moo 相同的访问模型：点应用图标走平台统一网关（`/app/moo-mate`），
TCP `127.0.0.1:38108` 仅在回环监听，用于本机调试与本机 Moo 拉取源索引，不对局域网暴露端口。

## 功能特点

- **仓库发现**：粘贴 GitHub 仓库 / Release / 目录链接，自动识别其中打包的 FPK 应用与版本信息
- **索引托管**：生成符合 Moo 源协议的 `moo.json`，由本机 HTTP 直接对外提供
- **一键加源**：写入同机 Moo 的源列表，装好即可在 Moo 中浏览与安装
- **批量同步 / 批量推送 / 批量删除**：对已纳管应用做批量操作
- **数据目录可切换**：索引与配置可迁移到新路径，热切换无需重启
- **备份与恢复**：支持在备份列表中选择历史备份恢复，也可生成最新备份并在弹窗中下载
- **移动端适配**：手机上操作按钮均分排布，触控友好
- **复用 Moo 的 GitHub Token**：无需在 Moo Mate 里再填一份 Token

## 版本信息

| 项目 | 内容 |
| --- | --- |
| 版本 | 0.2.31 |
| 架构 | x86_64 / arm64 |
| 包来源 | GitHub Release `mate-v0.2.31` |
| 更新说明 | 移动端操作按钮排布优化；数据目录与备份目录支持切换；新增恢复备份与下载备份弹窗；GitHub Token 复用本机 Moo 的配置 |

## 下载

- x86_64：[moo-mate_0.2.31_x86.fpk](https://github.com/Blue-Mink/moo/releases/download/mate-v0.2.31/moo-mate_0.2.31_x86.fpk) · [sha256](./moo-mate_0.2.31_x86.fpk.sha256)
- arm64：[moo-mate_0.2.31_arm.fpk](https://github.com/Blue-Mink/moo/releases/download/mate-v0.2.31/moo-mate_0.2.31_arm.fpk) · [sha256](./moo-mate_0.2.31_arm.fpk.sha256)

## 安装

### 通过 FnDepot 源安装

1. 打开 Moo → 「应用源」→ 添加 `FnDepot` 源（`https://github.com/Blue-Mink/FnDepot`）
2. 在应用列表中找到 **Moo Mate**，点击安装
3. 平台会按你的 CPU 架构自动选择 x86 或 arm 包

### 通过 Moo 源安装

若已添加本仓库或其它含 Moo Mate 的源，直接在 Moo 中搜索 **Moo Mate** 安装即可。

### 手动安装

下载上面对应架构的 `.fpk`，在飞牛 OS「应用中心 → 手动安装」中选择该文件。
安装前可用 `.sha256` 校验完整性：

```bash
sha256sum -c moo-mate_0.2.31_x86.fpk.sha256
```

## 使用步骤

1. 安装完成后，在飞牛 OS 桌面或应用中心打开 **Moo Mate**
2. 在「应用设置」里确认要扫描的 FPK 目录（也可切换到自定义数据目录）
3. 页面会自动发现本机可纳管的 FPK 包，并生成源索引
4. 复制页面给出的源链接，在 Moo → 「应用源」中粘贴添加
5. 之后 Moo 每次同步即可看到本机应用；新放入的 FPK 可在 Moo Mate 中点「同步更新」加入

## 说明

- **无自更新**：Moo Mate 不内置自动更新，新版本需下载 FPK 重新安装
- **Token 复用**：GitHub Token 直接读取本机 Moo 的配置，Moo Mate 侧无需单独填写
- **端口**：默认 `38108`，仅监听 `127.0.0.1`，通过平台网关 `/app/moo-mate` 访问
- **数据**：索引与配置存放在应用数据目录，升级安装会保留；卸载并删除数据将一并清除
