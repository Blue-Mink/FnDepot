#!/bin/bash
# Magpie — fnOS CGI 双入口
#   /cgi/ThirdParty/magpie/index.cgi/     → Web 控制台（302 携带 ?k= 访问密钥，未运行时自动拉起）
#   /cgi/ThirdParty/magpie/index.cgi/gw/  → API 网关说明页（地址/默认密钥/接口清单/示例）
APPNAME="magpie"
ETC="/var/apps/${APPNAME}/etc"
VAR="/var/apps/${APPNAME}/var"
[ -f "${ETC}/ports.conf" ] && . "${ETC}/ports.conf"
WEB_PORT="${WEB_PORT:-36140}"
GW_PORT="${GW_PORT:-36141}"

lan_host() {
    local h="${SERVER_ADDR:-}"
    if [ -z "$h" ] || [ "$h" = "127.0.0.1" ] || [ "$h" = "::1" ]; then
        h="$(ip route get 1 2>/dev/null | grep -oP 'src \K[\d.]+')"
    fi
    echo "${h:-127.0.0.1}"
}

port_up() { timeout 2 bash -c "exec 3<>/dev/tcp/127.0.0.1/$1" 2>/dev/null; }

ensure_running() {
    local p="$1"
    port_up "$p" && return 0
    /var/apps/${APPNAME}/cmd/main start >/dev/null 2>&1
    local i=0
    while ! port_up "$p" && [ $i -lt 25 ]; do sleep 1; i=$((i+1)); done
    port_up "$p"
}

# ---------- 接口清单（美化 JSON） ----------
if case "/${PATH_INFO:-}" in */api*) true;; *) false;; esac; then
    ensure_running "$GW_PORT" >/dev/null 2>&1
    API_JSON="$(curl -s --max-time 3 -H 'Accept: application/json' "http://127.0.0.1:${GW_PORT}/" 2>/dev/null)"
    PRETTY="$(printf '%s' "$API_JSON" | python3 -c 'import sys,json;print(json.dumps(json.load(sys.stdin),ensure_ascii=False,indent=2))' 2>/dev/null)"
    [ -z "$PRETTY" ] && PRETTY="${API_JSON:-（网关未返回数据）}"
    ESCAPED="$(printf '%s' "$PRETTY" | sed 's/&/\&amp;/g; s/</\&lt;/g; s/>/\&gt;/g')"
    echo "Content-Type: text/html; charset=utf-8"
    echo "Cache-Control: no-store"
    echo
    cat <<HTML
<!doctype html><html lang="zh-CN"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1,viewport-fit=cover">
<title>Magpie API · 接口清单</title>
<style>
*{box-sizing:border-box}
body{margin:0;background:#0f1420;color:#e6eaf2;font:14px/1.6 system-ui,-apple-system,"PingFang SC","Microsoft YaHei",sans-serif;-webkit-text-size-adjust:100%}
.wrap{max-width:860px;margin:0 auto;padding:1.4rem 1rem 2.4rem}
h1{font-size:1.2rem;margin:0 0 .3rem}
.sub{color:#9aa6b8;margin:0 0 1rem;font-size:13px}
pre{background:#0b111c;border:1px solid #212c3b;border-radius:10px;padding:.8rem .9rem;margin:0 0 1rem;
  font:12px/1.6 ui-monospace,Consolas,monospace;color:#cfe3ff;overflow-x:auto;white-space:pre-wrap;word-break:break-all}
a{color:#4f8bff;text-decoration:none}
</style></head><body><div class="wrap">
<h1>Magpie API · 接口清单</h1>
<p class="sub">来自网关实时返回的 JSON（美化显示）</p>
<pre>${ESCAPED}</pre>
<p class="sub"><a href="/cgi/ThirdParty/magpie/index.cgi/gw/">← 返回网关说明</a></p>
</div></body></html>
HTML
    exit 0
fi

# ---------- 网关说明页 ----------
if case "/${PATH_INFO:-}" in *gw*) true;; *) false;; esac; then
    HOST="$(lan_host)"
    ensure_running "$GW_PORT"
    GWKEY="$(head -n 1 "${VAR}/gateway-keys/NAS.txt" 2>/dev/null)"
    # 取 JSON 形态的接口清单（Accept: application/json），避免把整页 HTML 嵌进来
    API_JSON="$(curl -s --max-time 3 -H 'Accept: application/json' "http://127.0.0.1:${GW_PORT}/" 2>/dev/null)"
    API_TAGS="$(printf '%s' "$API_JSON" \
        | sed -n 's/.*"apis":\[\([^]]*\)\].*/\1/p' \
        | tr ',' '\n' | tr -d '"' \
        | sed 's/^[[:space:]]*//;s/[[:space:]]*$//' \
        | grep -v '^$' \
        | sed 's#.*#<span class="tag">&</span>#')"
    [ -z "$API_TAGS" ] && API_TAGS="（服务未就绪，稍后刷新）"
    if port_up "$GW_PORT"; then
        GW_STATE="运行中"
        GW_STATE_CLS="ok"
        REFRESH_META=""
    else
        GW_STATE="未运行（已尝试自动启动，5 秒后自动重试）"
        GW_STATE_CLS="bad"
        REFRESH_META='<meta http-equiv="refresh" content="5">'
    fi
    echo "Content-Type: text/html; charset=utf-8"
    echo "Cache-Control: no-store"
    echo
    cat <<HTML
<!doctype html><html lang="zh-CN"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1,viewport-fit=cover">
${REFRESH_META}
<title>Magpie API 网关</title>
<style>
*{box-sizing:border-box}
body{margin:0;background:#0f1420;color:#e6eaf2;font:14px/1.6 system-ui,-apple-system,"PingFang SC","Microsoft YaHei",sans-serif;-webkit-text-size-adjust:100%}
.wrap{max-width:860px;margin:0 auto;padding:2.2rem 1.4rem 3rem}
h1{font-size:1.35rem;margin:0 0 .3rem;line-height:1.35}
.sub{color:#9aa6b8;margin:0 0 1.4rem}
.card{background:#161c28;border:1px solid #242f41;border-radius:12px;padding:1rem 1.2rem;margin-bottom:1rem}
.card h2{font-size:.95rem;margin:0 0 .6rem;color:#c8d2e4}
.cmd{background:#0b111c;border:1px solid #212c3b;border-radius:8px;padding:.55rem .8rem;
  font:12.5px/1.55 ui-monospace,Consolas,monospace;color:#e6eaf2;overflow-x:auto;white-space:pre-wrap;word-break:break-all}
.kv{display:grid;grid-template-columns:max-content minmax(0,1fr);gap:.5rem .9rem;align-items:baseline;margin:.3rem 0}
.kv dt{color:#9aa6b8;white-space:nowrap}
.kv dd{margin:0;min-width:0;overflow-wrap:anywhere;word-break:break-word}
.kv dd.cmd{padding:.35rem .6rem}
a{color:#4f8bff;text-decoration:none;word-break:break-all}
.tag{display:inline-block;background:#1c2635;border:1px solid #2c3a4e;border-radius:6px;
  padding:.15rem .55rem;margin:.15rem .3rem .15rem 0;font:12px ui-monospace,Consolas,monospace;color:#9fd3ff;word-break:break-all}
.ok{color:#3ddc84}.bad{color:#ff7a7a}
@media (max-width:520px){
  .wrap{padding:1.2rem .9rem 2rem}
  h1{font-size:1.18rem}
  .sub{font-size:13px}
  .card{padding:.85rem .9rem}
  .kv{grid-template-columns:1fr;gap:0}
  .kv dt{margin-top:.6rem;font-size:12.5px}
  .kv dd{margin-bottom:.1rem}
  .cmd{font-size:12px}
}
</style></head><body><div class="wrap">
<h1>Magpie API 网关</h1>
<p class="sub">OpenAI 兼容端点（上游 yetone/magpie）· 状态：<span class="${GW_STATE_CLS}">${GW_STATE}</span></p>
<div class="card"><h2>网关地址与默认密钥</h2>
<dl class="kv">
<dt>网关地址</dt><dd><a href="http://${HOST}:${GW_PORT}/?key=${GWKEY}" target="_blank" rel="noreferrer">http://${HOST}:${GW_PORT}/</a></dd>
<dt>默认密钥</dt><dd class="cmd">${GWKEY:-（尚未生成，服务首启后自动创建）}</dd>
<dt>统一网关</dt><dd>http://${HOST}:5666/app/magpie/v1/…（需飞牛登录态，与上方同一后端）</dd>
</dl></div>
<div class="card"><h2>调用示例</h2>
<div class="cmd">curl http://${HOST}:${GW_PORT}/v1/chat/completions \
  -H "Authorization: Bearer ${GWKEY:-<默认密钥>}" \
  -H "Content-Type: application/json" \
  -d '{"model":"&lt;路由组或模型名&gt;","messages":[{"role":"user","content":"hi"}]}'</div></div>
<div class="card"><h2>支持的接口（来自网关实时返回）</h2>
${API_TAGS}</div>
<p class="sub"><a href="/cgi/ThirdParty/magpie/index.cgi/api">查看接口清单（美化 JSON）</a></p>
</div></body></html>
HTML
    exit 0
fi

# ---------- Web 控制台 302 ----------
KEY="$(cat "${VAR}/web-key" 2>/dev/null)"
HOST="$(lan_host)"
TARGET="http://${HOST}:${WEB_PORT}/?k=${KEY}"
if ! ensure_running "$WEB_PORT"; then
    echo "Content-Type: text/html; charset=utf-8"
    echo
    cat <<HTML
<!doctype html><html><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<meta http-equiv="refresh" content="5">
<title>Magpie</title>
<body style="font-family:sans-serif;padding:2rem">
<h3>Magpie 还没起来</h3>
<p>已尝试自动启动，5 秒后自动重试。若持续失败，请在应用中心启动 Magpie，
或查看应用数据目录下的 magpie.log。</p>
</body></html>
HTML
    exit 0
fi
echo "Status: 302 Found"
echo "Location: ${TARGET}"
echo "Cache-Control: no-store"
echo "Content-Type: text/html; charset=utf-8"
echo
cat <<HTML
<!doctype html><html><head><meta charset="utf-8">
<meta http-equiv="refresh" content="0;url=${TARGET}">
<title>Magpie</title></head>
<body style="font-family:sans-serif;padding:2rem">正在打开 Magpie 控制台…
<a href="${TARGET}">点此直达</a></body></html>
HTML
exit 0
