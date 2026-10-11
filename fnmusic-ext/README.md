# 🎧 fnMusic 音源扩展（fnmusic-ext）— fnOS 应用收录

> 收录自上游 [**javycoder/fnos_music_ext**](https://github.com/javycoder/fnos_music_ext) 官方 Release 的 FPK 原包（未重打包、字节未改），索引指向上游 release 直链。
>
> 🎯 **上游项目**：[javycoder/fnos_music_ext](https://github.com/javycoder/fnos_music_ext)（314★，MIT，Gitee 镜像同步）
> 📦 **上游 Release**：[官方 releases](https://github.com/javycoder/fnos_music_ext/releases)

## 当前版本

| | |
|---|---|
| 版本 | **v2.8.0** |
| FPK | [fnmusic-ext-2.8.0.fpk](https://github.com/javycoder/fnos_music_ext/releases/download/v2.8.0/fnmusic-ext-2.8.0.fpk)（25.9 MB，上游原包） |
| SHA256 | `8a30e3e293d7622823071219a57e8cac33ec91669390c36c5ad5804895397338`（见 [fpk.sha256](fpk.sha256)） |
| 运行方式 | Docker 单容器（默认）或原生 systemd（`install.sh --deploy native`），root |
| 平台 | fnOS all（x86_64 / arm64），要求 fnOS ≥ 1.2.0 |
| 依赖 | 官方飞牛音乐应用（trim.music） |

## 功能简介

- 零侵入接管飞牛官方音乐入口：不修改官方程序 / nginx / 数据库，应用中心停止即秒级还原官方直连
- 在线音源三选一：musicdl（57 平台聚合）/ 网易云 musicbox（扫码登录，VIP 无损）/ 洛雪自定义源 lxmusic（多源同时激活、解析失败自动接力）
- 本地优先排序、搜索深分页、在线结果逐曲来源标记
- 边听边存、封面内嵌、歌词自动下载
- 内置管理页（本机 `127.0.0.1:8774`，管理员口令保护）：音源切换、扫码登录、平台选择、储存目录自定义、运行日志导出

## 安装

1. 确认官方飞牛音乐（trim.music）已安装
2. 在 FnDepot / Moo 商店直接安装，或从上游 Release 下载 FPK 后经应用中心安装

## 版本记录

| 版本 | 日期 | 说明 |
|---|---|---|
| v2.8.0 | 2026-10-10 | 原生（无 Docker）部署形态；WebUI「储存」目录自定义与「关于」页日志导出；洛雪多源同时激活 + 逐曲来源标记；lxserver 用户源沙箱兼容补丁；修复下载音质档位误判、标准音质转码跳歌、网易 VIP 无封面等 |

详见 [上游 CHANGELOG](https://github.com/javycoder/fnos_music_ext/blob/main/CHANGELOG.md)。
