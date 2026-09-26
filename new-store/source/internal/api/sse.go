package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

type progressPayload struct {
	Step       string `json:"step"`
	Progress   int    `json:"progress,omitempty"`
	Message    string `json:"message,omitempty"`
	NewVersion string `json:"new_version,omitempty"`
	AppName    string `json:"appname,omitempty"`
	Speed      int64  `json:"speed,omitempty"`
	Downloaded int64  `json:"downloaded,omitempty"`
	Total      int64  `json:"total,omitempty"`
}

// pipelineSink 是安装/更新管道上报进度的抽象。
//
// 为什么要抽象：原来管道直接写 *sseStream（绑在请求上），客户端一断开
// r.Context() 取消 → 下载/安装中止。抽象后管道可写 *taskSink（进度落到
// 后台任务、客户端断开也继续），SSE 只是可选的实时视图。
//
// 实现：
//   - *sseStream：实时流到已连接的客户端（传统行为）
//   - *taskSink：把进度写入后台任务（退出应用后继续 + 进度持久）
//   - *teeSink：同时转发到多个 sink（task + SSE）
type pipelineSink interface {
	sendProgress(progressPayload) error
	sendError(string) error
}

type sseStream struct {
	w       http.ResponseWriter
	r       *http.Request
	flusher http.Flusher
	appname string
}

func newSSEStream(w http.ResponseWriter, r *http.Request, appname string) (*sseStream, error) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, errors.New("streaming not supported")
	}

	return &sseStream{w: w, r: r, flusher: flusher, appname: appname}, nil
}

func (s *sseStream) sendProgress(payload progressPayload) error {
	if err := s.r.Context().Err(); err != nil {
		return err
	}

	payload.AppName = s.appname

	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	if _, err := fmt.Fprintf(s.w, "event: progress\ndata: %s\n\n", raw); err != nil {
		return err
	}

	s.flusher.Flush()
	return s.r.Context().Err()
}

func (s *sseStream) sendError(message string) error {
	return s.sendProgress(progressPayload{Step: "error", Message: message})
}
