package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"fnos-store/internal/config"
	"fnos-store/internal/mirror"
)

func newMonitorServer(t *testing.T, initialMirror string) *Server {
	t.Helper()
	mgr := config.NewManager(t.TempDir())
	cfg := mgr.Get()
	cfg.Mirror = initialMirror
	if err := mgr.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	s := &Server{configMgr: mgr, mirrorMon: mirror.New()}
	return s
}

func TestMaybeAutoSwitch_SwitchesDegradedMirror(t *testing.T) {
	s := newMonitorServer(t, "gh-ddlc")
	// gh-ddlc 连续失败 3 次；gh-proxy-hk 健康
	for i := 0; i < mirrorSwitchFails; i++ {
		s.mirrorMon.Record("gh-ddlc", "GH DDLC", false, 10, 0)
	}
	s.mirrorMon.Record("gh-proxy-hk", "GH-Proxy HK", true, 400, 0)

	s.maybeAutoSwitch()

	got := s.configMgr.Get()
	if got.Mirror != "gh-proxy-hk" {
		t.Fatalf("expected auto-switch to gh-proxy-hk, got %q", got.Mirror)
	}
	sw := s.mirrorMon.LastSwitch()
	if sw == nil || sw.From != "gh-ddlc" || sw.To != "gh-proxy-hk" || sw.Reason == "" {
		t.Fatalf("last switch not recorded correctly: %+v", sw)
	}
}

func TestMaybeAutoSwitch_NoSwitchBelowThreshold(t *testing.T) {
	s := newMonitorServer(t, "gh-ddlc")
	for i := 0; i < mirrorSwitchFails-1; i++ {
		s.mirrorMon.Record("gh-ddlc", "GH DDLC", false, 10, 0)
	}
	s.mirrorMon.Record("gh-proxy-hk", "GH-Proxy HK", true, 400, 0)

	s.maybeAutoSwitch()

	if got := s.configMgr.Get().Mirror; got != "gh-ddlc" {
		t.Fatalf("should not switch below threshold, got %q", got)
	}
	if sw := s.mirrorMon.LastSwitch(); sw != nil {
		t.Fatalf("no switch should be recorded: %+v", sw)
	}
}

func TestMaybeAutoSwitch_NoOpForAutoMode(t *testing.T) {
	s := newMonitorServer(t, "auto")
	for i := 0; i < mirrorSwitchFails + 1; i++ {
		s.mirrorMon.Record("gh-ddlc", "GH DDLC", false, 10, 0)
	}
	s.mirrorMon.Record("gh-proxy-hk", "GH-Proxy HK", true, 400, 0)

	s.maybeAutoSwitch()

	// auto 模式本来就动态选最优，无需持久化切换
	if got := s.configMgr.Get().Mirror; got != "auto" {
		t.Fatalf("auto mode must not be rewritten, got %q", got)
	}
}

func TestMaybeAutoSwitch_NoOpWhenNoStableMirror(t *testing.T) {
	s := newMonitorServer(t, "gh-ddlc")
	for i := 0; i < mirrorSwitchFails; i++ {
		s.mirrorMon.Record("gh-ddlc", "GH DDLC", false, 10, 0)
	}
	// 没有健康的可切换目标
	s.mirrorMon.Record("gh-proxy", "GH-Proxy", false, 10, 0)

	s.maybeAutoSwitch()

	if got := s.configMgr.Get().Mirror; got != "gh-ddlc" {
		t.Fatalf("should keep selection when no stable mirror exists, got %q", got)
	}
}

// ---- Docker 镜像加速自动切换 ----

func newDockerMonitorServer(t *testing.T, initialDockerMirror string) *Server {
	t.Helper()
	mgr := config.NewManager(t.TempDir())
	cfg := mgr.Get()
	cfg.DockerMirror = initialDockerMirror
	if err := mgr.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	return &Server{configMgr: mgr, dockerMirrorMon: mirror.New()}
}

func TestMaybeAutoSwitchDocker_SwitchesDegradedMirror(t *testing.T) {
	s := newDockerMonitorServer(t, "docker-1ms")
	for i := 0; i < mirrorSwitchFails; i++ {
		s.dockerMirrorMon.Record("docker-1ms", "1ms.run", false, 10, 0)
	}
	s.dockerMirrorMon.Record("daocloud", "DaoCloud", true, 400, 0)

	s.maybeAutoSwitchDocker()

	got := s.configMgr.Get()
	if got.DockerMirror != "daocloud" {
		t.Fatalf("expected auto-switch to daocloud, got %q", got.DockerMirror)
	}
	sw := s.dockerMirrorMon.LastSwitch()
	if sw == nil || sw.From != "docker-1ms" || sw.To != "daocloud" || sw.Reason == "" {
		t.Fatalf("last switch not recorded correctly: %+v", sw)
	}
}

func TestMaybeAutoSwitchDocker_NoSwitchBelowThreshold(t *testing.T) {
	s := newDockerMonitorServer(t, "docker-1ms")
	for i := 0; i < mirrorSwitchFails-1; i++ {
		s.dockerMirrorMon.Record("docker-1ms", "1ms.run", false, 10, 0)
	}
	s.dockerMirrorMon.Record("daocloud", "DaoCloud", true, 400, 0)

	s.maybeAutoSwitchDocker()

	if got := s.configMgr.Get().DockerMirror; got != "docker-1ms" {
		t.Fatalf("should not switch below threshold, got %q", got)
	}
	if sw := s.dockerMirrorMon.LastSwitch(); sw != nil {
		t.Fatalf("no switch should be recorded: %+v", sw)
	}
}

func TestMaybeAutoSwitchDocker_NoOpForAutoAndDirectModes(t *testing.T) {
	for _, mode := range []string{"auto", "direct", "custom"} {
		t.Run(mode, func(t *testing.T) {
			s := newDockerMonitorServer(t, mode)
			for i := 0; i < mirrorSwitchFails+1; i++ {
				s.dockerMirrorMon.Record("docker-1ms", "1ms.run", false, 10, 0)
			}
			s.dockerMirrorMon.Record("daocloud", "DaoCloud", true, 400, 0)

			s.maybeAutoSwitchDocker()

			if got := s.configMgr.Get().DockerMirror; got != mode {
				t.Fatalf("%s mode must not be rewritten, got %q", mode, got)
			}
		})
	}
}

func TestDockerProbeURLFor(t *testing.T) {
	cases := map[string]string{
		"daocloud": "https://m.daocloud.io/v2/",
		"docker-1ms": "https://docker.1ms.run/v2/",
		"nju-ghcr": "https://ghcr.nju.edu.cn/v2/",
		"kspeeder": "https://127.0.0.1:5443/v2/",
		"custom":   "",
		"direct":   "",
		"auto":     "",
	}
	for _, m := range config.DockerMirrorOptions() {
		if want, ok := cases[m.Key]; ok && dockerProbeURLFor(m) != want {
			t.Fatalf("dockerProbeURLFor(%s) = %q, want %q", m.Key, dockerProbeURLFor(m), want)
		}
	}
	if got := dockerProbeURLFor(config.DockerMirror{Key: "custom", URL: "my.mirror/"}); got != "https://my.mirror/v2/" {
		t.Fatalf("custom URL probe = %q, want https://my.mirror/v2/", got)
	}
}

func TestCheckRegistry_StatusCodes(t *testing.T) {
	// Docker registry ping 语义：200 与 401（未鉴权挑战）均为存活
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/":
			w.WriteHeader(http.StatusOK)
		case "/v2-401/":
			w.Header().Set("WWW-Authenticate", `Bearer realm="x"`)
			w.WriteHeader(http.StatusUnauthorized)
		case "/v2-403/":
			w.WriteHeader(http.StatusForbidden)
		case "/v2-404/":
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	cases := []struct {
		path string
		want string
	}{
		{"/v2/", "ok"},
		{"/v2-401/", "ok"}, // 401 = registry 存活（Docker 标准 ping 响应）
		{"/v2-403/", "error"},
		{"/v2-404/", "error"},
	}
	for _, c := range cases {
		if _, got := checkRegistry(context.Background(), srv.URL+c.path); got != c.want {
			t.Fatalf("checkRegistry(%s) = %q, want %q", c.path, got, c.want)
		}
	}
	// 连接失败（关端口）
	if _, got := checkRegistry(context.Background(), "http://127.0.0.1:1/v2/"); got != "error" {
		t.Fatalf("unreachable registry should be error, got %q", got)
	}
}

// TestCheckRegistry_SelfSignedTLS：自签名证书 registry（KSpeeder 场景）——
// 默认 client 校验失败，kspeederProbeClient（loopback 专用）应判活。
func TestCheckRegistry_SelfSignedTLS(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	if _, got := checkRegistry(context.Background(), srv.URL+"/v2/"); got != "error" {
		t.Fatalf("default client must reject self-signed cert, got %q", got)
	}
	if _, got := checkRegistryWith(kspeederProbeClient, context.Background(), srv.URL+"/v2/"); got != "ok" {
		t.Fatalf("loopback client should accept self-signed cert, got %q", got)
	}
}
