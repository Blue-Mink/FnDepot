package main

import (
	"context"
	storeassets "fnos-store"
	"fnos-store/internal/api"
	"fnos-store/internal/cache"
	"fnos-store/internal/config"
	"fnos-store/internal/core"
	"fnos-store/internal/platform"
	"fnos-store/internal/scheduler"
	"fnos-store/internal/source"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

const storeAppName = "fnos-apps-store"

func main() {
	addr := envOr("LISTEN_ADDR", ":38011")
	projectRoot := envOr("PROJECT_ROOT", findProjectRoot())
	appsDir := envOr("APPS_DIR", defaultAppsDir(projectRoot))
	dataDir := envOr("DATA_DIR", defaultDataDir(projectRoot))
	cachePath := envOr("APPS_CACHE_PATH", filepath.Join(dataDir, "cache", "apps.json"))
	downloadDir := envOr("DOWNLOAD_DIR", filepath.Join(os.TempDir(), "fnos-store-downloads"))

	cfgMgr := config.NewManager(dataDir)
	cfg, err := cfgMgr.LoadConfig()
	if err != nil {
		log.Printf("load config failed, using defaults: %v", err)
	}

	cacheStore := cache.NewStore(dataDir)
	if err := cacheStore.Init(); err != nil {
		log.Printf("cache init failed: %v", err)
	}
	cacheStore.CleanupStaleFiles()

	ac := platform.NewAppCenter(projectRoot)
	src := source.NewFNOSAppsSource(
		cachePath,
		filepath.Join(projectRoot, "..", "fnos-apps", "apps.json"),
		cfgMgr,
	)
	recommendedSrc := source.NewRecommendedSource(
		filepath.Join(dataDir, "cache", "recommended.json"),
		filepath.Join(projectRoot, "..", "fnos-apps", "recommended.json"),
		cfgMgr,
	)
	reg := core.NewRegistry()
	downloader := core.NewDownloader(downloadDir)
	if err := downloader.CleanupStaleTmpFiles(); err != nil {
		log.Printf("cleanup stale tmp files failed: %v", err)
	}

	checkInterval := time.Duration(cfg.CheckIntervalHours) * time.Hour

	srv := api.NewServer(api.Config{
		AppCenter:         ac,
		Source:            src,
		RecommendedSource: recommendedSrc,
		Registry:          reg,
		Downloader:        downloader,
		ConfigMgr:         cfgMgr,
		CacheStore:        cacheStore,
		AppsDir:           appsDir,
		AppCenterDir:      appCenterDir(appsDir),
		Platform:          platform.DetectPlatform(),
		StoreApp:          storeAppName,
		StaticFS:          storeassets.WebFS,
		DataDir:           dataDir,
	})

	// 周期检查：刷新注册表（检测更新）后，若开启自动更新则后台自动安装。
	sched := scheduler.New(checkInterval, func(ctx context.Context) error {
		if err := srv.RefreshRegistry(ctx); err != nil {
			return err
		}
		srv.AutoUpdateIfEnabled()
		return nil
	}, cacheStore.LastCheckAt)
	srv.SetScheduler(sched)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go sched.Start(ctx)

	httpServer := &http.Server{
		Addr:    addr,
		Handler: srv.Mux,
	}

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Println("shutting down...")
		sched.Stop()
		// Graceful shutdown: drain in-flight SSE streams and CLI ops before
		// closing the listener. The detached appcenter-cli child started by
		// runSelfUpdate is already in its own session (Setsid) so SIGTERM to
		// the parent does NOT propagate to it; this drain is for normal
		// systemd/manual restarts, not for the self-update kill path.
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer shutdownCancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			log.Printf("graceful shutdown failed, forcing close: %v", err)
			_ = httpServer.Close()
		}
		cancel()
	}()

	log.Printf("fnos-store listening on %s", addr)
	if err := httpServer.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func findProjectRoot() string {
	exe, err := os.Executable()
	if err != nil {
		return "."
	}
	dir := filepath.Dir(exe)
	for i := 0; i < 5; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	return "."
}

func defaultAppsDir(projectRoot string) string {
	if _, err := os.Stat("/var/apps"); err == nil {
		return "/var/apps"
	}
	return filepath.Join(projectRoot, "dev", "mock-apps")
}

// appCenterDir 定位应用中心程序目录（存放 ui/images 图标）。
// 真机上 /var/apps 与 /vol1/@appcenter 是 fnOS 标准布局；
// 也可用环境变量 APPCENTER_DIR 覆盖。找不到则返回空（图标回退占位）。
func appCenterDir(appsDir string) string {
	if v := os.Getenv("APPCENTER_DIR"); v != "" {
		return v
	}
	for _, cand := range []string{"/vol1/@appcenter", "/vol2/@appcenter", "/vol3/@appcenter"} {
		if _, err := os.Stat(cand); err == nil {
			return cand
		}
	}
	if appsDir == "/var/apps" {
		return ""
	}
	return ""
}

func defaultDataDir(projectRoot string) string {
	prod := filepath.Join("/var/apps", storeAppName, "var")
	if _, err := os.Stat(filepath.Dir(prod)); err == nil {
		return prod
	}
	return filepath.Join(projectRoot, "var")
}
