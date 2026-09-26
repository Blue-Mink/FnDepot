package api

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"

	"fnos-store/internal/core"
)

// 外部源应用图标后台预热（参考 FNDepot：同步时批量落本地，页面零等待）。
//
// 此前外部应用图标是浏览器打开页面时才经 /asset?type=icon 代理现抓
// （首图 0.4s~2.5s，600+ 应用冷缓存时整面图标墙冷瀑布）。预热在启动后
// 与每 30 分钟把缺失图标按镜像链后台抓取进两级磁盘缓存，命中后 <1ms。
//
// 设计约束：
//   - 低优先级：6 并发、整轮限时（启动 10 分钟 / 常规 3 分钟），不与
//     前台请求抢带宽 CPU；单应用失败静默（详情页打开时仍走原代理路径）。
//   - 已缓存的应用 resolveAppIcon 查内存/磁盘即返回，整轮近似 no-op。
//   - 内置目录（fnos-apps）前端走 icon_url 直链、官方应用中心
//     （fnos-official）走应用中心本地/CDN 图标，均无需预热。
func (s *Server) startIconWarmLoop() {
	// 启动首轮：等目录就绪（注册表出现外部源应用）立即开跑，不再固定睡 30s——
	// 目录通常几秒内加载完，越早预热，用户首次打开页面时图标越热（秒开关键）。
	go func() {
		deadline := time.Now().Add(2 * time.Minute)
		for time.Now().Before(deadline) {
			if s.hasExternalCatalog() {
				break
			}
			time.Sleep(2 * time.Second)
		}
		s.warmIconsPass(10 * time.Minute)
	}()
	ticker := time.NewTicker(30 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		s.warmIconsPass(3 * time.Minute)
	}
}

// hasExternalCatalog 报告注册表是否已有外部源应用（目录加载完成的信号）。
func (s *Server) hasExternalCatalog() bool {
	apps := s.listRegistryApps()
	for _, app := range apps {
		if app.Source != "" && app.Source != "fnos-apps" && app.Source != "fnos-official" {
			return true
		}
	}
	return false
}

// warmIconsPass 预热一轮缺失图标。
func (s *Server) warmIconsPass(budget time.Duration) {
	apps := s.listRegistryApps()
	var pending []core.AppInfo
	for _, app := range apps {
		if app.Source == "" || app.Source == "fnos-apps" || app.Source == "fnos-official" {
			continue
		}
		if s.assets.hasIcon(app.AppKey()) {
			continue
		}
		pending = append(pending, app)
	}
	if len(pending) == 0 {
		return
	}
	log.Printf("icon warm: %d external apps pending (budget %s)", len(pending), budget)
	ctx, cancel := context.WithTimeout(context.Background(), budget)
	defer cancel()

	sem := make(chan struct{}, 6)
	var wg sync.WaitGroup
	for i := range pending {
		if ctx.Err() != nil {
			break
		}
		app := pending[i]
		sem <- struct{}{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			_, _, _ = s.resolveAppIcon(ctx, app)
		}()
	}
	wg.Wait()
}

// hasPrefix 报告内存缓存里是否存在该前缀的键（图标预热跳过用）。
func (c *assetCache) hasPrefix(prefix string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k := range c.entries {
		if strings.HasPrefix(k, prefix) {
			return true
		}
	}
	return false
}

// hasIcon 报告该应用（按注册表键）是否已有任一图标缓存条目（内存或磁盘层）。
func (st *appAssetStore) hasIcon(appKey string) bool {
	prefix := appKey + "/icon/"
	if st.mem.hasPrefix(prefix) {
		return true
	}
	if st.dir == "" {
		return false
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	for k := range st.idx {
		if strings.HasPrefix(k, prefix) {
			return true
		}
	}
	return false
}
