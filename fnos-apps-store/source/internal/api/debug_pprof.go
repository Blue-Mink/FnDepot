package api

import (
	"net/http"
	_ "net/http/pprof" // 注册标准 profile 处理器到 http.DefaultServeMux
)

// 真机诊断端点（/debug/pprof/*）。刷新卡住时：
//
//	curl -s "http://127.0.0.1:38011/debug/pprof/goroutine?debug=1"
//
// 30 秒 CPU profile：
//
//	curl -s -o cpu.prof "http://127.0.0.1:38011/debug/pprof/profile?seconds=30"
//
// 挂在默认 mux 上，获得全套标准端点（goroutine/heap/profile/threadcreate/...）。
func (s *Server) mountPprof() {
	s.Mux.Handle("/debug/pprof/", http.DefaultServeMux)
}
