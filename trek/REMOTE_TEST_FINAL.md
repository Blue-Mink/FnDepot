# TREK v4.1.1 最终备用机验证记录

## 测试机
- 地址：192.168.3.2
- 安装包：`dist/trek/final/com.trek.app-4.1.1-fnos-amd64.fpk`
- SHA256：`af328142e7830eaba0c56f1a5168f40901cc7ace8886fefd9ba2862e04b2238f`
- 测试时间：2026-09-01

## 最终源码修复
1. `cmd/install_callback` 调整为调用自身同目录 `cmd/main`，payload 目录通过 `TRIM_PKGDIR` 传入，修复安装后 AppCenter running 但 Docker Created 的问题。
2. `cmd/main status`：容器未运行时返回 exit 3，AppCenter 可正确识别 stopped。
3. `manifest`：移除 deprecated `arch=x86_64`，新增 `ctl_stop=true`。
4. `config/resource`：移除 `docker-project`，改为纯 lifecycle 脚本管理 Docker，避免 AppCenter 内置 docker-project stop 报 `open /docker/docker-compose.yaml`。
5. `cmd/config_callback`：改端口后执行延迟 DB 同步，避免 AppCenter 后半段覆盖 `status=running`。

## 验证结果
- 安装：`appcenter-cli install-fpk --env install.env --volume 1` 成功。
- Docker：`trek|Up ... (healthy)|0.0.0.0:32679->3000/tcp`。
- HTTP 首页：`http://192.168.3.2:32679/` 返回 200。
- HTTP 登录：`POST /api/auth/login` 返回 200。
- HTTPS：`https://192.168.3.2:32679/` 返回 OpenSSL `wrong version number`，说明该端口为纯 HTTP 服务，符合允许 HTTP 访问的预期。
- AppCenter stop：状态变 `stopped`，Docker 变 `Exited (137)`，HTTP 不可访问。
- AppCenter start：状态变 `running`，Docker 变 `Up healthy`，HTTP health 返回 `{"status":"ok"}`。
- 端口修改：`32679 → 32680 → 32679` 均成功；Docker 端口、HTTP health、`app.service_url`、`app_service.url/default_url`、`ui/config` 均同步。
- 卸载：AppCenter uninstall 成功；测试流程中只备份/迁移 TREK 相关目录，不动其它应用。

## 最终状态
- app id：124
- status：running
- service_url：`http://${host}:32679/`
- app_service.url/default_url：`http://${host}:32679/`
- ui/config protocol：`http`
- ui/config port：`32679`

## 剩余 warning
- `application requests root execution; review the least-privilege justification`
- 原因：生命周期脚本需要管理 Docker compose、容器启停和 AppCenter DB 同步，当前可接受。

## 图标恢复更新（2026-09-01）

- 按用户要求，应用图标恢复为旧版蓝色定位图钉视觉，而非深蓝底白色几何块。
- 已同步根目录、外层 UI、payload UI 以及 app.tgz 内所有 64/256 图标位。
- 更新后 FPK SHA256：`ecaec3a098778c173cb70b28f08b510a7c4619cc43e9d1622deabf3f874b942a`
