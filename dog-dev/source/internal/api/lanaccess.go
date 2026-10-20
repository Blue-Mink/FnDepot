package api

import (
	"net"
	"net/http"
	"strings"
)

// lanAccessSnippet 是一段给局域网设备使用的接入配置。
type lanAccessSnippet struct {
	Label  string `json:"label"`
	Target string `json:"target"`
	Value  string `json:"value"`
}

// handleLanAccess 下发「局域网其他设备如何接入加速」的配置片段。
//
// 背景：加速器是代理端点（不是网关），局域网设备必须逐台配置。
// 这里把四类可用路径一次性列全，避免用户去猜：
//  1. PAC：浏览器与系统级客户端，只有 GitHub 域名走代理；
//  2. Docker：registry-mirrors 指向本机（只影响 pull）；
//  3. git / curl：per-tool 代理，只对 GitHub 请求生效；
//  4. 嵌入式 URL：不需要任何配置、不需要装 CA，适合临时下载。
//
// 该接口只返回配置文本，不含凭据，故不要求登录。
func (s *Server) handleLanAccess(w http.ResponseWriter, r *http.Request) {
	cfg := s.app.CurrentConfig()

	host := r.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.TrimSpace(host)
	if host == "" {
		host = "127.0.0.1"
	}
	port := "37710"
	if _, p, err := net.SplitHostPort(cfg.Proxy.Listen); err == nil && p != "" {
		port = p
	}
	adminPort := "37717"
	if _, p, err := net.SplitHostPort(cfg.Server.Listen); err == nil && p != "" {
		adminPort = p
	}

	proxyAddr := host + ":" + port
	pacURL := "http://" + host + ":" + adminPort + "/proxy.pac"

	snippets := []lanAccessSnippet{
		{
			Label:  "浏览器 / 系统代理（PAC，推荐）",
			Target: "Windows / macOS / iOS / Android / 浏览器",
			Value:  pacURL,
		},
		{
			Label:  "Docker 镜像加速",
			Target: "/etc/docker/daemon.json",
			Value:  `"registry-mirrors": ["http://` + host + `:` + port + `"]`,
		},
		{
			Label:  "git（仅 GitHub 走代理）",
			Target: "命令行",
			Value:  "git config --global http.https://github.com.proxy http://" + proxyAddr,
		},
		{
			Label:  "curl / wget（单次）",
			Target: "命令行",
			Value:  "curl -x http://" + proxyAddr + " https://github.com/owner/repo",
		},
		{
			Label:  "免配置下载（嵌入式 URL）",
			Target: "任何设备，无需 CA",
			Value:  "http://" + proxyAddr + "/https://github.com/owner/repo/releases/download/vX/file",
		},
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"proxy":         proxyAddr,
		"pac_url":       pacURL,
		"docker_mirror": "http://" + proxyAddr,
		"snippets":      snippets,
		"notes": []string{
			"PAC 只对浏览器与支持 PAC 的系统客户端生效；curl/wget/git/pip/apt/docker 不读取 PAC。",
			"走代理访问 GitHub 需要信任 Dog-dev 本地 CA；嵌入式 URL 不需要。",
			"不要把设备的全局 http(s)_proxy 指向本机后用于非 GitHub 站点以外的流量——非 GitHub 域名虽然已改为透明透传，但多一跳且受代理进程可用性影响。",
		},
	})
}
