package api

import (
	"net/http"
	"path/filepath"

	"fnos-store/internal/core"
)

func (s *Server) handleGetStoreUpdate(w http.ResponseWriter, _ *http.Request) {
	if s.storeApp == "" {
		writeAPIError(w, http.StatusNotFound, "store app not configured")
		return
	}

	// 自更新必须跨源取版本最高条目：内置目录可能收录商店自己的旧版本
	// （conversun/fnos-apps 的 apps.json 有 fnos-apps-store 1.9.5），
	// 精确键命中会拿到旧条目，把 1.9.5 当「最新」甚至反向降级。
	app, ok := s.getRegistryBest(s.storeApp)
	if !ok {
		writeJSON(w, http.StatusOK, storeUpdateResponse{
			CurrentVersion: s.storeVersion(),
		})
		return
	}

	hasUpdate := app.Status == core.AppStatusUpdateAvailable
	available := ""
	if hasUpdate {
		available = app.FpkVersion
		if available == "" {
			available = app.LatestVersion
		}
	}

	writeJSON(w, http.StatusOK, storeUpdateResponse{
		CurrentVersion:   s.storeVersion(),
		AvailableVersion: available,
		HasUpdate:        hasUpdate,
	})
}

func (s *Server) handlePostStoreUpdate(w http.ResponseWriter, r *http.Request) {
	if s.storeApp == "" {
		writeAPIError(w, http.StatusNotFound, "store app not configured")
		return
	}

	app, ok := s.getRegistryBest(s.storeApp)
	if !ok {
		writeAPIError(w, http.StatusNotFound, "store app not found in registry")
		return
	}

	s.runSelfUpdate(w, r, app)
}

func (s *Server) storeVersion() string {
	app, ok := s.getRegistryBest(s.storeApp)
	if ok && app.InstalledVersion != "" {
		return app.InstalledVersion
	}

	// Fallback: read version directly from local manifest
	if s.appsDir != "" && s.storeApp != "" {
		manifestPath := filepath.Join(s.appsDir, s.storeApp, "manifest")
		if m, err := core.ParseManifest(manifestPath); err == nil {
			if m.Version != "" {
				return m.Version
			}
			if m.FpkVersion != "" {
				return m.FpkVersion
			}
		}
	}
	return "dev"
}
