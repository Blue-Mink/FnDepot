package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

const (
	DefaultCheckIntervalHours = 6
	DefaultDataDir            = "/var/apps/fnos-apps-store/var"
	DefaultMirror             = "gh-proxy"
	DefaultDockerMirror       = "daocloud"
	// DefaultSourceListURL 内置的社区 FnDepot 应用源列表（每行一个 GitHub 仓库地址）。
	// 同步时列表中未添加过的源会自动加入应用源。
	DefaultSourceListURL = "https://raw.githubusercontent.com/710850609/FnDepot/main/repo_list.txt"
)

type GitHubMirror struct {
	Key         string
	Label       string
	URL         string
	Description string
}

type DockerMirror struct {
	Key           string
	Label         string
	URL           string
	Description   string
	MultiRegistry bool // supports proxying multiple registries via path prefix (docker.io, ghcr.io, ...)
	// RegistryRewrites maps an upstream registry host to this mirror's
	// replacement base for that registry (e.g. "ghcr.io" -> "ghcr.nju.edu.cn/").
	// Unlike URL prefixing the registry host is REPLACED, not carried in the
	// path — the shape ghcr-only mirrors use. Empty for prefix-style mirrors.
	RegistryRewrites map[string]string
}

// gitHubMirrors 声明顺序 = 无健康数据时的静态回退顺序（按 2026-09-18 实测
// 延迟排序）。智能监测（internal/mirror）接入后，实际回退顺序由健康度决定。
// 注意：conversun hub 只代理 conversun 仓库，探测必须用 conversun 的 URL
// （见 api 层 probeURLFor），否则会误判为失败。
var gitHubMirrors = []GitHubMirror{
	{Key: "auto", Label: "智能 · 自动选最快", URL: "", Description: "系统周期性测速各源，自动选用最快且稳定的加速源"},
	{Key: "gh-proxy-hk", Label: "GH-Proxy HK", URL: "https://hk.gh-proxy.org/", Description: "HK 节点，实测最快（419ms）"},
	{Key: "cdn-ghproxy", Label: "CDN GHProxy", URL: "https://cdn.gh-proxy.org/", Description: "CDN 节点 GitHub 加速（450ms）"},
	{Key: "ghproxy-cxkpro", Label: "GHProxy CXK", URL: "https://ghproxy.cxkpro.top/", Description: "社区 GitHub 加速（实测 534ms）"},
	{Key: "yylx", Label: "YYLX Git", URL: "https://git.yylx.win/", Description: "社区 GitHub 加速（实测 535ms）"},
	{Key: "gh-proxy", Label: "GH-Proxy", URL: "https://gh-proxy.com/", Description: "公共 GitHub 文件代理，长期稳定运营（552ms）"},
	{Key: "gitproxy-mrhjx", Label: "GitProxy MRHJX", URL: "https://gitproxy.mrhjx.cn/", Description: "社区 GitHub 加速（实测 559ms）"},
	{Key: "gh-proxy-org", Label: "GH-Proxy.ORG", URL: "https://gh-proxy.org/", Description: "社区 GitHub 加速（实测 563ms）"},
	{Key: "wget-la", Label: "WGET.LA", URL: "https://wget.la/", Description: "社区 GitHub 下载加速（实测 606ms）"},
	{Key: "ghproxy-net", Label: "GHProxy.net", URL: "https://ghproxy.net/", Description: "社区维护的 GitHub 加速代理（769ms）"},
	{Key: "cors-isteed", Label: "Cors Proxy", URL: "https://cors.isteed.cc/", Description: "Cloudflare Workers GitHub 代理（804ms）"},
	{Key: "ghfast", Label: "GHFast", URL: "https://ghfast.top/", Description: "高速 GitHub 文件加速（1171ms）"},
	{Key: "conversun", Label: "Conversun Hub", URL: "https://hub.conversun.com/", Description: "Conversun 自建 GitHub 加速，仅代理 conversun 仓库"},
	{Key: "gh-ddlc", Label: "GH DDLC", URL: "https://gh.ddlc.top/", Description: "GitHub 文件下载加速（当前限流 429，智能监测会自动降权）"},
	{Key: "custom", Label: "自定义", URL: "", Description: "使用自定义加速地址"},
	{Key: "direct", Label: "直连 GitHub", URL: "", Description: "直接从 GitHub 下载，适合有代理的用户"},
}

// GitHubSmart 是 api 层注册进 config 的智能监测钩子：
//   - Rank：返回全部真实镜像 key 的优先顺序（按健康度；空 = 用声明顺序）
//   - Best：返回当前最稳定镜像的 URL（空 = 还没有健康数据）
//   - Degraded：显式选定的镜像是否已降级（连续失败达到阈值）
//
// 未注册（如纯配置包的单元测试）时行为退化为原有静态顺序。
type GitHubSmart struct {
	Rank     func(cfg Config) []string
	Best     func(cfg Config) string
	Degraded func(key string) bool
}

var ghSmart GitHubSmart

// RegisterGitHubSmart 由 api 层在启动监测后调用一次。
func RegisterGitHubSmart(h GitHubSmart) { ghSmart = h }

// DockerSmart 是 Docker 镜像加速的智能监测钩子（与 GitHubSmart 同构）：
//   - Rank：全部真实镜像 key 的健康度优先顺序
//   - Best：当前最稳定镜像的 URL（无健康数据时为空）
//   - Degraded：显式选定镜像是否已降级（连续失败达到阈值）
//
// 未注册时行为退化为原有静态顺序。
type DockerSmart struct {
	Rank     func(cfg Config) []string
	Best     func(cfg Config) string
	Degraded func(key string) bool
}

var dkSmart DockerSmart

// RegisterDockerSmart 由 api 层在启动监测后调用一次。
func RegisterDockerSmart(h DockerSmart) { dkSmart = h }

func firstRealGitHubMirrorURL() string {
	for _, m := range gitHubMirrors {
		if m.Key != "auto" && m.Key != "direct" && m.Key != "custom" && m.URL != "" {
			return m.URL
		}
	}
	return ""
}

// KSpeeder 是本地 iStoreEnhance 镜像缓存代理（registry 形态，HTTPS，
// 官方默认端口 5443）。与 URL 前缀型镜像不同：它按标准 registry 路径
// 服务 docker.io 镜像（官方镜像带 library/ 段），只代理 docker.io，
// 不支持 ghcr。未安装/未运行时探测失败，智能系统自动降权。
const (
	KSpeederDefaultAddr = "127.0.0.1:5443"
	KSpeederPrefix      = KSpeederDefaultAddr + "/"
)

var dockerMirrors = []DockerMirror{
	{Key: "auto", Label: "智能 · 自动选最快", URL: "", Description: "系统周期性测速各源，自动选用最快且稳定的加速源"},
	{Key: "daocloud", Label: "DaoCloud", URL: "m.daocloud.io/", Description: "DaoCloud 公共 Docker 镜像加速", MultiRegistry: true},
	{Key: "kspeeder", Label: "KSpeeder (本地)", Description: "本机 KSpeeder/iStoreEnhance 镜像缓存（:5443，仅 docker.io），未安装时自动降权"},
	// ghcr.io has effectively NO public path-prefix proxy: every prefix-style
	// mirror below denies ghcr refs (measured on a live fnOS box — allowlist or
	// manifest failures across 1ms/rat.dev/1panel/dockerproxy/registry.cyou),
	// so ghcr-sourced apps (paperless-ngx #286 and friends) fell through to a
	// direct ghcr.io pull that EOFs behind the GFW. NJU mirrors ghcr by host
	// rewrite (ghcr.io/owner/img -> ghcr.nju.edu.cn/owner/img), verified by a
	// full layer pull on fnOS 1.2.0203. It serves ONLY ghcr.io.
	{Key: "nju-ghcr", Label: "NJU ghcr", Description: "南京大学 ghcr.io 专用镜像（ghcr 应用推荐）", RegistryRewrites: map[string]string{"ghcr.io": "ghcr.nju.edu.cn/"}},
	{Key: "docker-1ms", Label: "1ms.run", URL: "docker.1ms.run/", Description: "社区 Docker 镜像加速"},
	{Key: "daocloud-docker", Label: "DaoCloud Docker", URL: "docker.m.daocloud.io/", Description: "DaoCloud Docker 镜像加速（全球可用）"},
	{Key: "ratdev", Label: "Rat.Dev", URL: "hub.rat.dev/", Description: "Rat 社区 Docker 镜像加速"},
	{Key: "1panel", Label: "1Panel", URL: "docker.1panel.live/", Description: "1Panel 官方 Docker 镜像加速（仅限国内）"},
	{Key: "dockerproxy", Label: "DockerProxy", URL: "dockerproxy.net/", Description: "Docker Proxy 社区镜像加速（仅限国内）"},
	{Key: "registry-cyou", Label: "Registry.cyou", URL: "registry.cyou/", Description: "Cloudflare Docker 镜像代理（仅限国内）"},
	{Key: "custom", Label: "自定义", URL: "", Description: "使用自定义加速地址"},
	{Key: "direct", Label: "直连 Docker Hub", URL: "", Description: "直接拉取，适合有代理的用户"},
}

func GitHubMirrorOptions() []GitHubMirror { return gitHubMirrors }
func DockerMirrorOptions() []DockerMirror { return dockerMirrors }

// GitHubMirrorPrefix 返回单个「当前应使用」的镜像前缀（下载重定向等单前缀
// 场景用；带回退链的场景用 GitHubFallbackPrefixes）。
// 智能行为：
//   - "auto"：返回当前最稳定源的 URL（无健康数据时退回默认镜像）
//   - 显式选定但已降级、且存在更稳定的源：返回更稳定的源
//     （避免把浏览器重定向到一个正在限流/挂掉的镜像）
func GitHubMirrorPrefix(key string, cfg Config) string {
	if key == "custom" && cfg.CustomGitHubMirror != "" {
		return cfg.CustomGitHubMirror
	}
	if key == "direct" {
		return ""
	}
	if key == "auto" {
		if ghSmart.Best != nil {
			if u := ghSmart.Best(cfg); u != "" {
				return u
			}
		}
		return firstRealGitHubMirrorURL()
	}
	for _, m := range gitHubMirrors {
		if m.Key == key {
			if ghSmart.Degraded != nil && ghSmart.Degraded(key) {
				if u := ghSmart.Best(cfg); u != "" && u != m.URL {
					return u
				}
			}
			return m.URL
		}
	}
	return firstRealGitHubMirrorURL()
}

// DockerMirrorPrefix 返回单个「当前应使用」的 Docker 镜像前缀。
// 智能行为（与 GitHubMirrorPrefix 同构）：
//   - "auto"：返回当前最稳定源的 URL（无健康数据时退回默认镜像）
//   - 显式选定但已降级、且存在更稳定的源：返回更稳定的源
func DockerMirrorPrefix(key string, cfg Config) string {
	if key == "custom" && cfg.CustomDockerMirror != "" {
		return cfg.CustomDockerMirror
	}
	if key == "kspeeder" {
		return KSpeederPrefix
	}
	if key == "auto" {
		if dkSmart.Best != nil {
			if u := dkSmart.Best(cfg); u != "" {
				return u
			}
		}
		return firstRealDockerMirrorURL()
	}
	for _, m := range dockerMirrors {
		if m.Key == key {
			// 改写型镜像（nju-ghcr 无 URL；kspeeder 已在上方特判）不进单前缀场景
			if m.URL == "" {
				return m.URL
			}
			if dkSmart.Degraded != nil && dkSmart.Degraded(key) {
				if u := dkSmartBestURL(cfg); u != "" && u != m.URL {
					return u
				}
			}
			return m.URL
		}
	}
	return firstRealDockerMirrorURL()
}

// DockerMirrorByKey 按 key 查镜像定义（含 URL 空的改写型镜像）。
func DockerMirrorByKey(key string) (DockerMirror, bool) {
	for _, m := range dockerMirrors {
		if m.Key == key {
			return m, true
		}
	}
	return DockerMirror{}, false
}

// DockerSmartDegraded 报告某 Docker 镜像是否已降级（监测钩子未注册时恒 false）。
func DockerSmartDegraded(key string) bool {
	return dkSmart.Degraded != nil && dkSmart.Degraded(key)
}

func firstRealDockerMirrorURL() string {
	for _, m := range dockerMirrors {
		if m.Key != "auto" && m.Key != "direct" && m.Key != "custom" && m.URL != "" {
			return m.URL
		}
	}
	return ""
}

func dkSmartBestURL(cfg Config) string {
	if dkSmart.Best != nil {
		return dkSmart.Best(cfg)
	}
	return ""
}

func IsDockerMirrorMultiRegistry(key string) bool {
	if key == "custom" || key == "direct" || key == "" {
		return false
	}
	for _, m := range dockerMirrors {
		if m.Key == key {
			return m.MultiRegistry
		}
	}
	return false
}

// RewriteRefsForMirror returns the refs mirror m serves for canonical by
// rewriting the upstream registry host (ghcr.io/x/y -> ghcr.nju.edu.cn/x/y).
// Empty when m has no rewrite covering the ref's registry.
//
// kspeeder 特例：本地 registry 只服务 docker.io，按标准 registry 路径改写
// （官方单段镜像补 library/ 段：docker.io/busybox -> 127.0.0.1:5443/library/busybox）。
func RewriteRefsForMirror(m DockerMirror, canonical string) []string {
	if m.Key == "kspeeder" {
		return kspeederRefs(canonical)
	}
	var refs []string
	registries := make([]string, 0, len(m.RegistryRewrites))
	for registry := range m.RegistryRewrites {
		registries = append(registries, registry)
	}
	sort.Strings(registries) // deterministic candidate order
	for _, registry := range registries {
		if strings.HasPrefix(canonical, registry+"/") {
			refs = append(refs, m.RegistryRewrites[registry]+canonical[len(registry)+1:])
		}
	}
	return refs
}

// kspeederRefs 把 docker.io canonical 改写为本地 KSpeeder registry ref。
// 官方单段镜像（busybox）补 library/ 段；命名空间镜像原样；非 docker.io
// 源（ghcr 等）返回空。
func kspeederRefs(canonical string) []string {
	const hub = "docker.io/"
	if !strings.HasPrefix(canonical, hub) {
		return nil
	}
	rest := canonical[len(hub):]
	name := rest
	if i := strings.IndexByte(rest, '@'); i >= 0 {
		name = rest[:i]
	} else if i := strings.IndexByte(rest, ':'); i >= 0 {
		name = rest[:i]
	}
	if !strings.Contains(name, "/") {
		rest = "library/" + rest
	}
	return []string{KSpeederPrefix + rest}
}
// GitHubFallbackPrefixes 返回按优先级排序的抓取候选前缀列表。
//
// 智能顺序（监测钩子已注册时）：
//   - 直连：["", 健康度排序的镜像...]
//   - auto：[健康度排序的镜像..., ""]
//   - 自定义：[自定义, 健康度排序的镜像..., ""]
//   - 显式镜像：健康 → [选定, 其余按健康度...]; 已降级 → [健康度排序...,
//     选定（沉底保留，防恢复误判）, ""]
//
// 无钩子时退化为：选定在前、其余按声明顺序（=实测延迟序）、直连兜底。
func GitHubFallbackPrefixes(selectedKey string, cfg Config) []string {
	real := make([]GitHubMirror, 0, len(gitHubMirrors))
	for _, m := range gitHubMirrors {
		if m.Key != "direct" && m.Key != "custom" && m.Key != "auto" && m.URL != "" {
			real = append(real, m)
		}
	}
	keyOrder := make([]string, len(real))
	for i, m := range real {
		keyOrder[i] = m.Key
	}
	if ghSmart.Rank != nil {
		if r := ghSmart.Rank(cfg); len(r) > 0 {
			keyOrder = r
		}
	}
	byKey := make(map[string]string, len(real))
	for _, m := range real {
		byKey[m.Key] = m.URL
	}

	prefixes := make([]string, 0, len(real)+2)
	addURL := func(u string) {
		if u == "" {
			return
		}
		for _, p := range prefixes {
			if p == u {
				return
			}
		}
		prefixes = append(prefixes, u)
	}

	switch selectedKey {
	case "":
		prefixes = append(prefixes, "") // 未配置 = 直连优先（保持旧行为）
		for _, k := range keyOrder {
			addURL(byKey[k])
		}
	case "direct":
		prefixes = append(prefixes, "")
		for _, k := range keyOrder {
			addURL(byKey[k])
		}
	case "auto":
		for _, k := range keyOrder {
			addURL(byKey[k])
		}
		prefixes = append(prefixes, "")
	case "custom":
		addURL(cfg.CustomGitHubMirror)
		for _, k := range keyOrder {
			addURL(byKey[k])
		}
		prefixes = append(prefixes, "")
	default:
		degraded := ghSmart.Degraded != nil && ghSmart.Degraded(selectedKey)
		if !degraded {
			addURL(byKey[selectedKey])
		}
		for _, k := range keyOrder {
			if k == selectedKey {
				continue
			}
			addURL(byKey[k])
		}
		if degraded {
			addURL(byKey[selectedKey]) // 降级源沉底保留（它可能随时恢复）
		}
		prefixes = append(prefixes, "")
	}
	return prefixes
}

// DockerFallbackPrefixes 返回按优先级排序的拉取候选前缀列表（镜像 GitHub
// FallbackPrefixes 的智能顺序）。前缀级拒绝（镜像 allowlist 没有该镜像）
// 绝不能让安装死路一条——这条链让拉取落到有货的源
// (conversun/fnos-apps#267, #266, #257, #248)。
//
// 智能顺序（监测钩子已注册时）：
//   - 直连：["", 健康度排序的镜像...]
//   - auto：[健康度排序的镜像..., ""]
//   - 自定义：[自定义, 健康度排序的镜像..., ""]
//   - 显式镜像：健康 → [选定, 其余按健康度...]; 已降级 → [健康度排序...,
//     选定（沉底保留，防恢复误判）, ""]
//
// 无钩子时退化为：选定在前、其余按声明顺序，直连兜底。
func DockerFallbackPrefixes(selectedKey string, cfg Config) []string {
	real := make([]DockerMirror, 0, len(dockerMirrors))
	for _, m := range dockerMirrors {
		if m.Key != "direct" && m.Key != "custom" && m.Key != "auto" && m.URL != "" {
			real = append(real, m)
		}
	}
	keyOrder := make([]string, len(real))
	for i, m := range real {
		keyOrder[i] = m.Key
	}
	if dkSmart.Rank != nil {
		if r := dkSmart.Rank(cfg); len(r) > 0 {
			keyOrder = r
		}
	}
	byKey := make(map[string]string, len(real))
	for _, m := range real {
		byKey[m.Key] = m.URL
	}

	prefixes := make([]string, 0, len(real)+2)
	addURL := func(u string) {
		if u == "" {
			return
		}
		for _, p := range prefixes {
			if p == u {
				return
			}
		}
		prefixes = append(prefixes, u)
	}

	switch selectedKey {
	case "":
		// 未配置 = 默认源在首（与 LoadConfig 归一化后的显式选定同构）
		if u, ok2 := byKey[DefaultDockerMirror]; ok2 {
			addURL(u)
		}
		for _, k := range keyOrder {
			addURL(byKey[k])
		}
		prefixes = append(prefixes, "")
	case "direct":
		prefixes = append(prefixes, "")
		for _, k := range keyOrder {
			addURL(byKey[k])
		}
	case "auto":
		for _, k := range keyOrder {
			addURL(byKey[k])
		}
		prefixes = append(prefixes, "")
	case "custom":
		addURL(cfg.CustomDockerMirror)
		for _, k := range keyOrder {
			addURL(byKey[k])
		}
		prefixes = append(prefixes, "")
	default:
		// 改写型镜像（nju-ghcr / kspeeder，无 URL 前缀链条目）：不进前缀链，
		// 其 ref 由 dockerPullCandidates 的 rewrite 循环放在链首
		if _, known := byKey[selectedKey]; !known {
			for _, k := range keyOrder {
				addURL(byKey[k])
			}
			prefixes = append(prefixes, "")
			return prefixes
		}
		degraded := dkSmart.Degraded != nil && dkSmart.Degraded(selectedKey)
		if !degraded {
			addURL(byKey[selectedKey])
		}
		for _, k := range keyOrder {
			if k == selectedKey {
				continue
			}
			addURL(byKey[k])
		}
		if degraded {
			addURL(byKey[selectedKey]) // 降级源沉底保留（它可能随时恢复）
		}
		prefixes = append(prefixes, "")
	}
	return prefixes
}

// Config holds the persistent store configuration.
type Config struct {
	CheckIntervalHours int      `json:"check_interval_hours"`
	Mirror             string   `json:"mirror"`
	DockerMirror       string   `json:"docker_mirror"`
	CustomGitHubMirror string   `json:"custom_github_mirror,omitempty"`
	CustomDockerMirror string   `json:"custom_docker_mirror,omitempty"`
	InstallVolume      int      `json:"install_volume"`
	IgnoredApps        []string `json:"ignored_apps,omitempty"`
	// LocalInstalls 记录本机每次成功安装/更新（FPK 下载）的次数（按应用名）。
	// FnDepot 官方规范声明外部源应用不统计/不上报下载量，第三方应用无全局
	// 下载量数据，前端用它回退展示「本机 N 次」。
	LocalInstalls      map[string]int `json:"local_installs,omitempty"`
	// Sources 是用户添加的 FnDepot 外部应用源（V1/V2 协议）。
	Sources []CustomSource `json:"sources,omitempty"`
	// SourceListURL 自定义源列表地址（空 = 内置 DefaultSourceListURL）。
	SourceListURL string `json:"source_list_url,omitempty"`
	// SourceListDisabled 关闭「自动同步源列表」（旧配置无此字段 = 未关闭 = 自动同步开启）。
	SourceListDisabled bool `json:"source_list_disabled,omitempty"`
}

// CustomSource 描述一个用户添加的外部应用源。
type CustomSource struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	URL  string `json:"url"`
}

// IsAppIgnored returns true if the given app is in the ignored list.
func (c Config) IsAppIgnored(appName string) bool {
	for _, name := range c.IgnoredApps {
		if name == appName {
			return true
		}
	}
	return false
}

// Manager handles loading and saving config to disk.
type Manager struct {
	mu       sync.RWMutex
	cfg      Config
	filePath string
}

// NewManager creates a config manager for the given data directory.
// If dataDir is empty, DefaultDataDir is used.
func NewManager(dataDir string) *Manager {
	if dataDir == "" {
		dataDir = DefaultDataDir
	}
	return &Manager{
		filePath: filepath.Join(dataDir, "config.json"),
		cfg:      defaultConfig(),
	}
}

func defaultConfig() Config {
	return Config{
		CheckIntervalHours: DefaultCheckIntervalHours,
		Mirror:             DefaultMirror,
		DockerMirror:       DefaultDockerMirror,
	}
}

// LoadConfig reads the config file from disk.
// If the file does not exist, defaults are used.
func (m *Manager) LoadConfig() (Config, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	raw, err := os.ReadFile(m.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			m.cfg = defaultConfig()
			return m.cfg, nil
		}
		return m.cfg, err
	}

	var cfg Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return m.cfg, err
	}

	if cfg.CheckIntervalHours < 1 {
		cfg.CheckIntervalHours = DefaultCheckIntervalHours
	}
	if cfg.Mirror == "" {
		cfg.Mirror = DefaultMirror
	}
	if cfg.DockerMirror == "" {
		cfg.DockerMirror = DefaultDockerMirror
	}

	m.cfg = cfg
	return m.cfg, nil
}

// SaveConfig writes the config to disk.
func (m *Manager) SaveConfig(cfg Config) error {
	if cfg.CheckIntervalHours < 1 {
		cfg.CheckIntervalHours = DefaultCheckIntervalHours
	}
	if cfg.Mirror == "" {
		cfg.Mirror = DefaultMirror
	}
	if cfg.DockerMirror == "" {
		cfg.DockerMirror = DefaultDockerMirror
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(m.filePath), 0o755); err != nil {
		return err
	}

	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	m.cfg = cfg
	return os.WriteFile(m.filePath, raw, 0o644)
}

// Get returns the current in-memory config.
func (m *Manager) Get() Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cfg
}
