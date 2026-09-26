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

// handleDownloadTask —— 「下载 fpk」按钮（POST SSE）。
//
// 流程：
//  1. 按镜像链把 FPK 下到商店本地缓存（复用安装下载器，带进度）
//  2. 官方应用中心已配置时：把文件登记进面板官方下载系统
//     （与面板应用中心安装本地包同一通道，文件进入面板下载缓存）
//  3. done 事件（message 说明是否已交给面板通道）
//
// 面板「下载中心」（appcgi.downloadcenter.*）是封闭通道，不接受第三方
// 进程（见 panel.FileDownloadTask 注释），此路径是官方可用的途径。
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

	stream, err := newSSEStream(w, r, "")
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	cfg := s.configMgr.Get()
	downloadURLs := make([]string, 0, 4)
	if isGitHubDownloadURL(app.DownloadURL) {
		for _, prefix := range config.GitHubFallbackPrefixes(cfg.Mirror, cfg) {
			if prefix != "" {
				downloadURLs = append(downloadURLs, prefix+app.DownloadURL)
			} else {
				downloadURLs = append(downloadURLs, app.DownloadURL)
			}
		}
	} else {
		downloadURLs = append(downloadURLs, app.DownloadURL)
	}
	fileName := path.Base(app.DownloadURL)

	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Minute)
	defer cancel()

	_ = stream.sendProgress(progressPayload{Step: "downloading", Message: "正在下载 FPK..."})
	var lastSend time.Time
	localPath, err := s.pipeline.downloads.Download(ctx, core.DownloadRequest{
		URLs:     downloadURLs,
		FileName: fileName,
		AppName:  app.AppName,
	}, func(downloaded, total int64) {
		if total <= 0 || downloaded >= total {
			return
		}
		now := time.Now()
		if now.Sub(lastSend) < 300*time.Millisecond {
			return
		}
		lastSend = now
		_ = stream.sendProgress(progressPayload{
			Step:       "downloading",
			Message:    "正在下载 FPK...",
			Downloaded: downloaded,
			Total:      total,
		})
	})
	if err != nil {
		_ = stream.sendError("FPK 下载失败: " + err.Error())
		return
	}

	panelTaskID := ""
	if s.panelClient != nil && s.panelClient.Configured() {
		if id, perr := s.panelClient.FileDownloadTask(ctx, localPath); perr == nil {
			panelTaskID = id
		}
		// 登记失败不阻断：本地缓存仍然可用（安装时会直接复用）
	}

	var size int64
	if info, statErr := os.Stat(localPath); statErr == nil {
		size = info.Size()
	}
	msg := "FPK 已下载到本地缓存"
	if panelTaskID != "" {
		msg = "FPK 已交给面板官方下载通道（" + panelTaskID + "）"
	}
	_ = stream.sendProgress(progressPayload{
		Step:       "done",
		Message:    msg,
		Downloaded: size,
		Total:      size,
	})
}
