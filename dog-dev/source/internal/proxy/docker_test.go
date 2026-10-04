package proxy

import (
	"crypto/tls"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ghpp/ghpp/internal/config"
	"github.com/ghpp/ghpp/internal/mirror"
	"github.com/ghpp/ghpp/internal/netutil"
)

// insec 返回信任自签证书且不跟随重定向的客户端（httptest TLS 服务器用；
// noRedirect 与生产客户端一致，302 必须原样交回代理逻辑处理）。
func insec() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		},
		CheckRedirect: noRedirect,
	}
}

func newDockerTestEngine(cfg *config.Config) *Engine {
	return &Engine{
		cfg:                 func() *config.Config { return cfg },
		pool:                mirror.NewPool(),
		hosts:               config.HostSet(),
		client:              insec(),
		dockerClient:        insec(),
		dockerPool:          NewDockerPool(),
		dockerRedirectAllow: newRedirectAllowSet(),
		logf:                func(string, string, ...any) {},
	}
}

// TestDockerExtraWiring：Options.DockerExtraUpstream 必须真正接进 Engine
// （回归：漏接赋值导致 KSpeeder 引擎从未置于 Docker 上游首位）。
func TestDockerExtraWiring(t *testing.T) {
	cfg := config.Default()
	cfg.Docker.Enabled = true
	cfg.Docker.Upstreams = []string{"https://docker.1ms.run", "official"}
	e := NewEngine(Options{
		Config:              func() *config.Config { return cfg },
		Pool:                mirror.NewPool(),
		Logf:                func(string, string, ...any) {},
		DockerExtraUpstream: func() string { return "https://127.0.0.1:5443" },
	})
	list := e.dockerUpstreams(cfg)
	if len(list) == 0 || list[0] != "https://127.0.0.1:5443" {
		t.Fatalf("额外上游必须置于首位: %v", list)
	}
	// 提供者为空串时不注入。
	e2 := NewEngine(Options{
		Config:              func() *config.Config { return cfg },
		Pool:                mirror.NewPool(),
		Logf:                func(string, string, ...any) {},
		DockerExtraUpstream: func() string { return "" },
	})
	list2 := e2.dockerUpstreams(cfg)
	if list2[0] == "https://127.0.0.1:5443" {
		t.Fatalf("空提供者不应注入: %v", list2)
	}
}

// TestDockerRegistryFastFail：/v2/ 探活遇到慢上游（3s 才回）时，
// 必须被 2s 小请求上限快速掐断并轮换到快上游成功，
// 总耗时压在 Docker daemon 探活耐心之内（回归：daemon 回退官方导致拉取 43s 超时）。
func TestDockerRegistryFastFail(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(3 * time.Second):
			_, _ = w.Write([]byte(`{"slow":true}`))
		case <-r.Context().Done():
		}
	}))
	defer slow.Close()
	fast := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"fast":true}`))
	}))
	defer fast.Close()

	cfg := newTestCfg(nil)
	cfg.Docker.Enabled = true
	cfg.Docker.Upstreams = []string{slow.URL, fast.URL}
	e := newDockerTestEngine(cfg)

	start := time.Now()
	rec := httptest.NewRecorder()
	e.serveDocker(rec, httptest.NewRequest(http.MethodGet, "/v2/", nil))
	elapsed := time.Since(start)

	body, _ := io.ReadAll(rec.Body)
	if rec.Code != http.StatusOK {
		t.Fatalf("应轮换到快上游成功: code=%d body=%q", rec.Code, body)
	}
	if !strings.Contains(string(body), `"fast"`) {
		t.Fatalf("响应应来自快上游: %q", body)
	}
	if elapsed > 2800*time.Millisecond {
		t.Fatalf("慢上游未被 2s 上限快速掐断，总耗时 %v", elapsed)
	}
}

// TestDockerBlobNotCapped：blob 下载不受 2s 小请求上限约束，
// 慢而完整的大对象传输应正常完成。
func TestDockerBlobNotCapped(t *testing.T) {
	slowBlob := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(2500 * time.Millisecond):
		case <-r.Context().Done():
			return
		}
		_, _ = w.Write(make([]byte, 512))
	}))
	defer slowBlob.Close()

	cfg := newTestCfg(nil)
	cfg.Docker.Enabled = true
	cfg.Docker.Upstreams = []string{slowBlob.URL}
	e := newDockerTestEngine(cfg)

	rec := httptest.NewRecorder()
	e.serveDocker(rec, httptest.NewRequest(http.MethodGet, "/v2/lib/hello/blobs/sha256:abc", nil))
	body, _ := io.ReadAll(rec.Body)
	if rec.Code != http.StatusOK || len(body) != 512 {
		t.Fatalf("blob 慢传输应完整完成: code=%d len=%d", rec.Code, len(body))
	}
}

// newStatusServer 起一个固定状态码的上游。
func newStatusServer(t *testing.T, code int, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if code == http.StatusUnauthorized {
			w.Header().Set("WWW-Authenticate", `Bearer realm="https://auth.example.com/token",service="registry.example.com"`)
		}
		w.WriteHeader(code)
		_, _ = w.Write([]byte(body))
	}))
}

// TestDocker403ContinuesChain：镜像 403（拒绝 token）应换下一上游，而不是透传终止
// （回归：m.daocloud 403 透传导致 daemon 放弃镜像源、回退官方超时整次拉取失败）。
func TestDocker403ContinuesChain(t *testing.T) {
	reject := newStatusServer(t, http.StatusForbidden, `{"errors":[{"code":"DENIED"}]}`)
	defer reject.Close()
	good := newStatusServer(t, http.StatusOK, `{"manifest":"ok"}`)
	defer good.Close()

	cfg := newTestCfg(nil)
	cfg.Docker.Enabled = true
	cfg.Docker.Upstreams = []string{reject.URL, good.URL}
	e := newDockerTestEngine(cfg)

	rec := httptest.NewRecorder()
	e.serveDocker(rec, httptest.NewRequest(http.MethodGet, "/v2/lib/busybox/manifests/1.37", nil))
	body, _ := io.ReadAll(rec.Body)
	if rec.Code != http.StatusOK || !strings.Contains(string(body), `"ok"`) {
		t.Fatalf("403 应换下一上游成功: code=%d body=%q", rec.Code, body)
	}
	// 403 必须回灌为失败：拒绝 token 的镜像要沉底。
	if st := e.dockerPool.Stats()[reject.URL]; st.OK {
		t.Fatalf("403 上游不应保持 OK: %+v", st)
	}
}

// TestDocker404ContinuesChain：镜像没有该镜像（404）同样换源，全链都没有才回 502。
func TestDocker404ContinuesChain(t *testing.T) {
	missing := newStatusServer(t, http.StatusNotFound, `{"errors":[{"code":"NOT_FOUND"}]}`)
	defer missing.Close()
	good := newStatusServer(t, http.StatusOK, `{"manifest":"ok"}`)
	defer good.Close()

	cfg := newTestCfg(nil)
	cfg.Docker.Enabled = true
	cfg.Docker.Upstreams = []string{missing.URL, good.URL}
	e := newDockerTestEngine(cfg)

	rec := httptest.NewRecorder()
	e.serveDocker(rec, httptest.NewRequest(http.MethodGet, "/v2/lib/nobody/manifests/x", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("404 应换下一上游成功: code=%d", rec.Code)
	}

	// 全链 404 → 502（daemon 回退官方源拿到真实 404）。
	cfg2 := newTestCfg(nil)
	cfg2.Docker.Enabled = true
	cfg2.Docker.Upstreams = []string{missing.URL}
	e2 := newDockerTestEngine(cfg2)
	rec2 := httptest.NewRecorder()
	e2.serveDocker(rec2, httptest.NewRequest(http.MethodGet, "/v2/lib/nobody/manifests/x", nil))
	if rec2.Code != http.StatusBadGateway {
		t.Fatalf("全链 404 应回 502: code=%d", rec2.Code)
	}
}

// TestDocker401ChallengePassthrough：/v2/ 的 401 认证质询必须透传
// （daemon 要拿 WWW-Authenticate 去换 token），不消耗后续上游。
func TestDocker401ChallengePassthrough(t *testing.T) {
	challenge := newStatusServer(t, http.StatusUnauthorized, `{}`)
	defer challenge.Close()
	good := newStatusServer(t, http.StatusOK, `{"manifest":"ok"}`)
	defer good.Close()

	cfg := newTestCfg(nil)
	cfg.Docker.Enabled = true
	cfg.Docker.Upstreams = []string{challenge.URL, good.URL}
	e := newDockerTestEngine(cfg)

	rec := httptest.NewRecorder()
	e.serveDocker(rec, httptest.NewRequest(http.MethodGet, "/v2/", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("/v2/ 401 质询应透传: code=%d", rec.Code)
	}
	if loc := rec.Header().Get("WWW-Authenticate"); !strings.Contains(loc, "/v2/token?_u=") {
		t.Fatalf("WWW-Authenticate 的 realm 应改写为本机换 token 端点: %q", loc)
	}
	// 质询不算该上游成功或失败（token 交换的正常第一步）。
	if st := e.dockerPool.Stats()[challenge.URL]; st.BadCount != 0 {
		t.Fatalf("/v2/ 质询不应记失败: %+v", st)
	}
}

// TestDocker401ManifestChallengeNoAuth：manifest 请求未带 token 时上游回 401，
// 且质询 realm 不可信（无法代理端代换）时，按"上游不可用"换源——不透传
// 质询给 daemon（daemon 的 token 协商在 /v2/ ping 阶段建立，ping 走免认证
// 上游时它收到 manifest 质询只会静默放弃该镜像）。
func TestDocker401ManifestChallengeNoAuth(t *testing.T) {
	challenge := newStatusServer(t, http.StatusUnauthorized, `{}`)
	defer challenge.Close()
	good := newStatusServer(t, http.StatusOK, `{"manifest":"ok"}`)
	defer good.Close()

	cfg := newTestCfg(nil)
	cfg.Docker.Enabled = true
	cfg.Docker.Upstreams = []string{challenge.URL, good.URL}
	e := newDockerTestEngine(cfg)

	rec := httptest.NewRecorder()
	e.serveDocker(rec, httptest.NewRequest(http.MethodGet, "/v2/lib/x/manifests/y", nil))
	body, _ := io.ReadAll(rec.Body)
	if rec.Code != http.StatusOK || !strings.Contains(string(body), `"ok"`) {
		t.Fatalf("不可信 realm 的 401 应换源成功: code=%d body=%q", rec.Code, body)
	}
	if st := e.dockerPool.Stats()[challenge.URL]; st.BadCount == 0 {
		t.Fatalf("token 代换失败应记失败: %+v", st)
	}
}

// TestDocker401TokenExchange：上游 401 质询的 realm 指向已配置上游自身时，
// 代理应自行换 token 并带 token 重放请求，daemon 直接拿到 200。
func TestDocker401TokenExchange(t *testing.T) {
	tokenQuery := make(chan url.Values, 4)
	var tokenOnce int32
	authed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/auth/token"):
			if atomic.CompareAndSwapInt32(&tokenOnce, 0, 1) {
				tokenQuery <- r.URL.Query()
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"token":"tok-xyz","expires_in":300}`))
		case r.URL.Path == "/v2/lib/x/manifests/y":
			if r.Header.Get("Authorization") == "Bearer tok-xyz" {
				_, _ = w.Write([]byte(`{"manifest":"ok"}`))
				return
			}
			w.Header().Set("WWW-Authenticate",
				`Bearer realm="http://`+r.Host+`/auth/token",service="registry.test",scope="repository:lib/x:pull"`)
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer authed.Close()

	cfg := newTestCfg(nil)
	cfg.Docker.Enabled = true
	cfg.Docker.Upstreams = []string{authed.URL}
	e := newDockerTestEngine(cfg)

	rec := httptest.NewRecorder()
	e.serveDocker(rec, httptest.NewRequest(http.MethodGet, "/v2/lib/x/manifests/y", nil))
	body, _ := io.ReadAll(rec.Body)
	if rec.Code != http.StatusOK || !strings.Contains(string(body), `"ok"`) {
		t.Fatalf("代换 token 后应拿到 200: code=%d body=%q", rec.Code, body)
	}
	select {
	case q := <-tokenQuery:
		if q.Get("service") != "registry.test" || q.Get("scope") != "repository:lib/x:pull" {
			t.Fatalf("换 token 请求应带 service/scope: %v", q)
		}
	default:
		t.Fatalf("应发生代理端换 token")
	}
	if st := e.dockerPool.Stats()[authed.URL]; st.BadCount != 0 {
		t.Fatalf("代换成功不应记失败: %+v", st)
	}
}

// TestDocker401TokenExchangeUntrustedRealm：质询 realm 的主机不在受信任名单
// （已配置上游/内置引擎/Docker 官方认证）时必须拒绝代换（SSRF 闸门），
// 且按上游不可用换源。
//
// 闸门按主机名（stripPort）比较：已配置上游的 host 都是 127.0.0.1，
// 所以 realm 的 host 用 "localhost" 名字——与上游主机名不同，必为不受信，
// 请求必须在拨号前被拒。evil 服务监听 127.0.0.1:port，若闸门失守，
// Go 的拨号器对 localhost 会回退 127.0.0.1 连到 evil，请求会被记录，
// 失守可被检出。
func TestDocker401TokenExchangeUntrustedRealm(t *testing.T) {
	evilHits := make(chan string, 4)
	_ = evilHits
	evil := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		evilHits <- r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer evil.Close()

	var evilRealm string
	{
		u, _ := url.Parse(evil.URL)
		evilRealm = "localhost:" + u.Port()
	}

	authed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2/lib/x/manifests/y" && r.Header.Get("Authorization") == "" {
			w.Header().Set("WWW-Authenticate",
				`Bearer realm="http://`+evilRealm+`/auth/token",service="registry.test"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"manifest":"ok"}`))
	}))
	defer authed.Close()

	good := newStatusServer(t, http.StatusOK, `{"manifest":"ok2"}`)
	defer good.Close()

	cfg := newTestCfg(nil)
	cfg.Docker.Enabled = true
	cfg.Docker.Upstreams = []string{authed.URL, good.URL}
	e := newDockerTestEngine(cfg)

	rec := httptest.NewRecorder()
	e.serveDocker(rec, httptest.NewRequest(http.MethodGet, "/v2/lib/x/manifests/y", nil))
	body, _ := io.ReadAll(rec.Body)
	if rec.Code != http.StatusOK || !strings.Contains(string(body), `"ok2"`) {
		t.Fatalf("不可信 realm 应换源到 good: code=%d body=%q", rec.Code, body)
	}
	select {
	case p := <-evilHits:
		t.Fatalf("不可信 realm 被访问（SSRF 闸门失守）: %s", p)
	default:
	}
	if st := e.dockerPool.Stats()[authed.URL]; st.BadCount == 0 {
		t.Fatalf("拒绝代换应记为上游失败: %+v", st)
	}
}

// TestDockerRealmTrustGate：token 代换的 SSRF 闸门语义——realm 主机只有在
// 属于已配置上游或内置引擎主机（按主机名、忽略端口）时才受信任；
// 内网/回环/陌生主机一律不受信任（Docker 官方认证由 isDockerAuthHost 单独放行）。
func TestDockerRealmTrustGate(t *testing.T) {
	cfg := newTestCfg(nil)
	cfg.Docker.Enabled = true
	cfg.Docker.Upstreams = []string{"https://mirror.example.com"}
	e := newDockerTestEngine(cfg)

	if !e.isTrustedDockerUpstreamHost("mirror.example.com", cfg) {
		t.Fatalf("已配置上游主机应受信任")
	}
	for _, h := range []string{"192.168.254.66", "10.0.0.1", "172.16.0.1", "127.0.0.1", "localhost", "internal.corp"} {
		if e.isTrustedDockerUpstreamHost(h, cfg) {
			t.Fatalf("名单外主机 %s 应不受信任", h)
		}
	}
	// 内置引擎接入后，其主机（127.0.0.1）转为受信任。
	e.dockerExtra = func() string { return "https://127.0.0.1:5443" }
	if !e.isTrustedDockerUpstreamHost("127.0.0.1", cfg) {
		t.Fatalf("内置引擎主机应受信任")
	}
}

// TestDocker401ManifestAuthed：manifest 请求已带 token 仍被 401 拒绝，
// 属于该上游服务不了，应换源而不是透传。
func TestDocker401ManifestAuthed(t *testing.T) {
	challenge := newStatusServer(t, http.StatusUnauthorized, `{}`)
	defer challenge.Close()
	good := newStatusServer(t, http.StatusOK, `{"manifest":"ok"}`)
	defer good.Close()

	cfg := newTestCfg(nil)
	cfg.Docker.Enabled = true
	cfg.Docker.Upstreams = []string{challenge.URL, good.URL}
	e := newDockerTestEngine(cfg)

	req := httptest.NewRequest(http.MethodGet, "/v2/lib/x/manifests/y", nil)
	req.Header.Set("Authorization", "Bearer fake-token")
	rec := httptest.NewRecorder()
	e.serveDocker(rec, req)
	body, _ := io.ReadAll(rec.Body)
	if rec.Code != http.StatusOK || !strings.Contains(string(body), `"ok"`) {
		t.Fatalf("带 token 的 manifest 401 应换源成功: code=%d body=%q", rec.Code, body)
	}
	if st := e.dockerPool.Stats()[challenge.URL]; st.BadCount == 0 {
		t.Fatalf("带 token 仍被拒应记失败: %+v", st)
	}
}

// TestDocker302RedirectAllowlist：上游 302 指向未列名域名时，
// 目标应被登记进观察名单、Location 改写为本机 /v2/_redirect，
// daemon 跟随中转应拿到 200；未登记过的陌生主机必须 403。
func TestDocker302RedirectAllowlist(t *testing.T) {
	cdn := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"blob":"ok"}`))
	}))
	defer cdn.Close()

	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", cdn.URL+"/v2/lib/x/blobs/sha256:abc")
		w.WriteHeader(http.StatusFound)
	}))
	defer upstream.Close()

	cfg := newTestCfg(nil)
	cfg.Docker.Enabled = true
	cfg.Docker.Upstreams = []string{upstream.URL}
	e := newDockerTestEngine(cfg)

	// 第一步：manifest 请求收到 302，Location 必须改写为本机中转端点。
	rec := httptest.NewRecorder()
	e.serveDocker(rec, httptest.NewRequest(http.MethodGet, "/v2/lib/x/manifests/y", nil))
	if rec.Code != http.StatusFound {
		t.Fatalf("302 应透传: code=%d", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, "/v2/_redirect?to=") {
		t.Fatalf("Location 应改写为本机中转端点: %q", loc)
	}
	cdnHost, _ := url.Parse(cdn.URL)
	if !e.redirectHostAllowed(stripPort(cdnHost.Host)) {
		t.Fatalf("302 目标主机应登记进观察名单: %s", stripPort(cdnHost.Host))
	}

	// 第二步：daemon 跟随 Location（去掉 scheme+host 换成相对路径）。
	to := loc[strings.Index(loc, "to=")+3:]
	rec2 := httptest.NewRecorder()
	e.serveDocker(rec2, httptest.NewRequest(http.MethodGet, "/v2/_redirect?to="+to, nil))
	body, _ := io.ReadAll(rec2.Body)
	if rec2.Code != http.StatusOK || !strings.Contains(string(body), `"ok"`) {
		t.Fatalf("跟随 _redirect 应拿到 200: code=%d body=%q", rec2.Code, body)
	}

	// 第三步：未登记过的陌生主机必须 403（SSRF 闸门未被观察名单冲掉）。
	rec3 := httptest.NewRecorder()
	e.serveDocker(rec3, httptest.NewRequest(http.MethodGet,
		"/v2/_redirect?to="+b64encode("https://evil.example.net/x"), nil))
	if rec3.Code != http.StatusForbidden {
		t.Fatalf("陌生主机应 403: code=%d", rec3.Code)
	}
}

// TestDockerDaemonHostSelfOrigin：daemon 经 registry-mirrors 访问时请求
// Host 是官方源域名（registry-1.docker.io）。302 Location 与 401 realm 的
// 改写 origin 必须是代理监听地址（回环请求 → 127.0.0.1:<listen>），
// 不能是官方域名——否则 daemon 被引向内网不可达地址，整次拉取超时。
func TestDockerDaemonHostSelfOrigin(t *testing.T) {
	cdn := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer cdn.Close()

	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", cdn.URL+"/v2/lib/x/blobs/sha256:abc")
		w.WriteHeader(http.StatusFound)
	}))
	defer upstream.Close()

	cfg := newTestCfg(nil)
	cfg.Docker.Enabled = true
	cfg.Docker.Upstreams = []string{upstream.URL}
	e := newDockerTestEngine(cfg)
	port := "37710"
	if _, p, err := net.SplitHostPort(cfg.Proxy.Listen); err == nil {
		port = p
	}

	// 302：daemon 形态（Host=registry-1.docker.io）的 Location 必须指回监听地址。
	req := httptest.NewRequest(http.MethodGet, "/v2/lib/x/manifests/y", nil)
	req.Host = "registry-1.docker.io"
	req.RemoteAddr = "127.0.0.1:54321" // 本机 daemon（httptest 默认远端是测试网段，需显式回环）
	rec := httptest.NewRecorder()
	e.serveDocker(rec, req)
	loc := rec.Header().Get("Location")
	want := "http://127.0.0.1:" + port + "/v2/_redirect?to="
	if rec.Code != http.StatusFound || !strings.HasPrefix(loc, want) {
		t.Fatalf("daemon 形态 302 应指回监听地址 %s…: code=%d loc=%q", want, rec.Code, loc)
	}
	if strings.Contains(loc, "registry-1.docker.io") {
		t.Fatalf("Location 不得指向官方域名: %q", loc)
	}

	// 401 握手质询：realm 同样必须改写到监听地址。
	chal := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="https://auth.docker.io/token",service="registry.docker.io"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer chal.Close()
	cfg2 := newTestCfg(nil)
	cfg2.Docker.Enabled = true
	cfg2.Docker.Upstreams = []string{chal.URL}
	e2 := newDockerTestEngine(cfg2)
	req2 := httptest.NewRequest(http.MethodGet, "/v2/", nil)
	req2.Host = "registry-1.docker.io"
	req2.RemoteAddr = "127.0.0.1:54321"
	rec2 := httptest.NewRecorder()
	e2.serveDocker(rec2, req2)
	if rec2.Code != http.StatusUnauthorized {
		t.Fatalf("/v2/ 401 质询应透传: code=%d", rec2.Code)
	}
	realm := rec2.Header().Get("WWW-Authenticate")
	if !strings.Contains(realm, `realm="http://127.0.0.1:`+port+`/v2/token?_u=`) {
		t.Fatalf("realm 应改写到监听地址: %q", realm)
	}
	if strings.Contains(realm, "realm=\"https://auth.docker.io") {
		t.Fatalf("realm 不得保留官方地址: %q", realm)
	}

	// 局域网 daemon（Host=官方域名，远端非回环）：origin 必须用本机主 LAN IP。
	req4 := httptest.NewRequest(http.MethodGet, "/v2/lib/x/manifests/y", nil)
	req4.Host = "registry-1.docker.io"
	req4.RemoteAddr = "192.0.2.76:12345"
	rec4 := httptest.NewRecorder()
	e.serveDocker(rec4, req4)
	loc4 := rec4.Header().Get("Location")
	if strings.Contains(loc4, "registry-1.docker.io") || strings.Contains(loc4, "127.0.0.1:") {
		t.Fatalf("局域网 daemon 的 Location 不得指向官方域名或回环: %q", loc4)
	}
	if lan := netutil.PrimaryLAN(); lan != "" {
		if want4 := "http://" + lan + ":" + port + "/v2/_redirect?to="; !strings.HasPrefix(loc4, want4) {
			t.Fatalf("局域网 daemon 的 Location 应指主 LAN IP: want %q… got %q", want4, loc4)
		}
	}

	// 非 daemon 形态（Host 非官方域名）保持旧行为：origin 仍取请求 Host。
	req3 := httptest.NewRequest(http.MethodGet, "/v2/lib/x/manifests/y", nil)
	rec3 := httptest.NewRecorder()
	e.serveDocker(rec3, req3)
	loc3 := rec3.Header().Get("Location")
	if want3 := "http://" + req3.Host + "/v2/_redirect?to="; !strings.HasPrefix(loc3, want3) {
		t.Fatalf("普通 Host 的改写 origin 应保持请求 Host: want %q… got %q", want3, loc3)
	}
}

// TestDockerRedirect401TokenSwap：中转目标回 401 质询时（daemon 的 token
// 由另一面镜像签发、本目标不认，或 daemon 全程匿名），代理必须代换 token
// 并重放，daemon 只见 200；换 token 失败时质询才交回 daemon。
func TestDockerRedirect401TokenSwap(t *testing.T) {
	tokenOK := atomic.Bool{}
	tokenSrv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/openapi/v1/auth/token" {
			tokenOK.Store(true)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"token":"tok123"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer tokenSrv.Close()

	relay := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer tok123" {
			_, _ = w.Write([]byte(`{"manifest":"ok"}`))
			return
		}
		w.Header().Set("WWW-Authenticate",
			`Bearer realm="`+tokenSrv.URL+`/openapi/v1/auth/token",service="docker.1ms.run",scope="repository:library/nginx:pull"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer relay.Close()

	cfg := newTestCfg(nil)
	cfg.Docker.Enabled = true
	cfg.Docker.Upstreams = []string{relay.URL}
	e := newDockerTestEngine(cfg)
	relayHost, _ := url.Parse(relay.URL)
	e.dockerRedirectAllow.add(stripPort(relayHost.Host)) // httptest 地址带端口，过不了上游名单比对，按"302 已登记"放行

	// 匿名 daemon：_redirect 中转目标 401 → 代换 token 重放 → 200。
	req := httptest.NewRequest(http.MethodGet,
		"/v2/_redirect?to="+b64encode(relay.URL+"/v2/lib/x/manifests/y"), nil)
	rec := httptest.NewRecorder()
	e.serveDocker(rec, req)
	body, _ := io.ReadAll(rec.Body)
	if rec.Code != http.StatusOK || !strings.Contains(string(body), `"ok"`) {
		t.Fatalf("401 质询应代换 token 重放拿到 200: code=%d body=%q", rec.Code, body)
	}
	if !tokenOK.Load() {
		t.Fatalf("应经 realm 换过 token")
	}

	// 换 token 失败（realm 不可达）：质询原样交回 daemon（401）。
	deadRealm := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	deadRealm.Close() // 立即关掉，realm 不可达
	relay2 := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			w.Header().Set("WWW-Authenticate",
				`Bearer realm="`+deadRealm.URL+`/token",service="x"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"manifest":"ok"}`))
	}))
	defer relay2.Close()
	cfg2 := newTestCfg(nil)
	cfg2.Docker.Enabled = true
	cfg2.Docker.Upstreams = []string{relay2.URL}
	e2 := newDockerTestEngine(cfg2)
	relay2Host, _ := url.Parse(relay2.URL)
	e2.dockerRedirectAllow.add(stripPort(relay2Host.Host))
	req2 := httptest.NewRequest(http.MethodGet,
		"/v2/_redirect?to="+b64encode(relay2.URL+"/v2/lib/x/manifests/y"), nil)
	rec2 := httptest.NewRecorder()
	e2.serveDocker(rec2, req2)
	if rec2.Code != http.StatusUnauthorized {
		t.Fatalf("换 token 失败应透传 401 质询: code=%d", rec2.Code)
	}
	if rec2.Header().Get("WWW-Authenticate") == "" {
		t.Fatalf("透传的 401 应带 WWW-Authenticate")
	}
}

// TestDockerRedirectBlobLongTimeout：_redirect 本机路径恒为 /v2/_redirect，
// "是否大文件"必须按 to 参数解码后的真实路径判定。慢 CDN（>2s）的 blob
// 中转必须拿到长超时窗口成功返回；非 blob 路径仍受 2 秒小请求窗口约束。
func TestDockerRedirectBlobLongTimeout(t *testing.T) {
	const slow = 2600 * time.Millisecond
	cdn := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(slow)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`blob-bytes`))
	}))
	defer cdn.Close()
	cdnHost, _ := url.Parse(cdn.URL)
	e := newDockerTestEngine(newTestCfg(nil))
	e.dockerRedirectAllow.add(stripPort(cdnHost.Host)) // 模拟"上游 302 已登记"

	// blob 路径（解码后含 /blobs/）：2.6s 慢响应也必须成功。
	start := time.Now()
	rec := httptest.NewRecorder()
	e.serveDocker(rec, httptest.NewRequest(http.MethodGet,
		"/v2/_redirect?to="+b64encode(cdn.URL+"/v2/lib/x/blobs/sha256:abc"), nil))
	elapsed := time.Since(start)
	if rec.Code != http.StatusOK {
		t.Fatalf("blob 中转应走长超时窗口拿到 200（%v 后）: code=%d", elapsed, rec.Code)
	}
	if elapsed < slow {
		t.Fatalf("blob 中转提前结束，疑似仍被 2s 窗口掐断: %v", elapsed)
	}

	// 非 blob 路径：同样慢的响应必须 ~2s 被掐断（窗口语义保留）。
	start = time.Now()
	rec2 := httptest.NewRecorder()
	e.serveDocker(rec2, httptest.NewRequest(http.MethodGet,
		"/v2/_redirect?to="+b64encode(cdn.URL+"/v2/lib/x/manifests/y"), nil))
	elapsed = time.Since(start)
	if rec2.Code != http.StatusBadGateway {
		t.Fatalf("非 blob 慢响应应被 2s 窗口掐断回 502: code=%d", rec2.Code)
	}
	if elapsed > 4*time.Second {
		t.Fatalf("非 blob 窗口失效，等了太久: %v", elapsed)
	}
}
