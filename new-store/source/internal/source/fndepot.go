package source

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"fnos-store/internal/config"
	"fnos-store/internal/platform"
)

// FnDepot 外部应用源协议（V1/V2）：
//   - V2: 根节点 { schema_version:"2", source_info, apps{<appname>: {...}} }
//   - V1: 根节点直接是 { <appname>: {...} }（无 schema_version）
//   - 源地址可以是 JSON 直链（任意文件名）或 GitHub 仓库根地址
//     （固定读取根目录 fnpack.json，默认分支，回退 main/master）。
// 参考: https://github.com/EWEDLCM/FnDepot README「FnDepot 外部应用源 V2 编写说明」
const fndepotFetchTimeout = 25 * time.Second

// FNDepotSource 实现 Source 接口，把 FnDepot 外部源翻译成 RemoteApp 列表。
type FNDepotSource struct {
	httpClient *http.Client
	configMgr  *config.Manager // 提供 GitHub 镜像链（raw.githubusercontent.com 抓取走镜像回退）
	sourceURL  string     // 用户填写的原始地址
	jsonURL    string     // 解析后的 JSON 地址
	fallbacks  []string   // GitHub 仓库模式的分支回退地址
	id         string     // 稳定 ID（sha1(原始地址)）
	name       string     // 源显示名（source_info.name 优先，其次仓库 owner，最后仓库名/主机名）
	owner      string     // GitHub 仓库 owner（自动源名用）
	author     string
	homepage   string
}

// fndepotGitHubOwner 从源地址提取 GitHub owner（非 GitHub 地址返回空）。
func fndepotGitHubOwner(rawURL string) string {
	if m := githubRepoRE.FindStringSubmatch(strings.TrimSuffix(strings.TrimSpace(rawURL), "/")); m != nil {
		return m[1]
	}
	return ""
}

// FNDepotSourceMeta 是源的管理元信息（列表展示用）。
type FNDepotSourceMeta struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Author   string `json:"author,omitempty"`
	Homepage string `json:"homepage,omitempty"`
	URL      string `json:"url"`
}

type fndepotV2 struct {
	SchemaVersion string                    `json:"schema_version"`
	SourceInfo    fndepotSourceInfo         `json:"source_info"`
	Apps          map[string]fndepotAppEntry `json:"apps"`
}

type fndepotSourceInfo struct {
	Name        string `json:"name"`
	Author      string `json:"author"`
	Homepage    string `json:"homepage"`
	Description string `json:"description"`
}

type fndepotRelease struct {
	Changelog string                `json:"changelog"`
	UpdatedAt string                `json:"updated_at"`
	Packages  map[string]fndepotPkg `json:"packages"`
}

type fndepotPkg struct {
	DownloadURL string `json:"download_url"`
	SHA256      string `json:"sha256"`
	Size        int64  `json:"size"`
}

type fndepotAppEntry struct {
	DisplayName   string          `json:"display_name"`
	Desc          string          `json:"desc"`
	Platform      json.RawMessage `json:"platform"`
	Categories    []string        `json:"categories"`
	IconURL       string          `json:"icon_url"`
	ReadmeURL     string          `json:"readme_url"`
	PreviewURLs   []string        `json:"preview_urls"`
	Maintainer     string          `json:"maintainer"`
	MaintainerURL  string          `json:"maintainer_url"`
	Distributor    string          `json:"distributor"`
	DistributorURL string          `json:"distributor_url"`
	BugReportURL  string          `json:"bug_report_url"`
	Homepage      string          `json:"homepage"`
	// 可选：FnDepot 官方规范声明外部源不统计下载量，但源提供该字段时
	// （V1 平铺 / V2 apps 均可）解析并展示；缺失时前端回退本地安装次数。
	DownloadCount int             `json:"download_count,omitempty"`
	IsDocker      bool            `json:"is_docker"`
	ServicePort   fndepotServicePort `json:"service_port"`
	Releases      map[string]fndepotRelease `json:"releases"`

	// V1 平铺单版本字段（旧格式源：无 schema_version、无 releases，
	// 每个应用一个 version + download_url；分类用 labels 字符串，
	// isdocker 为 "true"/"false" 字符串）。
	Changelog   fndepotFlexChangelog `json:"changelog"`
	Version     string               `json:"version"`
	Author      string               `json:"author"`
	AuthorURL   string               `json:"author_url"`
	DownloadURL string               `json:"download_url"`
	Labels      fndepotFlexLabels    `json:"labels"`
	IsDockerV1  fndepotFlexBool      `json:"isdocker"`

	// arch_diff：旧格式变体源的按架构包映射（条目无顶层 download_url，
	// 包地址按 x86/arm/all 分列在 arch_diff 里，如 DinDing1/FnDepot 的 MediaHub）。
	ArchDiff map[string]fndepotPkg `json:"arch_diff"`
}

// parseV1Labels 把 V1 的 labels 字符串（"工具，娱乐" / "tools, ai"）拆成分类数组。
func parseV1Labels(labels string) []string {
	labels = strings.ReplaceAll(labels, "，", ",")
	out := make([]string, 0, 2)
	for _, part := range strings.Split(labels, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// fndepotServicePort 兼容 service_port 的字符串与数字两种写法。
type fndepotServicePort string

func (p *fndepotServicePort) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || string(b) == "null" {
		*p = ""
		return nil
	}
	if b[0] == '"' {
		var str string
		if err := json.Unmarshal(b, &str); err != nil {
			return err
		}
		*p = fndepotServicePort(str)
		return nil
	}
	*p = fndepotServicePort(string(b)) // 数字字面量，如 32678
	return nil
}

// fndepotFlexBool 兼容布尔与字符串两种写法（社区源 isdocker 字段）。
type fndepotFlexBool bool

func (f *fndepotFlexBool) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || string(b) == "null" {
		*f = false
		return nil
	}
	if b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*f = fndepotFlexBool(strings.EqualFold(strings.TrimSpace(s), "true"))
		return nil
	}
	*f = fndepotFlexBool(string(b) == "true")
	return nil
}

// fndepotFlexLabels 兼容中文字符串与字符串数组两种写法（社区源 labels 字段）。
type fndepotFlexLabels []string

func (f *fndepotFlexLabels) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || string(b) == "null" {
		*f = nil
		return nil
	}
	if b[0] == '[' {
		var arr []string
		if err := json.Unmarshal(b, &arr); err != nil {
			return err
		}
		*f = arr
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	*f = parseV1Labels(s)
	return nil
}

// fndepotFlexChangelog 兼容字符串与 {版本: 文本} 对象两种写法
// （社区源 changelog 字段，如 moxyis/FnDepot 用对象）。
type fndepotFlexChangelog string

func (f *fndepotFlexChangelog) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || string(b) == "null" {
		*f = ""
		return nil
	}
	if b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*f = fndepotFlexChangelog(s)
		return nil
	}
	if b[0] == '{' {
		var m map[string]string
		if err := json.Unmarshal(b, &m); err != nil {
			return err
		}
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sortByVersionDesc(keys)
		for _, k := range keys {
			if strings.TrimSpace(m[k]) != "" {
				*f = fndepotFlexChangelog(m[k])
				return nil
			}
		}
		*f = ""
		return nil
	}
	*f = ""
	return nil
}

var (
	githubRepoRE = regexp.MustCompile(`^https?://(?:www\.)?github\.com/([^/]+)/([^/?#]+)/?$`)
	htmlTagRE    = regexp.MustCompile(`<[^>]+>`)
	// appnameRE：应用标识符安全门槛——拒绝含空格/斜杠的键（会破坏
	// @appdata/@appcenter 路径与应用中心登记，如 "One Server"）；放行中文等
	// Unicode 键（社区源真实存在，如 KKKK987/FnDepot 的 "小松鼠"）。
	appnameRE = regexp.MustCompile(`^[^\s/]+$`)
)

func fndepotCurrentArch() string {
	p := platform.DetectPlatform()
	if strings.Contains(p, "arm") {
		return "arm"
	}
	return "x86"
}

func platformMatches(p, current string) bool {
	p = strings.ToLower(strings.TrimSpace(p))
	return p == "all" || p == current
}

// fndepotPlatformOK 按源声明的 platform（字符串或数组）判断是否适配当前设备。
func fndepotPlatformOK(declared json.RawMessage, current string) bool {
	switch {
	case len(declared) == 0:
		return true // 历史规则：无 platform 默认 x86；保守放行，交给包选择兜底
	default:
		var one string
		if err := json.Unmarshal(declared, &one); err == nil {
			return platformMatches(one, current)
		}
		var many []string
		if err := json.Unmarshal(declared, &many); err == nil {
			for _, p := range many {
				if platformMatches(p, current) {
					return true
				}
			}
			return false
		}
		return true
	}
}

// NewFNDepotSource 解析并验证用户填写的源地址（立即 fetch+parse）。
func NewFNDepotSource(rawURL string, configMgr *config.Manager) (*FNDepotSource, error) {
	return NewFNDepotSourceCtx(context.Background(), rawURL, configMgr)
}

// NewFNDepotSourceCtx 同 NewFNDepotSource，但受 ctx 约束：ctx 到期立即中止
// 正在进行的抓取（批量同步源列表场景用，避免死链仓库把整个批次拖死）。
// 每个抓取候选各自最多 fndepotFetchTimeout，且不超过 ctx 剩余时间。
func NewFNDepotSourceCtx(ctx context.Context, rawURL string, configMgr *config.Manager) (*FNDepotSource, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, fmt.Errorf("源地址为空")
	}
	if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
		rawURL = "https://" + rawURL
	}
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("无效的源地址: %s", rawURL)
	}

	s := &FNDepotSource{
		httpClient: &http.Client{Timeout: fndepotFetchTimeout},
		configMgr:  configMgr,
		sourceURL:  rawURL,
		id:         fndepotSourceID(rawURL),
		owner:      fndepotGitHubOwner(rawURL),
	}

	jsonURL, fallbacks := resolveJSONURL(u)
	var lastErr error
	for _, candidate := range s.fetchCandidates(jsonURL, fallbacks) {
		cctx, cancel := context.WithTimeout(ctx, fndepotFetchTimeout)
		body, fetchErr := httpGetJSONCtx(cctx, candidate)
		cancel()
		if fetchErr != nil {
			if ctx.Err() != nil {
				return nil, fmt.Errorf("源验证超时: %w", ctx.Err())
			}
			lastErr = fetchErr
			continue
		}
		if err := s.parse(body, jsonURL); err != nil {
			lastErr = err
			continue
		}
		s.jsonURL = jsonURL
		s.fallbacks = fallbacks
		return s, nil
	}
	return nil, fmt.Errorf("源加载失败: %w", lastErr)
}

// resolveJSONURL 区分「JSON 直链」与「GitHub 仓库根地址」。
func resolveJSONURL(u *url.URL) (string, []string) {
	if m := githubRepoRE.FindStringSubmatch(u.String()); m != nil {
		owner, repo := m[1], m[2]
		return fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/HEAD/fnpack.json", owner, repo),
			[]string{
				fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/main/fnpack.json", owner, repo),
				fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/master/fnpack.json", owner, repo),
			}
	}
	return u.String(), nil
}

// fetchCandidates 返回按优先级排序的抓取候选列表。
// GitHub 仓库源（raw.githubusercontent.com）：直连优先（很多环境经代理可直连，
// 且直连实测比公共镜像更稳）→ GitHub 镜像链 → 分支回退直连。
// 用户 JSON 直链：原样抓取一次 + 重试一次（应对瞬时网络抖动）。
// 注意返回的是「抓取用 URL」，源逻辑地址（jsonURL）保持无镜像前缀。
func (s *FNDepotSource) fetchCandidates(jsonURL string, fallbacks []string) []string {
	if !strings.Contains(jsonURL, "raw.githubusercontent.com") {
		return []string{jsonURL, jsonURL}
	}
	out := []string{jsonURL}
	cfg := config.Config{Mirror: config.DefaultMirror}
	if s.configMgr != nil {
		cfg = s.configMgr.Get()
	}
	for _, prefix := range config.GitHubFallbackPrefixes(cfg.Mirror, cfg) {
		if prefix != "" { // 直连（"" 前缀）已在首位
			out = append(out, prefix+jsonURL)
		}
	}
	out = append(out, fallbacks...)
	return out
}

// fetchCandidateTimeout 是单个抓取候选的独立超时（镜像链中每个候选各自计时，
// 避免总超时被前面失败的候选耗光）。
const fetchCandidateTimeout = 15 * time.Second

// NewFNDepotSourceLazy 只做地址解析（离线），不立即抓取 JSON。
// 用于启动/配置变更时重建源列表；可达性在 FetchApps 时验证。
func NewFNDepotSourceLazy(rawURL string, configMgr *config.Manager) (*FNDepotSource, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, fmt.Errorf("源地址为空")
	}
	if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
		rawURL = "https://" + rawURL
	}
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("无效的源地址: %s", rawURL)
	}
	jsonURL, fallbacks := resolveJSONURL(u)
	return &FNDepotSource{
		httpClient: &http.Client{Timeout: fndepotFetchTimeout},
		configMgr:  configMgr,
		sourceURL:  rawURL,
		jsonURL:    jsonURL,
		fallbacks:  fallbacks,
		id:         fndepotSourceID(rawURL),
		owner:      fndepotGitHubOwner(rawURL),
	}, nil
}

// ID 返回源稳定标识（sha1 前 12 位）。
func (s *FNDepotSource) ID() string { return s.id }

// OverrideName 用添加时记录的显示名覆盖（惰性构造拿不到 source_info）。
func (s *FNDepotSource) OverrideName(name string) {
	if strings.TrimSpace(name) != "" {
		s.name = strings.TrimSpace(name)
	}
}

func fndepotSourceID(rawURL string) string {
	h := sha1.Sum([]byte(rawURL))
	return hex.EncodeToString(h[:])[:12]
}

func httpGetJSON(rawURL string, client *http.Client) ([]byte, error) {
	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "fnos-store/1.x")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %s", resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 8<<20))
}

// httpGetJSONCtx 是 httpGetJSON 的 ctx 版本（超时完全由 ctx 控制）。
func httpGetJSONCtx(ctx context.Context, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "fnos-store/1.x")
	client := &http.Client{} // 超时由 ctx 控制
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %s", resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 8<<20))
}

// parse 校验并提取源元信息（V1/V2）。
func (s *FNDepotSource) parse(body []byte, jsonURL string) error {
	appsMap, _ := decodeFndepotApps(body)
	if len(appsMap) == 0 {
		return fmt.Errorf("无法识别的源格式（需 FnDepot V1/V2 结构）")
	}
	// 防误判：至少一个条目带可安装包信息（download_url / releases / arch_diff），
	// 或是 GitHub 仓库源里的 version 条目（仓库内 FPK 约定候选）——坏键宽容化后
	// 仍要挡住把任意 JSON 字典当源加进来的情况。
	hasPkg := false
	for _, e := range appsMap {
		if strings.TrimSpace(e.DownloadURL) != "" || len(e.Releases) > 0 || len(e.ArchDiff) > 0 {
			hasPkg = true
			break
		}
		if s.owner != "" && strings.TrimSpace(e.Version) != "" {
			hasPkg = true
			break
		}
	}
	if !hasPkg {
		return fmt.Errorf("无法识别的源格式（未找到可安装应用条目）")
	}
	_ = jsonURL
	// V2 元信息
	var v2 fndepotV2
	if err := json.Unmarshal(body, &v2); err == nil && v2.SchemaVersion == "2" {
		s.name = strings.TrimSpace(v2.SourceInfo.Name)
		s.author = v2.SourceInfo.Author
		s.homepage = v2.SourceInfo.Homepage
	}
	if s.name == "" && s.owner != "" {
		s.name = s.owner // GitHub 源：owner 名（如 Blue-Mink）比仓库名更可读
	}
	if s.name == "" {
		s.name = sourceNameFromURL(jsonURL)
	}
	return nil
}

// Name 实现 Source。
func (s *FNDepotSource) Name() string {
	if s.name == "" {
		return sourceNameFromURL(s.jsonURL)
	}
	return s.name
}

// Meta 返回管理元信息。
func (s *FNDepotSource) Meta() FNDepotSourceMeta {
	return FNDepotSourceMeta{
		ID:       s.id,
		Name:     s.Name(),
		Author:   s.author,
		Homepage: s.homepage,
		URL:      s.sourceURL,
	}
}

// JSONURL 返回解析后的 JSON 地址（调试/展示用）。
func (s *FNDepotSource) JSONURL() string { return s.jsonURL }

// FetchApps 实现 Source：抓取源 JSON 并翻译为 RemoteApp（按当前架构过滤）。
func (s *FNDepotSource) FetchApps(ctx context.Context) ([]RemoteApp, error) {
	candidates := s.fetchCandidates(s.jsonURL, s.fallbacks)
	var lastErr error
	var body []byte
	var fetchedFrom string // 实际命中 200 的候选 URL（镜像前缀原样保留）
	for _, candidate := range candidates {
		cctx, cancel := context.WithTimeout(ctx, fetchCandidateTimeout)
		req, err := http.NewRequestWithContext(cctx, "GET", candidate, nil)
		if err != nil {
			cancel()
			return nil, err
		}
		req.Header.Set("User-Agent", "fnos-store/1.x")
		// 公共镜像 CDN 可能缓存旧版 manifest（实测同一刷新周期内版本在
		// 新旧两份之间跳动）→ 源清单必须禁用缓存。
		req.Header.Set("Cache-Control", "no-cache")
		req.Header.Set("Pragma", "no-cache")
		resp, err := s.httpClient.Do(req)
		if err != nil {
			cancel()
			lastErr = err
			continue
		}
		rb, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		resp.Body.Close()
		cancel()
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("源 HTTP %s", resp.Status)
			continue
		}
		body = rb
		fetchedFrom = candidate
		break
	}
	if body == nil {
		return nil, fmt.Errorf("源不可达: %w", lastErr)
	}

	// 仓库内 FPK 约定基址：去掉镜像前缀与 fnpack.json 后缀，得到裸
	// raw.githubusercontent.com/.../<owner>/<repo>/<branch> 路径。仅
	// GitHub raw 源可用（FNDepot 社区约定 <base>/<appname>/<appname>.fpk，
	// 如 moxyis/FnDepot 的 fpk-* 系列）；安装期下载照常走镜像链。
	conventionBase := ""
	if i := strings.Index(fetchedFrom, "https://raw.githubusercontent.com"); i >= 0 {
		conventionBase = strings.TrimSuffix(fetchedFrom[i:], "/fnpack.json")
	}

	appsMap, _ := decodeFndepotApps(body)
	apps := make([]RemoteApp, 0, len(appsMap))
	for appName, entry := range appsMap {
		if ra, ok := translateFndepotApp(appName, entry, s.jsonURL, s.Name(), conventionBase); ok {
			apps = append(apps, ra)
		}
	}
	sort.Slice(apps, func(i, j int) bool { return apps[i].AppName < apps[j].AppName })
	return apps, nil
}

// decodeFndepotApps 解析 V1/V2 两种结构。
func decodeFndepotApps(body []byte) (map[string]fndepotAppEntry, bool) {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return map[string]fndepotAppEntry{}, false
	}
	var v2 fndepotV2
	if err := json.Unmarshal(body, &v2); err == nil && v2.SchemaVersion == "2" && len(v2.Apps) > 0 {
		return v2.Apps, true
	}
	var v1 map[string]fndepotAppEntry
	if err := json.Unmarshal(body, &v1); err == nil {
		for k := range v1 {
			if !appnameRE.MatchString(k) {
				// 坏键（含空格等）：只跳过该条目，不拒绝整份源——社区源里
				// 合法条目与 "One Server" 这类坏键常并存（DinDing1/FnDepot
				// 实测）。全坏的文件会在此清空 → 返回空 → 仍按无效源拒绝。
				delete(v1, k)
			}
		}
		return v1, false
	}
	return map[string]fndepotAppEntry{}, false
}

// resolveAgainst 把源 JSON 内的相对 URL 解析为绝对 URL。
func resolveAgainst(base, ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") {
		return ref
	}
	baseU, err := url.Parse(base)
	if err != nil {
		return ref
	}
	refU, err := url.Parse(ref)
	if err != nil {
		return ref
	}
	return baseU.ResolveReference(refU).String()
}

// fndepotSelectedPkg 是翻译选中的单个安装包。
type fndepotSelectedPkg struct {
	version   string
	download  string
	changelog string
	updatedAt string
	sha256    string
	size      int64
}

// translateFndepotApp 把单个 FnDepot 应用翻译成 RemoteApp。
// 选择规则：当前架构包优先 → all 包；版本号取最高。
// 同时支持 V2（releases 多版本）、V1 平铺单版本（version + download_url）、
// V1 arch_diff（按架构分列的 download_url）与仓库内 FPK 约定
//（条目无任何下载字段时，GitHub 源按 <base>/<appname>/<appname>.fpk 构造）。
func translateFndepotApp(appName string, entry fndepotAppEntry, baseURL, sourceName, conventionBase string) (RemoteApp, bool) {
	if !appnameRE.MatchString(appName) {
		return RemoteApp{}, false
	}
	if !fndepotPlatformOK(entry.Platform, fndepotCurrentArch()) {
		return RemoteApp{}, false
	}
	displayName := strings.TrimSpace(entry.DisplayName)
	if displayName == "" {
		displayName = appName
	}
	current := fndepotCurrentArch()

	// 收集候选包：V2 走 releases（当前架构 → all，版本降序）；
	// V1 平铺只有一个 version + download_url。
	var sel fndepotSelectedPkg
	var ok bool
	if len(entry.Releases) > 0 {
		versions := make([]string, 0, len(entry.Releases))
		for v := range entry.Releases {
			versions = append(versions, v)
		}
		sortByVersionDesc(versions)
		for _, v := range versions {
			release := entry.Releases[v]
			pkg, found := release.Packages[current]
			if !found {
				pkg, found = release.Packages["all"]
			}
			if !found || strings.TrimSpace(pkg.DownloadURL) == "" {
				continue
			}
			sel = fndepotSelectedPkg{
				version:   v,
				download:  pkg.DownloadURL,
				changelog: release.Changelog,
				updatedAt: release.UpdatedAt,
				sha256:    pkg.SHA256,
				size:      pkg.Size,
			}
			ok = true
			break
		}
	} else {
		// V1 平铺单版本
		if ver := strings.TrimSpace(entry.Version); ver != "" && strings.TrimSpace(entry.DownloadURL) != "" {
			sel = fndepotSelectedPkg{version: ver, download: entry.DownloadURL}
			ok = true
		} else if ver := strings.TrimSpace(entry.Version); ver != "" && len(entry.ArchDiff) > 0 {
			// arch_diff：旧格式变体源把 download_url 按 x86/arm/all 分列
			// （如 DinDing1/FnDepot 的 MediaHub），选当前架构包。
			pkg, found := entry.ArchDiff[current]
			if !found {
				pkg, found = entry.ArchDiff["all"]
			}
			if found && strings.TrimSpace(pkg.DownloadURL) != "" {
				sel = fndepotSelectedPkg{version: ver, download: pkg.DownloadURL, sha256: pkg.SHA256, size: pkg.Size}
				ok = true
			}
		} else if conventionBase != "" && strings.TrimSpace(entry.Version) != "" {
			// 条目无任何下载字段：按 FNDepot 社区约定从仓库内取 FPK
			// raw.../<owner>/<repo>/<branch>/<appname>/<appname>.fpk
			// （moxyis/FnDepot 的 fpk-* 系列实测此布局）；下载侧走镜像链。
			sel = fndepotSelectedPkg{version: strings.TrimSpace(entry.Version), download: conventionBase + "/" + appName + "/" + appName + ".fpk"}
			ok = true
		}
		// 否则：无 releases 也无平铺包（如拆分模式 details_url）→ 暂不支持
	}
	if !ok {
		return RemoteApp{}, false
	}

	// 源 manifest 没给 updated_at（V1 平铺源全部缺失、V2 部分 release 缺失）时，
	// 从下载链接的 release 标签里解析发布日期（如 .../releases/download/
	// 2026.09.16-1052/xxx.fpk → 2026-09-16 10:52），让「最近更新」有值可显示。
	if sel.updatedAt == "" {
		sel.updatedAt = DateFromReleaseURL(sel.download)
	}

	port := 0
	if ps := strings.TrimSpace(string(entry.ServicePort)); ps != "" {
		if n, err := strconv.Atoi(ps); err == nil {
			port = n
		}
	}

	// 分类：V2 categories 数组优先；V1 用 labels（字符串或数组，flex 已归一）。
	cats := entry.Categories
	if len(cats) == 0 && len(entry.Labels) > 0 {
		cats = entry.Labels
	}

	// Docker：V2 is_docker 布尔；V1 isdocker（布尔或字符串，flex 已归一）。
	isDocker := entry.IsDocker || bool(entry.IsDockerV1)

	previewURLs := make([]string, 0, len(entry.PreviewURLs))
	for _, p := range entry.PreviewURLs {
		if abs := resolveAgainst(baseURL, p); abs != "" {
			previewURLs = append(previewURLs, abs)
		}
	}

	return RemoteApp{
		AppName:        appName,
		DisplayName:    displayName,
		Version:        sel.version,
		FpkVersion:     sel.version,
		// desc 原样保留（允许 HTML，如 QQ 群超链接）：列表侧由前端
		// descriptionPlainText 纯文本化，详情页按富文本渲染（链接可点）。
		Description:    strings.TrimSpace(entry.Desc),
		HomepageURL:    firstNonEmpty(entry.Homepage, entry.MaintainerURL, entry.BugReportURL),
		UpdatedAt:      sel.updatedAt,
		ServicePort:    port,
		Platforms:      []string{current},
		FpkURL:         resolveAgainst(baseURL, sel.download),
		IconURL:        resolveAgainst(baseURL, entry.IconURL),
		DownloadCount:  entry.DownloadCount,
		AppType:        appTypeOf(isDocker),
		Category:       mapFndepotCategory(cats),
		Source:         sourceName,
		ReadmeURL:      resolveAgainst(baseURL, entry.ReadmeURL),
		PreviewURLs:    previewURLs,
		Maintainer:     strings.TrimSpace(firstNonEmpty(entry.Maintainer, entry.Author)),
		MaintainerURL:  strings.TrimSpace(firstNonEmpty(entry.MaintainerURL, entry.AuthorURL)),
		Distributor:    strings.TrimSpace(entry.Distributor),
		DistributorURL: strings.TrimSpace(entry.DistributorURL),
		Changelog:      firstNonEmpty(sel.changelog, string(entry.Changelog)),
		SizeBytes:      sel.size,
		SHA256:         strings.TrimSpace(sel.sha256),
	}, true
}

func appTypeOf(docker bool) string {
	if docker {
		return "docker"
	}
	return ""
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func stripHTML(s string) string {
	s = htmlTagRE.ReplaceAllString(s, " ")
	return strings.Join(strings.Fields(s), " ")
}

// mapFndepotCategory 把 FnDepot 固定分类映射到本店分类键（未匹配返回空）。
// mapFndepotCategory 把 FnDepot 固定分类或 V1 自由分类词映射到本店分类键。
func mapFndepotCategory(cats []string) string {
	if len(cats) == 0 {
		return ""
	}
	switch strings.TrimSpace(cats[0]) {
	case "影音娱乐", "娱乐", "影音", "音乐", "视频":
		return "media"
	case "AI赋能", "AI", "ai":
		return "ai"
	case "系统工具", "工具", "安全", "网络", "编程开发", "硬件驱动", "智能智控", "下载", "浏览器", "系统":
		return "system"
	case "生活服务", "教育学习", "教育":
		return "content"
	case "游戏地带", "游戏":
		return "media"
	default:
		return ""
	}
}

func sourceNameFromURL(u string) string {
	p := strings.TrimSuffix(u, "/")
	p = strings.TrimSuffix(p, "/fnpack.json")
	if i := strings.LastIndex(p, "/"); i >= 0 {
		p = p[i+1:]
	}
	if p == "" {
		p = u
	}
	return p
}

// sortByVersionDesc 按版本号降序（SemVer 风格，非数字段回退 0）。
// releaseTagDateRE 匹配日期（可选 HHMM 时间）：2026.09.16 / 2026-09-16 / 20260916，
// 时间形如 2026.09.16-1052 / 20260916_1052。
var releaseTagDateRE = regexp.MustCompile(`(?i)(20\d{2})[.\-/]?(\d{1,2})[.\-/]?(\d{1,2})(?:[-_T ](\d{1,2})(\d{2}))?`)

// DateFromReleaseURL 从下载 URL 的 release 标签解析发布日期，返回 RFC3339
// （按 +08:00 时区，国内源惯例）；解析不出返回空串。
// 优先解析 "releases/download/<tag>/<file>" 的 tag 段，避免误匹配文件名里的数字。
func DateFromReleaseURL(rawURL string) string {
	tag := rawURL
	if i := strings.Index(rawURL, "/releases/download/"); i >= 0 {
		rest := rawURL[i+len("/releases/download/"):]
		if j := strings.LastIndex(rest, "/"); j > 0 {
			tag = rest[:j]
		}
	}
	m := releaseTagDateRE.FindStringSubmatch(tag)
	if m == nil {
		return ""
	}
	year, _ := strconv.Atoi(m[1])
	month, _ := strconv.Atoi(m[2])
	day, _ := strconv.Atoi(m[3])
	if year < 2000 || year > 2099 || month < 1 || month > 12 || day < 1 || day > 31 {
		return ""
	}
	hour, minute := 0, 0
	if m[4] != "" {
		hour, _ = strconv.Atoi(m[4])
		minute, _ = strconv.Atoi(m[5])
		if hour > 23 || minute > 59 {
			hour, minute = 0, 0
		}
	}
	// 标签里的时间按 +08:00 墙钟时间理解（国内源惯例），不做时区换算。
	loc := time.FixedZone("CST", 8*3600)
	return time.Date(year, time.Month(month), day, hour, minute, 0, 0, loc).Format(time.RFC3339)
}

func sortByVersionDesc(versions []string) {
	sort.Slice(versions, func(i, j int) bool {
		return compareFndepotVersions(versions[i], versions[j]) > 0
	})
}

func compareFndepotVersions(a, b string) int {
	pa, pb := versionParts(a), versionParts(b)
	for i := 0; i < len(pa) && i < len(pb); i++ {
		if pa[i] != pb[i] {
			if pa[i] < pb[i] {
				return -1
			}
			return 1
		}
	}
	if len(pa) != len(pb) {
		if len(pa) < len(pb) {
			return -1
		}
		return 1
	}
	return 0
}

func versionParts(v string) []int {
	v = strings.TrimPrefix(v, "v")
	v = strings.SplitN(v, "-", 2)[0]
	v = strings.SplitN(v, "+", 2)[0]
	parts := strings.Split(v, ".")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			n = 0
		}
		out = append(out, n)
	}
	return out
}
