package api

import "fnos-store/internal/diagnostics"

type appResponse struct {
	// Key 是注册表内部键（外部源应用为 appname@源名），前端用它做唯一标识与资源定位；
	// 安装/更新/卸载仍用 appname（应用中心守护进程按 appname 管理）。
	Key              string `json:"key"`
	AppName          string `json:"appname"`
	DisplayName      string `json:"display_name"`
	Description      string `json:"description,omitempty"`
	Installed        bool   `json:"installed"`
	InstalledVersion string `json:"installed_version"`
	LatestVersion    string `json:"latest_version"`
	// InstalledFpkVersion / AvailableVersion are the package versions the update
	// decision is actually made on. InstalledVersion / LatestVersion are upstream
	// strings from two different producers and can disagree (headscale reports
	// installed 0.29.7 against catalog 0.29.3 while both are 0.29.3-rN packages),
	// so clients should prefer this pair whenever both sides are present.
	InstalledFpkVersion string `json:"installed_fpk_version,omitempty"`
	AvailableVersion    string `json:"available_version,omitempty"`
	HasUpdate           bool   `json:"has_update"`
	UpdateIgnored       bool   `json:"update_ignored,omitempty"`
	Platform            string `json:"platform"`
	ReleaseURL          string `json:"release_url"`
	ReleaseNotes        string `json:"release_notes"`
	// Status is the app-center daemon vocabulary: running / stopped / starting /
	// stopping / nostart (system components). Empty when not installed.
	Status string `json:"status"`
	// StartStop / Uninstallable mirror the daemon's per-app capability bits.
	// Omitted (nil) when the capability is unknown (CLI fallback), in which
	// case the UI must default to permissive — same as before these fields.
	StartStop     *bool `json:"start_stop,omitempty"`
	Uninstallable *bool `json:"uninstallable,omitempty"`
	// Web* describe the installed app's openable web entry (daemon
	// appServiceInfo) backing the "打开" button, like the native App Center.
	// WebHost is usually empty: the UI fills in the host the user reached the
	// store on. Absent when the app has no openable web UI.
	WebProtocol string `json:"web_protocol,omitempty"`
	WebURL      string `json:"web_url,omitempty"`
	WebPort     int    `json:"web_port,omitempty"`
	WebPath     string `json:"web_path,omitempty"`
	// WebOnWebUI marks apps whose web entry is served by the fnOS web UI
	// itself (port 5666): the UI builds <host>:5666+WebPath.
	WebOnWebUI bool `json:"web_on_webui,omitempty"`
	// WebServiceName is the daemon's appServiceInfo.serviceName
	// (e.g. "Gitea.Application"). Embedded in the fnOS web UI, "打开" opens
	// the app in-shell via the bridge openApp(serviceName) — exactly what the
	// native App Center does (He(appServiceInfo.serviceName)).
	WebServiceName string `json:"web_service_name,omitempty"`
	ServicePort         int    `json:"service_port,omitempty"`
	Homepage            string `json:"homepage,omitempty"`
	IconURL             string `json:"icon_url,omitempty"`
	UpdatedAt           string `json:"updated_at,omitempty"`
	DownloadCount       int    `json:"download_count"`
	// 本机安装/更新次数（第三方源应用无全局下载量时前端回退展示「本机 N 次」）
	LocalInstalls       int    `json:"local_installs,omitempty"`
	AppType             string `json:"app_type,omitempty"`
	Category            string `json:"category,omitempty"`
	PostInstallNote     string `json:"post_install_note,omitempty"`
	// Source 标记应用来自哪个目录源（内置 fnos-apps 或用户添加的 FnDepot 外部源）。
	Source              string `json:"source,omitempty"`

	// 外部源详情页扩展元数据（内置目录应用通常无这些字段）。
	Maintainer     string   `json:"maintainer,omitempty"`
	MaintainerURL  string   `json:"maintainer_url,omitempty"`
	Distributor    string   `json:"distributor,omitempty"`
	DistributorURL string   `json:"distributor_url,omitempty"`
	Changelog      string   `json:"changelog,omitempty"`
	SizeBytes      int64    `json:"size_bytes,omitempty"`
	SHA256         string   `json:"sha256,omitempty"`
	PreviewCount   int      `json:"preview_count,omitempty"`
	HasReadme      bool     `json:"has_readme,omitempty"`
}

type appsListResponse struct {
	// UpgradeAllowed is false on fnOS builds where an in-store update would
	// destroy the app (see platform.UpgradeCapability). The UI uses it to
	// point users at manual install UP FRONT, instead of offering an update
	// button that always fails.
	UpgradeAllowed       bool   `json:"upgrade_allowed"`
	UpgradeBlockedReason string `json:"upgrade_blocked_reason,omitempty"`

	Apps      []appResponse `json:"apps"`
	LastCheck string        `json:"last_check"`
}

type recommendedAppResponse struct {
	Name          string `json:"name"`
	DisplayName   string `json:"display_name"`
	Description   string `json:"description"`
	SourceURL     string `json:"source_url"`
	GitHubRepo    string `json:"github_repo,omitempty"`
	LatestVersion string `json:"latest_version,omitempty"`
	UpdatedAt     string `json:"updated_at,omitempty"`
}

type recommendedListResponse struct {
	Apps []recommendedAppResponse `json:"apps"`
}

type checkResponse struct {
	Status           string `json:"status"`
	Checked          int    `json:"checked"`
	UpdatesAvailable int    `json:"updates_available"`
	Warning          string `json:"warning,omitempty"`
}

type statusResponse struct {
	Status    string        `json:"status"`
	Busy      bool          `json:"busy"`
	Operation string        `json:"operation"`
	AppName   string        `json:"appname"`
	StartedAt string        `json:"started_at"`
	LastCheck string        `json:"last_check"`
	Platform  string        `json:"platform"`
	ActiveOps []QueueStatus `json:"active_operations,omitempty"`
}

type storeUpdateResponse struct {
	CurrentVersion   string `json:"current_version"`
	AvailableVersion string `json:"available_version,omitempty"`
	HasUpdate        bool   `json:"has_update"`
}

type appLogsResponse struct {
	AppName    string   `json:"appname"`
	LogLines   []string `json:"log_lines"`
	Source     string   `json:"source"`
	Containers []string `json:"containers,omitempty"`
}

type diagnosticResponse struct {
	Report   diagnostics.DiagnosticReport `json:"report"`
	IssueURL string                       `json:"issue_url"`
}

type errorResponse struct {
	Error string `json:"error"`
}
