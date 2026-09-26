package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"fnos-store/internal/config"
	"fnos-store/internal/core"
	"fnos-store/internal/source"
)

func (s *Server) handleListApps(w http.ResponseWriter, r *http.Request) {
	apps := s.listRegistryApps()
	cfg := s.configMgr.Get()
	respApps := make([]appResponse, 0, len(apps))
	for _, app := range apps {
		if s.storeApp != "" && app.AppName == s.storeApp {
			continue
		}
		status := ""
		if app.Installed {
			status = s.getRuntimeStatus(app.AppName)
			if status == "" {
				status = "stopped"
			}
		}

		releaseURL := ""
		if app.ReleaseTag != "" {
			releaseURL = fmt.Sprintf("https://github.com/conversun/fnos-apps/releases/tag/%s", app.ReleaseTag)
		} else if app.DownloadURL != "" {
			// 外部 FnDepot 源应用：无 GitHub ReleaseTag，release_url 指向实际下载地址
			releaseURL = app.DownloadURL
		}

		hasUpdate := app.Status == core.AppStatusUpdateAvailable
		updateIgnored := false
		if hasUpdate && cfg.IsAppIgnored(app.AppName) {
			hasUpdate = false
			updateIgnored = true
		}

		availableVersion := ""
		if (hasUpdate || updateIgnored) && app.FpkVersion != "" {
			availableVersion = app.FpkVersion
		}

		// daemon 能力位（未知时保持 nil，前端按宽松默认处理，兼容 CLI 回退场景）
		var startStop, uninstallable *bool
		var webProtocol, webURL, webPath, webServiceName string
		var webPort int
		var webOnWebUI bool
		if app.Installed {
			if ctrl, known := s.getRuntimeControl(app.AppName); known {
				sv, uv := ctrl.IsStartStop, ctrl.IsUninstall
				startStop, uninstallable = &sv, &uv
			}
			// 打开按钮的目标（与 fnOS 应用中心 appServiceInfo 同源）
			if web, ok := s.getRuntimeWeb(app.AppName); ok {
				webProtocol = web.Protocol
				webPath = web.Path
				webServiceName = web.ServiceName
				if p, err := strconv.Atoi(web.Port); err == nil {
					webPort = p
				}
				if web.Host != "" {
					webURL = fmt.Sprintf("%s://%s:%s%s", web.Protocol, web.Host, web.Port, web.Path)
				}
				if web.HasWebUIPath() {
					webOnWebUI = true
				}
			}
		}

		respApps = append(respApps, appResponse{
			Key:                 app.AppKey(),
			AppName:             app.AppName,
			DisplayName:         app.DisplayName,
			Description:         app.Description,
			Installed:           app.Installed,
			InstalledVersion:    app.InstalledVersion,
			LatestVersion:       app.LatestVersion,
			InstalledFpkVersion: app.InstalledFpkVersion,
			AvailableVersion:    availableVersion,
			HasUpdate:           hasUpdate,
			UpdateIgnored:       updateIgnored,
			Platform:            app.Platform,
			ReleaseURL:          releaseURL,
			ReleaseNotes:        "",
			Status:              status,
			StartStop:           startStop,
			Uninstallable:       uninstallable,
			WebProtocol:         webProtocol,
			WebURL:              webURL,
			WebPort:             webPort,
			WebPath:             webPath,
			WebOnWebUI:          webOnWebUI,
			WebServiceName:      webServiceName,
			ServicePort:         app.ServicePort,
			Homepage:            app.HomepageURL,
			IconURL:             app.IconURL,
			UpdatedAt:           app.UpdatedAt,
			DownloadCount:       app.DownloadCount,
			LocalInstalls:       cfg.LocalInstalls[app.AppName],
			AppType:             app.AppType,
			Category:            app.Category,
			PostInstallNote:     app.PostInstallNote,
			Source:              app.Source,
			Maintainer:          app.Maintainer,
			MaintainerURL:       app.MaintainerURL,
			Distributor:         app.Distributor,
			DistributorURL:      app.DistributorURL,
			Changelog:           app.Changelog,
			SizeBytes:           app.SizeBytes,
			SHA256:              app.SHA256,
			PreviewCount:        len(app.PreviewURLs),
			HasReadme:           app.ReadmeURL != "",
		})
	}

	// 同名应用（内置目录与多个外部源重复收录）折叠为一张卡：
	// 后端操作全部按裸 appname 走，注册表 Get() 的解析顺序是内置目录优先；
	// 重复卡片会让「已安装」计数虚高（同一应用被多个源各标记一次 installed）。
	respApps = dedupeAppsByAppName(respApps)

	upgradeCap := s.ac.UpgradeCapability()
	writeJSON(w, http.StatusOK, appsListResponse{
		UpgradeAllowed:       upgradeCap.Allowed,
		UpgradeBlockedReason: upgradeCap.Reason,
		Apps:                 respApps,
		LastCheck:            formatTimestamp(s.getLastCheck()),
	})
}

// sourceRank 给出同名应用折叠时的来源优先级，与 core.SourceRank（Registry.Get
// 的回退裁决）共用同一实现，保证「用户看到的卡片」和「后端路由的条目」一致。
func sourceRank(source string) int { return core.SourceRank(source) }

// dedupeAppsByAppName 把同一 appname 的多条目录条目折叠为一张卡，保留在列表
// 中的原位置。保留规则：已安装条目优先于未安装条目；安装状态相同时来源等级
// 更高者（sourceRank 更小）胜出。其余字段各源基本一致，不引入版本比较，
// 避免把目录去重变成版本仲裁。
func dedupeAppsByAppName(apps []appResponse) []appResponse {
	pos := make(map[string]int, len(apps))
	kept := make([]appResponse, 0, len(apps))
	for _, a := range apps {
		idx, seen := pos[a.AppName]
		if !seen {
			pos[a.AppName] = len(kept)
			kept = append(kept, a)
			continue
		}
		cur := kept[idx]
		if (a.Installed && !cur.Installed) ||
			(a.Installed == cur.Installed && sourceRank(a.Source) < sourceRank(cur.Source)) {
			kept[idx] = a
		}
	}
	return kept
}

func (s *Server) handleIgnoreUpdate(w http.ResponseWriter, r *http.Request) {
	appName := r.PathValue("appname")
	if appName == "" {
		writeAPIError(w, http.StatusBadRequest, "missing app name")
		return
	}

	cfg := s.configMgr.Get()
	if cfg.IsAppIgnored(appName) {
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
		return
	}

	cfg.IgnoredApps = append(cfg.IgnoredApps, appName)
	if err := s.configMgr.SaveConfig(cfg); err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleUnignoreUpdate(w http.ResponseWriter, r *http.Request) {
	appName := r.PathValue("appname")
	if appName == "" {
		writeAPIError(w, http.StatusBadRequest, "missing app name")
		return
	}

	cfg := s.configMgr.Get()
	filtered := make([]string, 0, len(cfg.IgnoredApps))
	for _, name := range cfg.IgnoredApps {
		if name != appName {
			filtered = append(filtered, name)
		}
	}
	cfg.IgnoredApps = filtered

	if err := s.configMgr.SaveConfig(cfg); err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleDownloadFpk(w http.ResponseWriter, r *http.Request) {
	appName := r.PathValue("appname")
	if appName == "" {
		writeAPIError(w, http.StatusBadRequest, "missing app name")
		return
	}

	apps := s.listRegistryApps()
	var found *core.AppInfo
	for i := range apps {
		if apps[i].AppName == appName {
			found = &apps[i]
			break
		}
	}
	if found == nil {
		writeAPIError(w, http.StatusNotFound, "app not found")
		return
	}
	if found.DownloadURL == "" {
		writeAPIError(w, http.StatusNotFound, "no download available")
		return
	}

	cfg := s.configMgr.Get()
	prefix := config.GitHubMirrorPrefix(cfg.Mirror, cfg)
	http.Redirect(w, r, prefix+found.DownloadURL, http.StatusFound)
}

func (s *Server) handleReloadApps(w http.ResponseWriter, r *http.Request) {
	stream, err := newSSEStream(w, r, "")
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	src, ok := s.source.(*source.FNOSAppsSource)
	if !ok {
		_ = stream.sendError("unsupported source type")
		return
	}

	onProgress := func(p source.FetchProgress) {
		var msg string
		switch p.Status {
		case "trying":
			msg = fmt.Sprintf("正在使用 %s 加速...", p.Mirror)
		case "failed":
			msg = fmt.Sprintf("%s 连接失败", p.Mirror)
		case "success":
			msg = fmt.Sprintf("通过 %s 加载成功", p.Mirror)
		}
		_ = stream.sendProgress(progressPayload{Step: p.Status, Message: msg})
	}

	remoteApps, fetchErr := src.FetchAppsWithProgress(r.Context(), onProgress)
	if fetchErr != nil {
		_ = stream.sendProgress(progressPayload{
			Step:    "error",
			Message: "所有加速节点均无法连接，请更换加速节点或检查网络",
		})
		return
	}

	localApps, _ := core.ScanInstalled(s.appsDir)
	var installedTags map[string]string
	if s.cacheStore != nil {
		installedTags = s.cacheStore.InstalledTags()
	}

	now := time.Now()
	s.mu.Lock()
	s.registry.Merge(localApps, remoteApps, installedTags)
	s.lastCheck = now
	s.mu.Unlock()

	if s.cacheStore != nil {
		s.cacheStore.SetLastCheckAt(now)
	}
	s.refreshRuntimeStatus()

	apps := s.listRegistryApps()
	_ = stream.sendProgress(progressPayload{
		Step:    "done",
		Message: fmt.Sprintf("加载完成，共 %d 款应用", len(apps)),
	})
}

// handleGetWizard returns an app's install-time form definition, so the UI can
// ask the same questions the native App Center does before installing.
//
// Apps declare this in their package (fnos/wizard/install). Most have none;
// those that do need a token, password or path would otherwise start up
// misconfigured because the store silently accepted defaults.
func (s *Server) handleGetWizard(w http.ResponseWriter, r *http.Request) {
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
	if app.Installed {
		writeAPIError(w, http.StatusBadRequest, "应用已安装，请使用更新功能")
		return
	}

	// Official-panel channel: the panel only answers install/info for a
	// package that has already been downloaded, so the probe runs a silent
	// pre-download (the real install reuses it from the panel's download
	// cache) and maps the panel's wizardContent into the same
	// {has_wizard, content} shape the FPK channel returns.
	if s.isPanelApp(app) {
		info, err := s.preparePanelInstallInfo(r.Context(), app)
		if err != nil {
			// Same fallback contract as the FPK path below: a lookup
			// failure must not block installing with defaults.
			writeJSON(w, http.StatusOK, map[string]any{
				"appname": appName, "has_wizard": false, "error": err.Error(),
			})
			return
		}
		hasWizard := info.WizardInfo.HasWizard && len(info.WizardInfo.WizardContent) > 0
		resp := map[string]any{"appname": appName, "has_wizard": hasWizard}
		if hasWizard {
			resp["content"] = json.RawMessage(info.WizardInfo.WizardContent)
		}
		writeJSON(w, http.StatusOK, resp)
		return
	}

	if app.DownloadURL == "" {
		writeAPIError(w, http.StatusNotFound, "no download available")
		return
	}

	wizard, err := s.pipeline.fetchWizard(r.Context(), app)
	if err != nil {
		// A wizard lookup failure must not block installing: fall back to
		// "no wizard" so the user can still install with defaults, exactly as
		// before this feature existed.
		writeJSON(w, http.StatusOK, map[string]any{
			"appname": appName, "has_wizard": false, "error": err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, wizard)
}
