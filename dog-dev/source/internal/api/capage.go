package api

import (
	"encoding/base64"
	"fmt"
	"html"
	"net"
	"net/http"
	"strings"

	qrcode "github.com/skip2/go-qrcode"

	"github.com/ghpp/ghpp/internal/certtrack"
	"github.com/ghpp/ghpp/web"
)

// handleCAPage 提供局域网设备自助安装根证书的向导页。
//
// 这页刻意做成公开、免登录、纯静态渲染：
// 手机第一次打开时还没有任何账号，装证书这件事不该被登录挡住。
// 页面只暴露本机 CA 的公开信息（证书、指纹、下载地址）与「本机自己的」
// 信任状态，不列出其他设备，避免把局域网拓扑泄露出去。
func (s *Server) handleCAPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	w.Header().Set("Pragma", "no-cache")

	ca := s.app.CA()
	if ca == nil {
		_, _ = w.Write([]byte(caPageMessage("根证书不可用", fmt.Sprintf("本机 CA 初始化失败：%v", s.app.CAError()))))
		return
	}

	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		scheme = proto
	}
	host := r.Host
	if host == "" {
		host = "127.0.0.1:37717"
	}
	// 经飞牛统一网关进入时，下载地址必须带上 /app/<appname> 前缀，
	// 否则浏览器会去 NAS 根路径取证书（404）。
	base := scheme + "://" + host + web.PrefixFrom(r.Context())
	pemURL := base + "/api/cert/download"
	derURL := base + "/api/cert/download?format=der"
	// 免证书下载走的是「代理端口」（与执行加速的端口一致），
	// 不是控制台端口——两者写错会给出一个根本打不开的地址。
	bareURL := "http://" + hostOnly(host) + ":" + s.proxyPort()

	_, _ = w.Write([]byte(renderCAPage(calInstallPageData{
		Host:        host,
		BareURL:     bareURL,
		PEMURL:      pemURL,
		DERURL:      derURL,
		QR:          qrDataURI(pemURL),
		Fingerprint: ca.Fingerprint(),
		NotAfter:    ca.NotAfter().Format("2006-01-02"),
		State:       deviceState(r),
		SystemTrust: s.app.CAInSystemTrust(),
	})))
}

// hostOnly 去掉 host:port 里的端口。
func hostOnly(host string) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		return h
	}
	return host
}

// proxyPort 返回代理监听端口（免证书下载地址与 PAC 都用它）。
func (s *Server) proxyPort() string {
	if cfg := s.app.CurrentConfig(); cfg != nil {
		if _, port, err := net.SplitHostPort(cfg.Proxy.Listen); err == nil && port != "" {
			return port
		}
	}
	return "37710"
}

// deviceState 判断「发起这次访问的设备」当前的证书信任状态。
func deviceState(r *http.Request) calDeviceState {
	ip := directClientIP(r)
	entry, ok := certtrack.Lookup(ip)
	switch {
	case ok && entry.Trusted:
		return calDeviceState{Kind: "ok", IP: ip}
	case ok && entry.Failures > 0:
		return calDeviceState{Kind: "untrusted", IP: ip, Hosts: entry.Hosts, Failures: entry.Failures}
	default:
		return calDeviceState{Kind: "unknown", IP: ip}
	}
}

// directClientIP 取请求来源 IP（去掉端口）。
func directClientIP(r *http.Request) string {
	host := r.RemoteAddr
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.String()
	}
	return host
}

// handleCertClients 返回局域网各设备的证书信任状态。
func (s *Server) handleCertClients(w http.ResponseWriter, r *http.Request) {
	writeOK(w, map[string]any{
		"clients": certtrack.List(),
		"request": directClientIP(r),
	})
}

type calDeviceState struct {
	Kind     string
	IP       string
	Hosts    []string
	Failures int
}

type calInstallPageData struct {
	Host        string
	BareURL     string
	PEMURL      string
	DERURL      string
	QR          string
	Fingerprint string
	NotAfter    string
	State       calDeviceState
	SystemTrust bool
}

// qrDataURI 把下载地址编码成 PNG 二维码的 data URI。
func qrDataURI(text string) string {
	png, err := qrcode.Encode(text, qrcode.Medium, 512)
	if err != nil {
		return ""
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
}

func caPageMessage(title, detail string) string {
	return `<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">` +
		`<body style="font:16px/1.6 -apple-system,system-ui,sans-serif;padding:24px">` +
		`<h1 style="font-size:18px">` + html.EscapeString(title) + `</h1>` +
		`<p style="color:#666">` + html.EscapeString(detail) + `</p></body>`
}

func renderCAPage(d calInstallPageData) string {
	const tpl = `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1,viewport-fit=cover">
<meta name="robots" content="noindex">
<title>证书安装向导 · Dog-dev</title>
<style>
:root{
  --bg:#f5f6f8; --card:#ffffff; --line:#e6e8ee; --fg:#1b2330; --fg2:#6b7688;
  --accent:#3b82f6; --ok:#16a34a; --warn:#d97706; --code:#f2f4f8;
}
@media (prefers-color-scheme: dark){
  :root{ --bg:#0f1420; --card:#161c28; --line:#242f41; --fg:#e6eaf2; --fg2:#9aa6b8;
         --accent:#4f8bff; --ok:#34d399; --warn:#fbbf24; --code:#0b111c; }
}
*{box-sizing:border-box}
body{margin:0;padding:20px 16px calc(28px + env(safe-area-inset-bottom));
     background:var(--bg);color:var(--fg);
     font:15px/1.65 -apple-system,BlinkMacSystemFont,"PingFang SC","Microsoft YaHei",system-ui,sans-serif}
.wrap{max-width:560px;margin:0 auto}
h1{font-size:19px;margin:0 0 4px}
.sub{color:var(--fg2);font-size:13px;margin:0 0 16px}
.card{background:var(--card);border:1px solid var(--line);border-radius:14px;padding:16px;margin-bottom:12px}
.state{display:flex;gap:10px;align-items:flex-start}
.dot{width:9px;height:9px;border-radius:50%;margin-top:7px;flex:0 0 auto}
.ok .dot{background:var(--ok)} .untrusted .dot{background:var(--warn)} .unknown .dot{background:var(--fg2)}
.state b{font-size:15px}
.state p{margin:4px 0 0;color:var(--fg2);font-size:13px}
.qr{display:flex;gap:16px;align-items:center;flex-wrap:wrap}
.qr img{width:132px;height:132px;border-radius:10px;background:#fff;padding:6px}
.qr .hint{flex:1;min-width:150px;font-size:13px;color:var(--fg2)}
.btns{display:flex;gap:10px;margin-top:14px;flex-wrap:wrap}
a.btn{flex:1 1 140px;text-align:center;text-decoration:none;padding:12px 14px;border-radius:11px;
      font-weight:600;font-size:14px;border:1px solid transparent}
a.primary{background:var(--accent);color:#fff}
a.ghost{background:transparent;color:var(--fg);border-color:var(--line)}
.fp{margin-top:12px;font-size:12px;color:var(--fg2);word-break:break-all}
.fp code{background:var(--code);padding:2px 5px;border-radius:5px;font-size:11.5px}
h2{font-size:14px;margin:0 0 10px}
details{border-top:1px solid var(--line);padding:10px 0}
details:first-of-type{border-top:0}
summary{cursor:pointer;font-weight:600;font-size:14px;list-style:none}
summary::-webkit-details-marker{display:none}
summary::before{content:"▸ ";color:var(--fg2)}
details[open] summary::before{content:"▾ "}
ol{padding-left:20px;margin:10px 0 0;font-size:13.5px;color:var(--fg2)}
li{margin:5px 0}
b.mark{color:var(--fg)}
code{background:var(--code);padding:1px 5px;border-radius:4px;font-size:12.5px;word-break:break-all}
.tip{font-size:12.5px;color:var(--fg2);margin-top:10px}
</style>
</head>
<body><div class="wrap">
<h1>证书安装向导</h1>
<p class="sub">Dog-dev 通过本地根证书解密 GitHub 流量再走加速通道。装一次，这台设备以后都免配。</p>

<div class="card state @STATE_CLASS@">
  <span class="dot"></span>
  <div><b>@STATE_TITLE@</b><p>@STATE_DESC@</p></div>
</div>

<div class="card">
  <div class="qr">
    <img alt="下载二维码" src="@QR@">
    <div class="hint">
      手机扫这个二维码，直接下载根证书；<br>
      也可以点下面的按钮手动下载。
    </div>
  </div>
  <div class="btns">
    <a class="btn primary" href="@PEM_URL@">下载证书 .crt</a>
    <a class="btn ghost" href="@DER_URL@">下载 .cer（DER）</a>
  </div>
  <div class="fp">证书：Dog-dev 本地加速根证书　有效期至 @NOT_AFTER@<br>
    SHA-256：<code>@FINGERPRINT@</code></div>
</div>

<div class="card">
  <h2>安装步骤</h2>
  <details open>
    <summary>Android（含 Edge / Chrome / 微信）</summary>
    <ol>
      <li>先确认手机已设置<b class="mark">锁屏密码/图案</b>，否则系统会静默拒绝安装。</li>
      <li>「设置 → 安全（或 密码与安全）→ 更多安全设置 → 加密与凭据 → 安装证书」。</li>
      <li>选择 <b class="mark">CA 证书</b>（不是「VPN 和应用用户证书」，选错浏览器不认）。</li>
      <li>选刚下载的 <code>ghpp-ca.crt</code>，看清「你的数据可能被监视」后点「仍然安装」。</li>
      <li>验证：设置里「受信任的凭据 → 用户」能看到 <b class="mark">Dog-dev 本地加速根证书</b>。</li>
      <li><b class="mark">把浏览器从后台划掉再打开</b>（只点刷新不够）。</li>
    </ol>
  </details>
  <details>
    <summary>iPhone / iPad</summary>
    <ol>
      <li>用 Safari 打开本页并下载证书（Safari 才会写入「已下载描述文件」）。</li>
      <li>「设置 → 已下载描述文件 → 安装」，输入锁屏密码。</li>
      <li>再到「设置 → 通用 → 关于本机 → 证书信任设置」，把 Dog-dev 证书的开关<b class="mark">打开</b>。</li>
    </ol>
  </details>
  <details>
    <summary>Windows / 鸿蒙 PC</summary>
    <ol>
      <li>下载 <code>ghpp-ca.crt</code>，双击 →「安装证书」。</li>
      <li>存储位置选「<b class="mark">本地计算机</b>」，下一步选「将所有证书都放入下列存储」。</li>
      <li>浏览 → 选「<b class="mark">受信任的根证书颁发机构</b>」→ 完成 → 安全警告点「是」。</li>
    </ol>
  </details>
  <p class="tip">装完自检：浏览器打开 <b class="mark">https://github.com</b>，能正常显示页面 = 成功。
  若仍提示「连接不是专用连接」，说明证书没进 CA 库，按上面的第 3 步核对。</p>
</div>

<div class="card">
  <h2>不想装证书？</h2>
  <p class="tip" style="margin-top:0">GitHub 的网页在中间必须解密才能加速，所以浏览器必须信任本机证书。
  若只是<b class="mark">下载文件</b>，可以完全不装证书，用下面这种地址（把原链接整段接在代理地址后）：</p>
  <p class="tip"><code>@BASE@/https://github.com/用户/仓库/releases/download/...</code></p>
  <p class="tip">用完后想撤销信任：Android「受信任的凭据 → 用户 → 点证书 → 删除」；iOS「描述文件」里移除并关闭证书信任开关。</p>
</div>

<p class="sub" style="text-align:center;margin-top:18px">Dog-dev · @HOST@</p>
</div>
</body></html>`

	stateClass, stateTitle, stateDesc := "unknown", "还没有这台设备的记录", "装好证书后打开 https://github.com，这里就会显示已信任。"
	switch d.State.Kind {
	case "ok":
		stateClass = "ok"
		stateTitle = "这台设备已信任证书 ✓"
		stateDesc = "来源 " + d.State.IP + "，加速通道对她已生效。"
	case "untrusted":
		stateClass = "untrusted"
		desc := "来源 " + d.State.IP + "，之前有 " + fmt.Sprint(d.State.Failures) + " 次连接因证书未被信任被拒"
		if len(d.State.Hosts) > 0 {
			desc += "（最近：" + strings.Join(d.State.Hosts, "、") + "）"
		}
		desc += "。按下面步骤把证书装进「CA 证书」后，把浏览器彻底关掉重开。"
		stateTitle, stateDesc = "这台设备还没装好证书", desc
	}

	r := strings.NewReplacer(
		"@STATE_CLASS@", html.EscapeString(stateClass),
		"@STATE_TITLE@", html.EscapeString(stateTitle),
		"@STATE_DESC@", html.EscapeString(stateDesc),
		"@QR@", html.EscapeString(d.QR),
		"@PEM_URL@", html.EscapeString(d.PEMURL),
		"@DER_URL@", html.EscapeString(d.DERURL),
		"@NOT_AFTER@", html.EscapeString(d.NotAfter),
		"@FINGERPRINT@", html.EscapeString(d.Fingerprint),
		"@BASE@", html.EscapeString(d.BareURL),
		"@HOST@", html.EscapeString(d.Host),
	)
	return r.Replace(tpl)
}
