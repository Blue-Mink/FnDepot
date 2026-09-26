package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"fnos-store/internal/config"
)

type mirrorOptionResponse struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

type volumeOptionResponse struct {
	Index      int    `json:"index"`
	Path       string `json:"path"`
	TotalBytes uint64 `json:"total_bytes"`
	FreeBytes  uint64 `json:"free_bytes"`
}

type settingsResponse struct {
	CheckIntervalHours  int                    `json:"check_interval_hours"`
	Mirror              string                 `json:"mirror"`
	MirrorOptions       []mirrorOptionResponse `json:"mirror_options"`
	DockerMirror        string                 `json:"docker_mirror"`
	DockerMirrorOptions []mirrorOptionResponse `json:"docker_mirror_options"`
	CustomGitHubMirror  string                 `json:"custom_github_mirror,omitempty"`
	CustomDockerMirror  string                 `json:"custom_docker_mirror,omitempty"`
	InstallVolume       int                    `json:"install_volume"`
	VolumeOptions       []volumeOptionResponse `json:"volume_options"`
	// 内置源列表自动同步
	SourceListURL      string `json:"source_list_url,omitempty"`
	SourceListDisabled bool   `json:"source_list_disabled"`
	// FPK 下载目录 + 应用源自动监测
	DownloadDir            string `json:"download_dir"`
	SourceAutoCareDisabled bool   `json:"source_auto_care_disabled"`
	// 官方应用中心直连（面板账号）；密码不回传，仅表示是否已设置
	PanelEnabled   bool   `json:"panel_enabled"`
	PanelUsername  string `json:"panel_username,omitempty"`
	PanelBaseURL   string `json:"panel_base_url,omitempty"`
	PanelHasPassword bool `json:"panel_has_password"`
}

type settingsRequest struct {
	CheckIntervalHours int    `json:"check_interval_hours"`
	Mirror             string `json:"mirror"`
	DockerMirror       string `json:"docker_mirror"`
	CustomGitHubMirror string `json:"custom_github_mirror"`
	CustomDockerMirror string `json:"custom_docker_mirror"`
	InstallVolume      int    `json:"install_volume"`
	// 内置源列表自动同步（空 URL = 用内置默认列表）
	SourceListURL      string `json:"source_list_url"`
	SourceListDisabled bool   `json:"source_list_disabled"`
	// FPK 下载目录（空 = 保持默认）+ 应用源自动监测开关
	DownloadDir            string `json:"download_dir"`
	SourceAutoCareDisabled bool   `json:"source_auto_care_disabled"`
	// 官方应用中心直连（密码空 = 保持原值；显式清空用 PanelClearPassword）
	PanelEnabled         bool   `json:"panel_enabled"`
	PanelUsername        string `json:"panel_username"`
	PanelPassword        string `json:"panel_password"`
	PanelBaseURL         string `json:"panel_base_url"`
	PanelClearPassword   bool   `json:"panel_clear_password"`
}

func githubMirrorOptionsResponse() []mirrorOptionResponse {
	mirrors := config.GitHubMirrorOptions()
	opts := make([]mirrorOptionResponse, len(mirrors))
	for i, m := range mirrors {
		opts[i] = mirrorOptionResponse{Key: m.Key, Label: m.Label, Description: m.Description}
	}
	return opts
}

func dockerMirrorOptionsResponse() []mirrorOptionResponse {
	mirrors := config.DockerMirrorOptions()
	opts := make([]mirrorOptionResponse, len(mirrors))
	for i, m := range mirrors {
		opts[i] = mirrorOptionResponse{Key: m.Key, Label: m.Label, Description: m.Description}
	}
	return opts
}

func (s *Server) handleGetSettings(w http.ResponseWriter, _ *http.Request) {
	if s.configMgr == nil {
		writeAPIError(w, http.StatusInternalServerError, "config not available")
		return
	}

	cfg := s.configMgr.Get()

	var volOpts []volumeOptionResponse
	if volumes, err := s.ac.ListVolumes(); err == nil {
		volOpts = make([]volumeOptionResponse, len(volumes))
		for i, v := range volumes {
			volOpts[i] = volumeOptionResponse{Index: v.Index, Path: v.Path, TotalBytes: v.TotalBytes, FreeBytes: v.FreeBytes}
		}
	}

	writeJSON(w, http.StatusOK, settingsResponse{
		CheckIntervalHours:  cfg.CheckIntervalHours,
		Mirror:              cfg.Mirror,
		MirrorOptions:       githubMirrorOptionsResponse(),
		DockerMirror:        cfg.DockerMirror,
		DockerMirrorOptions: dockerMirrorOptionsResponse(),
		CustomGitHubMirror:  cfg.CustomGitHubMirror,
		CustomDockerMirror:  cfg.CustomDockerMirror,
		InstallVolume:       cfg.InstallVolume,
		VolumeOptions:       volOpts,
		SourceListURL:       cfg.SourceListURL,
		SourceListDisabled:  cfg.SourceListDisabled,
		DownloadDir:         s.pipeline.downloads.DownloadDir(),
		SourceAutoCareDisabled: cfg.SourceAutoCareDisabled,
		PanelEnabled:        cfg.PanelEnabled,
		PanelUsername:       cfg.PanelUsername,
		PanelBaseURL:        cfg.PanelBaseURL,
		PanelHasPassword:    cfg.PanelPassword != "",
	})
}

func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	if s.configMgr == nil {
		writeAPIError(w, http.StatusInternalServerError, "config not available")
		return
	}

	var req settingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid json")
		return
	}

	if req.CheckIntervalHours < 1 {
		req.CheckIntervalHours = config.DefaultCheckIntervalHours
	}

	if req.Mirror == "" {
		req.Mirror = config.DefaultMirror
	}
	if req.DockerMirror == "" {
		req.DockerMirror = config.DefaultDockerMirror
	}

	existing := s.configMgr.Get()
	panelPassword := existing.PanelPassword
	switch {
	case req.PanelClearPassword:
		panelPassword = ""
	case req.PanelPassword != "":
		panelPassword = req.PanelPassword
	}
	cfg := config.Config{
		CheckIntervalHours: req.CheckIntervalHours,
		Mirror:             req.Mirror,
		DockerMirror:       req.DockerMirror,
		CustomGitHubMirror: req.CustomGitHubMirror,
		CustomDockerMirror: req.CustomDockerMirror,
		InstallVolume:      req.InstallVolume,
		IgnoredApps:        existing.IgnoredApps,
		LocalInstalls:      existing.LocalInstalls, // 设置保存不能丢本机安装计数
		Sources:            existing.Sources, // 外部应用源由 /api/sources 管理，这里保持不动
		SourceListURL:      strings.TrimSpace(req.SourceListURL),
		SourceListDisabled: req.SourceListDisabled,
		DownloadDir:        strings.TrimSpace(req.DownloadDir),
		SourceAutoCareDisabled: req.SourceAutoCareDisabled,
		PanelEnabled:       req.PanelEnabled,
		PanelUsername:      strings.TrimSpace(req.PanelUsername),
		PanelPassword:      panelPassword,
		PanelBaseURL:       strings.TrimSpace(req.PanelBaseURL),
	}

	if err := s.configMgr.SaveConfig(cfg); err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// FPK 下载目录变更即时生效（下载器运行时切换）
	s.pipeline.downloads.SetDownloadDir(cfg.DownloadDir)

	s.rebuildPanelClient()
	if s.scheduler != nil {
		s.scheduler.SetInterval(time.Duration(req.CheckIntervalHours) * time.Hour)
	}
	// 官方源开关/账号变化会直接改变目录内容，后台刷新注册表。
	go s.refreshRegistryDebounced(context.Background())

	var volOpts []volumeOptionResponse
	if volumes, err := s.ac.ListVolumes(); err == nil {
		volOpts = make([]volumeOptionResponse, len(volumes))
		for i, v := range volumes {
			volOpts[i] = volumeOptionResponse{Index: v.Index, Path: v.Path, TotalBytes: v.TotalBytes, FreeBytes: v.FreeBytes}
		}
	}

	writeJSON(w, http.StatusOK, settingsResponse{
		CheckIntervalHours:  req.CheckIntervalHours,
		Mirror:              req.Mirror,
		MirrorOptions:       githubMirrorOptionsResponse(),
		DockerMirror:        req.DockerMirror,
		DockerMirrorOptions: dockerMirrorOptionsResponse(),
		CustomGitHubMirror:  req.CustomGitHubMirror,
		CustomDockerMirror:  req.CustomDockerMirror,
		InstallVolume:       req.InstallVolume,
		VolumeOptions:       volOpts,
		SourceListURL:       cfg.SourceListURL,
		SourceListDisabled:  cfg.SourceListDisabled,
		DownloadDir:         s.pipeline.downloads.DownloadDir(),
		SourceAutoCareDisabled: cfg.SourceAutoCareDisabled,
		PanelEnabled:        cfg.PanelEnabled,
		PanelUsername:       cfg.PanelUsername,
		PanelBaseURL:        cfg.PanelBaseURL,
		PanelHasPassword:    cfg.PanelPassword != "",
	})
}
