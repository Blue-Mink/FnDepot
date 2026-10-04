// dockerpool.go 实现 Docker 上游的连通性探测、打分与优先排序。
//
// 与 GitHub 加速源（internal/mirror）同构：
//   - 每个上游维护延迟的指数移动得分，兼顾稳定性与实时性；
//   - 连续失败达到阈值后进入冷却期，冷却内自动沉底，结束后凭新数据回归；
//   - 选择时按"健康按分降序 → 未探活 → 失效/冷却沉底"排序，
//     请求始终优先走最优上游，官方源恒为末尾兜底（不进入池内竞争）。
//
// 探活端点用 Registry v2 的 ping（GET /v2/）：
// 200（无需认证）与 401（带 WWW-Authenticate 的认证质询）都表示上游存活，
// 5xx / 连接失败 / 超时判为失效。
package proxy

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

// dockerRefLatencyMS 是 Docker 上游打分的参考延迟基准。
//
// Registry ping 是小请求，延迟比 GitHub 源更低；
// 100ms → 93 分、500ms → 75 分、1500ms → 50 分，量纲贴合实际。
const dockerRefLatencyMS = 1500.0

// dockerUpstreamOfficialURL 是 "official" 兜底对应的真实 Registry 地址。
const dockerUpstreamOfficialURL = "https://registry-1.docker.io"

// dockerStat 保存单个 Docker 上游的运行期统计。
type dockerStat struct {
	url        string
	name       string
	latency    float64 // 平滑后的延迟毫秒
	score      float64 // 0~100 综合得分，0 表示尚无有效数据
	probed     bool    // 是否完成过至少一次探活
	okCount    int64
	badCount   int64
	failStreak int
	cooldown   time.Time
	updated    time.Time
	lastErr    string
}

// DockerUpstreamProbe 是一次上游探活的结果。
type DockerUpstreamProbe struct {
	// URL 是被探上游（规范化后）。
	URL string `json:"url"`
	// Name 是展示名（主机名；official 为 Docker 官方）。
	Name string `json:"name"`
	// OK 表示探活是否成功。
	OK bool `json:"ok"`
	// LatencyMS 是 ping 延迟毫秒数。
	LatencyMS int64 `json:"latency_ms"`
	// Err 记录失败原因。
	Err string `json:"error,omitempty"`
	// At 是探活发生的时刻。
	At time.Time `json:"at"`
}

// DockerUpstreamStat 是上游的聚合统计，供 /api/docker 展示。
type DockerUpstreamStat struct {
	URL        string    `json:"url"`
	Name       string    `json:"name"`
	OK         bool      `json:"ok"`
	Probed     bool      `json:"probed"`
	LatencyMS  int64     `json:"latency_ms"`
	Score      float64   `json:"score"`
	LastCheck  time.Time `json:"last_check"`
	Error      string    `json:"error,omitempty"`
	InCooldown bool      `json:"in_cooldown,omitempty"`
	// BadCount 是累计失败次数（探活与真实转发合并计数）。
	BadCount int64 `json:"bad_count,omitempty"`
}

// DockerPool 管理 Docker 上游的探测、打分与排序。
type DockerPool struct {
	mu     sync.RWMutex
	stats  map[string]*dockerStat
	client *http.Client
}

// NewDockerPool 创建 Docker 上游池。
func NewDockerPool() *DockerPool {
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   3 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConns:        32,
		MaxIdleConnsPerHost: 8,
		IdleConnTimeout:     60 * time.Second,
		TLSHandshakeTimeout: 3 * time.Second,
		ForceAttemptHTTP2:   true,
		// 内置 KSpeeder 引擎的证书 CN 为官方域名（linkease.net），
		// 与 127.0.0.1 不匹配，需跳过主机名校验（仍是本机回环流量）。
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12},
	}
	return &DockerPool{
		stats: make(map[string]*dockerStat),
		client: &http.Client{
			Transport: transport,
			Timeout:   6 * time.Second,
		},
	}
}

// normalizeDockerUpstream 规范化上游标识：
// 补 https:// 前缀、去尾斜杠；"official" 保持原样。
func normalizeDockerUpstream(u string) string {
	u = strings.TrimSpace(u)
	if u == "" {
		return ""
	}
	if u == dockerUpstreamOfficial {
		return u
	}
	if !strings.Contains(u, "://") {
		u = "https://" + u
	}
	return strings.TrimSuffix(u, "/")
}

// upstreamName 返回上游的展示名。
func upstreamName(u string) string {
	if u == dockerUpstreamOfficial {
		return "Docker 官方（兜底）"
	}
	parsed, err := url.Parse(u)
	if err == nil && parsed.Hostname() != "" {
		return parsed.Hostname()
	}
	return u
}

// probeURL 返回探活实际请求的地址。
func probeURL(u string) string {
	base := u
	if base == dockerUpstreamOfficial {
		base = dockerUpstreamOfficialURL
	}
	return base + "/v2/"
}

// probeOne 探活单个上游。
func (p *DockerPool) probeOne(ctx context.Context, u string) DockerUpstreamProbe {
	res := DockerUpstreamProbe{URL: u, Name: upstreamName(u), At: time.Now()}

	reqCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, probeURL(u), nil)
	if err != nil {
		res.Err = "构造探测请求失败: " + err.Error()
		return res
	}
	req.Header.Set("User-Agent", "docker/ghpp-monitor")

	start := time.Now()
	resp, err := p.client.Do(req)
	if err != nil {
		res.Err = "连接失败: " + err.Error()
		return res
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	_ = resp.Body.Close()

	res.LatencyMS = time.Since(start).Milliseconds()

	switch {
	case resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusUnauthorized:
		// 200 = 无需认证；401 = 带认证质询（WWW-Authenticate），两者都证明 Registry 存活。
		res.OK = true
	default:
		res.Err = fmt.Sprintf("返回状态码 %d", resp.StatusCode)
	}
	return res
}

// Probe 并发探活全部上游（并发 4），并把结果并入统计。
func (p *DockerPool) Probe(ctx context.Context, urls []string) []DockerUpstreamProbe {
	seen := make(map[string]bool, len(urls))
	var list []string
	for _, u := range urls {
		n := normalizeDockerUpstream(u)
		if n == "" || seen[n] {
			continue
		}
		seen[n] = true
		list = append(list, n)
	}

	const concurrency = 4
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex
	results := make([]DockerUpstreamProbe, 0, len(list))

	for _, u := range list {
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(u string) {
			defer wg.Done()
			defer func() { <-sem }()
			res := p.probeOne(ctx, u)
			mu.Lock()
			results = append(results, res)
			mu.Unlock()
			p.applyProbe(res)
		}(u)
	}
	wg.Wait()
	return results
}

// applyProbe 把一次探活结果并入得分表。
func (p *DockerPool) applyProbe(res DockerUpstreamProbe) {
	p.mu.Lock()
	defer p.mu.Unlock()

	st := p.stats[res.URL]
	if st == nil {
		st = &dockerStat{url: res.URL, name: res.Name}
		p.stats[res.URL] = st
	}
	st.probed = true
	st.updated = res.At

	if !res.OK {
		st.badCount++
		st.failStreak++
		st.lastErr = res.Err
		st.score *= 0.6
		if st.score < 1 {
			st.score = 0
		}
		if st.failStreak >= 3 && st.cooldown.Before(res.At.Add(2*time.Minute)) {
			st.cooldown = res.At.Add(2 * time.Minute)
		}
		return
	}

	st.okCount++
	st.failStreak = 0
	st.lastErr = ""
	st.cooldown = time.Time{}

	lat := float64(res.LatencyMS)
	if lat <= 0 {
		lat = 1
	}
	if st.score == 0 {
		// 首个有效样本直接采用，避免从零起步。
		st.latency = lat
	} else {
		st.latency = st.latency*0.5 + lat*0.5
	}
	st.score = dockerScoreOf(st.latency)
}

// dockerScoreOf 把延迟换算为 0~100 得分。
func dockerScoreOf(latencyMS float64) float64 {
	if latencyMS <= 0 {
		latencyMS = 1
	}
	return dockerRefLatencyMS / (dockerRefLatencyMS + latencyMS) * 100
}

// Record 记录一次真实转发请求的表现（成功或失败），用于在线学习。
func (p *DockerPool) Record(u string, ok bool, latency time.Duration, err error) {
	n := normalizeDockerUpstream(u)
	if n == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	st := p.stats[n]
	if st == nil {
		st = &dockerStat{url: n, name: upstreamName(n)}
		p.stats[n] = st
	}
	now := time.Now()
	st.updated = now
	// 真实转发本身就是一次连通性证据：记为已探活，
	// 否则从未被 Probe 命中的上游得分会被 Ranked 忽略（恒按未探活段排序）。
	st.probed = true

	if !ok {
		st.badCount++
		st.failStreak++
		msg := ""
		if err != nil {
			msg = err.Error()
			if len(msg) > 120 {
				msg = msg[:120]
			}
		}
		if msg == "" {
			msg = "上游不可用"
		}
		st.lastErr = msg
		st.score *= 0.5
		if st.score < 1 {
			st.score = 0
		}
		if st.failStreak >= 3 && st.cooldown.Before(now.Add(2*time.Minute)) {
			st.cooldown = now.Add(2 * time.Minute)
		}
		return
	}

	st.okCount++
	st.failStreak = 0
	st.lastErr = ""
	st.cooldown = time.Time{}

	lat := float64(latency.Milliseconds())
	if lat <= 0 {
		lat = 1
	}
	if st.score == 0 {
		st.latency = lat
	} else {
		st.latency = st.latency*0.7 + lat*0.3
	}
	st.score = dockerScoreOf(st.latency)
}

// inCooldown 在持锁状态下判断上游是否处于冷却期。
func (p *DockerPool) inCooldownLocked(u string, now time.Time) bool {
	st := p.stats[u]
	return st != nil && now.Before(st.cooldown)
}

// Ranked 返回按优先级排序的上游列表。
//
// 分段：健康（得分降序）→ 未探活（保持传入顺序）→ 失效/冷却（得分降序，沉底）。
func (p *DockerPool) Ranked(urls []string) []string {
	seen := make(map[string]bool, len(urls))
	var list []string
	for _, u := range urls {
		n := normalizeDockerUpstream(u)
		if n == "" || seen[n] {
			continue
		}
		seen[n] = true
		list = append(list, n)
	}

	p.mu.RLock()
	type row struct {
		u   string
		seg int
		sc  float64
	}
	rows := make([]row, 0, len(list))
	now := time.Now()
	for _, u := range list {
		seg := 0
		sc := 0.0
		st := p.stats[u]
		if st == nil {
			seg = 1
		} else if st.probed && (st.score <= 0 || now.Before(st.cooldown)) {
			seg = 2
		} else if !st.probed {
			seg = 1
		} else {
			sc = st.score
		}
		rows = append(rows, row{u: u, seg: seg, sc: sc})
	}
	p.mu.RUnlock()

	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].seg != rows[j].seg {
			return rows[i].seg < rows[j].seg
		}
		return rows[i].sc > rows[j].sc
	})
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.u
	}
	return out
}

// Stats 返回所有已知上游的聚合统计快照。
func (p *DockerPool) Stats() map[string]DockerUpstreamStat {
	p.mu.RLock()
	defer p.mu.RUnlock()

	out := make(map[string]DockerUpstreamStat, len(p.stats))
	for u, st := range p.stats {
		cooling := time.Now().Before(st.cooldown)
		errMsg := st.lastErr
		if cooling && st.lastErr != "" {
			errMsg = fmt.Sprintf("%s（冷却至 %s）", st.lastErr, st.cooldown.Format("15:04:05"))
		}
		out[u] = DockerUpstreamStat{
			URL:        u,
			Name:       st.name,
			OK:         st.probed && st.score > 0 && !cooling,
			Probed:     st.probed,
			LatencyMS:  int64(st.latency),
			Score:      st.score,
			LastCheck:  st.updated,
			Error:      errMsg,
			InCooldown: cooling,
			BadCount:   st.badCount,
		}
	}
	return out
}

// redirectAllowSet 是"受信任上游 302 观察到的目标主机"允许名单（TTL 过期淘汰）。
type redirectAllowSet struct {
	mu    sync.Mutex
	hosts map[string]time.Time
}

// const 放在类型外以符合 gofmt 习惯。
const redirectAllowTTL = 30 * time.Minute

func newRedirectAllowSet() *redirectAllowSet {
	return &redirectAllowSet{hosts: make(map[string]time.Time)}
}

// add 登记一个 302 目标主机。
func (s *redirectAllowSet) add(host string) {
	if host == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for h, exp := range s.hosts {
		if now.After(exp) {
			delete(s.hosts, h)
		}
	}
	if len(s.hosts) >= 512 {
		for h := range s.hosts {
			delete(s.hosts, h)
		}
	}
	s.hosts[host] = now.Add(redirectAllowTTL)
}

// allowed 判断主机是否在有效名单内。
func (s *redirectAllowSet) allowed(host string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	exp, ok := s.hosts[host]
	return ok && time.Now().Before(exp)
}

// Decay 按半衰期把长期未更新的上游得分拉回中性值，促使重新探活。
func (p *DockerPool) Decay(now time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, st := range p.stats {
		if !st.probed || st.score <= 0 {
			continue
		}
		age := now.Sub(st.updated)
		if age <= 30*time.Minute {
			continue
		}
		halves := age.Minutes() / 30
		factor := 1.0
		for i := int(0); i < int(halves); i++ {
			factor *= 0.5
			if factor < 0.05 {
				factor = 0.05
				break
			}
		}
		st.score = 50 + (st.score-50)*factor
	}
}
