package proxy

import (
	"fmt"
	"net"
	"net/http"
	"strings"
)

// 非 GitHub 目标的透明透传（系统代理/局域网设备代理场景）。
//
// 背景：代理端口原先对所有非受管域名返回 403，导致把 https_proxy 指向本机后，
// pip / apt / 浏览器访问国内站点全部失败。这里改为直连透传：
//   - CONNECT：建立 TCP 隧道，不解密、不改写；
//   - 普通 HTTP 代理请求：由 forwardDirect 直接回源。
//
// 为避免 37710 在公网可达时变成开放代理，做了三层限制：
//  1. 客户端来源必须是本机或私网地址；
//  2. 目标端口仅放行 80/443；
//  3. 目标是 IP 字面量时禁止指向内网/保留地址（防 SSRF）。

// isNonPublicIP 判断 IP 是否属于不应被代理转发的范围。
func isNonPublicIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast()
}

// trustedProxyClient 判断客户端来源是否允许使用透明透传。
func trustedProxyClient(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	host = strings.Trim(host, "[]")
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate()
}

// checkTransparentTarget 校验透传目标：端口白名单 + 禁止打内网。
//
// 返回规范化后的 "host:port"。端口缺省按 443 处理（CONNECT 语义）。
func checkTransparentTarget(hostport string) (string, error) {
	host, port, err := net.SplitHostPort(hostport)
	if err != nil {
		host = strings.Trim(hostport, "[]")
		port = "443"
	}
	if port != "443" && port != "80" {
		return "", fmt.Errorf("端口 %s 不在透传白名单（仅 80/443）", port)
	}
	h := strings.Trim(host, "[]")
	if h == "" {
		return "", fmt.Errorf("目标为空")
	}
	if ip := net.ParseIP(h); ip != nil && isNonPublicIP(ip) {
		return "", fmt.Errorf("目标 %s 属于内网或保留地址，拒绝透传", h)
	}
	return net.JoinHostPort(h, port), nil
}

// checkTransparentTargetFor 按客户端可信度决定透传目标的放行范围。
//
// 本机/局域网客户端（trusted）与目标同处一个私网，本来就能直连，
// 因此允许任意端口与内网目标——手机或笔记本把 WiFi 代理指向本机后，
// 访问 NAS 自己的控制台、路由器管理页等内网地址必须照常可用
// （1.3.3 的严格模式会把 http://NAS:37717/ 这类请求判成 403，
// 导致「设了代理就再也打不开本机页面」）。
//
// 唯一保留的限制：禁止把本机回环暴露出去。代理运行在 NAS 上，
// 若允许 127.0.0.1，任何局域网设备都能借它访问只监听 loopback 的服务
// （例如刻意只绑 127.0.0.1 的其他应用），那是实打实的权限外溢。
//
// 公网客户端进不到这条路径（trustedProxyClient 已拦），所以放宽不会
// 增加对外的 SSRF 面。
func checkTransparentTargetFor(hostport string, trusted bool) (string, error) {
	if !trusted {
		return checkTransparentTarget(hostport)
	}

	host, port, err := net.SplitHostPort(hostport)
	if err != nil {
		host = strings.Trim(hostport, "[]")
		port = "443"
	}
	h := strings.Trim(host, "[]")
	if h == "" {
		return "", fmt.Errorf("目标为空")
	}
	if ip := net.ParseIP(h); ip != nil {
		if ip.IsLoopback() || ip.IsUnspecified() {
			return "", fmt.Errorf("目标 %s 是本机回环地址，拒绝透传", h)
		}
	}
	return net.JoinHostPort(h, port), nil
}

// allowTransparentHTTP 判断普通 HTTP 代理请求是否允许直连透传。
func (e *Engine) allowTransparentHTTP(r *http.Request) bool {
	if !trustedProxyClient(r.RemoteAddr) {
		return false
	}
	host := r.URL.Hostname()
	port := r.URL.Port()
	if port == "" {
		if strings.EqualFold(r.URL.Scheme, "http") {
			port = "80"
		} else {
			port = "443"
		}
	}
	_, err := checkTransparentTargetFor(net.JoinHostPort(host, port), true)
	return err == nil
}
