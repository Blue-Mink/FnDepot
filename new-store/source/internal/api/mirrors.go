package api

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"fnos-store/internal/config"
)

type mirrorCheckResult struct {
	Key       string `json:"key"`
	Label     string `json:"label"`
	LatencyMs int    `json:"latency_ms"`
	Status    string `json:"status"` // "ok", "timeout", "error"
}

type mirrorCheckResponse struct {
	GitHubMirrors []mirrorCheckResult `json:"github_mirrors"`
	DockerMirrors []mirrorCheckResult `json:"docker_mirrors"`
}

const checkTimeout = 5 * time.Second

func (s *Server) handleCheckMirrors(w http.ResponseWriter, r *http.Request) {
	checkType := r.URL.Query().Get("type") // "github", "docker", or "" (both)

	ghMirrors := config.GitHubMirrorOptions()
	dkMirrors := config.DockerMirrorOptions()

	cfg := config.Config{}
	if s.configMgr != nil {
		cfg = s.configMgr.Get()
	}

	skipGH := checkType == "docker"
	skipDK := checkType == "github"

	total := len(ghMirrors) + len(dkMirrors)
	type indexedResult struct {
		index  int
		result mirrorCheckResult
		isGH   bool
	}
	results := make(chan indexedResult, total)

	var wg sync.WaitGroup

	// Check GitHub mirrors concurrently
	if !skipGH {
		for i, m := range ghMirrors {
			if m.Key == "direct" || m.Key == "custom" {
				continue
			}
			wg.Add(1)
			go func(idx int, mirror config.GitHubMirror) {
				defer wg.Done()
				testURL := probeURLFor(mirror)
				latency, tp, status := checkURLThroughput(r.Context(), testURL)
				// 手动测速结果同样汇入智能监测（和后台探测共用计数器）
				if s.mirrorMon != nil {
					s.mirrorMon.Record(mirror.Key, mirror.Label, status == "ok", latency, tp)
				}
				results <- indexedResult{
					index:  idx,
					isGH:   true,
					result: mirrorCheckResult{Key: mirror.Key, Label: mirror.Label, LatencyMs: latency, Status: status},
				}
			}(i, m)
		}
	}

	// Check custom GitHub mirror if configured
	if !skipGH && cfg.CustomGitHubMirror != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			testURL := strings.TrimRight(cfg.CustomGitHubMirror, "/") + "/" + defaultProbeFile
			latency, tp, status := checkURLThroughput(r.Context(), testURL)
			if s.mirrorMon != nil {
				s.mirrorMon.Record("custom", "自定义", status == "ok", latency, tp)
			}
			results <- indexedResult{
				index:  -1,
				isGH:   true,
				result: mirrorCheckResult{Key: "custom", Label: "自定义", LatencyMs: latency, Status: status},
			}
		}()
	}

	// Check Docker mirrors concurrently（registry /v2/ ping，401=存活）
	if !skipDK {
		for i, m := range dkMirrors {
			if m.Key == "direct" || m.Key == "custom" || m.Key == "auto" {
				continue
			}
			testURL := dockerProbeURLFor(m)
			if testURL == "" {
				continue
			}
			wg.Add(1)
			go func(idx int, mirror config.DockerMirror, u string) {
				defer wg.Done()
				latency, status := checkRegistryWith(dockerProbeClient(mirror), r.Context(), u)
				// 手动测速结果同样汇入智能监测（和后台探测共用计数器）
				// Docker registry ping 只测存活/延迟，无吞吐 → 传 0
				if s.dockerMirrorMon != nil {
					s.dockerMirrorMon.Record(mirror.Key, mirror.Label, status == "ok", latency, 0)
				}
				results <- indexedResult{
					index:  idx,
					isGH:   false,
					result: mirrorCheckResult{Key: mirror.Key, Label: mirror.Label, LatencyMs: latency, Status: status},
				}
			}(i, m, testURL)
		}
	}

	// Check custom Docker mirror if configured
	if !skipDK && cfg.CustomDockerMirror != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			testURL := "https://" + strings.TrimRight(cfg.CustomDockerMirror, "/") + "/v2/"
			latency, status := checkRegistry(r.Context(), testURL)
			if s.dockerMirrorMon != nil {
				s.dockerMirrorMon.Record("custom", "自定义", status == "ok", latency, 0)
			}
			results <- indexedResult{
				index:  -1,
				isGH:   false,
				result: mirrorCheckResult{Key: "custom", Label: "自定义", LatencyMs: latency, Status: status},
			}
		}()
	}

	go func() {
		wg.Wait()
		close(results)
	}()

	var ghResults, dkResults []mirrorCheckResult
	for r := range results {
		if r.isGH {
			ghResults = append(ghResults, r.result)
		} else {
			dkResults = append(dkResults, r.result)
		}
	}

	if ghResults == nil {
		ghResults = []mirrorCheckResult{}
	}
	if dkResults == nil {
		dkResults = []mirrorCheckResult{}
	}

	writeJSON(w, http.StatusOK, mirrorCheckResponse{
		GitHubMirrors: ghResults,
		DockerMirrors: dkResults,
	})
}

// checkURLThroughput 探测镜像：同时测建连延迟（TTFB）与下载吞吐。
//
// 背景：只测建连延迟会误选「低延迟但低带宽」的源（实测 gh-proxy.org 延迟
// 563ms 被 auto 选为首选，下载吞吐却只有 31KB/s，比 cdn.gh-proxy.org 的
// 6.5MB/s 慢 ~200 倍）。故探测改为 GET+Range 读至多 128KB 样本、实测 B/s，
// 排序改为吞吐优先、延迟次之（见 mirror.Monitor.Rank / BestStable）。
//
// 返回：latencyMs=响应头时间(TTFB)；throughputBps=样本字节/总耗时；
// status=ok/error/timeout。
func checkURLThroughput(parent context.Context, url string) (latencyMs, throughputBps int, status string) {
	ctx, cancel := context.WithTimeout(parent, checkTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, 0, "error"
	}
	// 至多读 128KB 样本：足以区分 31KB/s 源与 6.5MB/s 源，同时限制每次
	// 探测的带宽开销（探测文件本身偏小，实读往往不足 128KB）。
	req.Header.Set("Range", "bytes=0-131071")

	start := time.Now()
	resp, err := http.DefaultClient.Do(req)
	ttfb := time.Since(start)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return int(ttfb.Milliseconds()), 0, "timeout"
		}
		return int(ttfb.Milliseconds()), 0, "error"
	}
	defer resp.Body.Close()

	// HTTP >= 400 视为不可用（部分镜像对探测 URL 返回 403/404/429）；
	// 416（range 不满足）视为源可达，按存活处理。
	if resp.StatusCode >= 400 && resp.StatusCode != http.StatusRequestedRangeNotSatisfiable {
		return int(ttfb.Milliseconds()), 0, "error"
	}

	// 读样本 body（至 Range 上限或 EOF），测总耗时。
	var buf [16 * 1024]byte
	var read int64
	for read < 131072 {
		n, rerr := resp.Body.Read(buf[:])
		if n > 0 {
			read += int64(n)
		}
		if rerr != nil {
			break
		}
	}
	elapsed := time.Since(start)

	// 拿到响应头却没送出任何字节 → 超时（context 到期）或错误。
	if read == 0 {
		if ctx.Err() == context.DeadlineExceeded {
			return int(ttfb.Milliseconds()), 0, "timeout"
		}
		return int(ttfb.Milliseconds()), 0, "error"
	}

	// 吞吐 = 样本字节 / 纯传输耗时（总耗时 − TTFB）。
	// 必须剔除建连时间：TCP+TLS 握手/TTFB（~150ms，慢源可达 ~1.8s）若混入
	// 分母，会把 6.5MB/s 的源稀释成几十 KB/s（实测 3.6KB 文件被 1.8s 建连
	// 完全淹没）。只算「响应头到达之后」的传输才是真实带宽。
	transfer := elapsed - ttfb
	if transfer < time.Millisecond {
		transfer = time.Millisecond // 快源传输快于测量精度，按 1ms 兜底防除零/爆表
	}
	tp := int(float64(read) / transfer.Seconds())
	return int(ttfb.Milliseconds()), tp, "ok"
}
