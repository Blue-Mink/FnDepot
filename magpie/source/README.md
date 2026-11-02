# magpie FPK 源码树（fnOS 打包部分）

本目录是 FnDepot 上的 fnOS 打包源码，**不含上游约 50MB 静态二进制**（二进制只随 release 的 FPK 分发）。
上游项目：[yetone/magpie](https://github.com/yetone/magpie)，二进制发布在 [yetone/magpie-releases](https://github.com/yetone/magpie-releases)（资产名 `magpie-cli-linux-amd64` / `magpie-cli-linux-arm64`）。

## 目录

```
app/
  ui/                      桌面入口配置与图标、入口 CGI（/gw/ 说明页列出全部网关密钥；控制台入口 302 到统一网关）
  magpie-gateway-proxy.py  统一网关边车（unix socket → 36140 控制台 / 36141 网关）；控制台移动端 CSS 适配在响应层注入
cmd/                       fnOS 生命周期脚本（install/upgrade/uninstall + config）
config/                    privilege / resource
wizard/                    安装向导（双端口）、配置向导、卸载向导（保留或删除数据）
manifest                   fnOS 应用清单（checksum 由 fnpack 打包时写回）
build_1119.py               打包脚本（staging + fnpack + 校验）
build_1119_arm64.py         arm64 打包脚本（换二进制 + platform=arm）
```

## 构建步骤

1. 取上游**最新**的纯官方二进制并放到 `app/bin/magpie`（可执行）：

   ```bash
   curl -L -o magpie-cli-linux-amd64 \
     https://github.com/yetone/magpie-releases/releases/download/<最新 tag>/magpie-cli-linux-amd64
   mkdir -p app/bin && cp magpie-cli-linux-amd64 app/bin/magpie && chmod 755 app/bin/magpie
   ```

   从 1.1.17 起，二进制保持**纯官方原版**（不做任何补丁）；控制台的移动端适配（safe-area 封顶、Tab 条留白、设置页行距等）全部下沉到统一网关边车（`magpie-gateway-proxy.py` 响应层注入）。上游发新版本后直接换二进制即可，无需重打 FPK。

2. 打包（需要 `fnpack`，可用 `FNPACK_BIN` 指定路径）：

   ```bash
   FNPACK_BIN=/path/to/fnpack python3 build_1119.py
   ```

   产物输出到 `dist/magpie-1.1.19-fnos-amd64.fpk`，脚本会自检：
   `manifest.checksum == md5(app.tgz)`、包内没有 `__pycache__`、入口图标槽位齐全。

3. **ARM64**：与 x86 同源树，只换两处 —— 二进制换成 `magpie-cli-linux-arm64`，`manifest` 里 `platform = x86` 改为 `platform = arm`。用 `build_1119_arm64.py`（脚本内已含换二进制与 platform 改写），产物名为 `dist/magpie-1.1.19-fnos-arm64.fpk`。

## 注意

- `manifest` 里的 `checksum` 在源码树中留空，`fnpack` 打包时会写成 `app.tgz` 的真实 MD5。
- 入口说明页（`app/ui/index.cgi` 的 `/gw/`）从 1.1.19 起改读真源 `caller-keys.json`，列出全部网关密钥（与 Web 控制台「Gateway」页同步），不再读首启静态抄本 `gateway-keys/NAS.txt`。
- 统一网关边车对响应体做了 chunked 处理：需要改写的 HTML/JS/CSS 先解块再重编码，SSE 等流式响应原样边到边转发。
