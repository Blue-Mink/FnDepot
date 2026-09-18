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
				latency, status := checkURL(r.Context(), testURL)
				// 手动测速结果同样汇入智能监测（和后台探测共用计数器）
				if s.mirrorMon != nil {
					s.mirrorMon.Record(mirror.Key, mirror.Label, status == "ok", latency)
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
			latency, status := checkURL(r.Context(), testURL)
			if s.mirrorMon != nil {
				s.mirrorMon.Record("custom", "自定义", status == "ok", latency)
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
				if s.dockerMirrorMon != nil {
					s.dockerMirrorMon.Record(mirror.Key, mirror.Label, status == "ok", latency)
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
				s.dockerMirrorMon.Record("custom", "自定义", status == "ok", latency)
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

func checkURL(parent context.Context, url string) (latencyMs int, status string) {
	ctx, cancel := context.WithTimeout(parent, checkTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		return 0, "error"
	}

	start := time.Now()
	resp, err := http.DefaultClient.Do(req)
	elapsed := time.Since(start)

	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return int(elapsed.Milliseconds()), "timeout"
		}
		return int(elapsed.Milliseconds()), "error"
	}
	defer resp.Body.Close()

	// HTTP >= 400 视为不可用：实测多个"限流/拒服"镜像对探测 URL 返回
	// 403/404/429（如 gh.ddlc.top 的 429），它们并不能代理下载。
	if resp.StatusCode >= 400 {
		return int(elapsed.Milliseconds()), "error"
	}
	return int(elapsed.Milliseconds()), "ok"
}
