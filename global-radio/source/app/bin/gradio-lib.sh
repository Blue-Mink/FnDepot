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
    local port="$1" pv cb
    pv="$(gradio_pkgvar)"; cb="$(gradio_compose_bin)"
    mkdir -p "$pv/data" 2>/dev/null
    ( cd "$GRADIO_APP_DIR/docker" && TRIM_PKGVAR="$pv" wizard_access_port="$port" $cb up -d )
}

gradio_is_running() {
    docker ps --filter "name=$GRADIO_CONTAINER" --filter status=running \
        --format '{{.Names}}' 2>/dev/null | grep -qx "$GRADIO_CONTAINER"
}

# ---- AppCenter DB 同步（service_url / 桌面入口 / status）----
gradio_db_once() {
    command -v psql >/dev/null 2>&1 || return 0
    local port="$1"
    psql -h /var/run/postgresql -d appcenter -U postgres -v ON_ERROR_STOP=0 >/dev/null 2>&1 <<SQL || true
UPDATE app
   SET service_url='http://' || chr(36) || '{host:${port}}/',
       status='running', is_stop=true, is_uninstall=true, updated_at=now()
 WHERE app_name='${GRADIO_NAME}';
UPDATE app_service
   SET url='http://' || chr(36) || '{host:${port}}/',
       default_url='http://' || chr(36) || '{host:${port}}/',
       title='${GRADIO_DISPLAY}', updated_at=now()
 WHERE app_id=(SELECT id FROM app WHERE app_name='${GRADIO_NAME}' LIMIT 1);
INSERT INTO system_config(type,k,v)
SELECT 'appAutoUpdate','${GRADIO_NAME}','false'
 WHERE NOT EXISTS (SELECT 1 FROM system_config WHERE type='appAutoUpdate' AND k='${GRADIO_NAME}');
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
