// docker.go 实现基于代理端口的 Docker Registry v2 加速。
//
// 工作原理：把 NAS 的代理地址填入 /etc/docker/daemon.json 的 registry-mirrors
// （或在飞牛 Docker 界面的镜像源里填写），Docker 拉取镜像时就会先访问本服务：
//
//	docker pull nginx
//	  -> GET http://nas:37710/v2/                     (握手，拿到 401 与认证地址)
//	  -> GET http://nas:37710/v2/token?...            (换取令牌，已改写为本机)
//	  -> GET http://nas:37710/v2/library/nginx/manifests/latest
//	  -> GET http://nas:37710/v2/library/nginx/blobs/<digest>
//	  -> (上游 302 时改写 Location 继续经本机中转)
//
// 本服务依次尝试配置的上游（公益镜像站、Docker 官方源），
// 对认证地址与重定向地址做改写，让整个拉取流程始终经过本机，
// 上游不可用时自动切换，实现自托管的镜像加速。
package proxy

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ghpp/ghpp/internal/config"
	"github.com/ghpp/ghpp/internal/netutil"
)

// Docker 官方源相关的固定地址。
const (
	dockerOfficialRegistry = "https://registry-1.docker.io"
	dockerOfficialAuth     = "https://auth.docker.io/token"
	dockerUpstreamOfficial = "official"
)

// isDockerRequest 判断请求是否属于 Docker 镜像拉取。
//
// 两种来源：
//  1. 透明反代：Docker 把 registry-mirrors 配置成本机，路径以 /v2/ 开头，
//     且 Host 不是 GitHub 域名；
//  2. 内部端点：/v2/token 与 /v2/_redirect 是本服务改写出来的回调地址。
func (e *Engine) isDockerRequest(r *http.Request) bool {
	if !strings.HasPrefix(r.URL.Path, "/v2/") && r.URL.Path != "/v2" {
		return false
	}
	if r.URL.IsAbs() && r.URL.Host != "" {
		return isDockerRegistryHost(stripPort(r.URL.Host))
	}
	host := stripPort(r.Host)
	// GitHub 域名上不存在 /v2/ 路径，不会误伤正常加速。
	if _, ok := e.hosts[host]; ok {
		return false
	}
	return !isGitHubInfra(host)
}

// isDockerRegistryHost 判断域名是否属于 Docker 官方的仓库与认证基础设施。
func isDockerRegistryHost(host string) bool {
	switch host {
	case "registry-1.docker.io", "index.docker.io", "registry.docker.io", "auth.docker.io":
		return true
	}
	return strings.HasSuffix(host, ".docker.com") || strings.HasSuffix(host, ".docker.io")
}

// serveDocker 处理一次 Docker Registry v2 请求。
func (e *Engine) serveDocker(w http.ResponseWriter, r *http.Request) {
	cfg := e.cfg()
	if !cfg.Docker.Enabled {
		http.Error(w, "Docker 加速未启用", http.StatusNotFound)
		return
	}

	e.metrics.CategoryDocker.Add(1)

	switch {
	case r.URL.Path == "/v2/token":
		e.serveDockerToken(w, r)
	case r.URL.Path == "/v2/_redirect":
		e.serveDockerRedirect(w, r, cfg)
	default:
		e.serveDockerRegistry(w, r, cfg)
	}
}

// serveDockerRegistry 把 /v2/ 请求转发给按序尝试的上游。
func (e *Engine) serveDockerRegistry(w http.ResponseWriter, r *http.Request, cfg *config.Config) {
	upstreams := e.dockerUpstreams(cfg)

	var lastErr error
	for _, up := range upstreams {
		done := e.tryDockerUpstream(w, r, cfg, up)
		if done {
			return
		}
		lastErr = fmt.Errorf("上游 %s 不可用", up)
		e.logf("warn", "Docker 上游 %s 请求失败，尝试下一个: %v", up, lastErr)
	}

	e.metrics.Failed.Add(1)
	if lastErr == nil {
		lastErr = errors.New("没有配置 Docker 上游")
	}
	http.Error(w, "Docker 加速失败: "+lastErr.Error(), http.StatusBadGateway)
}

// dockerUpstreams 返回按"最优在前、失效沉底"排序的上游列表。
//
// 社区镜像（含内置 KSpeeder 引擎动态注入的地址）进入 DockerPool 竞争排序：
// 健康上游按探活/实测得分降序，未探活的保持配置顺序，失效或冷却的沉底。
// "official"（Docker 官方源）不参与竞争，恒为末尾兜底，
// 保证镜像站全部失效时仍可拉取。
func (e *Engine) dockerUpstreams(cfg *config.Config) []string {
	var community []string
	hasOfficial := false

	// 动态上游（内置 KSpeeder 引擎）与其他上游一视同仁参与排序，
	// 其健康度由 /v2/ 探活与真实转发结果共同驱动。
	if e.dockerExtra != nil {
		if extra := e.dockerExtra(); extra != "" {
			community = append(community, strings.TrimSpace(extra))
		}
	}

	for _, u := range cfg.Docker.Upstreams {
		u = strings.TrimSpace(u)
		if u == "" {
			continue
		}
		if u == dockerUpstreamOfficial {
			hasOfficial = true
			continue
		}
		community = append(community, u)
	}

	ranked := e.dockerPool.Ranked(community)
	// 官方源以真实地址进入转发链，恒在最后兜底。
	if hasOfficial || len(ranked) > 0 {
		ranked = append(ranked, dockerOfficialRegistry)
	}
	return ranked
}

// DockerUpstreamEntry 是 /api/docker 展示用的一条上游记录。
type DockerUpstreamEntry struct {
	DockerUpstreamStat
	// Rank 是当前转发链中的优先级（1 = 最优；official 恒为末位）。
	Rank int `json:"rank"`
	// BuiltinEngine 表示该上游是内置 KSpeeder 引擎地址。
	BuiltinEngine bool `json:"builtin_engine,omitempty"`
	// Official 表示该上游是 Docker 官方兜底源。
	Official bool `json:"official,omitempty"`
}

// DockerUpstreamList 返回"最优在前、失效沉底"顺序的上游展示列表。
//
// 顺序与 dockerUpstreams 的转发顺序一致；official 以配置令牌 "official"
// 展示，其统计取真实地址 registry-1.docker.io 的探测数据。
func (e *Engine) DockerUpstreamList(cfg *config.Config) []DockerUpstreamEntry {
	var community []string
	hasOfficial := false
	extra := ""
	if e.dockerExtra != nil {
		extra = normalizeDockerUpstream(e.dockerExtra())
		if extra != "" {
			community = append(community, extra)
		}
	}
	for _, u := range cfg.Docker.Upstreams {
		u = strings.TrimSpace(u)
		if u == "" {
			continue
		}
		if u == dockerUpstreamOfficial {
			hasOfficial = true
			continue
		}
		community = append(community, u)
	}

	ranked := e.dockerPool.Ranked(community)
	stats := e.dockerPool.Stats()

	entries := make([]DockerUpstreamEntry, 0, len(ranked)+1)
	rank := 1
	for _, u := range ranked {
		stat := DockerUpstreamStat{URL: u, Name: upstreamName(u)}
		if st, ok := stats[u]; ok {
			stat = st
		}
		ent := DockerUpstreamEntry{DockerUpstreamStat: stat, Rank: rank}
		if u == extra {
			ent.BuiltinEngine = true
		}
		entries = append(entries, ent)
		rank++
	}
	if hasOfficial {
		stat := DockerUpstreamStat{URL: dockerUpstreamOfficial, Name: upstreamName(dockerUpstreamOfficial)}
		if st, ok := stats[dockerOfficialRegistry]; ok {
			stat = st
			stat.URL = dockerUpstreamOfficial // 展示用配置令牌，统计仍取自真实地址
		}
		entries = append(entries, DockerUpstreamEntry{DockerUpstreamStat: stat, Rank: rank, Official: true})
	}
	return entries
}

// DockerProbeAll 对当前转发链上的全部上游（含官方兜底）做一轮 /v2/ 探活。
func (e *Engine) DockerProbeAll(ctx context.Context) []DockerUpstreamProbe {
	cfg := e.cfg()
	var urls []string
	if e.dockerExtra != nil {
		if u := normalizeDockerUpstream(e.dockerExtra()); u != "" {
			urls = append(urls, u)
		}
	}
	for _, u := range cfg.Docker.Upstreams {
		u = strings.TrimSpace(u)
		if u == "" || u == dockerUpstreamOfficial {
			continue
		}
		urls = append(urls, u)
	}
	urls = append(urls, dockerOfficialRegistry)
	return e.dockerPool.Probe(ctx, urls)
}

// DockerUpstreamStats 返回上游聚合统计快照（供 API 组装）。
func (e *Engine) DockerUpstreamStats() map[string]DockerUpstreamStat {
	return e.dockerPool.Stats()
}

// DockerPoolDecay 定期衰减 Docker 上游得分（由 App 后台任务驱动）。
func (e *Engine) DockerPoolDecay(now time.Time) {
	e.dockerPool.Decay(now)
}

// isBuiltinEngineUpstream 判断给定上游是否为内置 KSpeeder 引擎地址。
func (e *Engine) isBuiltinEngineUpstream(upstream string) bool {
	if e.dockerExtra == nil {
		return false
	}
	return normalizeDockerUpstream(upstream) == normalizeDockerUpstream(e.dockerExtra())
}

// tryDockerUpstream 用指定上游转发一次 Registry 请求。
//
// 返回 true 表示响应已写给客户端（无论成功还是业务性失败）；
// 返回 false 表示该上游连接层失败或服务端错误，应换下一个上游。
func (e *Engine) tryDockerUpstream(w http.ResponseWriter, r *http.Request, cfg *config.Config, upstream string) bool {
	target := upstream + r.URL.Path
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}

	// Docker daemon 对 registry 探活的耐心有限：/v2/ 与清单请求若迟迟不回，
	// daemon 会放弃镜像直连官方（实测：首个上游卡 12s 才判死，daemon 早已回退，
	// 最终官方直连 43s 超时整次拉取失败）。
	// 因此 registry 小请求（探活/清单/标签列表）必须快速失败换上游，
	// 轮换总耗时压在 daemon 耐心之内；只有大 blob 下载允许保持长超时。
	timeout := time.Duration(cfg.Proxy.ReadTimeoutMS) * time.Millisecond
	if !strings.Contains(r.URL.Path, "/blobs/") {
		// 内置引擎是服务端聚合器（内部还要选节点、跨节点抓取），
		// 冷启动或换源时的首个 manifest 请求合法耗时可达数秒，
		// 给更宽的窗口；社区镜像继续 2s 快速失败换源。
		small := 2 * time.Second
		if e.isBuiltinEngineUpstream(upstream) {
			small = 8 * time.Second
		}
		if small < timeout {
			timeout = small
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, r.Method, target, r.Body)
	if err != nil {
		return false
	}
	copyDockerHeaders(req.Header, r.Header)
	req.ContentLength = r.ContentLength
	req.Host = hostOf(upstream)

	start := time.Now()
	resp, err := e.dockerClient.Do(req)
	if err != nil {
		// 回灌失败结果：连续失败会触发冷却沉底。
		e.dockerPool.Record(upstream, false, 0, err)
		return false
	}

	// manifest/blob 的无 token 401：代理端代换 token 并重放。
	//
	// daemon 的 token 协商在 /v2/ ping 阶段建立：ping 落在免认证上游
	// （内置引擎恒回 200）时 daemon 全程匿名；此后要求认证的镜像站回
	// 401 质询，docker 客户端不会在 manifest 阶段再去换 token（实测：
	// 收到质询后静默放弃该镜像，整次拉取失败回退官方）。因此由代理完成
	// 换 token 并重放（GET/HEAD），daemon 始终只见正常 2xx/3xx；换 token
	// 失败按"上游不可用"换源，不透传质询（透传对 daemon 无意义）。
	if resp.StatusCode == http.StatusUnauthorized &&
		r.URL.Path != "/v2/" && r.Header.Get("Authorization") == "" &&
		(r.Method == http.MethodGet || r.Method == http.MethodHead) {
		challenge := resp.Header.Get("WWW-Authenticate")
		_ = resp.Body.Close()
		var retried *http.Response
		if tok, terr := e.dockerFetchToken(ctx, upstream, challenge); terr == nil {
			if retryResp, rerr := e.dockerRetryWithToken(ctx, r, upstream, tok); rerr == nil {
				retried = retryResp
			} else {
				e.logf("warn", "Docker token 代换后重试 %s via %s 失败: %v", r.URL.Path, upstream, rerr)
			}
		} else {
			e.logf("warn", "Docker token 代换 %s via %s 失败: %v", r.URL.Path, upstream, terr)
		}
		if retried == nil {
			e.dockerPool.Record(upstream, false, 0, fmt.Errorf("上游 401 且代理端 token 代换失败"))
			return false
		}
		resp = retried
	}
	defer resp.Body.Close()

	// 上游 5xx 视为该源不稳定，换下一个上游重试。
	if resp.StatusCode >= 500 {
		e.dockerPool.Record(upstream, false, 0, fmt.Errorf("上游返回状态码 %d", resp.StatusCode))
		return false
	}

	// 其余 4xx：
	//   - /v2/ 的 401 是握手质询，照旧透传（标准 token 交换第一步）；
	//   - 非 GET/HEAD 的无 token 401（推送等流程）保持原行为透传；
	//   - GET/HEAD 的无 token 401 已被上面的代换块消费（代换失败已换源），
	//     走到这里的 401 只剩"带了 token 仍被拒"——该上游服务不了，换源。
	// 其他 4xx（403 镜像拒绝目标仓库 token、404 镜像没有该镜像等）一律视为
	// "该上游服务不了本请求"，换下一个上游。全链耗尽后 serveDockerRegistry
	// 回 502，daemon 回退官方源用原始凭证重试，私有仓库/不存在镜像的
	// 业务语义不变。
	if resp.StatusCode >= 400 {
		if resp.StatusCode == http.StatusUnauthorized &&
			(r.URL.Path == "/v2/" ||
				(r.Method != http.MethodGet && r.Method != http.MethodHead)) {
			e.passDockerChallenge(w, resp, upstream, e.dockerSelfHost(r))
			return true
		}
		e.dockerPool.Record(upstream, false, 0, fmt.Errorf("上游返回状态码 %d", resp.StatusCode))
		return false
	}

	copyResponseHeaders(w.Header(), resp, hostOf(upstream))

	// 重定向需要改写，让后续请求继续经过本机。
	// 302 目标（通常是镜像的对象存储/CDN 后端域名）先登记进观察名单，
	// /v2/_redirect 的 SSRF 判定除固定后缀白名单外还认这份名单。
	if isRedirect(resp.StatusCode) {
		if loc := resp.Header.Get("Location"); loc != "" {
			if tu, err := url.Parse(loc); err == nil && tu.Scheme == "https" {
				e.allowDockerRedirectHost(stripPort(tu.Host))
			}
			// 改写必须落在 w.Header()（copyResponseHeaders 后已是副本）。
			w.Header().Set("Location", e.rewriteDockerLocation(loc, e.dockerSelfHost(r)))
		}
	}

	w.WriteHeader(resp.StatusCode)
	counter := &countingWriter{w: w}
	n, _ := io.Copy(counter, resp.Body)
	e.metrics.BytesOut.Add(n)
	e.metrics.Accelerated.Add(1)

	latency := time.Since(start)
	// 内置引擎是服务端聚合器：首个请求含节点选择与跨节点抓取，
	// 1~3s 属正常行为。打分用的延迟钳到 1s，避免一次慢请求就把它
	// 挤出快镜像之后（其 8s 小请求窗口已保证慢请求不被中途掐断）。
	if e.isBuiltinEngineUpstream(upstream) && latency > 1000*time.Millisecond {
		latency = 1000 * time.Millisecond
	}
	// 回灌成功结果：真实转发数据让最优上游持续保持在最前。
	e.dockerPool.Record(upstream, true, latency, nil)
	e.logf("debug", "Docker %s %s via %s -> %d (%dms, %d bytes)",
		r.Method, r.URL.Path, upstream, resp.StatusCode, latency.Milliseconds(), n)
	return true
}

// serveDockerToken 转发令牌请求到改写前记录的认证源。
//
// _u 参数保存了原始 realm 的完整地址（base64url 编码），
// 例如 https://auth.docker.io/token。
func (e *Engine) serveDockerToken(w http.ResponseWriter, r *http.Request) {
	raw := r.URL.Query().Get("_u")
	if raw == "" {
		http.Error(w, "缺少 _u 参数", http.StatusBadRequest)
		return
	}
	decoded, err := b64decode(raw)
	if err != nil || !strings.Contains(decoded, "://") {
		http.Error(w, "_u 参数无效", http.StatusBadRequest)
		return
	}
	// 仅允许 Docker 官方与已配置上游的认证地址，防止被滥用为开放代理。
	u, err := url.Parse(decoded)
	if err != nil || !isDockerAuthHost(stripPort(u.Host)) {
		http.Error(w, "认证地址不受信任", http.StatusForbidden)
		return
	}

	q := r.URL.Query()
	q.Del("_u")
	target := decoded
	if eq := q.Encode(); eq != "" {
		target += "?" + eq
	}

	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, r.Method, target, r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	copyDockerHeaders(req.Header, r.Header)
	req.Host = u.Host

	start := time.Now()
	resp, err := e.client.Do(req)
	if err != nil {
		e.logf("warn", "Docker token 交换 %s 失败: %v", u.Host, err)
		http.Error(w, "获取令牌失败: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	copyResponseHeaders(w.Header(), resp, u.Host)
	w.WriteHeader(resp.StatusCode)
	counter := &countingWriter{w: w}
	n, _ := io.Copy(counter, resp.Body)
	e.metrics.BytesOut.Add(n)
	e.logf("debug", "Docker token via %s -> %d (%dms, %d bytes)",
		u.Host, resp.StatusCode, time.Since(start).Milliseconds(), n)
}

// serveDockerRedirect 中转上游的重定向目标（通常是镜像 blob 的 CDN 地址），
// 让大文件下载也走本机的加速与统计。
func (e *Engine) serveDockerRedirect(w http.ResponseWriter, r *http.Request, cfg *config.Config) {
	raw := r.URL.Query().Get("to")
	if raw == "" {
		http.Error(w, "缺少 to 参数", http.StatusBadRequest)
		return
	}
	decoded, err := b64decode(raw)
	if err != nil || !strings.Contains(decoded, "://") {
		http.Error(w, "to 参数无效", http.StatusBadRequest)
		return
	}
	u, err := url.Parse(decoded)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		http.Error(w, "to 参数无效", http.StatusBadRequest)
		return
	}
	// SSRF 防护：只允许中转 Docker 官方、已配置上游的地址，
	// 以及受信任上游 302 实际指过的主机（观察名单，TTL 30 分钟）。
	if !isDockerCDNHost(stripPort(u.Host), e.dockerUpstreams(cfg)) &&
		!e.redirectHostAllowed(stripPort(u.Host)) {
		http.Error(w, "重定向目标不受信任", http.StatusForbidden)
		return
	}

	// 超时按"真实资源"定，而不是按本机路径：_redirect 的本机路径恒为
	// /v2/_redirect，blob 地址藏在 to 参数里（解码后 u.Path 才含 /blobs/）。
	// 此前按 r.URL.Path 判定导致所有 blob 中转都被 2 秒小请求窗口掐断
	// （CDN 冷启动首字节常 >2s），daemon 拿 502 后整次拉取失败，且该路径
	// 无任何日志可查（访问日志只记 /api/*）。
	isBlob := strings.Contains(u.Path, "/blobs/")
	timeout := time.Duration(cfg.Proxy.ReadTimeoutMS) * time.Millisecond
	if !isBlob {
		const dockerAPITimeout = 2 * time.Second
		if dockerAPITimeout < timeout {
			timeout = dockerAPITimeout
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, r.Method, decoded, r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	copyDockerHeaders(req.Header, r.Header)
	req.ContentLength = r.ContentLength
	req.Host = u.Host

	start := time.Now()
	resp, err := e.client.Do(req)
	if err != nil {
		e.logf("warn", "Docker _redirect 中转 %s 失败: %v", u.Host, err)
		http.Error(w, "中转下载失败: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// 中转目标 401：daemon 的 token 是 /v2/ ping 命中的那面镜像签发的，
	// 本中转目标（往往是另一面镜像）不认（或 daemon 全程匿名）。docker
	// 客户端在 manifest 阶段收到 401 会静默放弃该镜像、整次拉取回退官方
	// 超时——代理必须代换 token 并重放（与 tryDockerUpstream 同语义），
	// 换 token 失败或重放仍 401 才把质询交回 daemon。
	if resp.StatusCode == http.StatusUnauthorized &&
		(r.Method == http.MethodGet || r.Method == http.MethodHead) {
		challenge := resp.Header.Get("WWW-Authenticate")
		_ = resp.Body.Close()
		if tok, terr := e.dockerFetchToken(ctx, u.Scheme+"://"+u.Host, challenge); terr == nil {
			req2, err2 := http.NewRequestWithContext(ctx, r.Method, decoded, nil)
			if err2 == nil {
				copyDockerHeaders(req2.Header, r.Header)
				req2.Header.Set("Authorization", "Bearer "+tok)
				req2.Host = u.Host
				if resp2, err3 := e.client.Do(req2); err3 == nil {
					resp.StatusCode = resp2.StatusCode
					resp.Header = resp2.Header
					resp.Body = resp2.Body
					e.logf("debug", "Docker _redirect token 代换后重试 via %s -> %d", u.Host, resp2.StatusCode)
				} else {
					e.logf("warn", "Docker _redirect token 代换后重试 %s via %s 失败: %v", u.Path, u.Host, err3)
				}
			}
		} else {
			e.logf("warn", "Docker _redirect token 代换 %s via %s 失败: %v", u.Path, u.Host, terr)
		}
	}

	copyResponseHeaders(w.Header(), resp, u.Host)
	if isRedirect(resp.StatusCode) {
		if loc := resp.Header.Get("Location"); loc != "" {
			// 嵌套重定向（CDN 换存储桶等）：目标同样登记后改写，
			// 让 daemon 继续经本机跟随。
			if tu, err := url.Parse(loc); err == nil && tu.Scheme == "https" {
				e.allowDockerRedirectHost(stripPort(tu.Host))
			}
			w.Header().Set("Location", e.rewriteDockerLocation(loc, e.dockerSelfHost(r)))
		}
	}
	w.WriteHeader(resp.StatusCode)
	counter := &countingWriter{w: w}
	n, _ := io.Copy(counter, resp.Body)
	e.metrics.BytesOut.Add(n)
	e.logf("debug", "Docker _redirect %s via %s -> %d (%dms, %d bytes)",
		r.Method, u.Host, resp.StatusCode, time.Since(start).Milliseconds(), n)
}

// rewriteDockerRealm 把认证质询里的 realm 改写为本机地址。
//
// 例如 Bearer realm="https://auth.docker.io/token",service="registry.docker.io"
// 变为   Bearer realm="http://nas:37710/v2/token?_u=aHR0cHM6...",service="registry.docker.io"
func rewriteDockerRealm(realm, selfHost string) string {
	marker := "realm=\""
	i := strings.Index(realm, marker)
	if i < 0 {
		return realm
	}
	rest := realm[i+len(marker):]
	j := strings.IndexByte(rest, '"')
	if j < 0 {
		return realm
	}
	origin := rest[:j]
	if strings.HasPrefix(origin, "http://"+selfHost) || strings.HasPrefix(origin, "https://"+selfHost) {
		return realm // 已经指向本机
	}
	encoded := b64encode(origin)
	tail := realm[i+len(marker)+j:]
	return realm[:i] + marker + "http://" + selfHost + "/v2/token?_u=" + encoded + tail
}

// parseDockerChallenge 解析 WWW-Authenticate 的 Bearer 质询：
// Bearer realm="https://auth.example/token",service="registry.example",scope="repository:x:pull"
func parseDockerChallenge(challenge string) (realm, service string, scopes []string) {
	if !strings.HasPrefix(challenge, "Bearer ") {
		return "", "", nil
	}
	rest := challenge[len("Bearer "):]
	for rest != "" {
		// 逐个 "key=value" 段：value 是带引号字符串，key 不含逗号。
		k := strings.IndexByte(rest, '=')
		if k < 0 {
			break
		}
		key := strings.TrimSpace(rest[:k])
		rest = rest[k+1:]
		if !strings.HasPrefix(rest, "\"") {
			break
		}
		rest = rest[1:]
		eq := strings.IndexByte(rest, '"')
		if eq < 0 {
			break
		}
		val := rest[:eq]
		rest = rest[eq+1:]
		// 吃掉逗号与空白
		rest = strings.TrimLeft(rest, ", \t")
		switch key {
		case "realm":
			realm = val
		case "service":
			service = val
		case "scope":
			scopes = append(scopes, val)
		}
	}
	return realm, service, scopes
}

// dockerFetchToken 按上游 401 质询去其 realm 换一枚 token（代理端代劳）。
//
// daemon 的 token 协商在 /v2/ ping 阶段建立：若 ping 落在免认证上游
// （内置引擎恒回 200），daemon 全程匿名，后续镜像站的 401 质询它不会
// 去换 token（实测：收到质询后静默放弃该镜像，整次拉取失败）。因此换
// token 由代理完成，daemon 只见到正常的 2xx/3xx。
//
// realm 主机白名单：Docker 官方认证、已配置上游与内置引擎。质询来自
// 我们刚对话过的受信任上游，但其指向的主机仍须在此名单内，防止被
// 指到内网地址（SSRF 闸门）。
func (e *Engine) dockerFetchToken(ctx context.Context, upstream, challenge string) (string, error) {
	realm, service, scopes := parseDockerChallenge(challenge)
	if realm == "" {
		return "", errors.New("质询缺少 realm")
	}
	u, err := url.Parse(realm)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return "", fmt.Errorf("realm 无效: %v", err)
	}
	host := stripPort(u.Host)
	cfg := e.cfg()
	if !isDockerAuthHost(host) && !e.isTrustedDockerUpstreamHost(host, cfg) {
		return "", fmt.Errorf("realm 主机不受信任: %s", host)
	}

	q := url.Values{}
	if service != "" {
		q.Set("service", service)
	}
	for _, s := range scopes {
		q.Add("scope", s)
	}
	tokURL := realm
	if enc := q.Encode(); enc != "" {
		sep := "?"
		if strings.Contains(realm, "?") {
			sep = "&"
		}
		tokURL += sep + enc
	}

	tctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(tctx, http.MethodGet, tokURL, nil)
	if err != nil {
		return "", err
	}
	req.Host = u.Host
	resp, err := e.dockerClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token 端点返回 %d", resp.StatusCode)
	}
	var doc struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return "", fmt.Errorf("token 响应不是 JSON: %v", err)
	}
	if doc.Token == "" && doc.AccessToken == "" {
		return "", errors.New("token 响应缺少 token 字段")
	}
	if doc.Token != "" {
		return doc.Token, nil
	}
	return doc.AccessToken, nil
}

// isTrustedDockerUpstreamHost 判断 realm 主机是否属于已配置上游或内置引擎。
func (e *Engine) isTrustedDockerUpstreamHost(host string, cfg *config.Config) bool {
	for _, up := range cfg.Docker.Upstreams {
		if h := stripPort(hostOf(strings.TrimSpace(up))); h == host {
			return true
		}
	}
	if e.dockerExtra != nil {
		if h := stripPort(hostOf(e.dockerExtra())); h == host {
			return true
		}
	}
	return false
}

// dockerRetryWithToken 带 token 重放同一次 Registry 请求（仅 GET/HEAD，
// 拉取链路只用这两种方法；带请求体的推送流程不做代换，保持原行为）。
func (e *Engine) dockerRetryWithToken(ctx context.Context, r *http.Request, upstream, token string) (*http.Response, error) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return nil, errors.New("仅支持 GET/HEAD 的 token 代换")
	}
	target := upstream + r.URL.Path
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	req, err := http.NewRequestWithContext(ctx, r.Method, target, nil)
	if err != nil {
		return nil, err
	}
	copyDockerHeaders(req.Header, r.Header)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Host = hostOf(upstream)
	return e.dockerClient.Do(req)
}

// passDockerChallenge 把 401 认证质询原样透传（realm 改写为本机换 token 端点）。
func (e *Engine) passDockerChallenge(w http.ResponseWriter, resp *http.Response, upstream, selfHost string) {
	copyResponseHeaders(w.Header(), resp, hostOf(upstream))
	if realm := resp.Header.Get("WWW-Authenticate"); realm != "" {
		// 必须在 copyResponseHeaders 之后改写 w.Header()（已是副本），
		// 改 resp.Header 不影响响应。
		w.Header().Set("WWW-Authenticate", rewriteDockerRealm(realm, selfHost))
	}
	w.WriteHeader(resp.StatusCode)
	n, _ := io.Copy(w, resp.Body)
	e.metrics.BytesOut.Add(n)
	e.logf("debug", "Docker 401 质询透传 via %s", upstream)
}

// allowDockerRedirectHost 登记一个受信任上游 302 的目标主机。
func (e *Engine) allowDockerRedirectHost(host string) {
	if e.dockerRedirectAllow != nil {
		e.dockerRedirectAllow.add(host)
	}
}

// redirectHostAllowed 判断主机是否在 302 观察名单内。
func (e *Engine) redirectHostAllowed(host string) bool {
	return e.dockerRedirectAllow != nil && e.dockerRedirectAllow.allowed(host)
}

// dockerSelfHost 返回 Docker 客户端可达的"本机"地址。
//
// daemon 经 registry-mirrors 访问本服务时，请求 Host 是官方源域名
// （registry-1.docker.io 等），若拿 r.Host 当改写地址的 origin，会把
// daemon 引向内网不可达的官方地址，整次拉取超时（302 Location 与
// 401 realm 两条改写路径都踩过）。此时改用代理监听地址拼 origin：
// 回环请求给 127.0.0.1，局域网请求给本机主 LAN IP。
func (e *Engine) dockerSelfHost(r *http.Request) string {
	if !isDockerRegistryHost(stripPort(r.Host)) {
		return r.Host
	}
	ip := "127.0.0.1"
	if h, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		if p := net.ParseIP(h); p != nil && !p.IsLoopback() {
			if lan := netutil.PrimaryLAN(); lan != "" {
				ip = lan
			}
		}
	}
	port := "37710"
	if _, p, err := net.SplitHostPort(e.cfg().Proxy.Listen); err == nil {
		port = p
	}
	return ip + ":" + port
}

// rewriteDockerLocation 把重定向地址改写为本机中转端点。
//
// 只改写指向 Docker 相关域名或 302 观察名单主机的绝对地址；其他地址保持
// 原样，由客户端直接访问（例如镜像站自有的对象存储）。
func (e *Engine) rewriteDockerLocation(loc, selfHost string) string {
	if loc == "" || strings.HasPrefix(loc, "/") {
		return loc
	}
	u, err := url.Parse(loc)
	if err != nil {
		return loc
	}
	host := stripPort(u.Host)
	if !isDockerCDNHost(host, nil) && !e.redirectHostAllowed(host) {
		return loc
	}
	if strings.HasPrefix(loc, "http://"+selfHost) || strings.HasPrefix(loc, "https://"+selfHost) {
		return loc // 已经指向本机
	}
	return "http://" + selfHost + "/v2/_redirect?to=" + b64encode(loc)
}

// isDockerAuthHost 判断是否为允许中转的认证地址。
func isDockerAuthHost(host string) bool {
	if host == "auth.docker.io" {
		return true
	}
	// 部分镜像站使用自己的认证端点。
	return strings.HasSuffix(host, ".daocloud.io") ||
		strings.HasSuffix(host, ".1ms.run") ||
		strings.HasSuffix(host, ".xuanyuan.me") ||
		strings.HasSuffix(host, ".docker.com")
}

// isDockerCDNHost 判断是否为允许中转的 blob 存储地址。
// extra 传入额外允许的上游地址列表（通常来自配置）。
func isDockerCDNHost(host string, extra []string) bool {
	if isDockerRegistryHost(host) {
		return true
	}
	for _, suffix := range []string{".daocloud.io", ".1ms.run", ".xuanyuan.me", ".cloudflarestorage.com"} {
		if strings.HasSuffix(host, suffix) {
			return true
		}
	}
	for _, up := range extra {
		if up == dockerUpstreamOfficial {
			continue
		}
		if h := hostOf(up); h == host {
			return true
		}
	}
	return false
}

// copyDockerHeaders 复制 Docker 客户端请求头。
// Registry 协议依赖 Accept（manifest 类型协商）与 Range（断点续传），
// 必须完整保留；Authorization 是仓库令牌，同样保留。
func copyDockerHeaders(dst, src http.Header) {
	for k, vv := range src {
		lk := strings.ToLower(k)
		switch lk {
		case "connection", "keep-alive", "proxy-authenticate", "proxy-authorization",
			"proxy-connection", "te", "trailer", "transfer-encoding", "upgrade",
			// Host 由上游请求单独设置，Host 头交给 Go 传输层管理。
			"host", "accept-encoding":
			continue
		}
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
	if dst.Get("User-Agent") == "" {
		dst.Set("User-Agent", "docker/ghpp")
	}
}

// b64encode 用 base64url 编码，供 query 参数携带 URL。
func b64encode(s string) string {
	return base64.URLEncoding.EncodeToString([]byte(s))
}

// b64decode 解码 base64url，兼容未带 padding 的变体。
func b64decode(s string) (string, error) {
	if b, err := base64.URLEncoding.DecodeString(s); err == nil {
		return string(b), nil
	}
	if b, err := base64.RawURLEncoding.DecodeString(s); err == nil {
		return string(b), nil
	}
	if b, err := base64.StdEncoding.DecodeString(s); err == nil {
		return string(b), nil
	}
	return "", errors.New("base64 解码失败")
}
