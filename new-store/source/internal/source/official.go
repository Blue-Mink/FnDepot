package source

import (
	"context"
	"errors"
	"strings"

	"fnos-store/internal/panel"
)

// ErrPanelNotConfigured 表示面板账号未配置（官方源不可用）。
var ErrPanelNotConfigured = errors.New("官方应用中心未启用：请先在设置里配置面板账号")

// OfficialSourceID is the registry name of the built-in official source.
const OfficialSourceID = "fnos-official"

// OfficialSource wraps the panel app-center API (fnos-official 内置源).
// It implements Source so its catalog merges into the registry alongside the
// built-in fnos-apps directory and user-added FnDepot sources.
//
// Installability: entries carry PanelSourceID and no FpkURL; the install
// pipeline routes them through the panel's cloud download + install/task
// channel (see internal/api panel install path) instead of FPK download.
type OfficialSource struct {
	client *panel.Client
	name   string
}

// NewOfficialSource creates the official source. client may be unconfigured
// (no panel credentials) — FetchApps then returns an error and the source
// degrades to empty (the store keeps working with its other sources).
func NewOfficialSource(client *panel.Client) *OfficialSource {
	return &OfficialSource{client: client, name: OfficialSourceID}
}

// Name implements Source.
func (s *OfficialSource) Name() string { return s.name }

// Client exposes the underlying panel client (detail/dependency lookups).
func (s *OfficialSource) Client() *panel.Client { return s.client }

// FetchApps implements Source: the full official catalog in one request.
func (s *OfficialSource) FetchApps(ctx context.Context) ([]RemoteApp, error) {
	if s.client == nil || !s.client.Configured() {
		return nil, ErrPanelNotConfigured
	}
	apps, err := s.client.AppList(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]RemoteApp, 0, len(apps))
	for _, a := range apps {
		appType := "fpk"
		if a.Docker {
			appType = "docker"
		}
		category := ""
		if len(a.Tags) > 0 {
			category = a.Tags[0]
		}
		out = append(out, RemoteApp{
			AppName:       a.AppName,
			DisplayName:   a.Name,
			Version:       a.Version,
			IconURL:       a.Icon,
			DownloadCount: int(a.Download),
			AppType:       appType,
			Category:      category,
			Source:        OfficialSourceID,
			PanelSourceID: a.SourceID,
			Platforms:     []string{platformFromIcon(a.Icon)},
		})
	}
	return out, nil
}

// platformFromIcon 从官方 CDN 图标 URL 推断架构：
// icon-<name>-<ver>-<platform>-<ts>.png，platform ∈ x86/arm64/all；
// 无平台段（如 icon-nodejs_v22-22.18.0-1.png）按 x86 计。
func platformFromIcon(icon string) string {
	switch {
	case strings.Contains(icon, "-arm64-") || strings.Contains(icon, "-arm-"):
		return "arm"
	case strings.Contains(icon, "-all-"):
		return "all"
	default:
		return "x86"
	}
}
