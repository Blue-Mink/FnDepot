package api

import (
	"net/http"

	"github.com/ghpp/ghpp/internal/config"
)

// handleMonitor 读写自动监测间隔（1.3.0）。
//
//	GET /api/monitor  返回加速源与 Docker 上游的有效监测间隔
//	PUT /api/monitor  {"mirror_minutes": N, "docker_minutes": N}
//
// docker_minutes = 0 表示沿用加速源间隔（旧行为）；>0 时 Docker 上游
// 按自己的节拍探活，两个页面的设置互不影响。
func (s *Server) handleMonitor(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeOK(w, s.monitorState())

	case http.MethodPut, http.MethodPost:
		var body struct {
			MirrorMinutes *int `json:"mirror_minutes"`
			DockerMinutes *int `json:"docker_minutes"`
		}
		if err := decodeBody(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, "%v", err)
			return
		}
		err := s.app.Config().Update(func(c *config.Config) error {
			if body.MirrorMinutes != nil {
				v := *body.MirrorMinutes
				if v < 1 {
					v = 1
				}
				if v > 1440 {
					v = 1440
				}
				c.Auto.MirrorMonitorMinutes = v
			}
			if body.DockerMinutes != nil {
				v := *body.DockerMinutes
				if v < 0 {
					v = 0
				}
				if v > 1440 {
					v = 1440
				}
				c.Docker.MonitorMinutes = v
			}
			return nil
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "保存失败：%v", err)
			return
		}
		s.logf("info", "监测间隔已更新：加速源=%d 分钟，Docker=%d 分钟",
			s.app.CurrentConfig().Auto.MirrorMonitorMinutes, s.app.CurrentConfig().Docker.MonitorMinutes)
		writeOK(w, s.monitorState())

	default:
		writeError(w, http.StatusMethodNotAllowed, "不支持的方法")
	}
}

// monitorState 组装监测间隔快照。
func (s *Server) monitorState() map[string]any {
	cfg := s.app.CurrentConfig()
	eff := cfg.Docker.MonitorMinutes
	if eff <= 0 {
		eff = cfg.Auto.MirrorMonitorMinutes
	}
	out := map[string]any{
		"mirror_minutes":           cfg.Auto.MirrorMonitorMinutes,
		"docker_minutes":           cfg.Docker.MonitorMinutes,
		"docker_effective_minutes": eff,
		"docker_auto":              cfg.Docker.AutoMonitor,
	}
	if t := s.app.LastDockerProbe(); !t.IsZero() {
		out["last_docker_probe"] = t
	}
	return out
}
