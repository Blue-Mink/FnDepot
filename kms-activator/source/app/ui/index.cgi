#!/bin/bash
# KMS Activator - fnOS CGI Web 界面
# 由 fnOS nginx CGI 代理调用，路径：/cgi/ThirdParty/KmsActivator/index.cgi/

# ===== CGI 环境 =====
REQUEST_URI="${REQUEST_URI:-/cgi/ThirdParty/KmsActivator/index.cgi/}"
QUERY_STRING="${QUERY_STRING:-}"
REQUEST_METHOD="${REQUEST_METHOD:-GET}"

# ===== 应用路径 =====
APPNAME="KmsActivator"
APPDEST="/var/apps/${APPNAME}/target"
PKGVAR="/var/apps/${APPNAME}/var"
PID_FILE="${PKGVAR}/vlmcsd.pid"
LOG_FILE="${PKGVAR}/info.log"
CGI_BASE="/cgi/ThirdParty/KmsActivator/index.cgi"
RUNAS_USER="KmsActivator"

# ===== 本机 IP 自动检测 =====
SERVER_IP="${SERVER_ADDR:-}"
if [ -z "$SERVER_IP" ] || [ "$SERVER_IP" = "127.0.0.1" ] || [ "$SERVER_IP" = "::1" ]; then
    SERVER_IP="$(ip route get 1 2>/dev/null | grep -oP 'src \K[\d.]+')"
fi
SERVER_IP="${SERVER_IP:-127.0.0.1}"

# ===== KMS 端口（单点来源：${PKGVAR}/port，默认 1688）=====
KMS_PORT="$(head -n 1 "${PKGVAR}/port" 2>/dev/null | tr -cd '0-9')"
case "${KMS_PORT}" in ""|0*) KMS_PORT=1688 ;; esac

# ===== 解析动作 =====
URI_NO_QS="${REQUEST_URI%%\?*}"
ACTION=""
case "${URI_NO_QS}" in
  *index.cgi/*)
    REL="${URI_NO_QS#*index.cgi}"
    REL="${REL#/}"
    ACTION="${REL%%/*}"
    ;;
esac

if [ "$REQUEST_METHOD" = "POST" ]; then
    POST_DATA=$(cat)
fi

# ===== 工具函数 =====
vlmcsd_pid() {
    if [ -f "$PID_FILE" ]; then
        pid=$(head -n 1 "$PID_FILE" | tr -d '[:space:]')
        if [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; then
            echo "$pid"
            return 0
        fi
        rm -f "$PID_FILE"
    fi
    return 1
}

# root 时绑定特权端口后立刻降权（特权只用于 bind）
vlmcsd_spawn() {
    if [ "$(id -u)" = "0" ] && id "$RUNAS_USER" >/dev/null 2>&1; then
        "$APPDEST/vlmcsd" -p "$PID_FILE" -L "0.0.0.0:${KMS_PORT}" -u "$RUNAS_USER" -e >> "$LOG_FILE" 2>&1
    else
        "$APPDEST/vlmcsd" -p "$PID_FILE" -L "0.0.0.0:${KMS_PORT}" -e >> "$LOG_FILE" 2>&1
    fi
}

# ===== API =====
if [ "$ACTION" = "api" ]; then
    echo "Content-Type: application/json; charset=utf-8"
    echo ""

    SUB="${REL#*/}"
    case "$SUB" in
    "status")
        pid=$(vlmcsd_pid)
        if [ -n "$pid" ]; then
            owner=$(ps -o user= -p "$pid" 2>/dev/null | tr -d ' ')
            echo "{\"status\":\"running\",\"pid\":$pid,\"port\":$KMS_PORT,\"user\":\"${owner}\"}"
        else
            echo "{\"status\":\"stopped\",\"port\":$KMS_PORT}"
        fi
        exit 0
        ;;
    "start")
        if vlmcsd_pid > /dev/null; then
            echo "{\"status\":\"ok\",\"note\":\"服务已在运行\"}"
            exit 0
        fi
        # 优先走应用中心，保证平台记账一致
        NOTE=""
        if command -v appcenter-cli >/dev/null 2>&1 && appcenter-cli start "$APPNAME" >/dev/null 2>&1; then
            sleep 2
        else
            vlmcsd_spawn
            NOTE="已由网页启动，应用中心状态可能不同步"
            sleep 1
        fi
        if vlmcsd_pid > /dev/null; then
            echo "{\"status\":\"ok\",\"note\":\"${NOTE}\"}"
        else
            echo "{\"status\":\"error\",\"message\":\"启动失败，请在应用中心启动后重试\"}"
        fi
        exit 0
        ;;
    "stop")
        NOTE=""
        if ! (command -v appcenter-cli >/dev/null 2>&1 && appcenter-cli stop "$APPNAME" >/dev/null 2>&1); then
            NOTE="已由网页停止，应用中心状态可能不同步"
        fi
        pid=$(vlmcsd_pid)
        if [ -n "$pid" ]; then
            kill -TERM "$pid" 2>/dev/null
            sleep 1
            kill -KILL "$pid" 2>/dev/null
            sleep 1
        fi
        rm -f "$PID_FILE"
        echo "{\"status\":\"ok\",\"note\":\"${NOTE}\"}"
        exit 0
        ;;
    *)
        echo "{\"status\":\"error\",\"message\":\"unknown api\"}"
        exit 0
        ;;
    esac
fi

# ===== 非默认端口提示 =====
if [ "$KMS_PORT" != "1688" ]; then
    PORT_NOTE='<div class="note">当前端口为 <b>'"$KMS_PORT"'</b>，客户端命令必须带端口，例如 <code>slmgr /skms '"$SERVER_IP"':'"$KMS_PORT"'</code>。</div>'
else
    PORT_NOTE=""
fi

# ===== 页面 =====
echo "Content-Type: text/html; charset=utf-8"
echo ""
cat << HTML
<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>KMS Activator</title>
<style>
:root{
  --bg:#fafafa; --card:#fff; --line:#eaebee; --line2:#f2f3f5;
  --fg:#1b1f24; --fg2:#6b7280; --fg3:#9ca3af; --ac:#2f6feb;
  /* 命令区：深色是设计本意，两种主题下都保持深色 */
  --cmd-bg:#111827; --cmd-bd:#212c3b; --cmd-fg:#e6eaf2;
  --btn:#fff; --btn-h:#f6f7f8; --btn-fg:var(--fg); --btn-bd:var(--line);
  --nb:#f0e0b8; --nbg:#fffbf1; --nfg:#7a6220;
}
html[data-t=dark]{
  --bg:#0e1116; --card:#161b22; --line:#262d38; --line2:#1f2630;
  --fg:#e6eaf2; --fg2:#98a2b3; --fg3:#6b7280; --ac:#4f88ff;
  --cmd-bg:#0b0f16; --cmd-bd:#26303f;
  --btn:#1f2630; --btn-h:#273140; --btn-fg:#e6eaf2; --btn-bd:#2c3746;
  --nb:#4a3c14; --nbg:#211b0b; --nfg:#e3c878;
}
*{box-sizing:border-box}
body{
  margin:0;background:var(--bg);color:var(--fg);
  font:14px/1.65 -apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,"PingFang SC","Hiragino Sans GB","Microsoft YaHei",sans-serif;
  -webkit-font-smoothing:antialiased;
}
main{max-width:720px;margin:0 auto;padding:32px 20px 64px}
h1{font-size:19px;font-weight:600;margin:0}
.sub{color:var(--fg2);font-size:13px;margin:4px 0 0}
h2{font-size:14px;font-weight:600;margin:0 0 4px}
.panel{background:var(--card);border:1px solid var(--line);border-radius:10px;padding:18px 20px;margin-top:14px}
.hint{color:var(--fg2);font-size:13px;margin:0 0 14px}
code{font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,"Liberation Mono",monospace;font-size:12.5px}
.rowhead{display:flex;align-items:center;justify-content:space-between;gap:12px;flex-wrap:wrap}
.st{display:flex;align-items:center;gap:8px;font-size:14px}
.st #stTxt{min-width:5em}
.dot{width:7px;height:7px;border-radius:50%;background:var(--fg3);flex:none}
.dot.on{background:#16a34a}.dot.off{background:#d4d7dc}.dot.busy{background:#f59e0b}
.ops{display:flex;gap:8px}
.btn{font:inherit;font-size:13px;padding:6px 14px;border-radius:8px;border:1px solid var(--btn-bd);background:var(--btn);color:var(--btn-fg);cursor:pointer}
.btn:hover:not(:disabled){background:var(--btn-h)}
.btn.pri{background:var(--ac);border-color:var(--ac);color:#fff}
.btn.pri:hover:not(:disabled){background:#2560d8}
.btn:disabled{opacity:.45;cursor:default}
.meta{display:flex;flex-wrap:wrap;gap:4px 28px;margin:16px 0 0;padding:14px 0 0;border-top:1px solid var(--line2)}
.meta div{display:flex;gap:8px;align-items:baseline}
.meta dt{color:var(--fg2);font-size:12.5px;margin:0}
.meta dd{margin:0;font-size:13px}
.note{margin-top:14px;padding:10px 12px;border:1px solid var(--nb);background:var(--nbg);border-radius:8px;font-size:12.5px;color:var(--nfg)}
ol.steps{list-style:none;counter-reset:s;margin:0;padding:0}
ol.steps>li{counter-increment:s;display:grid;grid-template-columns:18px 1fr;column-gap:6px;margin:0 0 10px}
ol.steps>li:last-child{margin-bottom:0}
ol.steps>li::before{content:counter(s);grid-column:1;grid-row:1;color:var(--fg3);font-size:12px;line-height:20px;text-align:center}
.lb{grid-column:2;grid-row:1;color:var(--fg2);font-size:12.5px;margin:0 0 5px}
/* 深色命令块 */
.cmd{grid-column:2;grid-row:2;display:flex;align-items:center;justify-content:space-between;gap:12px;
     padding:9px 10px 9px 12px;border:1px solid var(--cmd-bd);border-radius:8px;background:var(--cmd-bg)}
.cmd code{flex:1;min-width:0;color:var(--cmd-fg);white-space:nowrap;overflow-x:auto}
.cmd code::-webkit-scrollbar{height:0}
.copy{border:1px solid #2c3a4e;background:#1c2635;color:#c3cddb;font:inherit;font-size:12px;line-height:1.5;
      padding:2px 10px;border-radius:6px;cursor:pointer;flex:none;min-width:3.6em;text-align:center}
.copy:hover{background:#25334a;color:#e6eaf2}
/* 右上角主题切换 */
.theme{display:flex;gap:2px;padding:2px;border:1px solid var(--line);border-radius:999px;background:var(--line2)}
.theme button{font:inherit;font-size:12px;line-height:1.5;padding:3px 11px;border:0;border-radius:999px;
              background:none;color:var(--fg2);cursor:pointer}
.theme button.on{background:var(--card);color:var(--fg);box-shadow:0 1px 2px rgba(0,0,0,.12)}
.tabs{display:flex;gap:18px;border-bottom:1px solid var(--line);margin-bottom:4px}
.tab{border:0;background:none;font:inherit;font-size:13px;color:var(--fg2);padding:8px 0;margin-bottom:-1px;border-bottom:2px solid transparent;cursor:pointer}
.tab.on{color:var(--fg);border-bottom-color:var(--ac)}
.tr{display:flex;justify-content:space-between;align-items:center;gap:16px;padding:8px 0;border-bottom:1px solid var(--line2);cursor:pointer}
.tr:last-child{border-bottom:0}
.tr span{color:var(--fg2);font-size:13px}
.tr code{color:var(--fg)}
.tr:hover code{color:var(--ac)}
.foot{color:var(--fg3);font-size:12px;margin-top:22px;text-align:center}
@media (max-width:520px){.cmd{flex-wrap:wrap;gap:8px}.cmd code{white-space:normal;word-break:break-all;flex:1 1 100%;order:2}.copy{order:1;margin-left:auto}.theme button{padding:3px 9px}}
</style>
<script>
/* 首屏绘制前就定好主题，避免刷新时白闪一下 */
(function(){
  var t='auto';
  try{t=localStorage.getItem('kms-theme')||'auto'}catch(e){}
  if(t!=='dark'&&t!=='light'){
    t=(window.matchMedia&&window.matchMedia('(prefers-color-scheme: dark)').matches)?'dark':'light';
  }
  document.documentElement.setAttribute('data-t',t);
})();
</script>
</head>
<body>
<main>

<header class="rowhead">
  <div>
    <h1>KMS Activator</h1>
    <p class="sub">局域网 KMS 激活服务 · vlmcsd</p>
  </div>
  <div class="theme" role="group" aria-label="主题">
    <button id="tAuto" onclick="setTheme('auto')">自动</button>
    <button id="tLight" onclick="setTheme('light')">浅色</button>
    <button id="tDark" onclick="setTheme('dark')">深色</button>
  </div>
</header>

<section class="panel">
  <div class="rowhead">
    <div class="st"><span class="dot" id="dot"></span><span id="stTxt">检测中…</span></div>
    <div class="ops">
      <button class="btn pri" id="btnStart" onclick="act('start')">启动</button>
      <button class="btn" id="btnStop" onclick="act('stop')" disabled>停止</button>
      <button class="btn" onclick="check()">刷新</button>
    </div>
  </div>
  <dl class="meta">
    <div><dt>地址</dt><dd><code>$SERVER_IP</code></dd></div>
  </dl>
  $PORT_NOTE
</section>

<section class="panel">
  <h2>激活 Windows</h2>
  <p class="hint">以管理员身份打开 CMD 或 PowerShell，依次执行。</p>
  <ol class="steps">
    <li><p class="lb">安装 GVLK 密钥</p><div class="cmd"><code>slmgr /ipk W269N-WFGWX-YVC9B-4J6C9-T83GX</code><button class="copy" onclick="cp(this)">复制</button></div></li>
    <li><p class="lb">指向 KMS 服务器</p><div class="cmd"><code>slmgr /skms $SERVER_IP</code><button class="copy" onclick="cp(this)">复制</button></div></li>
    <li><p class="lb">立即激活</p><div class="cmd"><code>slmgr /ato</code><button class="copy" onclick="cp(this)">复制</button></div></li>
    <li><p class="lb">查看状态</p><div class="cmd"><code>slmgr /xpr</code><button class="copy" onclick="cp(this)">复制</button></div></li>
  </ol>
</section>

<section class="panel">
  <h2>激活 Windows Server</h2>
  <p class="hint">Server 需用对应版本的 GVLK（下方密钥表 Server 分页可点复制），其余步骤与普通 Windows 相同。示例为 Server 2022 标准／数据中心版。</p>
  <ol class="steps">
    <li><p class="lb">安装 GVLK 密钥</p><div class="cmd"><code>slmgr /ipk WX4NM-KYWYW-QJJR4-XV3QB-6VM33</code><button class="copy" onclick="cp(this)">复制</button></div></li>
    <li><p class="lb">指向 KMS 服务器</p><div class="cmd"><code>slmgr /skms $SERVER_IP</code><button class="copy" onclick="cp(this)">复制</button></div></li>
    <li><p class="lb">立即激活</p><div class="cmd"><code>slmgr /ato</code><button class="copy" onclick="cp(this)">复制</button></div></li>
    <li><p class="lb">查看状态</p><div class="cmd"><code>slmgr /xpr</code><button class="copy" onclick="cp(this)">复制</button></div></li>
  </ol>
  <div class="note">Server 走 KMS 需 Standard / Datacenter / Solutions 等批量许可（VOL）版本，Retail / OEM 版换 GVLK 也激活不了。若提示 <code>0xC004F074</code>，一般是客户端连不上 KMS——先在客户机用 PowerShell <code>Test-NetConnection $SERVER_IP -Port $KMS_PORT</code> 确认端口可达。</div>
</section>

<section class="panel">
  <h2>激活 Office</h2>
  <p class="hint">以管理员身份打开 CMD，先进入 Office 目录（如 <code>cd "C:\Program Files\Microsoft Office\Office16"</code>），再执行。</p>
  <ol class="steps">
    <li><p class="lb">指向 KMS 服务器</p><div class="cmd"><code>cscript ospp.vbs /sethst:$SERVER_IP</code><button class="copy" onclick="cp(this)">复制</button></div></li>
    <li><p class="lb">立即激活</p><div class="cmd"><code>cscript ospp.vbs /act</code><button class="copy" onclick="cp(this)">复制</button></div></li>
    <li><p class="lb">查看状态</p><div class="cmd"><code>cscript ospp.vbs /dstatus</code><button class="copy" onclick="cp(this)">复制</button></div></li>
  </ol>
</section>

<section class="panel">
  <h2>GVLK 密钥</h2>
  <p class="hint">点击任意一行即可复制密钥。</p>
  <div class="tabs">
    <button class="tab on" onclick="tab(this,'win')">Windows 10/11</button>
    <button class="tab" onclick="tab(this,'office')">Office</button>
    <button class="tab" onclick="tab(this,'server')">Server</button>
  </div>
  <div id="tab-win">
    <div class="tr" onclick="k(this)"><span>Pro</span><code>W269N-WFGWX-YVC9B-4J6C9-T83GX</code></div>
    <div class="tr" onclick="k(this)"><span>Enterprise</span><code>NPPR9-FWDCX-D2C8J-H872K-2YT43</code></div>
    <div class="tr" onclick="k(this)"><span>LTSC 2021</span><code>M7XTQ-FN8P6-TTKYV-9D4CC-J462D</code></div>
  </div>
  <div id="tab-office" style="display:none">
    <div class="tr" onclick="k(this)"><span>Office 2016 Pro Plus</span><code>XQNVK-8JYDB-WJ9W3-YJ8YR-WFG99</code></div>
    <div class="tr" onclick="k(this)"><span>Office 2013 Pro Plus</span><code>YC7DK-G2NP3-2QQC3-J6H88-GVGXT</code></div>
    <div class="tr" onclick="k(this)"><span>Visio 2016 Pro</span><code>PD3PC-RHNGV-FXJ29-8JK7D-RJRJK</code></div>
    <div class="tr" onclick="k(this)"><span>Project 2016 Pro</span><code>YG9NW-3K39V-2T3HJ-93F3Q-G83KT</code></div>
  </div>
  <div id="tab-server" style="display:none">
    <div class="tr" onclick="k(this)"><span>Server 2025 Standard</span><code>TV6PM-K4C26-2VWVC-WWY7J-6Y6F4</code></div>
    <div class="tr" onclick="k(this)"><span>Server 2025 Datacenter</span><code>X6NR7-D6C6C-2VDT2-7J2B6-W3P47</code></div>
    <div class="tr" onclick="k(this)"><span>Server 2022</span><code>WX4NM-KYWYW-QJJR4-XV3QB-6VM33</code></div>
    <div class="tr" onclick="k(this)"><span>Server 2019</span><code>WMDGN-G9PQG-XVVXX-R3X43-63DFG</code></div>
    <div class="tr" onclick="k(this)"><span>Server 2016</span><code>WC2BQ-8NRM3-FDDYY-2BFGV-KHKQY</code></div>
  </div>
</section>

<p class="foot">由 vlmcsd 提供 · 仅供本地网络技术研究学习使用，请支持正版</p>

</main>
<script>
var API='/cgi/ThirdParty/KmsActivator/index.cgi/api';
var busy=false,known=false;

function check(silent){
  var dot=document.getElementById('dot'),t=document.getElementById('stTxt');
  var s=document.getElementById('btnStart'),b=document.getElementById('btnStop');
  /* 只有首屏（或用户主动点刷新）才显示「检测中…」；后台轮询保持原文案，避免每 8 秒闪一下 */
  if(!busy && !(silent && known)){dot.className='dot busy';t.textContent='检测中…';}
  fetch(API+'/status').then(function(r){return r.json()}).then(function(d){
    known=true;
    if(d.status==='running'){
      dot.className='dot on';t.textContent='运行中';
      s.style.display='none';b.style.display='';b.disabled=false;
    }else{
      dot.className='dot off';t.textContent='已停止';
      s.style.display='';s.disabled=false;b.style.display='none';
    }
    busy=false;
  }).catch(function(){
    /* 后台轮询失败先当噪声：保持上一次状态，下一轮再校正 */
    if(!(silent && known)){dot.className='dot off';t.textContent='无法连接';s.disabled=false;b.disabled=true;}
    busy=false;
  });
}

function act(a){
  busy=true;
  var t=document.getElementById('stTxt');
  document.getElementById('btnStart').disabled=true;
  document.getElementById('btnStop').disabled=true;
  t.textContent=(a==='start'?'正在启动…':'正在停止…');
  fetch(API+'/'+a,{method:'POST'}).then(function(r){return r.json()}).then(function(d){
    if(d.note){t.textContent=d.note;busy=true;setTimeout(function(){check(true);},1600);}
    else{setTimeout(function(){check(true);},d.status==='ok'?900:400);}
  }).catch(function(){setTimeout(function(){check(true);},400)});
}

function cp(btn){
  var code=btn.parentNode.querySelector('code');
  txt(code.textContent,btn);
}
function k(row){
  var code=row.querySelector('code');
  var old=row.querySelector('.copy');
  txt(code.textContent,null,row);
}
function txt(s,btn,row){
  function done(){
    if(btn){var o=btn.textContent;btn.textContent='已复制';setTimeout(function(){btn.textContent=o},1200);}
    else if(row){var c=row.querySelector('span');var o=c.textContent;c.textContent='已复制';setTimeout(function(){c.textContent=o},1200);}
  }
  if(navigator.clipboard&&navigator.clipboard.writeText){navigator.clipboard.writeText(s).then(done,done);}
  else{
    var ta=document.createElement('textarea');ta.value=s;document.body.appendChild(ta);
    ta.select();try{document.execCommand('copy')}catch(e){}document.body.removeChild(ta);done();
  }
}

function tab(btn,name){
  var ts=document.querySelectorAll('.tab');
  for(var i=0;i<ts.length;i++)ts[i].className='tab';
  btn.className='tab on';
  var ids=['win','office','server'];
  for(var j=0;j<ids.length;j++)document.getElementById('tab-'+ids[j]).style.display=(ids[j]===name?'':'none');
}

/* ===== 主题切换（auto 跟随系统，选择记在本地） ===== */
function isDark(t){
  return t==='dark' || (t==='auto' && !!(window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches));
}
function applyTheme(){
  var t=curTheme();
  document.documentElement.setAttribute('data-t', isDark(t)?'dark':'light');
  var map={auto:'tAuto',light:'tLight',dark:'tDark'};
  for(var k in map){
    var b=document.getElementById(map[k]);
    if(b) b.className=(k===t?'on':'');
  }
}
function curTheme(){
  var t='auto';
  try{t=localStorage.getItem('kms-theme')||'auto'}catch(e){}
  return (t==='light'||t==='dark')?t:'auto';
}
function setTheme(t){
  try{localStorage.setItem('kms-theme',t)}catch(e){}
  applyTheme();
}
if(window.matchMedia){
  var mq=window.matchMedia('(prefers-color-scheme: dark)');
  if(mq.addEventListener) mq.addEventListener('change',applyTheme);
  else if(mq.addListener) mq.addListener(applyTheme);
}
applyTheme();

check();
setInterval(function(){check(true);},8000);
</script>
</body>
</html>
HTML
