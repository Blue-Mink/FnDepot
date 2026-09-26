package source

import "context"

// RemoteApp represents an app available from a remote source.
type RemoteApp struct {
	AppName         string
	DisplayName     string
	Version         string
	Description     string
	HomepageURL     string
	UpdatedAt       string
	ReleaseTag      string
	FilePrefix      string
	FpkVersion      string
	ServicePort     int
	Platforms       []string
	FpkURL          string
	IconURL         string
	DownloadCount   int
	AppType         string
	Category        string
	Source          string
	PostInstallNote string
	// PanelSourceID 是面板应用中心目录里的数字 sourceID（仅官方源
	// fnos-official 使用）：cloud 下载任务 download/task 需要它定位包。
	PanelSourceID string

	// 外部源详情页扩展元数据（内置目录通常为空）。
	ReadmeURL      string   // README 地址（详情页 Markdown 渲染）
	PreviewURLs    []string // 预览图（详情页画廊）
	Maintainer     string   // 开发者/作者
	MaintainerURL  string
	Distributor    string   // 发布者（与开发者不同时展示）
	DistributorURL string
	Changelog      string   // 当前版本更新说明
	SizeBytes      int64    // 安装包字节数（源提供时）
	SHA256         string   // 安装包 sha256（源提供时）
}

// Source provides access to a remote app catalog.
type Source interface {
	// Name returns the identifier of this source (e.g., "fnos-apps").
	Name() string

	// FetchApps retrieves all available apps from this source.
	FetchApps(ctx context.Context) ([]RemoteApp, error)
}
