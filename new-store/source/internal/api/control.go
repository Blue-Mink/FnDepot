package api

import "net/http"

// handleStartStop 同步 fnOS 应用中心的启动/停用：
// POST /api/apps/{appname}/start
// POST /api/apps/{appname}/stop
// 仅对已安装应用生效。执行走应用中心 daemon 的任务通道
//（start/check → start/task → 轮询确认，即官方应用中心同一套实现），
// 与安装/更新/卸载共用串行队列互斥，避免并发操作同一应用。
func (s *Server) handleStartStop(w http.ResponseWriter, r *http.Request, action string) {
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
		writeAPIError(w, http.StatusConflict, "该应用尚未安装")
		return
	}

	// daemon 能力位（未知时放行，保持旧行为）：nostart 系统组件与
	// 不支持启停的应用没有启停操作可做。
	if ctrl, known := s.getRuntimeControl(appname); known {
		if !ctrl.IsStartStop {
			writeAPIError(w, http.StatusConflict, "该应用不支持启动/停用（系统组件或应用自身声明不可控）")
			return
		}
	}

	if !s.queue.TryStart(action, appname) {
		writeAPIError(w, http.StatusConflict, "another operation is already running")
		return
	}
	defer s.queue.FinishApp(appname)

	verb, cliErr := "启动", s.queue.WithCLI(func() error { return s.ac.StartConfirmed(r.Context(), appname) })
	if action == "stop" {
		verb, cliErr = "停用", s.queue.WithCLI(func() error { return s.ac.StopConfirmed(r.Context(), appname) })
	}
	if cliErr != nil {
		writeAPIError(w, http.StatusInternalServerError, verb+"失败: "+cliErr.Error())
		return
	}

	// 任务已确认完成，立即刷新运行时状态缓存：否则前端随后的 /api/apps
	// 仍拿到旧状态，要等下一轮定时目录检查才变。refreshRuntimeStatus 走
	// cliMu，与本次操作已释放的锁不冲突（daemon GET 一次，毫秒级）。
	s.refreshRuntimeStatus()

	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "appname": appname})
}
