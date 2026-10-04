package api

import (
	"net/http"

	"github.com/ghpp/ghpp/internal/km"
)

// handleKSpeeder 返回 kspeeder 依赖应用的检测状态、本机接入地址与节点运行态。
//
// 1.2.0 起本应用不再托管引擎：kspeeder 由独立应用提供（默认 registry
// 5443 / 管理 5003），本接口只读探测并展示，未安装时附可选下载链接。
func (s *Server) handleKSpeeder(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "仅支持 GET", http.StatusMethodNotAllowed)
		return
	}

	st := s.app.KSpeederStatus()

	// kspeeder 应用运行时给出本机 Docker 接入地址，前端可直接展示复制。
	var dock string
	if st.Mode == km.ModeRunning {
		dock = st.URL
	}

	// 节点运行态（docker:dockerhub + docker:ghcr）：管理口不可用时 nodes
	// 为空、带 error 说明，前端据此显示"kspeeder 未运行"而不是空白。
	nodes, err := s.app.KSpeederNodes(r.Context())
	resp := map[string]any{
		"status":             st,
		"docker":             map[string]any{"registry": dock},
		"nodes":              []any{},
		"download_url":       s.app.CurrentConfig().KSpeeder.DownloadURL,
		"recommended_engine": km.RecommendedEngineMin,
	}
	if err != nil {
		resp["nodes_error"] = err.Error()
	} else {
		resp["nodes"] = nodes
	}

	writeJSON(w, http.StatusOK, resp)
}
