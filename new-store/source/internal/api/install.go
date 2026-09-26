package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"fnos-store/internal/core"
	"fnos-store/internal/platform"
)

func (s *Server) handleInstall(w http.ResponseWriter, r *http.Request) {
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
	// Reject an already-installed app. install-local treats this as an UPGRADE:
	// it would uninstall the existing copy before reinstalling, yet the "install"
	// operation name skips the update-only volume pin in runStandard(), so the
	// destructive step would run without the guard that protects existing data.
	// Callers that mean to upgrade must use /update, which pins the app's current
	// volume and fails closed when it cannot.
	if app.Installed {
		writeAPIError(w, http.StatusBadRequest, "应用已安装，请使用更新功能")
		return
	}

	// Wizard answers ride along as a query param so the SSE POST body stays
	// free; the browser sends them from the form rendered off /wizard.
	s.runInstallLikeOperation(w, r, "install", appname, app, parseWizardParams(r))
}

// parseWizardParams reads the user's install-wizard answers from the request.
// Absent or malformed input yields no params, which installs with defaults —
// the behavior before wizards were supported.
func parseWizardParams(r *http.Request) []platform.WizardParam {
	raw := r.URL.Query().Get("wizard")
	if raw == "" {
		return nil
	}
	var params []platform.WizardParam
	if err := json.Unmarshal([]byte(raw), &params); err != nil {
		return nil
	}
	return params
}

func (s *Server) runInstallLikeOperation(w http.ResponseWriter, r *http.Request, opName, appname string, app core.AppInfo, params []platform.WizardParam) {
	if !s.queue.TryStart(opName, appname) {
		writeAPIError(w, http.StatusConflict, "another operation is already running")
		return
	}

	// 官方应用中心通道：fnos-official 源应用走面板 cloud 下载 + install/task
	// （含依赖自动安装），与 FPK 下载通道互斥。保持请求作用域（需要请求里的
	// 面板参数/会话）。
	if s.isPanelApp(app) {
		defer s.queue.FinishApp(appname)
		stream, err := newSSEStream(w, r, appname)
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if opName == "update" {
			_ = stream.sendError("官方应用请在官方应用中心内更新（New Store 后续版本支持）")
			return
		}
		s.runPanelInstall(r.Context(), stream, opName, app, parsePanelParams(r), params)
		return
	}

	// 标准 FPK 安装/更新：解耦到服务端后台任务。客户端（退出应用）断开后
	// r.Context() 取消，但任务用自己的 detached context 继续跑；进度写入
	// Task（持久化），SSE 只是可选的实时视图。
	task := s.tasks.GetOrCreate(appname, opName)
	stream, err := newSSEStream(w, r, appname)
	if err != nil {
		// 即便 SSE 不可用，任务仍后台继续（客户端可轮询 GET /task）。
		go s.runOperation(task, opName, appname, app, params, nil)
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	go s.runOperation(task, opName, appname, app, params, stream)

	// 把任务进度转发给客户端，直到任务结束或客户端断开。
	s.streamTaskToClient(stream, task, r)
}

// runOperation 在后台 goroutine 里跑安装/更新管道（detached context）。
// 进度同时写入 task（持久化）与 liveSink（SSE 实时，可空）。
// 队列槽位在此释放（而非请求 handler），保证操作真正完成才放行下一个。
func (s *Server) runOperation(task *Task, opName, appname string, app core.AppInfo, params []platform.WizardParam, liveSink pipelineSink) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	ts := &taskSink{task: task}
	var sink pipelineSink = ts
	if liveSink != nil {
		sink = &teeSink{sinks: []pipelineSink{ts, liveSink}}
	}

	s.pipeline.runStandard(ctx, sink, opName, app, params, s.refreshRegistry)

	if ts.hadError() {
		// sendError 已把 task 标记为 failed
	} else {
		task.markDone()
	}
	s.tasks.Persist()
	s.queue.FinishApp(appname)
	s.refreshInstalledNames()
}

// streamTaskToClient 把任务进度转发到 SSE 客户端，直到任务结束或客户端断开。
// 客户端断开（退出应用）时静默返回——后台任务继续，进度已落到 Task。
func (s *Server) streamTaskToClient(stream *sseStream, task *Task, r *http.Request) {
	sub, unsub := task.subscribe()
	defer unsub()
	for {
		select {
		case p, ok := <-sub:
			if !ok {
				return
			}
			_ = stream.sendProgress(p)
		case <-task.done():
			// 任务结束：补发最终快照后收尾。
			_ = stream.sendProgress(task.snapshot())
			return
		case <-r.Context().Done():
			// 客户端断开；任务后台继续。
			return
		}
	}
}

// handleGetTask GET /api/apps/{appname}/task —— 返回该应用后台任务状态。
// 供 UI 轮询：退出应用后重开，可看到安装/更新是否还在后台跑、进度如何。
func (s *Server) handleGetTask(w http.ResponseWriter, r *http.Request) {
	appname := r.PathValue("appname")
	task := s.tasks.Get(appname)
	if task == nil {
		writeJSON(w, http.StatusOK, map[string]any{"active": false})
		return
	}
	t := task
	type taskResp struct {
		Active     bool       `json:"active"`
		Op         string     `json:"op"`
		Status     TaskStatus `json:"status"`
		Step       string     `json:"step,omitempty"`
		Progress   int        `json:"progress,omitempty"`
		Message    string     `json:"message,omitempty"`
		NewVersion string     `json:"new_version,omitempty"`
		Downloaded int64      `json:"downloaded,omitempty"`
		Total      int64      `json:"total,omitempty"`
		Speed      int64      `json:"speed,omitempty"`
		Error      string     `json:"error,omitempty"`
		StartedAt  time.Time  `json:"started_at"`
		UpdatedAt  time.Time  `json:"updated_at,omitempty"`
		FinishedAt time.Time  `json:"finished_at,omitempty"`
	}
	t.mu.Lock()
	resp := taskResp{
		// 已持有 t.mu：直接读 finished 字段，不能调 IsFinished()（内部再 Lock → 非重入锁自死锁）
		Active:     !t.finished,
		Op:         t.Op,
		Status:     t.Status,
		Step:       t.Step,
		Progress:   t.Progress,
		Message:    t.Message,
		NewVersion: t.NewVersion,
		Downloaded: t.Downloaded,
		Total:      t.Total,
		Speed:      t.Speed,
		Error:      t.Error,
		StartedAt:  t.StartedAt,
		UpdatedAt:  t.UpdatedAt,
		FinishedAt: t.FinishedAt,
	}
	t.mu.Unlock()
	writeJSON(w, http.StatusOK, resp)
}

// handleListTasks GET /api/tasks —— 返回「进行中」+「刚完成(20s 内)」的后台任务。
// 供 UI 全局通知轮询：退出应用重开能看到仍在后台跑的任务；刚完成的任务保留
// 20s，让顶部通知能闪过一条「完成/失败」提示（之后自动收起）。
func (s *Server) handleListTasks(w http.ResponseWriter, r *http.Request) {
	tasks := s.tasks.List()
	now := time.Now()
	type taskBrief struct {
		AppName    string     `json:"appname"`
		Op         string     `json:"op"`
		Status     TaskStatus `json:"status"`
		Step       string     `json:"step,omitempty"`
		Progress   int        `json:"progress,omitempty"`
		Message    string     `json:"message,omitempty"`
		NewVersion string     `json:"new_version,omitempty"`
		Downloaded int64      `json:"downloaded,omitempty"`
		Total      int64      `json:"total,omitempty"`
		Speed      int64      `json:"speed,omitempty"`
	}
	out := make([]taskBrief, 0)
	for _, t := range tasks {
		t.mu.Lock()
		finished := t.finished
		finishedAt := t.FinishedAt
		// 进行中任务必返；已完成任务仅在最近 20s 内返回（供完成提示）。
		if finished && now.Sub(finishedAt) > 20*time.Second {
			t.mu.Unlock()
			continue
		}
		out = append(out, taskBrief{
			AppName: t.AppName, Op: t.Op, Status: t.Status, Step: t.Step,
			Progress: t.Progress, Message: t.Message, NewVersion: t.NewVersion,
			Downloaded: t.Downloaded, Total: t.Total, Speed: t.Speed,
		})
		t.mu.Unlock()
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) runSelfUpdate(w http.ResponseWriter, r *http.Request, app core.AppInfo) {
	if !s.queue.TryStartExclusive("update", s.storeApp) {
		writeAPIError(w, http.StatusConflict, "another operation is already running")
		return
	}
	defer s.queue.FinishExclusive(s.storeApp)

	stream, err := newSSEStream(w, r, s.storeApp)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.pipeline.runSelfUpdate(r.Context(), stream, app)
}
