package api

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"fnos-store/internal/config"
	"fnos-store/internal/mirror"
)

const (
	mirrorProbeInterval = 5 * time.Minute
	mirrorProbeDelay    = 8 * time.Second // 首探等服务器稳定后再跑
	mirrorSwitchFails   = 3               // 选定源连续失败 N 次 → 自动切换到最稳定源
)

// defaultProbeFile 探测用公共 raw 文件。
//
// 选 conversun/fnos-apps 的 apps.json（~121KB）而非旧的 repo_list.txt（3.6KB）：
//   - 足够大 → 传输耗时占主导，配合「吞吐=字节/(总耗时−TTFB)」能实测出真实带宽
//     （3.6KB 文件的传输是亚毫秒级，被 ~150ms 建连时间完全淹没，测不出吞吐）。
//   - 所有镜像可达：通用镜像代理任意 raw.githubusercontent.com URL；conversun
//     hub 只代理 conversun 自家仓库，而 fnos-apps 正是 conversun 的仓库。
//   - 稳定：是应用目录的核心清单文件，只要仓库存在就在。
const defaultProbeFile = "https://raw.githubusercontent.com/conversun/fnos-apps/HEAD/apps.json"

func probeURLFor(m config.GitHubMirror) string {
	// 统一探测文件（见 defaultProbeFile 说明）：通用镜像与 conversun hub 均可达。
	return m.URL + defaultProbeFile
}

// startMirrorMonitor 创建 GitHub + Docker 两个监测器、注册 config 智能钩子、
// 启动后台探测循环（每 5 分钟一轮，两类源一起探）。
func (s *Server) startMirrorMonitor() {
	s.mirrorMon = mirror.New()
	s.dockerMirrorMon = mirror.New()
	s.ctx, s.cancel = context.WithCancel(context.Background())

	config.RegisterDockerSmart(config.DockerSmart{
		Rank: func(cfg config.Config) []string {
			var keys []string
			for _, m := range config.DockerMirrorOptions() {
				if m.Key != "direct" && m.Key != "custom" && m.Key != "auto" && dockerProbeURLFor(m) != "" {
					keys = append(keys, m.Key)
				}
			}
			ranked := s.dockerMirrorMon.Rank(keys)
			if cfg.DockerMirror != "" && cfg.DockerMirror != "auto" && cfg.DockerMirror != "direct" && cfg.DockerMirror != "custom" {
				if s.dockerMirrorMon.ConsecFails(cfg.DockerMirror) < mirrorSwitchFails {
					out := []string{cfg.DockerMirror}
					for _, k := range ranked {
						if k != cfg.DockerMirror {
							out = append(out, k)
						}
					}
					return out
				}
			}
			return ranked
		},
		Best: func(cfg config.Config) string {
			k, ok := s.dockerMirrorMon.BestStable()
			if !ok {
				return ""
			}
			for _, m := range config.DockerMirrorOptions() {
				if m.Key == k {
					if m.Key == "kspeeder" {
						return config.KSpeederPrefix
					}
					return m.URL
				}
			}
			return ""
		},
		Degraded: func(key string) bool {
			return s.dockerMirrorMon.ConsecFails(key) >= mirrorSwitchFails
		},
	})

	config.RegisterGitHubSmart(config.GitHubSmart{
		Rank: func(cfg config.Config) []string {
			var keys []string
			for _, m := range config.GitHubMirrorOptions() {
				if m.Key != "direct" && m.Key != "custom" && m.Key != "auto" && m.URL != "" {
					keys = append(keys, m.Key)
				}
			}
			ranked := s.mirrorMon.Rank(keys)
			// 显式选定（未降级）时钉在链首 —— 尊重用户选择；
			// auto/direct/custom/未配置/已降级 → 纯按健康度。
			if cfg.Mirror != "" && cfg.Mirror != "auto" && cfg.Mirror != "direct" && cfg.Mirror != "custom" {
				if s.mirrorMon.ConsecFails(cfg.Mirror) < mirrorSwitchFails {
					out := []string{cfg.Mirror}
					for _, k := range ranked {
						if k != cfg.Mirror {
							out = append(out, k)
						}
					}
					return out
				}
			}
			return ranked
		},
		Best: func(cfg config.Config) string {
			k, ok := s.mirrorMon.BestStable()
			if !ok {
				return ""
			}
			for _, m := range config.GitHubMirrorOptions() {
				if m.Key == k {
					return m.URL
				}
			}
			return ""
		},
		Degraded: func(key string) bool {
			return s.mirrorMon.ConsecFails(key) >= mirrorSwitchFails
		},
	})

	go func() {
		time.Sleep(mirrorProbeDelay)
		s.probeAllMirrors()
		t := time.NewTicker(mirrorProbeInterval)
		defer t.Stop()
		for {
			select {
			case <-s.ctx.Done():
				return
			case <-t.C:
				s.probeAllMirrors()
			}
		}
	}()
}

// probeAllMirrors 一轮完整探测：GitHub + Docker 镜像一起测，
// 共用 30 秒防抖（triggerProbe）。
func (s *Server) probeAllMirrors() {
	s.probeMirrors()
	s.probeDockerMirrors()
}

// dockerProbeURLFor 返回该 Docker 镜像的存活探测地址（registry /v2/ ping）。
// nju-ghcr 无 URL（host 改写型），用其改写目标 ghcr.nju.edu.cn 探测；
// kspeeder 是本地代理，探本机 127.0.0.1:5443（未安装=连接拒绝=失败降权）。
func dockerProbeURLFor(m config.DockerMirror) string {
	if m.URL != "" {
		return "https://" + m.URL + "v2/"
	}
	switch m.Key {
	case "nju-ghcr":
		return "https://ghcr.nju.edu.cn/v2/"
	case "kspeeder":
		return "https://" + config.KSpeederDefaultAddr + "/v2/"
	}
	return ""
}

// probeDockerMirrors 并发探测全部 Docker 镜像（含自定义源），结果汇入
// Docker 监测器，然后评估是否需要对降级源做自动切换。
func (s *Server) probeDockerMirrors() {
	type result struct {
		key, label string
		ok         bool
		latency    int
	}
	mirrors := config.DockerMirrorOptions()
	if cfg := s.configMgr.Get(); cfg.CustomDockerMirror != "" {
		mirrors = append(mirrors, config.DockerMirror{
			Key: "custom", Label: "自定义", URL: strings.TrimRight(cfg.CustomDockerMirror, "/") + "/",
		})
	}

	var wg sync.WaitGroup
	ch := make(chan result, len(mirrors)+1)
	for _, m := range mirrors {
		u := dockerProbeURLFor(m)
		if m.Key == "direct" || m.Key == "auto" || u == "" {
			continue
		}
		wg.Add(1)
		go func(m config.DockerMirror, u string) {
			defer wg.Done()
			lat, status := checkRegistryWith(dockerProbeClient(m), context.Background(), u)
			ch <- result{key: m.Key, label: m.Label, ok: status == "ok", latency: lat}
		}(m, u)
	}
	go func() { wg.Wait(); close(ch) }()
	for r := range ch {
		s.dockerMirrorMon.Record(r.key, r.label, r.ok, r.latency, 0) // Docker 只测存活/延迟
	}
	s.dockerMirrorMon.SetLastProbe(time.Now())
	s.maybeAutoSwitchDocker()
}

// checkRegistry 探测 registry 存活（HEAD /v2/）。
// Docker 语义：200 与 401（未鉴权挑战）都表示 registry 存活；
// 3xx 由 client 跟随后按最终码判定；其余 4xx/5xx 不可用。
func checkRegistry(parent context.Context, url string) (latencyMs int, status string) {
	return checkRegistryWith(http.DefaultClient, parent, url)
}

func checkRegistryWith(client *http.Client, parent context.Context, url string) (latencyMs int, status string) {
	ctx, cancel := context.WithTimeout(parent, checkTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, url, nil)
	if err != nil {
		return 0, "error"
	}
	start := time.Now()
	resp, err := client.Do(req)
	elapsed := time.Since(start)
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return int(elapsed.Milliseconds()), "timeout"
		}
		return int(elapsed.Milliseconds()), "error"
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusUnauthorized:
		return int(elapsed.Milliseconds()), "ok"
	case resp.StatusCode >= 400:
		return int(elapsed.Milliseconds()), "error"
	default:
		return int(elapsed.Milliseconds()), "ok"
	}
}

// kspeederProbeClient：本地 KSpeeder 使用自签名证书。探测目标是
// 127.0.0.1 回环地址（仅本机进程可达，无外部 MITM 面），跳过证书
// 校验 —— 与 docker daemon 对 127.0.0.1:port 的 insecure-registry
// 处理等效。
var kspeederProbeClient = &http.Client{
	Transport: &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
		MaxIdleConns:    4,
		IdleConnTimeout: 30 * time.Second,
	},
}

// dockerProbeClient 按镜像类型返回探测 client（kspeeder 用跳过证书校验的本地 client）。
func dockerProbeClient(m config.DockerMirror) *http.Client {
	if m.Key == "kspeeder" {
		return kspeederProbeClient
	}
	return http.DefaultClient
}

// maybeAutoSwitchDocker 显式选定的 Docker 镜像连续探测失败达到阈值时，
// 持久化切换到当前最稳定源，并记录最近一次切换供 UI 展示。
func (s *Server) maybeAutoSwitchDocker() {
	if s.configMgr == nil {
		return
	}
	cfg := s.configMgr.Get()
	key := cfg.DockerMirror
	if key == "" || key == "auto" || key == "direct" || key == "custom" {
		return
	}
	if s.dockerMirrorMon.ConsecFails(key) < mirrorSwitchFails {
		return
	}
	bestKey, ok := s.dockerMirrorMon.BestStable()
	if !ok || bestKey == key {
		return
	}
	latest := s.configMgr.Get()
	if latest.DockerMirror != key {
		return // 期间已被用户/其它路径改掉
	}
	latest.DockerMirror = bestKey
	if err := s.configMgr.SaveConfig(latest); err != nil {
		log.Printf("docker mirror auto-switch: save config failed: %v", err)
		return
	}
	var toLabel string
	for _, m := range config.DockerMirrorOptions() {
		if m.Key == bestKey {
			toLabel = m.Label
			break
		}
	}
	s.dockerMirrorMon.SetLastSwitch(mirror.SwitchInfo{
		From:   key,
		To:     bestKey,
		Time:   time.Now(),
		Reason: fmt.Sprintf("所选 Docker 加速源连续 %d 次探测失败，已自动切换到最稳定源", mirrorSwitchFails),
	})
	log.Printf("docker mirror auto-switch: %s -> %s (%s)", key, bestKey, toLabel)
}

// probeMirrors 并发探测全部 GitHub 镜像（含自定义源），结果汇入监测器，
// 然后评估是否需要对降级源做自动切换。
func (s *Server) probeMirrors() {
	type result struct {
		key, label string
		ok         bool
		latency    int
		tp         int // 实测吞吐 B/s（排序主指标）
	}
	mirrors := config.GitHubMirrorOptions()
	if cfg := s.configMgr.Get(); cfg.CustomGitHubMirror != "" {
		mirrors = append(mirrors, config.GitHubMirror{
			Key: "custom", Label: "自定义", URL: strings.TrimRight(cfg.CustomGitHubMirror, "/") + "/",
		})
	}

	var wg sync.WaitGroup
	ch := make(chan result, len(mirrors)+1)
	for _, m := range mirrors {
		if m.Key == "direct" || m.Key == "auto" || m.URL == "" {
			continue
		}
		wg.Add(1)
		go func(m config.GitHubMirror) {
			defer wg.Done()
			lat, tp, status := checkURLThroughput(context.Background(), probeURLFor(m))
			ch <- result{key: m.Key, label: m.Label, ok: status == "ok", latency: lat, tp: tp}
		}(m)
	}
	go func() { wg.Wait(); close(ch) }()
	for r := range ch {
		s.mirrorMon.Record(r.key, r.label, r.ok, r.latency, r.tp)
	}
	s.mirrorMon.SetLastProbe(time.Now())
	s.maybeAutoSwitch()
}

// maybeAutoSwitch 显式选定的镜像连续探测失败达到阈值时，持久化切换到
// 当前最稳定源，并记录最近一次切换供 UI 展示。
func (s *Server) maybeAutoSwitch() {
	if s.configMgr == nil {
		return
	}
	cfg := s.configMgr.Get()
	key := cfg.Mirror
	if key == "" || key == "auto" || key == "direct" || key == "custom" {
		return
	}
	if s.mirrorMon.ConsecFails(key) < mirrorSwitchFails {
		return
	}
	bestKey, ok := s.mirrorMon.BestStable()
	if !ok || bestKey == key {
		return
	}
	latest := s.configMgr.Get()
	if latest.Mirror != key {
		return // 期间已被用户/其它路径改掉
	}
	latest.Mirror = bestKey
	if err := s.configMgr.SaveConfig(latest); err != nil {
		log.Printf("mirror auto-switch: save config failed: %v", err)
		return
	}
	var toLabel string
	for _, m := range config.GitHubMirrorOptions() {
		if m.Key == bestKey {
			toLabel = m.Label
			break
		}
	}
	s.mirrorMon.SetLastSwitch(mirror.SwitchInfo{
		From:   key,
		To:     bestKey,
		Time:   time.Now(),
		Reason: fmt.Sprintf("所选加速源连续 %d 次探测失败，已自动切换到最稳定源", mirrorSwitchFails),
	})
	log.Printf("mirror auto-switch: %s -> %s (%s)", key, bestKey, toLabel)
}

// triggerProbe 触发一次立即探测（30 秒防抖，供健康接口 ?refresh=1 使用）。
func (s *Server) triggerProbe() {
	probeDebounceMu.Lock()
	defer probeDebounceMu.Unlock()
	if time.Since(probeDebouncedAt) < 30*time.Second {
		return
	}
	probeDebouncedAt = time.Now()
	go s.probeAllMirrors()
}

var (
	probeDebounceMu  sync.Mutex
	probeDebouncedAt time.Time
)

// handleMirrorHealth GET /api/mirrors/health
// 返回镜像健康快照 + 当前选定/实际生效源 + 最近一次自动切换。
func (s *Server) handleMirrorHealth(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("refresh") == "1" {
		s.triggerProbe()
	}
	cfg := s.configMgr.Get()
	active := cfg.Mirror
	switch active {
	case "":
		active = "direct"
	case "auto":
		if k, ok := s.mirrorMon.BestStable(); ok {
			active = k
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"mirrors":    s.mirrorMon.Snapshot(),
		"selected":   cfg.Mirror,
		"active":     active,
		"last_probe": s.mirrorMon.LastProbe(),
		"last_switch": s.mirrorMon.LastSwitch(),
		"interval_s": int(mirrorProbeInterval.Seconds()),
	})
}

// handleDockerMirrorHealth GET /api/mirrors/docker/health
// Docker 镜像加速健康快照（结构与 GitHub 版同构）。
func (s *Server) handleDockerMirrorHealth(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("refresh") == "1" {
		s.triggerProbe()
	}
	cfg := s.configMgr.Get()
	active := cfg.DockerMirror
	switch active {
	case "":
		active = "direct"
	case "auto":
		if k, ok := s.dockerMirrorMon.BestStable(); ok {
			active = k
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"mirrors":    s.dockerMirrorMon.Snapshot(),
		"selected":   cfg.DockerMirror,
		"active":     active,
		"last_probe": s.dockerMirrorMon.LastProbe(),
		"last_switch": s.dockerMirrorMon.LastSwitch(),
		"interval_s": int(mirrorProbeInterval.Seconds()),
	})
}
