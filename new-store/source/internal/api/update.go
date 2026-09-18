package api

import "net/http"

func (s *Server) handleUpdate(w http.ResponseWriter, r *http.Request) {
	appname := r.PathValue("appname")
	if appname == "" {
		writeAPIError(w, http.StatusBadRequest, "appname is required")
		return
	}

	app, ok := s.getRegistryApp(appname)
	if !ok {
		writeAPIError(w, http.StatusNotFound, "app not found")
		return
	}
	if !app.Installed {
		writeAPIError(w, http.StatusBadRequest, "app is not installed")
		return
	}

	if s.storeApp != "" && appname == s.storeApp {
		// 自更新跨源取版本最高条目，避免命中内置目录收录的商店旧版本
		// （conversun/fnos-apps apps.json 里的 fnos-apps-store 1.9.5）。
		if best, bok := s.getRegistryBest(s.storeApp); bok {
			app = best
		}
		s.runSelfUpdate(w, r, app)
		return
	}

	s.runInstallLikeOperation(w, r, "update", appname, app, nil)
}
