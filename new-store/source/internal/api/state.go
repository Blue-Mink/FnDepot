package api

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"fnos-store/internal/cache"
	"fnos-store/internal/config"
	"fnos-store/internal/core"
	"fnos-store/internal/platform"
	"fnos-store/internal/source"
)

type refreshDebouncer struct {
	mu      sync.Mutex
	timer   *time.Timer
	pending bool
}

func (s *Server) refreshRegistry(ctx context.Context) error {
	if s.source == nil || s.registry == nil {
		return errors.New("source/registry not configured")
	}

	// 刷新是服务端工作，与任何调用方（HTTP 请求、安装管道、源详情查看）的
	// 生命周期解耦：调用方断开/超时会取消其 context，实测会让所有外部源
	// 以 "context canceled" 集体失败、注册表丢掉整个外部目录（2026-09-18
	// 测试机 08:13 全源取消即此路径）。统一在此脱离取消传播。
	ctx = context.WithoutCancel(ctx)

	localApps, err := core.ScanInstalled(s.appsDir)
	if err != nil {
		return err
	}

	// 内置源列表自动同步：每次目录检查时，把源列表里未添加过的 FnDepot 源
	// 自动加入（设置 source_list_disabled 可整体关闭）。失败只记日志，
	// 不阻断本次目录刷新。refreshOnAdd=false —— 外层紧接着抓取全部源。
	if s.configMgr != nil {
		if cfg := s.configMgr.Get(); !cfg.SourceListDisabled {
			if res := s.syncSourceList(ctx, false); res.Added > 0 || len(res.Errors) > 0 {
				for _, e := range res.Errors {
					log.Printf("source list: %s", e)
				}
			}
		}
	}

	remoteApps, fetchErr := s.source.FetchApps(ctx)

	// FnDepot 外部源：并发抓取，与内置目录合并。
	// 同名应用（appname 相同）全部保留：注册表用 appname@源名 区分，
	// 内置目录与外部源的同名应用会同时展示。
	customApps, customStatus := s.fetchCustomSources(ctx)
	if customApps != nil {
		remoteApps = append(remoteApps, customApps...)
	}

	var installedTags map[string]string
	if s.cacheStore != nil {
		installedTags = s.cacheStore.InstalledTags()
	}

	// 全部外部源抓取失败 + 本次没有外部应用 + 现有注册表已有外部应用时，
	// 不重建注册表（否则整个外部目录被清空，要等下次定时刷新才恢复）；
	// 保留旧数据，失败原因由源状态在 UI 展示。冷启动（注册表为空）照常合并。
	allCustomFailed := false
	if len(customStatus) > 0 {
		allCustomFailed = true
		for _, st := range customStatus {
			if st.Error == "" {
				allCustomFailed = false
				break
			}
		}
	}
	newExternal := 0
	if remoteApps != nil {
		for _, a := range remoteApps {
			if a.Source != "" && a.Source != "fnos-apps" {
				newExternal++
			}
		}
	}

	now := time.Now()
	s.mu.Lock()
	// Preserve existing registry when all remote/cache/local fallbacks fail.
	if remoteApps != nil || fetchErr == nil {
		if allCustomFailed && newExternal == 0 && s.registry.HasExternalApps() {
			log.Printf("refresh: all %d custom sources failed; keeping previous registry to avoid losing external catalog", len(customStatus))
		} else {
			s.registry.Merge(localApps, remoteApps, installedTags)
		}
	}
	s.lastCheck = now
	if len(customStatus) > 0 {
		s.sourceStatus = customStatus
	}
	s.mu.Unlock()

	if s.cacheStore != nil {
		s.cacheStore.SetLastCheckAt(now)
	}

	_ = s.refreshRecommended(ctx)

	s.refreshRuntimeStatus()
	return fetchErr
}

func (s *Server) RefreshRegistry(ctx context.Context) error {
	return s.refreshRegistry(ctx)
}

func (s *Server) refreshRuntimeStatus() {
	if s.ac == nil || s.queue == nil || s.registry == nil {
		return
	}

	var apps []platform.InstalledApp
	err := s.queue.WithCLI(func() error {
		var listErr error
		apps, listErr = s.ac.List()
		return listErr
	})
	if err != nil {
		return
	}

	status := make(map[string]string, len(apps))
	versions := make(map[string]string, len(apps))
	control := make(map[string]platform.AppControl, len(apps))
	web := make(map[string]platform.WebService, len(apps))
	for _, app := range apps {
		status[app.AppName] = app.Status
		versions[app.AppName] = app.Version
		control[app.AppName] = app.Control
		web[app.AppName] = app.Web
	}

	s.mu.Lock()
	s.statusByApp = status
	s.controlByApp = control
	s.webByApp = web
	// The daemon list is the authority on whether an app exists; fold it in
	// so apps whose /var/apps manifest the scan missed still show as
	// installed instead of dead-ending on install (#280/#281).
	s.registry.ReconcileInstalled(versions)
	// 应用中心已安装但目录未收录的应用（如官方应用中心安装的 app）并入列表
	localApps := make([]core.LocalApp, 0, len(apps))
	for _, a := range apps {
		localApps = append(localApps, core.LocalApp{
			AppName:     a.AppName,
			DisplayName: a.DisplayName,
			Version:     a.Version,
			Status:      a.Status,
		})
	}
	s.registry.AddLocalApps(localApps)
	s.mu.Unlock()
}

func (s *Server) refreshRecommended(ctx context.Context) error {
	if s.recommendedSource == nil {
		return nil
	}

	apps, err := s.recommendedSource.FetchRecommendedApps(ctx)
	if err != nil {
		return err
	}

	s.mu.Lock()
	s.recommendedApps = append([]source.RecommendedApp(nil), apps...)
	s.mu.Unlock()

	return nil
}

func (s *Server) listRegistryApps() []core.AppInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.registry == nil {
		return nil
	}
	return s.registry.List()
}

func (s *Server) listRecommendedApps() []source.RecommendedApp {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]source.RecommendedApp(nil), s.recommendedApps...)
}

func (s *Server) getRegistryApp(name string) (core.AppInfo, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.registry == nil {
		return core.AppInfo{}, false
	}
	return s.registry.Get(name)
}

func (s *Server) getRuntimeStatus(name string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.statusByApp[name]
}

// getRuntimeControl returns the daemon's per-app capability bits for name.
// found is false when the daemon list has no entry (e.g. CLI fallback) and
// callers must treat the result as "capabilities unknown".
func (s *Server) getRuntimeControl(name string) (platform.AppControl, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.controlByApp[name]
	return c, ok
}

// getRuntimeWeb returns the daemon's per-app openable web entry for name.
// found is false when the daemon list has no entry or the app has no
// openable web UI at all (neither own port nor web-UI path) — no "打开"
// button may be rendered.
func (s *Server) getRuntimeWeb(name string) (platform.WebService, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	w, found := s.webByApp[name]
	return w, found && (w.HasURL() || w.HasWebUIPath())
}

func (s *Server) getLastCheck() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastCheck
}

func (s *Server) refreshRegistryDebounced(ctx context.Context) {
	s.refreshDebouncer.mu.Lock()
	defer s.refreshDebouncer.mu.Unlock()

	if s.refreshDebouncer.pending {
		if s.refreshDebouncer.timer != nil {
			s.refreshDebouncer.timer.Stop()
		}
	}

	s.refreshDebouncer.pending = true
	s.refreshDebouncer.timer = time.AfterFunc(2*time.Second, func() {
		s.refreshDebouncer.mu.Lock()
		s.refreshDebouncer.pending = false
		s.refreshDebouncer.mu.Unlock()

		_ = s.refreshRegistry(context.Background())
	})
}

// rebuildCustomSources 从配置重建外部源列表（惰性构造，不立即抓取）。
// 启动与每次源配置变更后调用。
func (s *Server) rebuildCustomSources() {
	if s.configMgr == nil {
		return
	}
	cfg := s.configMgr.Get()
	srcs := make([]*source.FNDepotSource, 0, len(cfg.Sources))
	for _, entry := range cfg.Sources {
		cs, err := source.NewFNDepotSourceLazy(entry.URL, s.configMgr)
		if err != nil {
			continue // 无效地址在源列表 API 中按错误展示
		}
		cs.OverrideName(entry.Name)
		srcs = append(srcs, cs)
	}
	s.mu.Lock()
	s.customSources = srcs
	s.mu.Unlock()
}

// fetchCustomSources 并发抓取所有外部源，返回合并后的 RemoteApp 列表
// （仅当前架构可用的应用）与每源状态。单源失败不影响其他源与内置目录。
func (s *Server) fetchCustomSources(ctx context.Context) ([]source.RemoteApp, map[string]sourceStatusInfo) {
	s.mu.RLock()
	srcs := s.customSources
	s.mu.RUnlock()
	if len(srcs) == 0 {
		return nil, nil
	}

	type customResult struct {
		id   string
		apps []source.RemoteApp
		err  error
	}
	ch := make(chan customResult, len(srcs))
	for _, cs := range srcs {
		go func(cs *source.FNDepotSource) {
			// 父预算要覆盖整条候选链（直连重试+镜像+分支回退，单候选 15s），
			// 否则前面候选耗光预算后，后面的直接 context deadline exceeded。
			cctx, cancel := context.WithTimeout(ctx, 120*time.Second)
			defer cancel()
			apps, err := cs.FetchApps(cctx)
			ch <- customResult{cs.ID(), apps, err}
		}(cs)
	}

	merged := []source.RemoteApp{}
	status := make(map[string]sourceStatusInfo, len(srcs))
	now := time.Now()
	for range srcs {
		r := <-ch
		if r.err != nil {
			status[r.id] = sourceStatusInfo{Error: r.err.Error(), LastFetched: now}
			continue
		}
		status[r.id] = sourceStatusInfo{AppCount: len(r.apps), LastFetched: now}
		merged = append(merged, r.apps...)
	}
	// V1 平铺源 / 部分 V2 release 的 manifest 没有 updated_at（URL 里也没有
	// 日期标签）：用 HEAD Last-Modified 回填「最近更新」，顺带补缺失的包大小。
	s.enrichMissingDates(ctx, merged)
	return merged, status
}

// probeDateTTL 探测结果有效期：同一下载链接 7 天内不重探。
// 源发布新版本时下载链接会变，新 URL 立即触发探测，7 天只是给
// 「同 URL 悄悄重传」与文件元数据变化的兜底。
const probeDateTTL = 7 * 24 * time.Hour

// enrichMissingDates 为缺失 updated_at 的外部应用回填文件 Last-Modified。
// 磁盘缓存（cache.Store）按 URL 去重：缓存有效期内零网络请求；
// 并发 8、单请求 4s、总预算 90s —— 首轮约百余条链接，后续增量极少。
// 探测失败不缓存、不影响其他字段（日期保持空，UI 显示 "-"）。
func (s *Server) enrichMissingDates(ctx context.Context, apps []source.RemoteApp) {
	if s.cacheStore == nil || s.configMgr == nil {
		return
	}
	need := make(map[string][]int) // url -> 下标列表（转载同源可能多条）
	for i := range apps {
		if apps[i].UpdatedAt == "" && apps[i].FpkURL != "" {
			need[apps[i].FpkURL] = append(need[apps[i].FpkURL], i)
		}
	}
	if len(need) == 0 {
		return
	}
	probeCache := s.cacheStore.LoadProbeCache()
	now := time.Now()

	// 1) 磁盘缓存命中直接应用（零网络请求）
	for u, idxs := range need {
		e, ok := probeCache[u]
		if !ok || now.Sub(e.ProbedAt) > probeDateTTL {
			continue
		}
		for _, i := range idxs {
			if apps[i].UpdatedAt == "" {
				apps[i].UpdatedAt = e.Date
			}
			// >1：1 是旧版 Range 分片长度误存的值，视为无效
			if apps[i].SizeBytes == 0 && e.Size > 1 {
				apps[i].SizeBytes = e.Size
			}
		}
	}

	// 2) 无缓存或已过期的链接才真正探测（按 URL 去重）。
	//    size-only 缓存（HEAD 只有大小、没有 Last-Modified）：GitHub 链接
	//    仍可经 API 拿到精确日期（raw HEAD 无 Last-Modified 但 contents API
	//    有 last_modified），继续探；非 GitHub 链接 size-only 即终态，跳过。
	var toProbe []string
	for u := range need {
		e, ok := probeCache[u]
		if ok && now.Sub(e.ProbedAt) <= probeDateTTL {
			if e.Date != "" || !source.IsGitHubFileURL(u) {
				continue
			}
		}
		toProbe = append(toProbe, u)
	}
	if len(toProbe) == 0 {
		return
	}

	cfg := s.configMgr.Get()
	prefix := config.GitHubMirrorPrefix(cfg.Mirror, cfg)
	// 预算随待探链接数伸缩（8 并发 × 最坏 8s/条），上限 5 分钟；
	// 预算耗尽时剩余探测快速失败并照常回报，不留悬挂。
	budget := time.Duration(60+len(toProbe)*4) * time.Second
	if budget > 5*time.Minute {
		budget = 5 * time.Minute
	}
	pctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	client := &http.Client{Timeout: 4 * time.Second}

	type probeResult struct {
		url  string
		date string
		size int64
		ok   bool // 每个 URL 必有且仅有一次回报（失败也回），接收端才不会悬挂
	}
	resCh := make(chan probeResult, len(toProbe))
	sem := make(chan struct{}, 8)
	for _, u := range toProbe {
		sem <- struct{}{}
		go func(u string) {
			defer func() { <-sem }()
			// GitHub 链接优先走当前选中的加速镜像（境内直连经常超时），
			// 直连兜底；非 GitHub 直链只探一次。
			candidates := []string{u}
			if strings.Contains(u, "github.com") {
				if prefix != "" {
					candidates = []string{prefix + u, u}
				}
			}
			modTime, size, ok := source.ProbeFileMeta(pctx, client, u, candidates)
			date := ""
			if ok && !modTime.IsZero() {
				date = modTime.Format(time.RFC3339)
			}
			resCh <- probeResult{u, date, size, ok}
		}(u)
	}
	okCount, failCount := 0, 0
	for range toProbe {
		pr := <-resCh
		if !pr.ok {
			failCount++ // 失败不缓存：下次刷新重试
			continue
		}
		okCount++
		probeCache[pr.url] = cache.ProbeEntry{
			Date:     pr.date,
			Size:     pr.size,
			ProbedAt: time.Now(),
		}
		for _, i := range need[pr.url] {
			if apps[i].UpdatedAt == "" {
				apps[i].UpdatedAt = pr.date
			}
			if apps[i].SizeBytes == 0 && pr.size > 1 {
				apps[i].SizeBytes = pr.size
			}
		}
	}
	log.Printf("meta probe: %d 条链接（%d 成功 / %d 失败），缓存 %d 条", len(toProbe), okCount, failCount, len(probeCache))
	_ = s.cacheStore.SaveProbeCache(probeCache)
}

// ListSources 返回外部源管理视图（配置 + 最近抓取状态）。
func (s *Server) ListSources() []SourceEntry {
	cfg := s.configMgr.Get()
	s.mu.RLock()
	srcs := s.customSources
	status := s.sourceStatus
	s.mu.RUnlock()

	byID := make(map[string]*source.FNDepotSource, len(srcs))
	for _, cs := range srcs {
		byID[cs.ID()] = cs
	}

	entries := make([]SourceEntry, 0, len(cfg.Sources))
	for _, entry := range cfg.Sources {
		e := SourceEntry{
			ID:   entry.ID,
			Name: entry.Name,
			URL:  entry.URL,
		}
		if cs, ok := byID[entry.ID]; ok {
			meta := cs.Meta()
			e.Name = meta.Name
			e.Author = meta.Author
			e.Homepage = meta.Homepage
		}
		if st, ok := status[entry.ID]; ok {
			e.AppCount = st.AppCount
			e.Error = st.Error
			e.LastFetched = st.LastFetched
		}
		entries = append(entries, e)
	}
	return entries
}
