package api

import (
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/ghpp/ghpp/internal/config"
)

// handlePAC 返回 Proxy Auto-Config 脚本（/proxy.pac）。
//
// 作用：浏览器与支持 PAC 的系统客户端按下发规则决定是否走加速器——
// 只有 GitHub 相关域名走 PROXY，其余一律 DIRECT。这样既拿到 GitHub 加速，
// 又不会像全局 http(s)_proxy 那样把非 GitHub 站点也送进代理。
//
// 注意：curl / wget / git / pip / apt / docker 不读取 PAC，
// 它们仍需要 per-tool 配置（见 /api/lan-access 下发的片段）。
func (s *Server) handlePAC(w http.ResponseWriter, r *http.Request) {
	cfg := s.app.CurrentConfig()

	// 代理地址取客户端访问控制台时用的主机名，
	// 保证返回的地址对这台客户端可达（NAS 本机是 127.0.0.1，局域网是 NAS 的 LAN IP）。
	proxyHost := r.Host
	if h, _, err := net.SplitHostPort(proxyHost); err == nil {
		proxyHost = h
	}
	proxyHost = strings.TrimSpace(proxyHost)
	if proxyHost == "" {
		proxyHost = "127.0.0.1"
	}
	port := "37710"
	if _, p, err := net.SplitHostPort(cfg.Proxy.Listen); err == nil && p != "" {
		port = p
	}

	hosts := make([]string, 0, 32)
	for _, h := range config.GitHubHosts() {
		hosts = append(hosts, h.Host)
	}
	// 补上常见基础设施域名，避免因内置清单调整而漏网走直连。
	hosts = append(hosts,
		"github.com", "api.github.com", "ghcr.io",
		"codeload.github.com", "objects.githubusercontent.com",
		"raw.githubusercontent.com", "gist.github.com", "github.io",
	)

	body := buildPAC(hosts, proxyHost+":"+port)

	w.Header().Set("Content-Type", "application/x-ns-proxy-autoconfig")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Content-Disposition", `inline; filename="dogdev.pac"`)
	_, _ = w.Write([]byte(body))
}

// buildPAC 生成 PAC 脚本内容。
//
// 规则：host 等于清单中某项、或为其子域时走代理，其余一律 DIRECT。
// 不用 dnsResolve / isInNet —— 前者会阻塞，后者对 IPv6 支持不可靠。
func buildPAC(hosts []string, proxyAddr string) string {
	var b strings.Builder
	b.WriteString("// Dog-dev 加速器 PAC —— 仅 GitHub 相关域名走代理，其余直连\n")
	b.WriteString("var DOGDEV_HOSTS = [\n")
	for _, h := range dedupeStrings(hosts) {
		b.WriteString(fmt.Sprintf("  %q,\n", h))
	}
	b.WriteString("];\n")
	b.WriteString(fmt.Sprintf("var DOGDEV_PROXY = \"PROXY %s; DIRECT\";\n", proxyAddr))
	b.WriteString(`
function FindProxyForURL(url, host) {
  host = host.toLowerCase();
  for (var i = 0; i < DOGDEV_HOSTS.length; i++) {
    var d = DOGDEV_HOSTS[i];
    if (host === d || host.slice(-(d.length + 1)) === "." + d) return DOGDEV_PROXY;
  }
  return "DIRECT";
}
`)
	return b.String()
}

func dedupeStrings(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, v := range in {
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}
