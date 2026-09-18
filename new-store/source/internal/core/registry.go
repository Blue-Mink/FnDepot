package core

import (
	"fnos-store/internal/source"
	"sort"
	"strings"
	"time"
)

type AppStatus string

const (
	AppStatusNotInstalled      AppStatus = "not_installed"
	AppStatusInstalledUpToDate AppStatus = "installed_up_to_date"
	AppStatusUpdateAvailable   AppStatus = "update_available"
)

// AppKey 返回注册表内部键：外部源应用为 appname@源名（允许与内置目录同名共存），
// 内置目录（fnos-apps）保持裸 appname。
func (a AppInfo) AppKey() string {
	if a.Source != "" && a.Source != "fnos-apps" {
		return a.AppName + "@" + a.Source
	}
	return a.AppName
}

type AppInfo struct {
	AppName     string
	DisplayName string
	Description string
	HomepageURL string
	UpdatedAt   string
	ServicePort int
	Platform    string
	Source      string
	IconURL     string
	Installed   bool
	// InstalledVersion is the installed manifest's `version` field. It is the
	// UPSTREAM version string and is NOT comparable with LatestVersion: the two
	// come from different producers and routinely disagree (headscale ships
	// version=0.29.7 with fpk_version=0.29.3-r3 while the catalog says 0.29.3).
	// Only the FpkVersion pair below drives the update decision.
	InstalledVersion string
	LatestVersion    string
	ReleaseTag       string
	// FpkVersion is the CATALOG's package version; InstalledFpkVersion is the
	// installed package's. This pair is authoritative for both the update
	// decision and for display. InstalledFpkVersion is empty for packages built
	// before fpk_version existed, which fall back to the revision heuristic.
	FpkVersion          string
	InstalledFpkVersion string
	DownloadURL         string
	DownloadCount       int
	AppType             string
	Category            string
	Status              AppStatus
	HasRevisionUpdate   bool
	PostInstallNote     string

	// 外部源详情页扩展元数据（内置目录通常为空）。
	ReadmeURL      string
	PreviewURLs    []string
	Maintainer     string
	MaintainerURL  string
	Distributor    string
	DistributorURL string
	Changelog      string
	SizeBytes      int64
	SHA256         string
}

type Registry struct {
	apps       map[string]AppInfo
	updatedAt  time.Time
	lastResult []AppInfo
}

func NewRegistry() *Registry {
	return &Registry{
		apps: make(map[string]AppInfo),
	}
}

func (r *Registry) Merge(local []Manifest, remote []source.RemoteApp, installedTags map[string]string) []AppInfo {
	localByName := make(map[string]Manifest, len(local))
	for _, item := range local {
		localByName[item.AppName] = item
	}

	r.apps = make(map[string]AppInfo, len(remote))
	result := make([]AppInfo, 0, len(remote))
	// 外部源去重：同名且同版本的应用在不同源里经常是同一份包的转载，
	// 列表里只保留第一个（remote 顺序=内置目录在前，外部源按配置顺序）。
	// 不同版本仍分别展示（如内置 3.2.7 与外部 3.2.6 并存）。
	seenExtNameVersion := make(map[string]bool)
	for _, item := range remote {
		if item.Source != "" && item.Source != "fnos-apps" {
			dupKey := item.AppName + "|" + item.Version
			if seenExtNameVersion[dupKey] {
				continue
			}
			seenExtNameVersion[dupKey] = true
		}
		localManifest, installed := localByName[item.AppName]
		app := AppInfo{
			AppName:         item.AppName,
			DisplayName:     item.DisplayName,
			Description:     item.Description,
			HomepageURL:     item.HomepageURL,
			UpdatedAt:       item.UpdatedAt,
			ServicePort:     item.ServicePort,
			Platform:        strings.Join(item.Platforms, ","),
			Source:          item.Source,
			IconURL:         item.IconURL,
			Installed:       installed,
			LatestVersion:   item.Version,
			ReleaseTag:      item.ReleaseTag,
			FpkVersion:      item.FpkVersion,
			DownloadURL:     item.FpkURL,
			DownloadCount:   item.DownloadCount,
			AppType:         item.AppType,
			Category:        item.Category,
			Status:          AppStatusNotInstalled,
			PostInstallNote: item.PostInstallNote,
			ReadmeURL:           item.ReadmeURL,
			PreviewURLs:         item.PreviewURLs,
			Maintainer:          item.Maintainer,
			MaintainerURL:       item.MaintainerURL,
			Distributor:         item.Distributor,
			DistributorURL:      item.DistributorURL,
			Changelog:           item.Changelog,
			SizeBytes:           item.SizeBytes,
			SHA256:              item.SHA256,
		}

		// 无分类的应用（外部源/未标注目录）按项目类型自动归类
		if app.Category == "" {
			app.Category = InferCategory(app.DisplayName, app.AppName, app.Description)
		}

		if installed {
			app.InstalledVersion = localManifest.Version
			app.InstalledFpkVersion = localManifest.FpkVersion
			if app.ServicePort == 0 {
				app.ServicePort = localManifest.ServicePort
			}
			if app.Platform == "" {
				app.Platform = localManifest.Platform
			}

			// Use fpk_version comparison if both versions are available
			if localManifest.FpkVersion != "" && item.FpkVersion != "" {
				fpkCmp := CompareFpkVersions(localManifest.FpkVersion, item.FpkVersion)
				if fpkCmp < 0 {
					app.Status = AppStatusUpdateAvailable
				} else {
					app.Status = AppStatusInstalledUpToDate
				}
				app.HasRevisionUpdate = false
			} else {
				// Fallback to existing logic: version comparison + installedTags + revision check.
				// 已装版本不含任何非零数字段（"latest"/"dev" 等）时无法比较，
				// 保守视为最新，避免误报「有更新」。
				revisionUpdate := false
				if versionHasNumeric(localManifest.Version) {
					versionCmp := CompareVersions(localManifest.Version, item.Version)
					installedTag := installedTags[item.AppName]
					revisionUpdate = versionCmp == 0 && installedTag != item.ReleaseTag && hasRevisionUpdate(item.ReleaseTag, localManifest.Version)
					if versionCmp < 0 || revisionUpdate {
						app.Status = AppStatusUpdateAvailable
					} else {
						app.Status = AppStatusInstalledUpToDate
					}
				} else {
					app.Status = AppStatusInstalledUpToDate
				}
				app.HasRevisionUpdate = revisionUpdate
			}
		}

		r.apps[app.AppKey()] = app
		result = append(result, app)
	}

	// 内容级去重：跨源（含内置目录）同名（忽略大小写）+ 同 SHA256 = 同一份包，
	// 只保留元数据最全的一个；sha256 缺失的条目无法证明同一性，全部保留
	// （不同开发者/不同构建由用户按详情页的开发者与哈希自行区分）。
	result = dedupeIdenticalApps(result)

	sort.Slice(result, func(i, j int) bool {
		if result[i].UpdatedAt != result[j].UpdatedAt {
			return result[i].UpdatedAt > result[j].UpdatedAt
		}
		return result[i].DisplayName < result[j].DisplayName
	})

	r.updatedAt = time.Now()
	r.lastResult = result
	return result
}

// dedupePriority 决定同一份包保留哪个目录条目：元数据更完整者胜
// （有精确包大小 > 来源等级[内置目录 > 应用中心镜像 > 任意外部源] > 有下载量）。
func dedupePriority(app AppInfo) int {
	p := 0
	if app.SizeBytes > 0 {
		p += 4
	}
	switch app.Source {
	case "fnos-apps":
		p += 2
	case "fnOS应用中心":
		p += 1
	}
	if app.DownloadCount > 0 {
		p += 1
	}
	return p
}

// dedupeIdenticalApps 移除跨源重复收录的同一份包。
// 判定依据是 SHA256（内容同一性的硬证据）；同分保留先出现的条目。
func dedupeIdenticalApps(apps []AppInfo) []AppInfo {
	type groupKey struct{ name, sha string }
	winners := make(map[groupKey]int)
	for i, app := range apps {
		if app.SHA256 == "" {
			continue
		}
		gk := groupKey{strings.ToLower(app.AppName), app.SHA256}
		if cur, ok := winners[gk]; !ok || dedupePriority(app) > dedupePriority(apps[cur]) {
			winners[gk] = i
		}
	}
	drop := make(map[int]bool)
	for i, app := range apps {
		if app.SHA256 == "" {
			continue
		}
		gk := groupKey{strings.ToLower(app.AppName), app.SHA256}
		if winners[gk] != i {
			drop[i] = true
		}
	}
	if len(drop) == 0 {
		return apps
	}
	out := make([]AppInfo, 0, len(apps)-len(drop))
	for i, app := range apps {
		if !drop[i] {
			out = append(out, app)
		}
	}
	return out
}

func (r *Registry) List() []AppInfo {
	out := make([]AppInfo, len(r.lastResult))
	copy(out, r.lastResult)
	return out
}

func (r *Registry) Get(appname string) (AppInfo, bool) {
	// 优先按内部键（appname 或 appname@源名）精确命中；
	// 回退按裸 appname 扫描（内置目录优先），兼容只传 appname 的旧调用。
	if app, ok := r.apps[appname]; ok {
		return app, ok
	}
	var fallback AppInfo
	hasFallback := false
	for _, app := range r.apps {
		if app.AppName != appname {
			continue
		}
		if !hasFallback || app.Source == "fnos-apps" {
			fallback, hasFallback = app, true
			if app.Source == "fnos-apps" {
				break
			}
		}
	}
	return fallback, hasFallback
}

// GetBest 返回同名（AppName 精确匹配）条目中版本最高的一个。
// 自更新专用：内置目录可能收录商店自己的旧版本（conversun/fnos-apps 的
// apps.json 里有 fnos-apps-store 1.9.5），Get 的精确键命中会优先拿到内置
// 旧条目，直接用于自更新会把商店降级。这里跨所有源取版本最高者；
// 版本平局时优先外部源（内置目录只维护到上游停止发布的版本）。
func (r *Registry) GetBest(appname string) (AppInfo, bool) {
	var best AppInfo
	hasBest := false
	for _, app := range r.apps {
		if app.AppName != appname {
			continue
		}
		if !hasBest {
			best, hasBest = app, true
			continue
		}
		// cmp>0: app 比 best 新 → 取 app；平局: 外部源优先于内置目录。
		cmp := CompareFpkVersions(bestVersionStr(app), bestVersionStr(best))
		if cmp > 0 || (cmp == 0 && best.Source == "fnos-apps" &&
			app.Source != "" && app.Source != "fnos-apps") {
			best = app
		}
	}
	return best, hasBest
}

// bestVersionStr 返回条目的可比版本号（FpkVersion 优先，回退 LatestVersion）。
func bestVersionStr(app AppInfo) string {
	if app.FpkVersion != "" {
		return app.FpkVersion
	}
	return app.LatestVersion
}

func hasRevisionUpdate(releaseTag, installedVersion string) bool {
	prefix, ok := releaseTagPrefix(releaseTag)
	if !ok {
		return false
	}
	expectedTag := prefix + "/v" + installedVersion
	return releaseTag != expectedTag
}

func releaseTagPrefix(releaseTag string) (string, bool) {
	idx := strings.Index(releaseTag, "/v")
	if idx <= 0 {
		return "", false
	}
	return releaseTag[:idx], true
}

// HasExternalApps 报告注册表是否含外部源应用（排除内置目录与应用中心本地项）。
// 调用方必须持有与 Merge 相同的锁。
func (r *Registry) HasExternalApps() bool {
	for _, app := range r.apps {
		if app.Source != "" && app.Source != "fnos-apps" && app.Source != "fnOS应用中心" {
			return true
		}
	}
	return false
}

// LocalApp 是应用中心 daemon 上报的已安装应用（不限于 conversun 发行）。
type LocalApp struct {
	AppName     string
	DisplayName string
	Version     string
	Status      string // "running" / "stopped" / "nostart"
}

// AddLocalApps 把「应用中心已安装、但任何目录源都没有收录」的应用并入列表，
// 源标记为「fnOS应用中心」。目录条目（任意源的同名应用）优先，不重复添加。
// 每次 Merge 后由 refreshRuntimeStatus 重新调用，保证与 daemon 列表同步。
// 调用方必须持有与 Merge 相同的锁。
func (r *Registry) AddLocalApps(local []LocalApp) {
	for _, la := range local {
		if la.AppName == "" {
			continue
		}
		exists := false
		for _, app := range r.apps {
			if app.AppName == la.AppName {
				exists = true
				break
			}
		}
		if exists {
			continue
		}
		display := la.DisplayName
		if display == "" {
			display = la.AppName
		}
		app := AppInfo{
			AppName:          la.AppName,
			DisplayName:      display,
			Source:           "fnOS应用中心",
			Installed:        true,
			InstalledVersion: la.Version,
			LatestVersion:    la.Version,
			Status:           AppStatusInstalledUpToDate,
			Category:         InferCategory(display, la.AppName, ""),
		}
		r.apps[app.AppKey()] = app
		r.lastResult = append(r.lastResult, app)
	}
}

// ReconcileInstalled folds daemon-reported installed apps into the registry.
//
// The /var/apps manifest scan is the primary source of installed state, but
// the app-center daemon is authoritative for WHETHER an app is installed.
// When the daemon knows an app the scan missed, the store must not offer
// 安装 on it — the daemon would reject the install with
// "已安装，请使用更新功能" while the update tab shows nothing, dead-ending
// the user (conversun/fnos-apps#280 daidai-panel, #281 mihomo).
//
// Daemon-discovered apps show the daemon's version string and are treated as
// up to date: there is no local manifest to compare against, and a wrong
// "update available" badge is worse than none. Callers must hold the same
// lock they hold for Merge.
func (r *Registry) ReconcileInstalled(daemon map[string]string) {
	for i := range r.lastResult {
		if r.lastResult[i].Installed {
			continue
		}
		ver, known := daemon[r.lastResult[i].AppName]
		if !known {
			continue
		}
		r.lastResult[i].Installed = true
		r.lastResult[i].InstalledVersion = ver
		r.lastResult[i].Status = AppStatusInstalledUpToDate
		r.lastResult[i].HasRevisionUpdate = false

		// 注意：外部源条目的 map key 是 appname@源名，必须用 AppKey() 回写，
		// 否则详情页/安装/启停判断（走 r.apps）看不到已安装状态。
		if app, ok := r.apps[r.lastResult[i].AppKey()]; ok {
			app.Installed = true
			app.InstalledVersion = ver
			app.Status = AppStatusInstalledUpToDate
			app.HasRevisionUpdate = false
			r.apps[r.lastResult[i].AppKey()] = app
		}
	}
}
