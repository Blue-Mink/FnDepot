#!/bin/bash
# fnradio_setup.sh —— 电台中继后台环境准备（由 cmd/main start 以 nohup setsid 拉起）
# 流程：创建 venv → 安装依赖 → 接管官方音乐 socket → 启动 uvicorn
# 用法: fnradio_setup.sh <python_bin> <venv_dir> <code_dir> <log_dir> <pid_file>
set -u
PY="${1:?}"; VENV="${2:?}"; CODE="${3:?}"; LOGD="${4:?}"; PIDF="${5:?}"
SOCK="/var/run/trim_music.socket"
UPSTREAM="/var/run/trim_music_upstream.socket"
PIP_MIRROR="https://pypi.tuna.tsinghua.edu.cn/simple"

log() { echo "[$(date '+%F %T')] $*"; }

log "电台中继环境准备开始: py=$PY venv=$VENV code=$CODE"

# 1. venv
if [ ! -x "$VENV/bin/python3" ]; then
    mkdir -p "$(dirname "$VENV")" 2>/dev/null
    "$PY" -m venv "$VENV" || { log "ERROR: venv 创建失败"; exit 1; }
    log "venv 已创建"
fi

# 2. 依赖（缺才装；已有则秒过）
if ! "$VENV/bin/python3" -c "import fastapi, uvicorn, httpx, PIL" >/dev/null 2>&1; then
    log "安装依赖（requirements.txt）..."
    "$VENV/bin/pip" install -q --upgrade pip -i "$PIP_MIRROR" 2>/dev/null || true
    if ! "$VENV/bin/pip" install -q -r "$CODE/requirements.txt" -i "$PIP_MIRROR" 2>&1 | tail -3; then
        log "清华源失败，改用默认源重试..."
        "$VENV/bin/pip" install -q -r "$CODE/requirements.txt" || { log "ERROR: 依赖安装失败"; exit 1; }
    fi
    log "依赖安装完成"
else
    log "依赖已就绪（复用现有 venv）"
fi

# 3. 接管官方音乐 socket（身份感知 + 链式共存）
#    确认 target 是官方才接管；target 是飞牛音源扩展则链式接管（二者共存）；其他外来进程拒绝。
RADIO_UPSTREAM="$UPSTREAM"
if OUT="$("$PY" "$CODE/takeover_cli.py" take 2>&1)"; then
    log "接管结果: $OUT"
    UPS="$(printf '%s\n' "$OUT" | sed -n 's/^FNRADIO_UPSTREAM_SOCK=//p' | head -1)"
    [ -n "$UPS" ] && RADIO_UPSTREAM="$UPS"
else
    log "WARN: 接管失败（检测到音乐 socket 被未知应用占用，fail-closed）: $OUT"
    exit 0
fi

# 4. 启动 uvicorn（新会话，setup 退出后存活）
cd "$CODE" || exit 1
FNRADIO_UPSTREAM_SOCK="$RADIO_UPSTREAM" \
    setsid "$VENV/bin/python3" -m uvicorn radio_relay:app \
    --uds "$SOCK" --log-level warning >>"$LOGD/fnradio.log" 2>&1 </dev/null &
echo $! > "$PIDF"
sleep 2
if [ -S "$SOCK" ]; then
    log "电台中继已启动 pid=$(cat "$PIDF" 2>/dev/null)"
else
    log "WARN: 中继 socket 未就绪（查看 $LOGD/fnradio.log）"
fi

# 5. 启动自愈守护（官方重启抢回 socket 时自动重新接管）
if [ -f "$CODE/watchdog.sh" ]; then
    nohup setsid bash "$CODE/watchdog.sh" "$CODE" "$PY" "$VENV" "$LOGD" \
        >>"$LOGD/watchdog.log" 2>&1 </dev/null &
    log "自愈守护已启动"
fi
exit 0
