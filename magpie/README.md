# Magpie

AI 模型统一网关，基于上游 [yetone/magpie](https://github.com/yetone/magpie) 的 fnOS 打包版：把多家模型供应商收进一个 OpenAI 兼容端点，支持路由组、故障回退、按密钥限定可用模型与用量统计。单进程同时提供网关与浏览器控制台。

<div align="center">

[![FPK](https://img.shields.io/badge/FPK-v1.1.14-green?style=flat-square)](https://github.com/Blue-Mink/FnDepot/releases/tag/magpie-v1.1.14)
![Platform](https://img.shields.io/badge/Platform-x86__64%20%7C%20ARM64-lightgrey?style=flat-square)
![Port](https://img.shields.io/badge/Port-36140%20%2F%2036141-blue?style=flat-square)

</div>

## 安装

1. 应用中心 → 手动安装 → 按架构选择 `magpie-1.1.14-fnos-amd64.fpk`（x86_64）或 `magpie-1.1.14-fnos-arm64.fpk`（ARM64）（校验值见 [fpk.sha256](fpk.sha256)）
2. 安装向导里填写两个端口（默认 Web 控制台 **36140**、网关 API **36141**）
3. 安装完成后从桌面「Magpie」打开控制台，或直接访问 `http://<NAS IP>:36140/`

> 控制台需要访问密钥。桌面入口会自动携带，不要手动去猜：首次打开时入口会 302 到带 `?k=` 的地址。

## 两个入口

| 入口 | 地址 | 用途 |
| :--- | :--- | :--- |
| Magpie | `http://<NAS IP>:36140/` | 浏览器控制台：供应商、调用密钥、路由组、用量 |
| Magpie API | `http://<NAS IP>:36141/v1` | OpenAI 兼容网关，客户端 base_url 填这里 |

飞牛统一网关另有 `/app/magpie/` 入口（经 unix socket 转发，登录飞牛即可打开控制台，`/app/magpie/v1/*` 同样可调）。

## 配置流程

1. **供应商**：填 base_url 与 key，点「拉取模型」勾选要开放的模型
2. **调用密钥**：在 Gateway 页新建密钥，可限定该密钥只能使用某些模型（形如 `provider/*`）
3. **路由组**（可选）：同一个对外模型名背后挂多个供应商，按权重/优先级自动回退
4. 客户端：`base_url = http://<NAS IP>:36141/v1`，`Authorization: Bearer <你的调用密钥>`

## 支持的协议

`/v1/chat/completions`（含流式）、`/v1/responses`、`/v1/messages`（Anthropic 格式）、`/v1beta/models/<model>:generateContent`（Gemini 格式）。

> 后三种走的是「请求上游流式、边收边翻译」的路径，要求供应商真的返回 SSE。若供应商忽略 `stream: true` 只回整包 JSON，这三个端点会返回 502 `did not stream`，而 `/v1/chat/completions` 仍可正常使用。

## 已知边界

- **局域网模式默认开启**：同一网段的请求不填密钥也能调用，密钥只承担模型白名单与用量归属；网关默认监听 `0.0.0.0`，请只在可信网络内使用。
- 上游的订阅账号桥接、本机 agent/插件等桌面端功能在 NAS 上不适用，忽略即可。
- 卸载向导可选「保留数据」或「删除数据」，选删除会清空应用数据目录且不可恢复。

## 构建

源码树见 [source/](source/)，其中不含上游二进制（约 50MB）。构建前把上游 `magpie-cli-linux-amd64` 放到 `app/bin/magpie`，再执行 `build_1114.py`。控制台的移动端样式是在二进制内嵌 CSS 上做等长补丁（`patch_magpie_css2.py`），换上游版本后需重新施加。

## 许可与致谢

- 上游项目：[yetone/magpie](https://github.com/yetone/magpie)（二进制发布在 [yetone/magpie-releases](https://github.com/yetone/magpie-releases)）
- 本仓库只提供 fnOS 打包（安装脚本、统一网关边车、桌面入口、两个端口的向导与卸载向导），不包含也不修改上游的模型逻辑
- 打包与分发：[Blue-Mink](https://github.com/Blue-Mink)
