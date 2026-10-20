package api

import (
	"context"
	"net/http"
	"os/exec"
	"strings"
	"time"

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

// handleKSpeederService 启停本机 kspeeder 应用。
//
//	POST /api/kspeeder/service  {"action":"start"|"stop"|"restart"}
//
// 应用以 root 运行（run-as: root），可直接调用 appcenter-cli；
// 启停后清空探测缓存并回读最新状态，前端据此刷新徽章与按钮。
func (s *Server) handleKSpeederService(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "仅支持 POST")
		return
	}
	var body struct {
		Action string `json:"action"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, "%v", err)
		return
	}
	action := strings.ToLower(strings.TrimSpace(body.Action))
	switch action {
	case "start", "stop", "restart":
	default:
		writeError(w, http.StatusBadRequest, "不支持的动作：%s", body.Action)
		return
	}

	bin, err := exec.LookPath("appcenter-cli")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "未找到 appcenter-cli，无法控制 kspeeder 应用")
		return
	}

	steps := []string{action}
	if action == "restart" {
		steps = []string{"stop", "start"}
	}
	var output []string
	var failed string
	for _, act := range steps {
		ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
		cmd := exec.CommandContext(ctx, bin, act, "kspeeder")
		out, cerr := cmd.CombinedOutput()
		cancel()
		text := strings.TrimSpace(string(out))
		if text != "" {
			output = append(output, text)
		}
		if cerr != nil {
			// 10500 = 平台已经启动过（应用中心与实际状态同步），按成功处理。
			if strings.Contains(text, "10500") || strings.Contains(strings.ToLower(text), "already") {
				continue
			}
			failed = act + " 失败：" + cerr.Error()
			break
		}
	}

	// 无论成功与否都清缓存，让界面看到真实状态。
	s.app.KSpeederInvalidate()
	time.Sleep(300 * time.Millisecond)
	st := s.app.KSpeederStatus()

	if failed != "" {
		writeError(w, http.StatusInternalServerError, "%s（%s）", failed, strings.Join(output, " / "))
		return
	}
	writeOK(w, map[string]any{
		"action": action,
		"status": st,
		"output": strings.Join(output, "\n"),
	})
}
