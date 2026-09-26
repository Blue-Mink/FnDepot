package api

import (
	"context"
	"net/http"
	"os"
	"path"
	"time"

	"fnos-store/internal/config"
	"fnos-store/internal/core"
	"fnos-store/internal/source"
)

// 「下载 fpk」：后台任务 + 可暂停/继续。
//
// 设计（参考官方应用中心 fndepot 的下载方式）：
//   - 下载解耦到服务端后台 goroutine（detached context）——客户端退出应用后继续跑。
//   - 进度写入 Task（持久化）：SSE 实时视图 / GET /task 轮询 / 磁盘 三通道可读。
//   - 可暂停（task/pause）：取消在途请求、.part 保留、任务转 paused（非终态）。
//   - 可继续（再点下载 fpk）：从 .part Range 续传，任务转 running。
//   - 队列按 app 串行：下载与安装/更新互斥，暂停时释放槽位（可安装）。
//
// 官方应用中心已配置时，下载完成后把文件登记进面板官方下载系统
// （与面板应用中心安装本地包同一通道，文件进入面板下载缓存）。
// 面板「下载中心」（appcgi.downloadcenter.*）是封闭通道，不接受第三方进程。

// buildDownloadURLs 按镜像链构造 FPK 下载 URL 列表（GitHub 源走加速链）。
func buildDownloadURLs(app core.AppInfo, cfg config.Config) []string {
	urls := make([]string, 0, 4)
	if isGitHubDownloadURL(app.DownloadURL) {
		for _, prefix := range config.GitHubFallbackPrefixes(cfg.Mirror, cfg) {
			if prefix != "" {
				urls = append(urls, prefix+app.DownloadURL)
			} else {
				urls = append(urls, app.DownloadURL)
			}
		}
	} else {
		urls = append(urls, app.DownloadURL)
	}
	return urls
}

// handleDownloadTask —— 「下载 fpk」按钮（POST，SSE 可选实时视图）。
// 若该应用已有暂停的下载，则本次点击 = 继续（Range 续传）。
func (s *Server) handleDownloadTask(w http.ResponseWriter, r *http.Request) {
	appName := r.PathValue("appname")
	app, ok := s.getRegistryApp(appName)
	if !ok {
		writeAPIError(w, http.StatusNotFound, "app not found")
		return
	}
	if app.Source == source.OfficialSourceID {
		writeAPIError(w, http.StatusBadRequest, "官方应用没有可直链的 FPK（走面板通道安装）")
		return
	}
	if app.DownloadURL == "" {
		writeAPIError(w, http.StatusNotFound, "no download available")
		return
	}

	// 槽位检查：已有进行中的操作（下载/安装/更新）
	existing := s.tasks.Get(appName)
	resuming := false
	if existing != nil && !existing.IsFinished() {
		if existing.Op == "download" && existing.status() == TaskRunning {
			writeAPIError(w, http.StatusConflict, "下载已在进行中")
			return
		}
		if existing.Op != "download" {
			writeAPIError(w, http.StatusConflict, "该应用有进行中的操作："+string(existing.Op))
			return
		}
		// 暂停的下载 → 本次点击 = 继续
		resuming = true
	}

	if !s.queue.TryStart("download", appName) {
		writeAPIError(w, http.StatusConflict, "another operation is already running")
		return
	}

	stream, err := newSSEStream(w, r, appName)
	var liveSink pipelineSink
	if err == nil {
		liveSink = stream
	}

	task := s.tasks.GetOrCreate(appName, "download")
	if resuming && task.status() == TaskPaused {
		task.markRunning()
	}

	go s.runDownloadTask(task, appName, liveSink)

	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// 把任务进度转发给客户端，直到任务结束或客户端断开（断开后后台继续）。
	s.streamTaskToClient(stream, task, r)
}

// handlePauseDownload —— 「暂停」按钮（POST /api/apps/{appname}/task/pause）。
// 取消在途下载、.part 保留、任务转 paused，并释放队列槽位（可安装/更新）。
func (s *Server) handlePauseDownload(w http.ResponseWriter, r *http.Request) {
	appName := r.PathValue("appname")
	task := s.tasks.Get(appName)
	if task == nil || task.Op != "download" || task.status() != TaskRunning {
		writeAPIError(w, http.StatusNotFound, "没有可暂停的下载")
		return
	}
	if !task.pause() {
		writeAPIError(w, http.StatusConflict, "暂停失败")
		return
	}
	// 释放队列槽位：暂停后允许对该应用安装/更新。
	s.queue.FinishApp(appName)
	s.tasks.Persist()
	writeJSON(w, http.StatusOK, map[string]any{"status": "paused"})
}

// handleResumeDownload —— 「继续」按钮（POST /api/apps/{appname}/task/resume）。
// 从 .part Range 续传，立即返回（非 SSE）；进度走后台任务，可轮询。
func (s *Server) handleResumeDownload(w http.ResponseWriter, r *http.Request) {
	appName := r.PathValue("appname")
	task := s.tasks.Get(appName)
	if task == nil || task.Op != "download" || task.status() != TaskPaused {
		writeAPIError(w, http.StatusNotFound, "没有可继续的暂停下载")
		return
	}
	if !s.queue.TryStart("download", appName) {
		writeAPIError(w, http.StatusConflict, "another operation is already running")
		return
	}
	task.markRunning()
	go s.runDownloadTask(task, appName, nil)
	writeJSON(w, http.StatusOK, map[string]any{"status": "running"})
}

// runDownloadTask 在后台 goroutine 里跑 FPK 下载（detached context，可暂停）。
// 进度同时写入 task（持久化）与 liveSink（SSE 实时，可空）。
func (s *Server) runDownloadTask(task *Task, appname string, liveSink pipelineSink) {
	app, ok := s.getRegistryApp(appname)
	if !ok {
		task.fail("应用不存在: " + appname)
		s.queue.FinishApp(appname)
		s.tasks.Persist()
		return
	}
	if app.DownloadURL == "" {
		task.fail("无可直链的 FPK")
		s.queue.FinishApp(appname)
		s.tasks.Persist()
		return
	}

	cfg := s.configMgr.Get()
	downloadURLs := buildDownloadURLs(app, cfg)
	fileName := path.Base(app.DownloadURL)

	ctx, cancel := context.WithCancel(context.Background())
	task.setCancel(cancel)
	defer func() {
		cancel()
		task.clearCancel()
	}()

	ts := &taskSink{task: task}
	var sink pipelineSink = ts
	if liveSink != nil {
		sink = &teeSink{sinks: []pipelineSink{ts, liveSink}}
	}
	_ = sink.sendProgress(progressPayload{Step: "downloading", Message: "正在下载 FPK...", AppName: appname})

	var lastSend time.Time
	localPath, err := s.pipeline.downloads.Download(ctx, core.DownloadRequest{
		URLs:     downloadURLs,
		FileName: fileName,
		AppName:  appname,
	}, func(downloaded, total int64) {
		if total <= 0 || downloaded >= total {
			return
		}
		now := time.Now()
		if now.Sub(lastSend) < 300*time.Millisecond {
			return
		}
		lastSend = now
		_ = sink.sendProgress(progressPayload{
			Step: "downloading", Message: "正在下载 FPK...", AppName: appname,
			Downloaded: downloaded, Total: total,
		})
	})

	if err != nil {
		if task.status() == TaskPaused || ctx.Err() == context.Canceled {
			// 暂停：.part 保留（可继续），释放队列槽位。
			s.queue.FinishApp(appname)
			s.tasks.Persist()
			return
		}
		_ = sink.sendError("FPK 下载失败: " + err.Error())
		s.queue.FinishApp(appname)
		s.tasks.Persist()
		return
	}

	// 下载完成：登记面板官方下载系统（可选，失败不阻断）。
	panelTaskID := ""
	if s.panelClient != nil && s.panelClient.Configured() {
		if id, perr := s.panelClient.FileDownloadTask(context.Background(), localPath); perr == nil {
			panelTaskID = id
		}
	}
	var size int64
	if info, statErr := os.Stat(localPath); statErr == nil {
		size = info.Size()
	}
	msg := "FPK 已下载到本地缓存"
	if panelTaskID != "" {
		msg = "FPK 已交给面板官方下载通道（" + panelTaskID + "）"
	}
	_ = sink.sendProgress(progressPayload{
		Step: "done", Message: msg, AppName: appname, Progress: 100,
		Downloaded: size, Total: size,
	})
	task.markDone()
	s.queue.FinishApp(appname)
	s.tasks.Persist()
}
