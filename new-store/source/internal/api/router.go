package api

import (
	"context"
	"fnos-store/internal/cache"
	"fnos-store/internal/config"
	"fnos-store/internal/core"
	"fnos-store/internal/mirror"
	"fnos-store/internal/panel"
	"fnos-store/internal/platform"
	"fnos-store/internal/scheduler"
	"fnos-store/internal/source"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"sync"
	"time"
)

type Server struct {
	Mux               *http.ServeMux
	ac                platform.AppCenter
	source            source.Source
	recommendedSource *source.RecommendedSource
	registry          *core.Registry
	queue             *OperationQueue
	pipeline          *installPipeline
	configMgr         *config.Manager
	cacheStore        *cache.Store
	scheduler         *scheduler.Scheduler
	appsDir           string
	// appCenterDir 是应用中心的程序目录（/vol1/@appcenter），用于读取
	// 「fnOS应用中心」来源应用的本地图标（ui/images/icon-*.png）。可空。
	appCenterDir      string
	platform          string
	storeApp          string
	staticFS          fs.FS
	lastCheck         time.Time
	statusByApp       map[string]string
	controlByApp      map[string]platform.AppControl
	webByApp          map[string]platform.WebService
	recommendedApps   []source.RecommendedApp
	// customSources 是用户添加的 FnDepot 外部应用源；sourceStatus 记录
	// 每个源最近一次抓取的应用数与错误（按源 ID 索引）。
	customSources []*source.FNDepotSource
	sourceStatus  map[string]sourceStatusInfo

	// panelClient 是官方应用中心（fnos-official）直连客户端；未配置/未启用
	// 时为 nil，官方源自动降级为空（其余源不受影响）。
	panelClient    *panel.Client
	officialSource *source.OfficialSource

	// mirrorMon 是 GitHub 加速源健康监测器（智能排序 + 自动切换），
	// 由 startMirrorMonitor 在启动时创建。
	mirrorMon *mirror.Monitor
	// dockerMirrorMon 是 Docker 镜像加速健康监测器（同构，独立计数）。
	dockerMirrorMon *mirror.Monitor
	ctx       context.Context
	cancel    context.CancelFunc

	mu               sync.RWMutex
	refreshDebouncer *refreshDebouncer

	// installedNamesCache 已安装应用小缓存（appname 小写 → 版本），供
	// 「FPK 下载列表显示已安装」等高频查询；60s TTL，安装操作后 force 刷新。
	installedNamesCache installedNamesCache

	// officialDetailFetched 记录哪些官方应用已经拉过 app/detail（无论字段
	// 是否为空），避免面板某字段真为空时每次刷新都重复拉。受 mu 保护。
	officialDetailFetched map[string]bool
}

type installedNamesCache struct {
	mu    sync.Mutex
	at    time.Time
	names map[string]string
}

// installedAppNames 返回当前已安装应用（appname 小写 → 版本）。
// 三处取并集，覆盖「其他方式安装」的应用：
//   - 本地 manifest 扫描（@appcenter 磁盘事实，含官方中心/手动装的应用）
//   - 商店自己记住的安装 tag（New Store 安装/更新写回）
//   - 注册表 Installed 条目（daemon 对账过）
func (s *Server) installedAppNames(force bool) map[string]string {
	c := &s.installedNamesCache
	c.mu.Lock()
	if !force && c.names != nil && time.Since(c.at) < 60*time.Second {
		out := c.names
		c.mu.Unlock()
		return out
	}
	out := make(map[string]string)
	if s.appsDir != "" {
		if local, err := core.ScanInstalled(s.appsDir); err == nil {
			for _, m := range local {
				if m.AppName == "" {
					continue
				}
				ver := m.FpkVersion
				if ver == "" {
					ver = m.Version
				}
				out[strings.ToLower(m.AppName)] = ver
			}
		}
	}
	if s.cacheStore != nil {
		for k, v := range s.cacheStore.InstalledTags() {
			if _, ok := out[strings.ToLower(k)]; !ok {
				out[strings.ToLower(k)] = v
			}
		}
	}
	s.mu.RLock()
	if s.registry != nil {
		for _, app := range s.registry.List() {
			if !app.Installed || app.AppName == "" {
				continue
			}
			ver := app.InstalledFpkVersion
			if ver == "" {
				ver = app.InstalledVersion
			}
			if _, ok := out[strings.ToLower(app.AppName)]; !ok {
				out[strings.ToLower(app.AppName)] = ver
			}
		}
	}
	s.mu.RUnlock()
	c.names = out
	c.at = time.Now()
	c.mu.Unlock()
	return out
}

// refreshInstalledNames 安装/更新操作后强制刷新已安装缓存。
func (s *Server) refreshInstalledNames() {
	_ = s.installedAppNames(true)
}

// sourceStatusInfo 是单个外部源最近一次抓取的结果摘要。
type sourceStatusInfo struct {
	AppCount    int       `json:"app_count"`
	Error       string    `json:"error,omitempty"`
	LastFetched time.Time `json:"last_fetched"`
}

type Config struct {
	AppCenter         platform.AppCenter
	Source            source.Source
	RecommendedSource *source.RecommendedSource
	Registry          *core.Registry
	Downloader        *core.Downloader
	ConfigMgr         *config.Manager
	CacheStore        *cache.Store
	Scheduler         *scheduler.Scheduler
	AppsDir           string
	AppCenterDir      string
	Platform          string
	StoreApp          string
	StaticFS          fs.FS
}

func NewServer(cfg Config) *Server {
	queue := NewOperationQueue()
	s := &Server{
		Mux:               http.NewServeMux(),
		ac:                cfg.AppCenter,
		source:            cfg.Source,
		recommendedSource: cfg.RecommendedSource,
		registry:          cfg.Registry,
		queue:             queue,
		pipeline: &installPipeline{
			downloads:  cfg.Downloader,
			ac:         cfg.AppCenter,
			queue:      queue,
			appsDir:    cfg.AppsDir,
			configMgr:  cfg.ConfigMgr,
			cacheStore: cfg.CacheStore,
		},
		configMgr:        cfg.ConfigMgr,
		cacheStore:       cfg.CacheStore,
		scheduler:        cfg.Scheduler,
		appsDir:          cfg.AppsDir,
		appCenterDir:     cfg.AppCenterDir,
		platform:         cfg.Platform,
		storeApp:         cfg.StoreApp,
		staticFS:         cfg.StaticFS,
		statusByApp:            make(map[string]string),
		controlByApp:           make(map[string]platform.AppControl),
		webByApp:               make(map[string]platform.WebService),
		officialDetailFetched:  make(map[string]bool),
		refreshDebouncer: &refreshDebouncer{},
		sourceStatus:     make(map[string]sourceStatusInfo),
	}
	s.routes()
	s.rebuildPanelClient()
	s.rebuildCustomSources()
	s.startMirrorMonitor()
	// 首次刷新放后台：源列表自动同步（首跑要验证几十个仓库）+ 目录抓取
	// 可能耗时数分钟，不能阻塞 HTTP 监听。UI 先出骨架/「检查中」，数据就绪后
	// SSE/轮询自然补齐。scheduler 的即时首查由 lastCheck 防重。
	go s.refreshRecommended(context.Background())
	go s.refreshRegistry(context.Background())
	return s
}

func (s *Server) routes() {
	// 诊断端点：真机上刷新卡住/慢时抓 goroutine 栈与 profile。
	// 内网端口（默认 8011 仅 LAN 可达），无需鉴权但仅限本机/局域网。
	s.mountPprof()
	s.Mux.HandleFunc("GET /api/apps", s.handleListApps)
	s.Mux.HandleFunc("GET /api/recommended", s.handleListRecommended)
	s.Mux.HandleFunc("POST /api/apps/{appname}/install", s.handleInstall)
	s.Mux.HandleFunc("POST /api/apps/{appname}/update", s.handleUpdate)
	s.Mux.HandleFunc("POST /api/apps/{appname}/uninstall", s.handleUninstall)
	s.Mux.HandleFunc("POST /api/apps/{appname}/start", func(w http.ResponseWriter, r *http.Request) { s.handleStartStop(w, r, "start") })
	s.Mux.HandleFunc("POST /api/apps/{appname}/stop", func(w http.ResponseWriter, r *http.Request) { s.handleStartStop(w, r, "stop") })
	s.Mux.HandleFunc("GET /api/apps/{appname}/download", s.handleDownloadFpk)
	s.Mux.HandleFunc("POST /api/apps/{appname}/download-task", s.handleDownloadTask)
	s.Mux.HandleFunc("GET /api/fpk-downloads", s.handleListFpkDownloads)
	s.Mux.HandleFunc("DELETE /api/fpk-downloads/{name}", s.handleDeleteFpkDownload)
	s.Mux.HandleFunc("POST /api/fpk-downloads/{name}/install", s.handleInstallFpkDownload)
	s.Mux.HandleFunc("GET /api/apps/{appname}/asset", s.handleAppAsset)
	s.Mux.HandleFunc("GET /api/apps/{appname}/wizard", s.handleGetWizard)
	s.Mux.HandleFunc("GET /api/apps/{appname}/logs", s.handleGetAppLogs)
	s.Mux.HandleFunc("GET /api/apps/{appname}/diagnostic", s.handleGetAppDiagnostic)
	s.Mux.HandleFunc("PUT /api/apps/{appname}/ignore-update", s.handleIgnoreUpdate)
	s.Mux.HandleFunc("DELETE /api/apps/{appname}/ignore-update", s.handleUnignoreUpdate)
	s.Mux.HandleFunc("POST /api/apps/reload", s.handleReloadApps)
	s.Mux.HandleFunc("POST /api/check", s.handleCheck)
	s.Mux.HandleFunc("GET /api/status", s.handleStatus)
	s.Mux.HandleFunc("GET /api/settings", s.handleGetSettings)
	s.Mux.HandleFunc("PUT /api/settings", s.handlePutSettings)
	s.Mux.HandleFunc("GET /api/sources", s.handleListSources)
	s.Mux.HandleFunc("POST /api/sources", s.handleAddSource)
	s.Mux.HandleFunc("POST /api/sources/batch", s.handleBatchAddSources)
	s.Mux.HandleFunc("DELETE /api/sources/{id}", s.handleRemoveSource)
	s.Mux.HandleFunc("POST /api/sources/{id}/sync", s.handleSyncSource)
	s.Mux.HandleFunc("POST /api/sources/{id}/toggle", s.handleToggleSource)
	s.Mux.HandleFunc("POST /api/sources/reorder", s.handleReorderSources)
	s.Mux.HandleFunc("POST /api/sources/sync-list", s.handleSyncSourceList)
	s.Mux.HandleFunc("GET /api/apps/{appname}/panel-detail", s.handlePanelDetail)
	s.Mux.HandleFunc("POST /api/panel/test", s.handlePanelTest)
	s.Mux.HandleFunc("GET /api/store-update", s.handleGetStoreUpdate)
	s.Mux.HandleFunc("POST /api/store-update", s.handlePostStoreUpdate)
	s.Mux.HandleFunc("POST /api/mirrors/check", s.handleCheckMirrors)
	s.Mux.HandleFunc("GET /api/mirrors/health", s.handleMirrorHealth)
	s.Mux.HandleFunc("GET /api/mirrors/docker/health", s.handleDockerMirrorHealth)
	s.Mux.HandleFunc("/", s.handleSPA)
}

func (s *Server) handleSPA(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		http.NotFound(w, r)
		return
	}

	if s.staticFS == nil {
		http.NotFound(w, r)
		return
	}

	if r.URL.Path == "/" {
		http.ServeFileFS(w, r, s.staticFS, "web/index.html")
		return
	}

	assetPath := path.Clean(strings.TrimPrefix(r.URL.Path, "/"))
	if assetPath == "." {
		http.ServeFileFS(w, r, s.staticFS, "web/index.html")
		return
	}

	fullPath := path.Join("web", assetPath)
	if _, err := fs.Stat(s.staticFS, fullPath); err == nil {
		http.ServeFileFS(w, r, s.staticFS, fullPath)
		return
	}

	http.ServeFileFS(w, r, s.staticFS, "web/index.html")
}

func (s *Server) SetScheduler(sched *scheduler.Scheduler) {
	s.scheduler = sched
}

// rebuildPanelClient 依据配置重建官方应用中心直连客户端（启动与设置变更后
// 调用）。未启用或未填账号时 panelClient=nil，官方源自动降级为空。
func (s *Server) rebuildPanelClient() {
	var client *panel.Client
	if s.configMgr != nil {
		cfg := s.configMgr.Get()
		if cfg.PanelEnabled && strings.TrimSpace(cfg.PanelUsername) != "" {
			client = panel.NewClient(cfg.PanelBaseURL, cfg.PanelUsername, cfg.PanelPassword)
		}
	}
	s.panelClient = client
	s.officialSource = source.NewOfficialSource(client)
}
