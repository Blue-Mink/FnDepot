package api

import (
	"net/http"
)

// handleSysProxy 管理飞牛 NAS 系统级 HTTP 代理开关。
//
//	GET /api/sysproxy  查询当前是否已开启系统代理与本地 CA 的系统信任状态
//	PUT /api/sysproxy  开启或关闭系统代理（body: {"enabled": true|false, "install_ca": bool}）
//
// 开启后所有新 login shell 启动的软件默认走加速器，无需软件自身配置代理。
// 已运行的进程不受影响，需重启对应软件才生效——前端会给出明确提示。
//
// install_ca 仅在开启时生效：为 true（或不传，默认 true）时同时把本地 CA
// 装入系统信任，让 git/ssh 的 HTTPS 也走 MITM 全速加速；显式 false 表示
// 用户拒绝（ CONNECT 侧自动降级纯隧道兜底，流量可用不加速）。
func (s *Server) handleSysProxy(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeOK(w, map[string]any{
			// 实际状态以文件是否存在为准，避免配置与文件不一致时误报。
			"enabled":      s.app.Status().SystemProxyEnabled,
			"ca_available": s.app.CA() != nil,
			"ca_trusted":   s.app.CAInSystemTrust(),
		})
	case http.MethodPut, http.MethodPost:
		var body struct {
			Enabled   *bool `json:"enabled"`
			InstallCA *bool `json:"install_ca"`
		}
		if err := decodeBody(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, "%v", err)
			return
		}
		if body.Enabled == nil {
			writeError(w, http.StatusBadRequest, "缺少 enabled 字段")
			return
		}
		// 默认 true：快乐路径是一次点击全速加速；用户可在确认框显式拒绝。
		installCA := true
		if body.InstallCA != nil {
			installCA = *body.InstallCA
		}
		res, err := s.app.SetSystemProxy(*body.Enabled, installCA)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "%v", err)
			return
		}
		writeOK(w, res)
	default:
		writeError(w, http.StatusMethodNotAllowed, "不支持的方法")
	}
}
