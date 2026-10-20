package proxy

import (
	"bytes"
	"net/http"
	"strings"
)

// wantsHTML 判断客户端是否在请求网页（浏览器）。
//
// 用于全源失败时的兜底决策：网页请求回退直连官方，
// API/文件下载仍保留原始 4xx 语义（调用方需要区分 404/403）。
func wantsHTML(r *http.Request) bool {
	if r == nil {
		return false
	}
	return strings.Contains(strings.ToLower(r.Header.Get("Accept")), "text/html")
}

// isMirrorChallenge 判断镜像返回的是否为 Cloudflare 挑战页 / 拦截页。
//
// 背景：多数加速源挂在 Cloudflare 后面。当镜像站把来自 NAS 的请求判定为
// 异常流量时，Cloudflare 会回一个挑战页：状态码可能是 200、403 或 404，
// 正文是空壳 HTML + 混淆脚本（实测中文版是 `<html lang="zh-CN"
// class="overflow-x-clip">` + `function a0_0xc5c6(...)`），根本不是业务内容。
//
// 这类响应不能透传给客户端（否则 GitHub 网页变成白屏 404 / 无限挑战），
// 必须当作「本源失败」换下一个源。
func isMirrorChallenge(resp *http.Response, body []byte) bool {
	if resp == nil {
		return false
	}
	// 新版 Cloudflare 会显式标注被拦截的原因。
	if resp.Header.Get("Cf-Mitigated") != "" {
		return true
	}
	server := strings.ToLower(strings.TrimSpace(resp.Header.Get("Server")))
	cfRay := resp.Header.Get("Cf-Ray")
	if server != "cloudflare" && cfRay == "" {
		return false
	}
	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	if ct != "" && !strings.Contains(ct, "text/html") {
		return false
	}

	// 挑战特征只出现在页面头部，截断判断避免对大页面做全量转换。
	n := len(body)
	if n > 64<<10 {
		n = 64 << 10
	}
	head := bytes.ToLower(body[:n])

	signatures := [][]byte{
		[]byte("challenge-platform"),
		[]byte("__cf_chl"),
		[]byte("_cf_chl_opt"),
		[]byte("cf-challenge"),
		[]byte("cdn-cgi/challenge"),
		[]byte("just a moment"),
		[]byte("attention required"),
	}
	for _, sig := range signatures {
		if bytes.Contains(head, sig) {
			return true
		}
	}
	// 实测到的中文版挑战页：混淆脚本把域名/参数藏在字符串表里。
	if bytes.Contains(head, []byte("a0_0x")) && bytes.Contains(head, []byte("<script")) {
		return true
	}
	return false
}
