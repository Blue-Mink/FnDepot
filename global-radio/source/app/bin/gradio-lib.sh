#!/bin/bash
# gradio-lib.sh — global-radio 生命周期脚本共用工具（被 cmd/* source）
GRADIO_NAME="global-radio"
GRADIO_DISPLAY="全球电台"
GRADIO_CONTAINER="global-radio"
GRADIO_DEFAULT_PORT=32678

gradio_log() {
    echo "$*"
    [ -n "${TRIM_TEMP_LOGFILE:-}" ] && echo "[$(date '+%F %T')] $*" >>"$TRIM_TEMP_LOGFILE" 2>/dev/null
    return 0
}
gradio_fail() { gradio_log "ERROR: $*"; exit 1; }

# ---- 路径归一化：payload 可能在 $DIR 或 $DIR/target ----
gradio_norm_appdir() {
    local d
    for d in "${TRIM_PKGDIR:-}" "${TRIM_APPDEST:-}" "/var/apps/$GRADIO_NAME/target" \
             "/var/apps/$GRADIO_NAME" "/vol1/@appcenter/$GRADIO_NAME"; do
        [ -n "$d" ] && [ -f "$d/docker/docker-compose.yaml" ] && { GRADIO_APP_DIR="$d"; return 0; }
    done
    for d in "${TRIM_PKGDIR:-}" "${TRIM_APPDEST:-}" "/var/apps/$GRADIO_NAME" "/vol1/@appcenter/$GRADIO_NAME"; do
        [ -n "$d" ] && [ -f "$d/target/docker/docker-compose.yaml" ] && { GRADIO_APP_DIR="$d/target"; return 0; }
    done
    return 1
}

gradio_pkgvar() {
    [ -n "${TRIM_PKGVAR:-}" ] && { echo "$TRIM_PKGVAR"; return; }
    [ -e "/var/apps/$GRADIO_NAME/var" ] && { echo "/var/apps/$GRADIO_NAME/var"; return; }
    echo "/vol1/@appdata/$GRADIO_NAME"
}

gradio_etc() {
    [ -n "${TRIM_PKGETC:-}" ] && { echo "$TRIM_PKGETC"; return; }
    [ -d "/var/apps/$GRADIO_NAME/etc" ] && { echo "/var/apps/$GRADIO_NAME/etc"; return; }
    echo "/vol1/@appconf/$GRADIO_NAME"
}

# ---- 统一网关 sidecar（v1.3.0）----
GRADIO_GW_PREFIX="/app/$GRADIO_NAME"
GRADIO_BIN_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" 2>/dev/null && pwd)"

gradio_gw_sock() { echo "$(gradio_etc)/gateway/gr-gw.sock"; }

gradio_gw_stop() {
    local dir pidf
    dir="$(dirname "$(gradio_gw_sock)")"
    pidf="$dir/gr-gw.pid"
    if [ -f "$pidf" ]; then
        kill "$(cat "$pidf" 2>/dev/null)" 2>/dev/null
        rm -f "$pidf"
    fi
    pkill -f "global-radio-gw[-]proxy.py" 2>/dev/null
    return 0
}

gradio_gw_start() {
    command -v python3 >/dev/null 2>&1 || { gradio_log "网关: 缺 python3，跳过 sidecar"; return 0; }
    local port sock dir py pidf logd i
    port="${1:-$(gradio_read_port)}"
    sock="$(gradio_gw_sock)"; dir="$(dirname "$sock")"; pidf="$dir/gr-gw.pid"
    py="$GRADIO_BIN_DIR/global-radio-gw-proxy.py"
    [ -f "$py" ] || { gradio_log "网关: 缺少 $py，跳过"; return 0; }
    mkdir -p "$dir" 2>/dev/null || return 0
    logd="$(gradio_pkgvar)/logs"; mkdir -p "$logd" 2>/dev/null
    gradio_gw_stop
    rm -f "$sock"
    GW_SOCK="$sock" GW_PORT="$port" GW_PREFIX="$GRADIO_GW_PREFIX" \
        nohup setsid python3 "$py" >>"$logd/gateway.log" 2>&1 </dev/null &
    echo $! >"$pidf"
    i=0
    while [ "$i" -lt 10 ] && [ ! -S "$sock" ]; do sleep 0.5; i=$((i+1)); done
    if [ -S "$sock" ]; then
        gradio_log "网关 sidecar 已启动: $sock -> :$port (前缀 $GRADIO_GW_PREFIX)"
    else
        gradio_log "网关 sidecar 未就绪（不影响端口直连入口）"
    fi
    return 0
}

gradio_valid_port() {
    case "$1" in ''|*[!0-9]*) return 1 ;; esac
    [ "$1" -ge 1 ] && [ "$1" -le 65535 ]
}

# ---- 端口读取优先级：持久 port.conf > 已安装 ui/config > 默认 ----
gradio_read_port() {
    local pf port="" uic
    pf="$(gradio_etc)/port.conf"
    [ -f "$pf" ] && port=$(tr -dc '0-9' < "$pf")
    if [ -z "$port" ]; then
        for uic in "$GRADIO_APP_DIR/ui/config" "/var/apps/$GRADIO_NAME/target/ui/config" \
                   "/vol1/@appcenter/$GRADIO_NAME/ui/config"; do
            [ -f "$uic" ] || continue
            port=$(grep -oE '"port": *"[0-9]+"' "$uic" | grep -oE '[0-9]+' | head -1)
            [ -n "$port" ] && break
        done
    fi
    gradio_valid_port "$port" || port="$GRADIO_DEFAULT_PORT"
    echo "$port"
}

# ---- 端口写入：port.conf(持久) + docker/.env + .env ----
gradio_write_port() {
    local etc; etc="$(gradio_etc)"
    mkdir -p "$etc" 2>/dev/null
    echo "$1" > "$etc/port.conf"
    echo "wizard_access_port=$1" > "$GRADIO_APP_DIR/docker/.env"
    echo "wizard_access_port=$1" > "$GRADIO_APP_DIR/.env"
    return 0
}

gradio_fix_ui_config() {
    local uic="$GRADIO_APP_DIR/ui/config"
    [ -f "$uic" ] || return 0
    sed -i "s/\"port\": *\"[0-9]*\"/\"port\": \"$1\"/g; s/\"protocol\": *\"\"/\"protocol\": \"http\"/g" "$uic"
    return 0
}

gradio_compose_bin() {
    command -v docker-compose >/dev/null 2>&1 && echo docker-compose || echo "docker compose"
}

gradio_clean_container() {
    local cb; cb="$(gradio_compose_bin)"
    ( cd "$GRADIO_APP_DIR/docker" && $cb down --remove-orphans ) >/dev/null 2>&1
    docker ps -a --format '{{.Names}}' 2>/dev/null | grep -qx "$GRADIO_CONTAINER" \
        && docker rm -f "$GRADIO_CONTAINER" >/dev/null 2>&1
    return 0
}

gradio_up() {
    local port="$1" pv cb img
    pv="$(gradio_pkgvar)"; cb="$(gradio_compose_bin)"
    img="$(grep -oE 'image:[[:space:]]*[^[:space:]]+' "$GRADIO_APP_DIR/docker/docker-compose.yaml" | head -1 | awk '{print $2}')"
    # 平台 uninstall 会删除应用镜像：优先从本地守护副本恢复，避免重装重拉大包
    if [ -n "$img" ] && ! docker image inspect "$img" >/dev/null 2>&1; then
        docker image inspect global-radio-cache:keep >/dev/null 2>&1 \
            && docker tag global-radio-cache:keep "$img" \
            && gradio_log "已从本地守护副本恢复镜像 $img"
    fi
    mkdir -p "$pv/data" 2>/dev/null
    ( cd "$GRADIO_APP_DIR/docker" && TRIM_PKGVAR="$pv" wizard_access_port="$port" $cb up -d ) || return 1
    # 成功后维护守护副本（uninstall 删了原 tag 也能秒级恢复）
    [ -n "$img" ] && docker tag "$img" global-radio-cache:keep 2>/dev/null
    return 0
}

gradio_is_running() {
    docker ps --filter "name=$GRADIO_CONTAINER" --filter status=running \
        --format '{{.Names}}' 2>/dev/null | grep -qx "$GRADIO_CONTAINER"
}

# ---- SW 图片缓存补丁（v1.2.6）----
# 从当前镜像抽取原始 sw.js，拼接图片缓存片段后注入运行中的容器并 reload。
# 每次 start 都基于镜像原始文件重建，天然幂等，且随上游镜像升级自动跟随。
# 任何失败仅记日志，不影响启动。
gradio_patch_sw() {
    local libdir snip img tmp id rc=1
    libdir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
    snip="$libdir/sw-img-cache.snippet.js"
    [ -f "$snip" ] || { gradio_log "sw补丁: 未找到片段文件，跳过"; return 0; }
    gradio_is_running || { gradio_log "sw补丁: 容器未运行，跳过"; return 0; }
    img="$(grep -oE 'image:[[:space:]]*[^[:space:]]+' "$GRADIO_APP_DIR/docker/docker-compose.yaml" | head -1 | awk '{print $2}')"
    [ -n "$img" ] || { gradio_log "sw补丁: 无法解析镜像名，跳过"; return 0; }
    tmp="$(mktemp /tmp/gr-sw.XXXXXX)" || return 0
    id="$(docker create "$img" 2>/dev/null)"
    if [ -n "$id" ] && docker cp "$id:/usr/share/nginx/html/sw.js" "$tmp.orig" 2>/dev/null && [ -s "$tmp.orig" ]; then
        if cat "$tmp.orig" "$snip" > "$tmp" && chmod 644 "$tmp" \
           && docker cp "$tmp" "$GRADIO_CONTAINER:/usr/share/nginx/html/sw.js" \
           && docker exec "$GRADIO_CONTAINER" sh -c 'chmod 644 /usr/share/nginx/html/sw.js; nginx -s reload'; then
            rc=0
        fi
        gradio_log "sw补丁: 图片缓存注入$([ "$rc" -eq 0 ] && echo 成功 || echo '失败(不影响使用)')"
    else
        gradio_log "sw补丁: 无法从镜像抽取 sw.js，跳过"
    fi
    [ -n "$id" ] && docker rm -f "$id" >/dev/null 2>&1
    rm -f "$tmp" "$tmp.orig"
    return 0
}

# ---- 加速缓存注入（v1.2.7）----
# 从镜像抽取 default.conf / index.html，注入 /rb + /imgproxy 代理缓存 locations
# 与 gr-accel.js 包装层，回写容器并 reload。每次基于镜像原件重建，幂等且随
# 镜像升级自动跟随；nginx -t 不通过则保留原配置。失败仅记日志，不影响启动。
gradio_patch_accel() {
    local libdir img tmp id res rc=1
    libdir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
    if [ ! -f "$libdir/gr-accel.js" ] || [ ! -f "$libdir/nginx-graccel-locations.conf" ] \
       || [ ! -f "$libdir/nginx-graccel-header.conf" ]; then
        gradio_log "加速器: 未找到 payload 文件，跳过"; return 0
    fi
    gradio_is_running || { gradio_log "加速器: 容器未运行，跳过"; return 0; }
    img="$(grep -oE 'image:[[:space:]]*[^[:space:]]+' "$GRADIO_APP_DIR/docker/docker-compose.yaml" | head -1 | awk '{print $2}')"
    [ -n "$img" ] || { gradio_log "加速器: 无法解析镜像名，跳过"; return 0; }
    id="$(docker create "$img" 2>/dev/null)"
    [ -n "$id" ] || { gradio_log "加速器: 无法创建临时容器，跳过"; return 0; }
    tmp="$(mktemp -d /tmp/graccel.XXXXXX)"
    if docker cp "$id:/etc/nginx/conf.d/default.conf" "$tmp/default.conf" 2>/dev/null \
       && docker cp "$id:/usr/share/nginx/html/index.html" "$tmp/index.html" 2>/dev/null; then
        res="$(docker exec "$GRADIO_CONTAINER" awk '/^nameserver/{print $2; exit}' /etc/resolv.conf 2>/dev/null)"
        case "$res" in ''|*[!0-9.]*) res="223.5.5.5" ;; esac
        awk -v hdr="$libdir/nginx-graccel-header.conf" -v loc="$libdir/nginx-graccel-locations.conf" -v res="$res" '
            !hs && /^server[[:space:]]*\{/ {
                while ((getline l < hdr) > 0) { print l }
                close(hdr); hs=1
            }
            $0 ~ /^}[[:space:]]*$/ && !ins {
                while ((getline l < loc) > 0) { gsub(/__GRRESOLVER__/, res, l); print l }
                close(loc); ins=1
            }
            { print }
        ' "$tmp/default.conf" > "$tmp/default.new"
        sed 's#</head>#<script src="/gr-accel.js"></script></head>#' "$tmp/index.html" > "$tmp/index.new"
        cp "$libdir/gr-accel.js" "$tmp/gr-accel.js"
        if grep -q graccel_api "$tmp/default.new" && grep -q gr-accel.js "$tmp/index.new"; then
            if docker cp "$tmp/default.new" "$GRADIO_CONTAINER:/etc/nginx/conf.d/default.conf" \
               && docker cp "$tmp/index.new" "$GRADIO_CONTAINER:/usr/share/nginx/html/index.html" \
               && docker cp "$tmp/gr-accel.js" "$GRADIO_CONTAINER:/usr/share/nginx/html/gr-accel.js" \
               && docker exec "$GRADIO_CONTAINER" sh -c 'chmod 644 /etc/nginx/conf.d/default.conf /usr/share/nginx/html/index.html /usr/share/nginx/html/gr-accel.js && nginx -t' \
               && docker exec "$GRADIO_CONTAINER" nginx -s reload; then
                rc=0
            fi
        else
            gradio_log "加速器: 注入点未命中（default.conf/index.html 结构变化？）"
        fi
        gradio_log "加速器: 代理缓存注入$([ "$rc" -eq 0 ] && echo 成功 || echo '失败(不影响使用)')"
    else
        gradio_log "加速器: 无法从镜像抽取配置文件，跳过"
    fi
    docker rm -f "$id" >/dev/null 2>&1
    rm -rf "$tmp"
    return 0
}

# ---- AppCenter DB 同步（service_url / 桌面入口 / status）----
gradio_db_once() {
    command -v psql >/dev/null 2>&1 || return 0
    local port="$1"
    psql -h /var/run/postgresql -d appcenter -U postgres -v ON_ERROR_STOP=0 >/dev/null 2>&1 <<SQL || true
UPDATE app
   SET service_url='http://' || chr(36) || '{host}:${port}/',
       status='running', is_stop=true, is_uninstall=true, updated_at=now()
 WHERE app_name='${GRADIO_NAME}';
UPDATE app_service
   SET url='http://' || chr(36) || '{host}:${port}/',
       default_url='http://' || chr(36) || '{host}:${port}/',
       gateway_socket='$(gradio_gw_sock)',
       gateway_prefix='${GRADIO_GW_PREFIX}',
       title='${GRADIO_DISPLAY}', updated_at=now()
 WHERE app_id=(SELECT id FROM app WHERE app_name='${GRADIO_NAME}' LIMIT 1);
INSERT INTO system_config(type,k,v)
SELECT 'appAutoUpdate','${GRADIO_NAME}','false'
 WHERE NOT EXISTS (SELECT 1 FROM system_config WHERE type='appAutoUpdate' AND k='${GRADIO_NAME}');
SQL
    # 桌面/控制台服务条目注册在 trim_sac.entry（“端口编辑”对话框读这里），
    # 不同步会出现“编辑显示旧端口、打开走新端口”的分裂（v1.2.8 修复）
    psql -h /var/run/postgresql -d trim_sac -U postgres -v ON_ERROR_STOP=0 >/dev/null 2>&1 <<SQL || true
UPDATE entry
   SET url='{"protocol":"http","port":"${port}","path":"/"}', updated_at=now()
 WHERE app_name='${GRADIO_NAME}' AND service_name='${GRADIO_NAME}.Application';
SQL
    return 0
}

gradio_sync_db() {
    local port="$1"
    gradio_db_once "$port"
    (
        for d in 3 8 15; do
            sleep "$d"
            gradio_db_once "$port"
        done
    ) >/dev/null 2>&1 &
    return 0
}
