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

# ===== 本机 IP 自动检测 =====
# 优先用 nginx CGI 环境变量 SERVER_ADDR，否则 ip route 探测
SERVER_IP="${SERVER_ADDR:-}"
if [ -z "$SERVER_IP" ] || [ "$SERVER_IP" = "127.0.0.1" ] || [ "$SERVER_IP" = "::1" ]; then
    SERVER_IP="$(ip route get 1 2>/dev/null | grep -oP 'src \K[\d.]+')"
fi
SERVER_IP="${SERVER_IP:-127.0.0.1}"

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

# ===== 读取 POST 数据 =====
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

# ===== API 响应 =====
if [ "$ACTION" = "api" ]; then
    echo "Content-Type: application/json; charset=utf-8"
    echo ""

    SUB="${REL#*/}"  # api/status → status
    case "$SUB" in
    "status")
        pid=$(vlmcsd_pid)
        if [ -n "$pid" ]; then
            echo "{\"status\":\"running\",\"pid\":$pid}"
        else
            echo "{\"status\":\"stopped\"}"
        fi
        exit 0
        ;;
    "start")
        "${APPDEST}/vlmcsd" -p "$PID_FILE" -L "0.0.0.0:11688" -e > /dev/null 2>&1
        sleep 1
        if vlmcsd_pid > /dev/null; then
            # 尝试 iptables 转发
            command -v iptables &>/dev/null && \
                iptables -t nat -C PREROUTING -p tcp --dport 1688 -j REDIRECT --to-port 11688 2>/dev/null || \
                iptables -t nat -A PREROUTING -p tcp --dport 1688 -j REDIRECT --to-port 11688 > /dev/null 2>&1 || true
            echo "{\"status\":\"ok\"}"
        else
            echo "{\"status\":\"error\",\"message\":\"启动失败\"}"
        fi
        exit 0
        ;;
    "stop")
        pid=$(vlmcsd_pid)
        if [ -n "$pid" ]; then
            kill -TERM "$pid" 2>/dev/null
            sleep 1
            kill -KILL "$pid" 2>/dev/null
            sleep 1
        fi
        command -v iptables &>/dev/null && \
            iptables -t nat -C PREROUTING -p tcp --dport 1688 -j REDIRECT --to-port 11688 2>/dev/null && \
            iptables -t nat -D PREROUTING -p tcp --dport 1688 -j REDIRECT --to-port 11688 > /dev/null 2>&1 || true
        echo "{\"status\":\"ok\"}"
        exit 0
        ;;
    *)
        echo "{\"status\":\"error\",\"message\":\"unknown api\"}"
        exit 0
        ;;
    esac
fi

# ===== HTML 静态页面 =====
echo "Content-Type: text/html; charset=utf-8"
echo ""
cat << HTML
<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>KMS Activator - KMS 激活服务</title>
<style>
*{margin:0;padding:0;box-sizing:border-box}
body{font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,sans-serif;background:#f0f2f5;color:#333;line-height:1.6}
.header{background:linear-gradient(135deg,#1a73e8,#0d47a1);color:#fff;padding:28px 32px}
.header h1{font-size:26px;font-weight:600}
.header p{opacity:.85;margin-top:4px;font-size:14px}
.container{max-width:860px;margin:20px auto;padding:0 16px}

.card{background:#fff;border-radius:12px;padding:24px;margin-bottom:16px;box-shadow:0 1px 3px rgba(0,0,0,.08)}
.card-title{font-size:16px;font-weight:600;margin-bottom:16px;display:flex;align-items:center;gap:8px}

.status-bar{display:flex;align-items:center;gap:12px;padding:12px 16px;border-radius:8px;font-size:15px}
.status-bar.running{background:#e8f5e9;color:#2e7d32}
.status-bar.stopped{background:#ffebee;color:#c62828}
.status-bar.loading{background:#fff3e0;color:#e65100}
.dot{width:10px;height:10px;border-radius:50%;display:inline-block}
.dot.running{background:#4caf50;box-shadow:0 0 6px rgba(76,175,80,.6)}
.dot.stopped{background:#f44336;box-shadow:0 0 6px rgba(244,67,54,.6)}
.dot.loading{background:#ff9800;animation:pulse 1s infinite}
@keyframes pulse{0%,100%{opacity:1}50%{opacity:.4}}

.btn-group{display:flex;gap:8px;margin-top:12px;flex-wrap:wrap}
.btn{padding:8px 20px;border:none;border-radius:8px;font-size:14px;font-weight:500;cursor:pointer;transition:all .2s}
.btn-primary{background:#1a73e8;color:#fff}
.btn-primary:hover{background:#1557b0}
.btn-danger{background:#f44336;color:#fff}
.btn-danger:hover{background:#d32f2f}
.btn-outline{background:transparent;border:1px solid #dadce0;color:#5f6368}
.btn-outline:hover{border-color:#1a73e8;color:#1a73e8}
.btn:disabled{opacity:.5;cursor:not-allowed}

.step{margin-bottom:20px}
.step:last-child{margin-bottom:0}
.step-num{display:inline-flex;width:24px;height:24px;border-radius:50%;background:#1a73e8;color:#fff;align-items:center;justify-content:center;font-size:13px;font-weight:600;margin-right:8px;flex-shrink:0}
.step-title{font-weight:500;margin-bottom:6px;display:flex;align-items:center}
.code-block{background:#1e293b;color:#e2e8f0;border-radius:8px;padding:14px 16px;font-family:"SF Mono","Fira Code","Consolas",monospace;font-size:13px;overflow-x:auto;margin:6px 0;position:relative}
.copy-btn{position:absolute;top:8px;right:8px;background:rgba(255,255,255,.1);border:1px solid rgba(255,255,255,.15);color:#94a3b8;border-radius:4px;padding:2px 8px;font-size:11px;cursor:pointer}
.copy-btn:hover{background:rgba(255,255,255,.2);color:#e2e8f0}

.gvlk-grid{display:grid;grid-template-columns:repeat(auto-fill,minmax(280px,1fr));gap:12px;margin-top:8px}
.gvlk-item{background:#f8f9fa;border-radius:8px;padding:12px}
.gvlk-item .product{font-weight:500;font-size:13px;color:#1a73e8}
.gvlk-item .key{font-family:monospace;font-size:12px;color:#5f6368;word-break:break-all;margin-top:2px}

.tabs{display:flex;gap:8px;margin-bottom:12px;flex-wrap:wrap}
.tab{padding:6px 16px;border-radius:20px;border:1px solid #dadce0;background:transparent;color:#5f6368;cursor:pointer;font-size:13px;transition:all .2s}
.tab.active{background:#1a73e8;color:#fff;border-color:#1a73e8}
.tab:hover:not(.active){border-color:#1a73e8;color:#1a73e8}
.tab-content{display:none}
.tab-content.active{display:block}

footer{text-align:center;padding:24px;color:#9aa0a6;font-size:13px}
footer a{color:#1a73e8;text-decoration:none}
</style>
</head>
<body>
<div class="header">
  <h1>KMS Activator</h1>
  <p>基于 vlmcsd 的局域网 KMS 激活服务｜适用于 Windows / Office 批量激活</p>
</div>
<div class="container">

  <!-- 状态 -->
  <div class="card">
    <div class="card-title">🔌 服务状态</div>
    <div id="statusBar" class="status-bar loading">
      <span class="dot loading" id="statusDot"></span>
      <span id="statusText">检测中...</span>
    </div>
    <div style="font-size:13px;color:#666;margin-top:8px" id="serverInfo">服务器地址: $SERVER_IP</div>
    <div class="btn-group" id="actionBtns">
      <button class="btn btn-primary" id="btnStart" onclick="doAction('start')">启动</button>
      <button class="btn btn-danger" id="btnStop" onclick="doAction('stop')" disabled>停止</button>
      <button class="btn btn-outline" onclick="checkStatus()">刷新</button>
    </div>
  </div>

  <!-- 激活 Windows -->
  <div class="card">
    <div class="card-title">🪟 激活 Windows</div>
    <p style="font-size:14px;color:#666;margin-bottom:12px">以管理员身份运行 CMD 或 PowerShell，依次执行：</p>

    <div class="step">
      <div class="step-title"><span class="step-num">1</span>安装 GVLK 密钥</div>
      <div class="code-block" id="gvlkWinCode">slmgr /ipk W269N-WFGWX-YVC9B-4J6C9-T83GX<button class="copy-btn" onclick="copyText(this)">复制</button></div>
    </div>
    <div class="step">
      <div class="step-title"><span class="step-num">2</span>设置 KMS 服务器</div>
      <div class="code-block" id="setServerCode">slmgr /skms $SERVER_IP<button class="copy-btn" onclick="copyText(this)">复制</button></div>
    </div>
    <div class="step">
      <div class="step-title"><span class="step-num">3</span>立即激活</div>
      <div class="code-block">slmgr /ato<button class="copy-btn" onclick="copyText(this)">复制</button></div>
    </div>
    <div class="step">
      <div class="step-title"><span class="step-num">4</span>验证激活状态</div>
      <div class="code-block">slmgr /xpr<button class="copy-btn" onclick="copyText(this)">复制</button></div>
    </div>
  </div>

  <!-- 激活 Windows Server -->
  <div class="card">
    <div class="card-title">🖥️ 激活 Windows Server</div>
    <p style="font-size:14px;color:#666;margin-bottom:12px">以管理员身份运行 CMD 或 PowerShell，依次执行：</p>
    <div class="step">
      <div class="step-title"><span class="step-num">1</span>安装 GVLK 密钥</div>
      <div class="code-block">slmgr /ipk WX4NM-KYWYW-QJJR4-XV3QB-6VM33<button class="copy-btn" onclick="copyText(this)">复制</button></div>
    </div>
    <div class="step">
      <div class="step-title"><span class="step-num">2</span>设置 KMS 服务器</div>
      <div class="code-block" id="setServerCodeSrv">slmgr /skms $SERVER_IP<button class="copy-btn" onclick="copyText(this)">复制</button></div>
    </div>
    <div class="step">
      <div class="step-title"><span class="step-num">3</span>立即激活</div>
      <div class="code-block">slmgr /ato<button class="copy-btn" onclick="copyText(this)">复制</button></div>
    </div>
    <div class="step">
      <div class="step-title"><span class="step-num">4</span>验证激活状态</div>
      <div class="code-block">slmgr /xpr<button class="copy-btn" onclick="copyText(this)">复制</button></div>
    </div>
    <p style="font-size:13px;color:#999;margin-top:12px;border-top:1px solid #eee;padding-top:12px">💡 Windows Server 的 GVLK 密钥根据版本不同，请参考下方「产品 GVLK 密钥」中 Server 标签页选择对应密钥替换步骤 1 中的密钥。</p>
  </div>

  <!-- 激活 Office -->
  <div class="card">
    <div class="card-title">📊 激活 Office</div>
    <p style="font-size:14px;color:#666;margin-bottom:12px">以管理员身份运行 CMD，先进入 Office 目录，再执行：</p>
    <div class="step">
      <div class="step-title"><span class="step-num">1</span>进入 Office 目录 (64位 Office 2016+)</div>
      <div class="code-block">cd "C:\Program Files\Microsoft Office\Office16"<button class="copy-btn" onclick="copyText(this)">复制</button></div>
    </div>
    <div class="step">
      <div class="step-title"><span class="step-num">2</span>设置 KMS 服务器</div>
      <div class="code-block" id="setOfficeServer">cscript ospp.vbs /sethst:$SERVER_IP<button class="copy-btn" onclick="copyText(this)">复制</button></div>
    </div>
    <div class="step">
      <div class="step-title"><span class="step-num">3</span>立即激活</div>
      <div class="code-block">cscript ospp.vbs /act<button class="copy-btn" onclick="copyText(this)">复制</button></div>
    </div>
    <div class="step">
      <div class="step-title"><span class="step-num">4</span>验证激活状态</div>
      <div class="code-block">cscript ospp.vbs /dstatus<button class="copy-btn" onclick="copyText(this)">复制</button></div>
    </div>
  </div>

  <!-- GVLK 密钥 -->
  <div class="card">
    <div class="card-title">🔑 产品 GVLK 密钥</div>
    <div class="tabs">
      <button class="tab active" onclick="switchTab(this,'win')">Windows 10/11</button>
      <button class="tab" onclick="switchTab(this,'office')">Office</button>
      <button class="tab" onclick="switchTab(this,'server')">Server</button>
    </div>
    <div id="tab-win" class="tab-content active">
      <div class="gvlk-grid">
        <div class="gvlk-item"><div class="product">Windows 10/11 Pro</div><div class="key">W269N-WFGWX-YVC9B-4J6C9-T83GX</div></div>
        <div class="gvlk-item"><div class="product">Windows 10/11 Enterprise</div><div class="key">NPPR9-FWDCX-D2C8J-H872K-2YT43</div></div>
        <div class="gvlk-item"><div class="product">Windows 10 LTSC 2021</div><div class="key">M7XTQ-FN8P6-TTKYV-9D4CC-J462D</div></div>
      </div>
    </div>
    <div id="tab-office" class="tab-content">
      <div class="gvlk-grid">
        <div class="gvlk-item"><div class="product">Office 2016 Pro Plus</div><div class="key">XQNVK-8JYDB-WJ9W3-YJ8YR-WFG99</div></div>
        <div class="gvlk-item"><div class="product">Visio 2016 Pro</div><div class="key">PD3PC-RHNGV-FXJ29-8JK7D-RJRJK</div></div>
        <div class="gvlk-item"><div class="product">Project 2016 Pro</div><div class="key">YG9NW-3K39V-2T3HJ-93F3Q-G83KT</div></div>
        <div class="gvlk-item"><div class="product">Office 2013 Pro Plus</div><div class="key">YC7DK-G2NP3-2QQC3-J6H88-GVGXT</div></div>
      </div>
    </div>
    <div id="tab-server" class="tab-content">
      <div class="gvlk-grid">
        <div class="gvlk-item"><div class="product">Windows Server 2022</div><div class="key">WX4NM-KYWYW-QJJR4-XV3QB-6VM33</div></div>
        <div class="gvlk-item"><div class="product">Windows Server 2019</div><div class="key">WMDGN-G9PQG-XVVXX-R3X43-63DFG</div></div>
        <div class="gvlk-item"><div class="product">Windows Server 2016</div><div class="key">WC2BQ-8NRM3-FDDYY-2BFGV-KHKQY</div></div>
        <div class="gvlk-item"><div class="product">Windows Server 2025 Datacenter</div><div class="key">X6NR7-D6C6C-2VDT2-7J2B6-W3P47</div></div>
        <div class="gvlk-item"><div class="product">Windows Server 2025 Standard</div><div class="key">TV6PM-K4C26-2VWVC-WWY7J-6Y6F4</div></div>
      </div>
    </div>
  </div>

</div>
<footer>
  KMS 服务由 vlmcsd 提供技术支持 | 页面仅供技术学习参考，请支持正版软件
</footer>

<script>
// API 使用绝对路径
const API = '/cgi/ThirdParty/KmsActivator/index.cgi/api';

function checkStatus() {
  const bar=document.getElementById('statusBar');
  const dot=document.getElementById('statusDot');
  const text=document.getElementById('statusText');
  const sinfo=document.getElementById('serverInfo');
  const btnStart=document.getElementById('btnStart');
  const btnStop=document.getElementById('btnStop');

  bar.className='status-bar loading';
  dot.className='dot loading';
  text.textContent='检测中...';

  fetch(API+'/status')
    .then(r=>r.json())
    .then(d=>{
      if(d.status==='running'){
        bar.className='status-bar running';
        dot.className='dot running';
        text.textContent='运行中';
        btnStart.disabled=true;
        btnStop.disabled=false;
      }else{
        bar.className='status-bar stopped';
        dot.className='dot stopped';
        text.textContent='已停止';
        btnStart.disabled=false;
        btnStop.disabled=true;
      }
    })
    .catch(()=>{
      bar.className='status-bar stopped';
      dot.className='dot stopped';
      text.textContent='无法连接';
      btnStart.disabled=false;
      btnStop.disabled=true;
    });
}

function doAction(action) {
  document.getElementById('btnStart').disabled=true;
  document.getElementById('btnStop').disabled=true;
  document.getElementById('statusText').textContent='操作中...';

  fetch(API+'/'+action,{method:'POST'})
    .then(r=>r.json())
    .then(d=>{ if(d.status==='ok') setTimeout(checkStatus,1000); else setTimeout(checkStatus,500); })
    .catch(()=>{ setTimeout(checkStatus,500); });
}

function copyText(btn) {
  const code=btn.parentElement;
  const text=code.textContent.replace('复制','').trim();
  navigator.clipboard.writeText(text).then(()=>{
    btn.textContent='已复制';
    setTimeout(()=>{btn.textContent='复制';},1500);
  }).catch(()=>{
    const range=document.createRange();
    range.selectNodeContents(code);
    const sel=window.getSelection();
    sel.removeAllRanges();sel.addRange(range);
    document.execCommand('copy');
    sel.removeAllRanges();
    btn.textContent='已复制';
    setTimeout(()=>{btn.textContent='复制';},1500);
  });
}

function switchTab(el,id) {
  document.querySelectorAll('.tab').forEach(t=>t.classList.remove('active'));
  document.querySelectorAll('.tab-content').forEach(t=>t.classList.remove('active'));
  el.classList.add('active');
  document.getElementById('tab-'+id).classList.add('active');
}

// 页面加载时检测状态
checkStatus();
// 每10秒自动刷新
setInterval(checkStatus,10000);
</script>
</body>
</html>
HTML
exit 0
