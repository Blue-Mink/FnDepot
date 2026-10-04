package proxy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// newProbeServer 起一个可控状态码的 /v2/ 服务。
func newProbeServer(t *testing.T, code int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2/" {
			w.WriteHeader(code)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
}

func TestDockerProbeStatusCodes(t *testing.T) {
	// 200 → 存活
	s200 := newProbeServer(t, http.StatusOK)
	defer s200.Close()
	// 401 → 存活（认证质询）
	s401 := newProbeServer(t, http.StatusUnauthorized)
	defer s401.Close()
	// 500 → 失效
	s500 := newProbeServer(t, http.StatusInternalServerError)
	defer s500.Close()

	p := NewDockerPool()
	ctx := context.Background()

	r200 := p.probeOne(ctx, s200.URL)
	if !r200.OK {
		t.Errorf("200 应判存活，实际 %s", r200.Err)
	}
	r401 := p.probeOne(ctx, s401.URL)
	if !r401.OK {
		t.Errorf("401 应判存活，实际 %s", r401.Err)
	}
	r500 := p.probeOne(ctx, s500.URL)
	if r500.OK {
		t.Errorf("500 应判失效")
	}
}

func TestDockerProbeConnectionRefused(t *testing.T) {
	p := NewDockerPool()
	// 一个没有监听的地址 → 连接失败。
	res := p.probeOne(context.Background(), "http://127.0.0.1:1/v2/")
	if res.OK {
		t.Errorf("连接被拒应判失效")
	}
	if res.Err == "" {
		t.Errorf("应记录失败原因")
	}
}

func TestDockerRankedOrder(t *testing.T) {
	p := NewDockerPool()

	// 造三个上游：fast(快) / slow(慢) / dead(死)。
	fast := newProbeServer(t, http.StatusOK)
	slow := newProbeServer(t, http.StatusOK)
	defer fast.Close()
	defer slow.Close()

	// fast 给低延迟、slow 给高延迟，dead 直接失败。
	p.applyProbe(DockerUpstreamProbe{URL: fast.URL, Name: "fast", OK: true, LatencyMS: 50, At: time.Now()})
	p.applyProbe(DockerUpstreamProbe{URL: slow.URL, Name: "slow", OK: true, LatencyMS: 1200, At: time.Now()})
	p.applyProbe(DockerUpstreamProbe{URL: "http://127.0.0.1:1", Name: "dead", OK: false, Err: "refused", At: time.Now()})

	// 一个从未探活的。
	unseen := "http://example.com/unseen"

	got := p.Ranked([]string{slow.URL, unseen, fast.URL, "http://127.0.0.1:1"})
	// 期望：fast → slow → unseen → dead
	if len(got) != 4 {
		t.Fatalf("长度应为 4，实际 %d (%v)", len(got), got)
	}
	if got[0] != fast.URL || got[1] != slow.URL || got[2] != unseen || got[3] != "http://127.0.0.1:1" {
		t.Fatalf("排序错误：%v", got)
	}
}

func TestDockerRecordFailureCooldown(t *testing.T) {
	p := NewDockerPool()
	u := "http://example.com/x"
	// 先成功一次建立得分。
	p.Record(u, true, 100*time.Millisecond, nil)
	st := p.Stats()[u]
	if st.Score <= 0 {
		t.Fatalf("成功应有得分，实际 %+v", st)
	}
	// 连续失败 3 次应触发冷却。
	for i := 0; i < 3; i++ {
		p.Record(u, false, 0, errForTest())
	}
	st = p.Stats()[u]
	if !st.InCooldown {
		t.Fatalf("连续失败后应进入冷却，实际 %+v", st)
	}
	if st.OK {
		t.Fatalf("冷却中不应 OK")
	}
}

func errForTest() error { return &testErr{} }

type testErr struct{}

func (testErr) Error() string { return "boom" }

func TestDockerDecay(t *testing.T) {
	p := NewDockerPool()
	u := "http://example.com/y"
	p.applyProbe(DockerUpstreamProbe{URL: u, Name: "y", OK: true, LatencyMS: 20, At: time.Now()})
	high := p.Stats()[u].Score

	// 把 updated 推到 2 小时前（半衰期 30 分钟 → 4 个半衰期）。
	p.mu.Lock()
	p.stats[u].updated = time.Now().Add(-2 * time.Hour)
	p.mu.Unlock()

	p.Decay(time.Now())
	decayed := p.Stats()[u].Score
	if decayed >= high {
		t.Fatalf("衰减后得分应下降，before=%v after=%v", high, decayed)
	}
	// 应向 50 收敛而非归零。
	if decayed <= 0 {
		t.Fatalf("衰减不应归零，实际 %v", decayed)
	}
}

func TestNormalizeDockerUpstream(t *testing.T) {
	cases := map[string]string{
		"https://docker.1ms.run":  "https://docker.1ms.run",
		"https://docker.1ms.run/": "https://docker.1ms.run",
		"docker.1ms.run":          "https://docker.1ms.run",
		"official":                "official",
		"  https://a.b/c/ ":       "https://a.b/c",
		"":                        "",
	}
	for in, want := range cases {
		if got := normalizeDockerUpstream(in); got != want {
			t.Errorf("normalize(%q)=%q，期望 %q", in, got, want)
		}
	}
}

func TestProbeURLOfficial(t *testing.T) {
	if got := probeURL("official"); !strings.HasSuffix(got, "registry-1.docker.io/v2/") {
		t.Fatalf("official 探活应指向 registry-1.docker.io/v2/，实际 %q", got)
	}
}
