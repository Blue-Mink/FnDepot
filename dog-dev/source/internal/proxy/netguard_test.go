package proxy

import (
	"net/http"
	"net/url"
	"testing"
)

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse %q: %v", raw, err)
	}
	return u
}

func TestTrustedProxyClient(t *testing.T) {
	cases := []struct {
		addr string
		want bool
	}{
		{"127.0.0.1:54321", true},
		{"[::1]:54321", true},
		{"192.168.1.76:1234", true},
		{"10.1.2.3:1", true},
		{"172.16.5.5:9", true},
		{"8.8.8.8:443", false},
		{"1.1.1.1:80", false},
		{"pipe", false},
		{"", false},
	}
	for _, c := range cases {
		if got := trustedProxyClient(c.addr); got != c.want {
			t.Errorf("trustedProxyClient(%q)=%v, want %v", c.addr, got, c.want)
		}
	}
}

func TestCheckTransparentTarget(t *testing.T) {
	ok := []struct {
		in   string
		want string
	}{
		{"example.com:443", "example.com:443"},
		{"example.com", "example.com:443"},
		{"example.com:80", "example.com:80"},
		{"8.8.8.8:443", "8.8.8.8:443"},
		{"[2606:4700::1111]:443", "[2606:4700::1111]:443"},
	}
	for _, c := range ok {
		got, err := checkTransparentTarget(c.in)
		if err != nil {
			t.Errorf("checkTransparentTarget(%q) unexpected error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("checkTransparentTarget(%q)=%q, want %q", c.in, got, c.want)
		}
	}

	bad := []string{
		"example.com:22",
		"example.com:3306",
		"192.168.1.1:443",
		"127.0.0.1:443",
		"[::1]:443",
		"10.0.0.5:80",
		"169.254.1.1:443",
		"0.0.0.0:443",
		"",
	}
	for _, in := range bad {
		if got, err := checkTransparentTarget(in); err == nil {
			t.Errorf("checkTransparentTarget(%q)=%q, want error", in, got)
		}
	}
}

func TestAllowTransparentHTTP(t *testing.T) {
	e := &Engine{}
	req := &http.Request{RemoteAddr: "192.168.1.76:1000", URL: mustURL(t, "https://www.baidu.com/")}
	if !e.allowTransparentHTTP(req) {
		t.Error("局域网来源的 https 公网目标应允许透传")
	}

	req2 := &http.Request{RemoteAddr: "8.8.8.8:1000", URL: mustURL(t, "https://www.baidu.com/")}
	if e.allowTransparentHTTP(req2) {
		t.Error("公网来源必须拒绝透传（防止成为开放代理）")
	}

	req3 := &http.Request{RemoteAddr: "192.168.1.76:1000", URL: mustURL(t, "http://192.168.1.2:37717/ca")}
	if !e.allowTransparentHTTP(req3) {
		t.Error("局域网来源访问内网设备（NAS 控制台/路由器）必须允许——否则设了代理就打不开本机页面")
	}

	req4 := &http.Request{RemoteAddr: "192.168.1.76:1000", URL: mustURL(t, "http://127.0.0.1:38100/")}
	if e.allowTransparentHTTP(req4) {
		t.Error("指向本机回环的请求必须拒绝：代理运行在 NAS 上，放行等于把 loopback 服务暴露给整个局域网")
	}
}

// TestCheckTransparentTargetForTrusted 覆盖「手机设了 WiFi 代理」这一真实场景：
// 局域网客户端需要能经代理访问 NAS 自己的控制台与其他内网设备。
func TestCheckTransparentTargetForTrusted(t *testing.T) {
	allow := []struct{ in, want string }{
		{"192.168.1.2:37717", "192.168.1.2:37717"}, // NAS 控制台（非 80/443）
		{"192.168.1.1:80", "192.168.1.1:80"},       // 路由器
		{"example.com:443", "example.com:443"},
		{"example.com", "example.com:443"},
	}
	for _, c := range allow {
		got, err := checkTransparentTargetFor(c.in, true)
		if err != nil {
			t.Errorf("可信客户端访问 %q 应允许，实际报错: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("checkTransparentTargetFor(%q)=%q, want %q", c.in, got, c.want)
		}
	}

	deny := []string{"127.0.0.1:38100", "[::1]:8080", "0.0.0.0:80", ""}
	for _, in := range deny {
		if got, err := checkTransparentTargetFor(in, true); err == nil {
			t.Errorf("回环/空目标 %q 必须拒绝，实际放行成 %q", in, got)
		}
	}

	// 不可信客户端仍走严格模式：端口与内网限制都保留。
	if _, err := checkTransparentTargetFor("192.168.1.2:37717", false); err == nil {
		t.Error("公网来源仍需按严格规则拒绝非 80/443 端口")
	}
}
