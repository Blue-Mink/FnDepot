package proxy

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ghpp/ghpp/internal/config"
	"github.com/ghpp/ghpp/internal/mirror"
)

// newFailoverTestEngine 构造换源测试用的完整 Engine。
//
// pool 用真实 Pool（调度侧的降分/冷却行为一并被验证），
// client 用默认 HTTP 客户端直打 httptest 上游，不触外网。
func newFailoverTestEngine(cfg *config.Config) *Engine {
	return &Engine{
		cfg:        func() *config.Config { return cfg },
		pool:       mirror.NewPool(),
		hosts:      config.HostSet(),
		client:     &http.Client{},
		dockerPool: NewDockerPool(),
		logf:       func(string, string, ...any) {},
	}
}

// newTestCfg 基于导出的 config.Default() 构造测试配置（mu 已初始化），
// 换上测试镜像并收紧失败阈值。
func newTestCfg(mirrors []config.Mirror) *config.Config {
	cfg := config.Default()
	cfg.Mode = config.ModeProxy
	cfg.Mirrors = mirrors
	cfg.Proxy.FailoverThreshold = 3
	cfg.Proxy.CooldownSeconds = 30
	return cfg
}

// rawTarget 用非"直连优先"的 raw 域名构造目标，
// 使测试不触发直连通路（避免真实网络依赖）。
func rawTarget(path string) *target {
	return &target{host: "raw.githubusercontent.com", path: path, category: config.CatRaw}
}

func newAbsRequest(t *testing.T) *http.Request {
	t.Helper()
	r, err := http.NewRequestWithContext(context.Background(), http.MethodGet,
		"https://raw.githubusercontent.com/o/r/f.txt", nil)
	if err != nil {
		t.Fatalf("构造请求失败: %v", err)
	}
	return r
}

// doRequest 直接走 serveAccelerated（加速通道的完整路径：选源→转发→换源）。
func doRequest(e *Engine, cfg *config.Config, r *http.Request, t *target) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	e.serveAccelerated(w, r, cfg, t)
	return w
}

// seedHot 把源 A 的成绩预置为"最优"，使加权随机首次几乎必然选中 A
// （权重 50 倍于初始分，Pick 首选概率 >99%）。
func seedHot(p *mirror.Pool, id string) {
	p.Record(id, 100*time.Millisecond, 1<<20, nil)
}

func TestMirrorFailoverOn400(t *testing.T) {
	// 上游 A：首次 400（模拟镜像不支持该路径），之后 200。
	var hitsA atomic.Int64
	srvA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if hitsA.Add(1) == 1 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte("unsupported"))
			return
		}
		_, _ = w.Write([]byte("ok-A"))
	}))
	defer srvA.Close()

	// 上游 B：恒 200。
	srvB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok-B"))
	}))
	defer srvB.Close()

	mirrors := []config.Mirror{
		{ID: "A", Name: "源A", URL: srvA.URL, Kind: config.KindPrefix, Enabled: true, Weight: 50},
		{ID: "B", Name: "源B", URL: srvB.URL, Kind: config.KindPrefix, Enabled: true, Weight: 1},
	}
	cfg := newTestCfg(mirrors)

	e := newFailoverTestEngine(cfg)
	seedHot(e.pool, "A")

	w := doRequest(e, cfg, newAbsRequest(t), rawTarget("/o/r/f.txt"))

	body, _ := io.ReadAll(w.Body)
	code := w.Code

	// 新行为：400 被换源，客户端最终拿到 200。
	// 旧代码会把首个源的 400 直接提交给客户端（本用例 99%+ 概率选中 A）。
	if code != http.StatusOK {
		t.Fatalf("客户端应看到 200（换源后成功），实际 %d body=%q", code, body)
	}
	// failovers=0 只可能是 B 被首选（~1%）；此时 body 必为 ok-B。
	// body=ok-A 却 failovers=0 在新行为下不可能出现（ok-A 只能来自第二次尝试）。
	if e.metrics.Failovers.Load() == 0 && string(body) != "ok-B" {
		t.Fatalf("换源未计数却拿到了非首选源内容: failovers=0 body=%q", body)
	}
	if e.metrics.Failed.Load() != 0 {
		t.Fatalf("换源成功不应计入失败, failed=%d", e.metrics.Failed.Load())
	}
}

func TestMirrorAll403PassThrough(t *testing.T) {
	// 两个源都恒 403（模拟全部源都拒绝）：
	// 客户端必须拿到 403 业务响应（含 body），而不是被降级成 502。
	mk := func(tag string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte("forbidden-" + tag))
		}))
	}
	srvA, srvB := mk("A"), mk("B")
	defer srvA.Close()
	defer srvB.Close()

	cfg := newTestCfg([]config.Mirror{
		{ID: "A", Name: "源A", URL: srvA.URL, Kind: config.KindPrefix, Enabled: true, Weight: 1},
		{ID: "B", Name: "源B", URL: srvB.URL, Kind: config.KindPrefix, Enabled: true, Weight: 1},
	})

	e := newFailoverTestEngine(cfg)
	w := doRequest(e, cfg, newAbsRequest(t), rawTarget("/o/r/f.txt"))

	body, _ := io.ReadAll(w.Body)
	if w.Code != http.StatusForbidden {
		t.Fatalf("全源 403 时应透传 403，实际 %d", w.Code)
	}
	if s := string(body); s != "forbidden-A" && s != "forbidden-B" {
		t.Fatalf("透传的 body 应来自某个源的业务响应，实际 %q", s)
	}
	if e.metrics.Failed.Load() != 0 {
		t.Fatalf("业务 403 透传不应计入 Failed, failed=%d", e.metrics.Failed.Load())
	}
}

func TestMirror404NoFailover(t *testing.T) {
	// 404 是真实"不存在"：直接透传，不换源（第二个源绝不被访问）。
	var hitsB atomic.Int64
	srvA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("nf"))
	}))
	defer srvA.Close()
	srvB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitsB.Add(1)
		_, _ = w.Write([]byte("ok-B"))
	}))
	defer srvB.Close()

	cfg := newTestCfg([]config.Mirror{
		{ID: "A", Name: "源A", URL: srvA.URL, Kind: config.KindPrefix, Enabled: true, Weight: 50},
		{ID: "B", Name: "源B", URL: srvB.URL, Kind: config.KindPrefix, Enabled: true, Weight: 1},
	})

	e := newFailoverTestEngine(cfg)
	seedHot(e.pool, "A")

	w := doRequest(e, cfg, newAbsRequest(t), rawTarget("/o/r/f.txt"))

	body, _ := io.ReadAll(w.Body)
	if w.Code != http.StatusNotFound || string(body) != "nf" {
		t.Fatalf("404 应直接透传, 实际 code=%d body=%q", w.Code, body)
	}
	if e.metrics.Failovers.Load() != 0 {
		t.Fatalf("404 不应触发换源, failovers=%d", e.metrics.Failovers.Load())
	}
	if hitsB.Load() != 0 {
		t.Fatalf("404 换源会多打一个源，B 被访问 %d 次", hitsB.Load())
	}
}

func TestMirror403WithAuthNoFailover(t *testing.T) {
	// 带 Authorization 的 403 是权限问题（与镜像无关）：直接透传，不换源。
	var hitsB atomic.Int64
	srvA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("no-authz"))
	}))
	defer srvA.Close()
	srvB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hitsB.Add(1)
		_, _ = w.Write([]byte("ok-B"))
	}))
	defer srvB.Close()

	cfg := newTestCfg([]config.Mirror{
		{ID: "A", Name: "源A", URL: srvA.URL, Kind: config.KindPrefix, Enabled: true, Weight: 50},
		{ID: "B", Name: "源B", URL: srvB.URL, Kind: config.KindPrefix, Enabled: true, Weight: 1},
	})

	e := newFailoverTestEngine(cfg)
	seedHot(e.pool, "A")

	r := newAbsRequest(t)
	r.Header.Set("Authorization", "Bearer test-token")

	w := doRequest(e, cfg, r, rawTarget("/o/r/f.txt"))

	body, _ := io.ReadAll(w.Body)
	if w.Code != http.StatusForbidden || string(body) != "no-authz" {
		t.Fatalf("带认证 403 应直接透传, 实际 code=%d body=%q", w.Code, body)
	}
	if e.metrics.Failovers.Load() != 0 || hitsB.Load() != 0 {
		t.Fatalf("带认证 403 不应换源: failovers=%d B访问=%d",
			e.metrics.Failovers.Load(), hitsB.Load())
	}
}

func TestTryMirrorHeldSemantics(t *testing.T) {
	// 直接测 tryMirror 的三态：
	//   200 → done=true 提交；400 → done=false 且 held 非 nil（body 未关闭）；
	//   404 → done=true 提交（不保留）。
	cases := []struct {
		name     string
		status   int
		body     string
		auth     bool
		wantDone bool
		wantHeld bool
	}{
		{"200 提交", http.StatusOK, "fine", false, true, false},
		{"400 保留", http.StatusBadRequest, "bad-req", false, false, true},
		{"403 无认证保留", http.StatusForbidden, "denied", false, false, true},
		{"403 带认证提交", http.StatusForbidden, "denied-auth", true, true, false},
		{"404 提交", http.StatusNotFound, "nf", false, true, false},
		{"429 保留", http.StatusTooManyRequests, "slow-down", false, false, true},
		{"500 换源但不保留", http.StatusInternalServerError, "oops", false, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()

			cfg := newTestCfg([]config.Mirror{
				{ID: "X", Name: "源X", URL: srv.URL, Kind: config.KindPrefix, Enabled: true},
			})
			e := newFailoverTestEngine(cfg)

			r := newAbsRequest(t)
			if tc.auth {
				r.Header.Set("Authorization", "Bearer x")
			}
			w := httptest.NewRecorder()
			done, held, err := e.tryMirror(w, r, cfg, rawTarget("/o/r/f.txt"), cfg.Mirrors[0])

			if done != tc.wantDone {
				t.Fatalf("done=%v, want %v (err=%v)", done, tc.wantDone, err)
			}
			if (held != nil) != tc.wantHeld {
				t.Fatalf("held=%v, want %v", held != nil, tc.wantHeld)
			}
			if held != nil {
				// held 的 body 必须仍可读（所有权已移交调用方）。
				b, err := io.ReadAll(held.Body)
				if err != nil || string(b) != tc.body {
					t.Fatalf("held body 不可读或内容不符: %q %v", b, err)
				}
				if held.StatusCode != tc.status {
					t.Fatalf("held 状态码=%d, want %d", held.StatusCode, tc.status)
				}
				_ = held.Body.Close()
			}
			if done {
				if w.Code != tc.status {
					t.Fatalf("提交的响应 code=%d, want %d", w.Code, tc.status)
				}
				b, _ := io.ReadAll(w.Body)
				if string(b) != tc.body {
					t.Fatalf("提交的 body=%q, want %q", b, tc.body)
				}
			}
		})
	}
}

// TestMirrorFailoverBodyPreserved 验证换源重试时请求 body 被完整重放
// （旧实现首个源消耗 body 后，第二个源会收到空 body）。
func TestMirrorFailoverBodyPreserved(t *testing.T) {
	var gotA, gotB atomic.Value
	srvA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotA.Store(string(b))
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("x"))
	}))
	defer srvA.Close()
	srvB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotB.Store(string(b))
		_, _ = w.Write([]byte("ok-B"))
	}))
	defer srvB.Close()

	cfg := newTestCfg([]config.Mirror{
		{ID: "A", Name: "源A", URL: srvA.URL, Kind: config.KindPrefix, Enabled: true, Weight: 50},
		{ID: "B", Name: "源B", URL: srvB.URL, Kind: config.KindPrefix, Enabled: true, Weight: 1},
	})
	// 阈值压到 1：A 首次失败即进冷却，第二次选源必然是 B（消除加权随机的偶发重选）。
	cfg.Proxy.FailoverThreshold = 1

	e := newFailoverTestEngine(cfg)
	seedHot(e.pool, "A")

	body := strings.Repeat("payload-", 100)
	r, _ := http.NewRequestWithContext(context.Background(), http.MethodPost,
		"https://raw.githubusercontent.com/o/r/f.txt", strings.NewReader(body))
	r.ContentLength = int64(len(body))

	w := doRequest(e, cfg, r, rawTarget("/o/r/f.txt"))

	if w.Code != http.StatusOK {
		t.Fatalf("换源后应成功, code=%d", w.Code)
	}
	// A 先收到 400，B 收到 200；两个源拿到的 body 都必须完整。
	if v := gotA.Load(); v != nil && v.(string) != body {
		t.Fatalf("源A 收到的 body 不完整: len=%d want %d", len(v.(string)), len(body))
	}
	if v := gotB.Load(); v != nil && v.(string) != body {
		t.Fatalf("源B 收到的 body 不完整（换源重试丢了 body）: len=%d want %d", len(v.(string)), len(body))
	}
}
