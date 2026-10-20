package proxy

import (
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ghpp/ghpp/internal/config"
)

// TestExtractEmbeddedURLForms 覆盖嵌入式 URL 的三种形态：标准、折叠、历史嵌套。
func TestExtractEmbeddedURLForms(t *testing.T) {
	cases := []struct {
		name     string
		path     string
		wantHost string
		wantRest string
		wantOK   bool
	}{
		{"标准", "/https://github.com/o/r", "github.com", "/o/r", true},
		{"折叠", "/https:/raw.githubusercontent.com/o/r/f.txt", "raw.githubusercontent.com", "/o/r/f.txt", true},
		{"历史嵌套两层", "/https://raw.githubusercontent.com/https:/raw.githubusercontent.com/o/r/f.txt",
			"raw.githubusercontent.com", "/o/r/f.txt", true},
		{"历史嵌套多层", "/https://raw.githubusercontent.com/https:/raw.githubusercontent.com/https:/raw.githubusercontent.com/o/r/f",
			"raw.githubusercontent.com", "/o/r/f", true},
		{"非嵌入路径", "/api/state", "", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			host, rest, ok := extractEmbeddedURL(c.path)
			if ok != c.wantOK {
				t.Fatalf("ok=%v want %v", ok, c.wantOK)
			}
			if host != c.wantHost || rest != c.wantRest {
				t.Fatalf("got (%q,%q) want (%q,%q)", host, rest, c.wantHost, c.wantRest)
			}
		})
	}
}

// newEmbeddedRequest 构造"经本代理"的请求：Host 是代理自身地址。
func newEmbeddedRequest(t *testing.T, rawPath string) *http.Request {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:37710"+rawPath, nil)
	r.Host = "127.0.0.1:37710"
	return r
}

// upstreamResponse 构造带有上游请求 URL 的响应，供相对重定向解析用。
func upstreamResponse(t *testing.T, upstreamURL string) *http.Response {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, upstreamURL, nil)
	return &http.Response{StatusCode: http.StatusFound, Request: req, Header: http.Header{}}
}

// TestRewriteRedirectRelativeMirror 镜像返回相对重定向（moeyy / githubfast 实测行为）时，
// 必须解析成上游绝对地址后折叠为单层嵌入式 URL，而不是原样透传。
func TestRewriteRedirectRelativeMirror(t *testing.T) {
	r := newEmbeddedRequest(t, "/https://raw.githubusercontent.com/o/r/f.txt")
	resp := upstreamResponse(t, "https://moeyy.xyz/https://raw.githubusercontent.com/o/r/f.txt")

	got := rewriteRedirect("/https:/raw.githubusercontent.com/o/r/f.txt", resp, r)
	want := "http://127.0.0.1:37710/https://raw.githubusercontent.com/o/r/f.txt"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

// TestRewriteRedirectAlreadyNested 上游给出已经嵌套的重定向时不得继续加深。
func TestRewriteRedirectAlreadyNested(t *testing.T) {
	r := newEmbeddedRequest(t, "/https://raw.githubusercontent.com/o/r/f.txt")
	resp := upstreamResponse(t, "https://moeyy.xyz/https:/raw.githubusercontent.com/https:/raw.githubusercontent.com/o/r/f.txt")

	loc := "/https:/raw.githubusercontent.com/https:/raw.githubusercontent.com/o/r/f.txt"
	got := rewriteRedirect(loc, resp, r)
	want := "http://127.0.0.1:37710/https://raw.githubusercontent.com/o/r/f.txt"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

// TestRewriteRedirectGitHubAbsolute 绝对 GitHub 地址仍需改写成嵌入式 URL。
func TestRewriteRedirectGitHubAbsolute(t *testing.T) {
	r := newEmbeddedRequest(t, "/https://github.com/o/r/releases/download/v1/a.bin")
	resp := upstreamResponse(t, "https://gh-proxy.com/https://github.com/o/r/releases/download/v1/a.bin")

	got := rewriteRedirect("https://github.com/o/r/releases/download/v1/a.bin", resp, r)
	want := "http://127.0.0.1:37710/https://github.com/o/r/releases/download/v1/a.bin"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

// TestRewriteRedirectExternal 非 GitHub 域名保持绝对地址，客户端直连。
func TestRewriteRedirectExternal(t *testing.T) {
	r := newEmbeddedRequest(t, "/https://github.com/o/r")
	resp := upstreamResponse(t, "https://hub.gitmirror.com/https://github.com/o/r")

	got := rewriteRedirect("https://cdn.example.com/objects/a.bin?x=1", resp, r)
	want := "https://cdn.example.com/objects/a.bin?x=1"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

// TestRewriteRedirectConnectMode CONNECT 隧道内（Host 已是 GitHub 域名）不应再嵌一层。
func TestRewriteRedirectConnectMode(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "https://raw.githubusercontent.com/o/r/f.txt", nil)
	r.TLS = &tls.ConnectionState{}
	resp := upstreamResponse(t, "https://moeyy.xyz/https://raw.githubusercontent.com/o/r/f.txt")

	got := rewriteRedirect("/https:/raw.githubusercontent.com/o/r/f.txt", resp, r)
	want := "https://raw.githubusercontent.com/o/r/f.txt"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

// TestMirrorSelfRedirectRejected 镜像把请求原地打回（moeyy / githubfast 的实测行为）时，
// 必须判该源不可用，而不是把跳转环丢给客户端。
func TestMirrorSelfRedirectRejected(t *testing.T) {
	const origPath = "/https://raw.githubusercontent.com/o/r/f.txt"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "/https:/raw.githubusercontent.com/o/r/f.txt")
		w.WriteHeader(http.StatusPermanentRedirect)
	}))
	defer srv.Close()

	mirrors := []config.Mirror{
		{ID: "A", Name: "源A", URL: srv.URL, Kind: config.KindPrefix, Enabled: true, Weight: 1},
	}
	cfg := newTestCfg(mirrors)
	e := newFailoverTestEngine(cfg)
	// 与生产一致：重定向不自动跟随，交给引擎判断是否推进。
	e.client = &http.Client{CheckRedirect: noRedirect}
	seedHot(e.pool, "A")

	r := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:37710"+origPath, nil)
	r.Host = "127.0.0.1:37710"

	w := doRequest(e, cfg, r, rawTarget("/o/r/f.txt"))
	if w.Code == http.StatusPermanentRedirect || w.Code == http.StatusFound {
		t.Fatalf("不应把原地重定向透传给客户端，实际 %d Location=%q", w.Code, w.Header().Get("Location"))
	}
	if !strings.Contains(w.Body.String(), "重定向未推进") {
		t.Fatalf("应报重定向未推进，实际 %d body=%q", w.Code, w.Body.String())
	}
}

// TestMirrorRealRedirectStillPassed 真正推进的重定向（如 GitHub → 对象存储）仍需交给客户端。
func TestMirrorRealRedirectStillPassed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "https://objects.githubusercontent.com/obj/x?y=1")
		w.WriteHeader(http.StatusFound)
	}))
	defer srv.Close()

	mirrors := []config.Mirror{
		{ID: "A", Name: "源A", URL: srv.URL, Kind: config.KindPrefix, Enabled: true, Weight: 50},
	}
	cfg := newTestCfg(mirrors)
	e := newFailoverTestEngine(cfg)
	e.client = &http.Client{CheckRedirect: noRedirect}
	seedHot(e.pool, "A")

	r := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:37710/https://github.com/o/r/releases/download/v1/a.bin", nil)
	r.Host = "127.0.0.1:37710"

	w := doRequest(e, cfg, r, rawTarget("/o/r/releases/download/v1/a.bin"))
	if w.Code != http.StatusFound {
		t.Fatalf("应把 302 交给客户端，实际 %d", w.Code)
	}
	loc := w.Header().Get("Location")
	want := "http://127.0.0.1:37710/https://objects.githubusercontent.com/obj/x?y=1"
	if loc != want {
		t.Fatalf("Location=%q want %q", loc, want)
	}
}
