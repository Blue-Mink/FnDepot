#!/bin/bash
# fnradio 自愈守护：官方 trim-music 重启抢回音乐 socket 时，自动重新接管。
# 用法：watchdog.sh <code_dir> <py> <venv> <logd>
#   code_dir  fnradio 代码目录（含 takeover_cli.py / radio_relay.py）
#   py        宿主 python3（跑 takeover_cli，不依赖 venv）
#   venv      中继 venv 根（含 bin/python3）
#   logd      日志目录（写 watchdog.log / fnradio.log）
set -u

CODE_DIR="$1"; PY="$2"; VENV="$3"; LOGD="$4"
TARGET="/var/run/trim_music.socket"
UPSTREAM="/var/run/trim_music_upstream.socket"
INTERVAL="${FNRADIO_WATCHDOG_INTERVAL:-30}"

log() { echo "[$(date '+%F %T')] $*" >>"$LOGD/watchdog.log" 2>/dev/null; }

# 单例：用 pid 文件 + kill -0 检查（不用 pgrep，避免把启动链里含 "watchdog.sh"
# 的 ssh/source 父进程误判成「已有实例」导致自杀）。
PIDFILE="$LOGD/watchdog.pid"
if [ -f "$PIDFILE" ]; then
    OLD=$(cat "$PIDFILE" 2>/dev/null)
    if [ -n "$OLD" ] && kill -0 "$OLD" 2>/dev/null; then
        log "已有 watchdog (pid=$OLD) 在跑，本实例退出"
        exit 0
    fi
fi
echo "$$" > "$PIDFILE"
trap 'rm -f "$PIDFILE" 2>/dev/null' EXIT

log "watchdog 启动 (interval=${INTERVAL}s)"

while true; do
    sleep "$INTERVAL"

    # 目标 socket 被官方/外来进程抢回？
    KIND=$("$PY" "$CODE_DIR/takeover_cli.py" status 2>/dev/null | sed -n 's/^target=.*kind=\([a-z]*\).*/\1/p' | head -1)

    case "$KIND" in
    official|foreign)
        log "检测到音乐 socket 被抢回 (kind=$KIND)，开始重新接管"
        # 停旧中继：SIGKILL 避免 uvicorn 优雅退出 unlink 误删官方新 socket
        pkill -9 -f "uvicorn radio_relay" 2>/dev/null
        sleep 1
        # 重新接管（takeover_cli take 会把官方 rename 到 upstream）
        OUT=$("$PY" "$CODE_DIR/takeover_cli.py" take 2>/dev/null)
        RC=$?
        if [ "$RC" -eq 0 ]; then
            UPS=$(printf '%s\n' "$OUT" | sed -n 's/^FNRADIO_UPSTREAM_SOCK=//p' | head -1)
            [ -n "$UPS" ] || UPS="$UPSTREAM"
            # 重启中继，绑回 target
            ( cd "$CODE_DIR" && FNRADIO_UPSTREAM_SOCK="$UPS" \
                nohup setsid "$VENV/bin/python3" -m uvicorn radio_relay:app \
                --uds "$TARGET" --log-level warning >>"$LOGD/fnradio.log" 2>&1 </dev/null & )
            log "重新接管成功（take=$OUT），中继已重启"
        else
            log "重新接管失败（fail-closed）：$OUT"
        fi
        ;;
    esac
done
