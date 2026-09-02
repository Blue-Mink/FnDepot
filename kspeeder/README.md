# KSpeeder for fnOS

KSpeeder 是一款 Docker 镜像加速管理工具，支持多源镜像缓存、加速规则管理、流量统计与自动清理，帮助提升容器镜像拉取速度。

本目录提供适配飞牛 fnOS 应用中心的 KSpeeder FPK 打包版本，基于 [kspeeder/docker_kspeeder](https://github.com/kspeeder/docker_kspeeder) 与 iStoreEnhance v0.7.17 构建。

## 功能特点

- Docker 镜像加速管理 Web UI
- Docker Registry Mirror 代理服务
- 管理端口与镜像代理端口均支持安装/应用设置中配置
- 应用中心“打开”入口会随管理端口自动同步
- 支持应用中心启动、停用、端口修改与重启

## 版本信息

| 项目 | 信息 |
| :--- | :--- |
| 应用名称 | KSpeeder |
| fnOS 包名 | `kspeeder` |
| 当前版本 | `0.7.17` |
| 平台 | fnOS x86_64 |
| 默认管理端口 | `5003` |
| 默认镜像代理端口 | `5443` |
| 原项目 | [kspeeder/docker_kspeeder](https://github.com/kspeeder/docker_kspeeder) |
| 发布者 | [Blue-Mink](https://github.com/Blue-Mink) |

## 安装方式

1. 下载 Release 中的 `kspeeder-0.7.17-fnos-amd64.fpk`
2. 在 fnOS 应用中心选择“手动安装”
3. 按向导设置端口（默认管理端口 `5003`、代理端口 `5443`）
4. 安装完成后点击“打开”进入 KSpeeder Web UI

也可以在飞牛 FnDepot 中添加本源后搜索「KSpeeder」安装。

## Docker 代理配置

安装后，将 Docker daemon 的 registry mirror 指向 KSpeeder 代理端口，例如默认端口：

```json
{
  "registry-mirrors": ["https://<fnOS-IP>:5443"]
}
```

如果在应用设置中修改了镜像代理端口，请同步修改 Docker daemon 的 registry mirror 地址。

## v0.7.17 更新内容

- 同步 iStoreEnhance / KSpeeder v0.7.17
- 修复飞牛应用设置页修改管理端口后，“打开”入口仍指向旧端口的问题
- 修复端口修改时旧进程按新端口查找导致无法停止的问题
- 修复真实 AppCenter 环境下 `config_callback` 路径解析不一致导致服务未重启的问题
- 应用设置页会回显当前实际管理端口与代理端口
- 管理端口、代理端口、`ui/config`、AppCenter 入口与实际监听端口保持同步
- 保留原 KSpeeder 图标，并补齐 fnOS 图标兼容文件

## 注意事项

- `5443`/自定义代理端口是 HTTPS Docker Registry Mirror 端口，HTTP 访问该端口并不是 Web 管理页面。
- Web 管理页面默认使用 HTTP 管理端口 `5003`。
- 修改端口后，应用会自动重启并同步应用中心“打开”入口。
