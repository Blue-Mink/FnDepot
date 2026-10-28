# magpie FPK 源码树（fnOS 打包部分）

本目录是 FnDepot 上的 fnOS 打包源码，**不含上游约 50MB 静态二进制**（二进制只随 release 的 FPK 分发）。
上游项目：[yetone/magpie](https://github.com/yetone/magpie)，二进制发布在 [yetone/magpie-releases](https://github.com/yetone/magpie-releases)（资产名 `magpie-cli-linux-amd64`）。

## 目录

```
app/
  ui/                      桌面入口配置与图标、入口 CGI（302 到带 ?k= 的控制台）
  magpie-gateway-proxy.py  统一网关边车（unix socket -> 36140 控制台 / 36141 网关）
cmd/                       fnOS 生命周期脚本（install/upgrade/uninstall + config）
config/                    privilege / resource
wizard/                    安装向导（双端口）、配置向导、卸载向导（保留或删除数据）
manifest                   fnOS 应用清单（checksum 由 fnpack 打包时写回）
build_1114.py               打包脚本（staging + fnpack + 校验）
patch_magpie_css2.py       控制台移动端 CSS 等长补丁（换上游版本后需重新施加）
```

## 构建步骤

1. 取上游二进制并放到 `app/bin/magpie`（可执行）：

   ```bash
   curl -L -o magpie-cli-linux-amd64 \
     https://github.com/yetone/magpie-releases/releases/download/v0.1.1635/magpie-cli-linux-amd64
   mkdir -p app/bin && cp magpie-cli-linux-amd64 app/bin/magpie && chmod 755 app/bin/magpie
   ```

2. 施加控制台的移动端样式补丁（等长、可逆）：

   ```bash
   python3 patch_magpie_css2.py app/bin/magpie --write
   ```

   脚本会自动定位内嵌 CSS 区段、断言三个锚点各命中一次，并校验补丁前后二进制长度不变。

3. 打包（需要 `fnpack`，可用 `FNPACK_BIN` 指定路径）：

   ```bash
   FNPACK_BIN=/path/to/fnpack python3 build_1114.py
   ```

   产物输出到 `dist/magpie-1.1.14-fnos-amd64.fpk`，脚本会自检：
   `manifest.checksum == md5(app.tgz)`、包内没有 `__pycache__`、入口图标槽位齐全。

4. **ARM64**：与 x86 同源树，只换两处 —— 二进制换成 `magpie-cli-linux-arm64`，`manifest` 里 `platform = x86` 改为 `platform = arm`，其余步骤（CSS 补丁、打包、自检）完全相同，产物名为 `dist/magpie-1.1.14-fnos-arm64.fpk`。

## 注意

- `manifest` 里的 `checksum` 在源码树中留空，`fnpack` 打包时会写成 `app.tgz` 的真实 MD5。
- 控制台的样式是编译进二进制的，改磁盘上的 `ui/` 不会影响线上页面；只能对二进制做等长补丁。
- 统一网关边车对响应体做了 chunked 处理：需要改写的 HTML/JS/CSS 先解块再重编码，SSE 等流式响应原样边到边转发。
