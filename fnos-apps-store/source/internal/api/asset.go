package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"fnos-store/internal/config"
	"fnos-store/internal/core"
)

// 应用详情页资源代理：README（Markdown）与预览图。
// 浏览器端直连 raw.githubusercontent.com 在多数网络下不可靠，这里由后端
// 按配置的 GitHub 镜像链抓取后回传。只代理应用自身声明的 URL，不接受
// 任意地址（避免 SSRF）。

const assetTTL = 10 * time.Minute

type assetCacheEntry struct {
	body        []byte
	contentType string
	expiresAt   time.Time
}

type assetCache struct {
	mu      sync.Mutex
	entries map[string]assetCacheEntry
	max     int
}

var appAssetCache = newAssetCache(128)

// iconProbeCache 记录「该应用探测过所有候选图标路径都没有图标」的结论，
// 30 分钟内不再重复探测，避免每次打开详情页都打一轮请求。
type iconProbe struct {
	mu sync.Mutex
	m  map[string]time.Time
}

var iconProbeCache = &iconProbe{m: make(map[string]time.Time)}

const iconProbeTTL = 30 * time.Minute

func (c *iconProbe) isBlocked(key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	t, ok := c.m[key]
	if !ok || time.Now().After(t) {
		return false
	}
	return true
}

func (c *iconProbe) mark(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.m) > 512 {
		c.m = make(map[string]time.Time)
	}
	c.m[key] = time.Now().Add(iconProbeTTL)
}

func newAssetCache(max int) *assetCache {
	return &assetCache{entries: make(map[string]assetCacheEntry), max: max}
}

func (c *assetCache) get(key string) (assetCacheEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok || time.Now().After(e.expiresAt) {
		delete(c.entries, key)
		return assetCacheEntry{}, false
	}
	return e, true
}

func (c *assetCache) put(key string, e assetCacheEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) >= c.max {
		c.entries = make(map[string]assetCacheEntry) // 简单全量淘汰
	}
	c.entries[key] = e
}

func assetContentTypeFor(name string) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	case ".gif":
		return "image/gif"
	case ".md", ".markdown":
		return "text/markdown; charset=utf-8"
	default:
		return "application/octet-stream"
	}
}

// handleAppAsset 代理应用自身声明的 readme / preview 资源。
// GET /api/apps/{appname}/asset?type=readme
// GET /api/apps/{appname}/asset?type=preview&index=0
func (s *Server) handleAppAsset(w http.ResponseWriter, r *http.Request) {
	appName := r.PathValue("appname")
	if appName == "" {
		writeAPIError(w, http.StatusBadRequest, "appname is required")
		return
	}
	app, ok := s.getRegistryApp(appName)
	if !ok {
		writeAPIError(w, http.StatusNotFound, "app not found")
		return
	}

	var target, kind string
	switch t := r.URL.Query().Get("type"); t {
	case "readme":
		target, kind = app.ReadmeURL, "readme"
	case "preview":
		idx, _ := strconv.Atoi(r.URL.Query().Get("index"))
		if idx < 0 || idx >= len(app.PreviewURLs) {
			writeAPIError(w, http.StatusBadRequest, "preview index out of range")
			return
		}
		target, kind = app.PreviewURLs[idx], "preview"
	case "icon":
		body, ct, err := s.resolveAppIcon(r.Context(), app)
		if err != nil {
			writeAPIError(w, http.StatusNotFound, "该应用没有可用图标")
			return
		}
		w.Header().Set("Content-Type", ct)
		w.Header().Set("Cache-Control", "private, max-age=3600")
		_, _ = w.Write(body)
		return
	default:
		writeAPIError(w, http.StatusBadRequest, "type must be readme, preview or icon")
		return
	}
	target = strings.TrimSpace(target)
	if target == "" {
		writeAPIError(w, http.StatusNotFound, "该应用没有此资源")
		return
	}

	cacheKey := appName + "/" + kind + "/" + target
	if e, ok := appAssetCache.get(cacheKey); ok {
		w.Header().Set("Content-Type", e.contentType)
		w.Header().Set("Cache-Control", "private, max-age=600")
		_, _ = w.Write(e.body)
		return
	}

	body, contentType, err := s.fetchAsset(r.Context(), target)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "资源获取失败: "+err.Error())
		return
	}
	appAssetCache.put(cacheKey, assetCacheEntry{
		body: body, contentType: contentType, expiresAt: time.Now().Add(assetTTL),
	})
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "private, max-age=600")
	_, _ = w.Write(body)
}

// normalizeGitHubURL 把 github.com blob 页面链接归一化为 raw 直链
// （部分源把 ICON 写成 https://github.com/.../blob/main/ICON.PNG，
// 直接 <img> 出来是 HTML 页面，图标必然加载失败）。
func normalizeGitHubURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || !strings.HasSuffix(u.Host, "github.com") {
		return rawURL
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	// 期望 [owner, repo, blob|raw, branch, path...]
	for i, seg := range parts {
		if seg == "blob" && i+2 < len(parts) {
			branch := parts[i+1]
			filePath := strings.Join(parts[i+2:], "/")
			return "https://raw.githubusercontent.com/" + parts[0] + "/" + parts[1] + "/" + branch + "/" + filePath
		}
	}
	return rawURL
}

// sourceBaseURL 按源名查配置里的源地址。
func (s *Server) sourceBaseURL(name string) string {
	if s.configMgr == nil || name == "" {
		return ""
	}
	cfg := s.configMgr.Get()
	for _, src := range cfg.Sources {
		if src.Name == name || src.ID == name {
			return strings.TrimRight(src.URL, "/")
		}
	}
	return ""
}

// iconCandidates 推导外部源应用的候选图标地址（按优先级）：
// FnDepot 仓库布局为 <appname>/ICON.PNG，兼容 ICON_256/icon.png 及仓库根 ICON。
func iconCandidates(appName, sourceBase string) []string {
	u, err := url.Parse(sourceBase)
	if err != nil || u.Host == "" {
		return nil
	}
	var base string
	if strings.HasSuffix(u.Host, "github.com") {
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) < 2 {
			return nil
		}
		base = "https://raw.githubusercontent.com/" + parts[0] + "/" + parts[1] + "/HEAD/"
	} else {
		// 源地址本身就是 raw 或任意 https 直链（指向 fnpack.json 等）：
		// 以其所在目录为基准。
		base = strings.TrimSuffix(sourceBase, path.Base(sourceBase))
		if base == sourceBase || !strings.HasSuffix(base, "/") {
			base += "/"
		}
	}
	return []string{
		base + appName + "/ICON.PNG",
		base + appName + "/ICON_256.PNG",
		base + appName + "/icon.png",
		base + "ICON.PNG",
		base + "ICON_256.PNG",
	}
}

// resolveAppIcon 返回应用图标字节：优先应用声明的 icon_url，
// 为空则按源仓库布局探测候选路径（整体 25 秒预算）。
// localAppIcon 读取应用中心程序目录里的本地图标
// （/vol1/@appcenter/<app>/ui/images/，兼容 icon-256.png / icon_0_256.png 等命名）。
func (s *Server) localAppIcon(appName string) ([]byte, string, bool) {
	if s.appCenterDir == "" {
		return nil, "", false
	}
	dir := filepath.Join(s.appCenterDir, appName, "ui", "images")
	for _, f := range []string{
		"icon-256.png", "icon_256.png", "icon_0_256.png", "icon_256.PNG",
		"icon.png", "icon_0.png", "icon-64.png", "icon_64.png",
	} {
		b, err := os.ReadFile(filepath.Join(dir, f))
		if err != nil || len(b) < 64 {
			continue
		}
		if looksLikeImage(b) {
			return b, assetContentTypeFor(f), true
		}
	}
	return nil, "", false
}

func looksLikeImage(b []byte) bool {
	if len(b) >= 8 {
		if b[0] == 0x89 && b[1] == 'P' && b[2] == 'N' && b[3] == 'G' {
			return true
		}
		if b[0] == 'R' && b[1] == 'I' && b[2] == 'F' && b[3] == 'F' &&
			b[8] == 'W' && b[9] == 'E' && b[10] == 'B' && b[11] == 'P' {
			return true
		}
		if b[0] == 'G' && b[1] == 'I' && b[2] == 'F' && b[3] == '8' {
			return true
		}
	}
	return len(b) >= 3 && b[0] == 0xFF && b[1] == 0xD8 && b[2] == 0xFF
}

func (s *Server) resolveAppIcon(ctx context.Context, app core.AppInfo) ([]byte, string, error) {
	// 「fnOS应用中心」来源应用：直接读应用中心本地图标（最可靠）
	if app.Source == "fnOS应用中心" {
		if body, ct, ok := s.localAppIcon(app.AppName); ok {
			return body, ct, nil
		}
	}
	cacheKey := app.AppKey() + "/icon/"
	if app.IconURL != "" {
		target := normalizeGitHubURL(strings.TrimSpace(app.IconURL))
		key := cacheKey + target
		if e, ok := appAssetCache.get(key); ok {
			return e.body, e.contentType, nil
		}
		body, ct, err := s.fetchAsset(ctx, target)
		if err != nil {
			// 声明的图标拉不到时继续尝试按仓库布局探测。
			return s.probeIconCandidates(ctx, app, cacheKey)
		}
		appAssetCache.put(key, assetCacheEntry{body: body, contentType: ct, expiresAt: time.Now().Add(assetTTL)})
		return body, ct, nil
	}
	return s.probeIconCandidates(ctx, app, cacheKey)
}

func (s *Server) probeIconCandidates(ctx context.Context, app core.AppInfo, cacheKey string) ([]byte, string, error) {
	if iconProbeCache.isBlocked(app.AppKey()) {
		return nil, "", fmt.Errorf("无图标")
	}
	candidates := iconCandidates(app.AppName, s.sourceBaseURL(app.Source))
	if len(candidates) == 0 {
		return nil, "", fmt.Errorf("无法推导图标路径")
	}
	pctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	var lastErr error
	for _, cand := range candidates {
		key := cacheKey + cand
		if e, ok := appAssetCache.get(key); ok {
			return e.body, e.contentType, nil
		}
		body, ct, err := s.fetchAsset(pctx, cand)
		if err != nil {
			lastErr = err
			continue
		}
		appAssetCache.put(key, assetCacheEntry{body: body, contentType: ct, expiresAt: time.Now().Add(1 * time.Hour)})
		return body, ct, nil
	}
	iconProbeCache.mark(app.AppKey())
	return nil, "", lastErr
}

// fetchAsset 抓取目标资源：GitHub raw 地址按镜像链回退，其余直连。
func (s *Server) fetchAsset(ctx context.Context, target string) ([]byte, string, error) {
	u, err := url.Parse(target)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, "", fmt.Errorf("无效的资源地址")
	}

	var candidates []string
	if strings.Contains(u.Host, "githubusercontent.com") {
		cfg := config.Config{}
		if s.configMgr != nil {
			cfg = s.configMgr.Get()
		}
		for _, prefix := range config.GitHubFallbackPrefixes(cfg.Mirror, cfg) {
			candidates = append(candidates, prefix+target)
		}
	}
	if len(candidates) == 0 {
		candidates = []string{target}
	}

	client := &http.Client{Timeout: 30 * time.Second}
	var lastErr error
	for _, cand := range candidates {
		cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		req, err := http.NewRequestWithContext(cctx, "GET", cand, nil)
		if err != nil {
			cancel()
			lastErr = err
			continue
		}
		req.Header.Set("User-Agent", "fnos-store/1.x")
		resp, err := client.Do(req)
		if err != nil {
			cancel()
			lastErr = err
			continue
		}
		body, rerr := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
		resp.Body.Close()
		cancel()
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("HTTP %s", resp.Status)
			continue
		}
		if rerr != nil {
			lastErr = rerr
			continue
		}
		if len(body) == 0 {
			lastErr = fmt.Errorf("资源为空")
			continue
		}
		ct := resp.Header.Get("Content-Type")
		if ct == "" || strings.HasPrefix(ct, "text/html") {
			ct = assetContentTypeFor(u.Path)
		}
		return body, ct, nil
	}
	return nil, "", fmt.Errorf("所有候选均失败: %w", lastErr)
}
