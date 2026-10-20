package config

import (
	"strings"
	"time"
)

// BuiltinRevision 是出厂源清单的修订号。
//
// 每次向出厂清单新增源时递增。配置加载会把「修订号落后」的配置
// 一次性补齐到当前修订（见 config.normalize），用户删除的出厂源
// 不会被反复复活。
//
//   - 1：初版 19 个前缀源 + 2 个 raw CDN + 2 个 git 镜像 + 官方直连
//   - 2：合并 Moo 应用实测源（+14 前缀源、+4 Docker 上游）
//   - 3：修复迁移缺口——修订 2 只合入了 GitHub 镜像，漏补 Docker 上游
//   - 4：合入用户提供的 25 个社区前缀源
//   - 5：新增路径抓取型源类型（xget 形态）并合入 xget.xi-xu.me
//   - 6：按两轮真实请求实测剔除长期不可用的出厂源（38 个），只留能用的
const BuiltinRevision = 6

// BuiltinMirrors 返回出厂内置的 GitHub 加速源列表。
//
// 这些源均为社区公开的公益中转服务，可用性随时间波动，
// 因此程序会在运行期持续测速并按实测结果动态排序，
// 用户也可以在控制台中增删自己的私有源。
// BuiltinMirrors 返回出厂内置的 GitHub 加速源列表。
//
// 1.3.0 起这份清单只保留「实测可用」的源：
//   - 2026-10-19 在本机（192.168.1.2）对 64 个出厂源各做两轮真实请求
//     （raw.githubusercontent.com 的 gitignore 文件 / git 仓库 info/refs），
//     两轮都失败且非偶发抖动的源全部下线（见 retiredBuiltinMirrorIDs）；
//   - 保留的源都在两轮中返回 200（g.blfrp.cn / ghproxy.vip 各有一轮抖动，
//     属可用源的正常波动，保留）；
//   - GitHub 官方直连始终保留作兜底（探测目标是 api.github.com/meta，
//     被 GitHub 限流返回 403 是探测口径问题，不代表直连不可用）。
//
// 用户仍可在控制台增删自己的私有源。
func BuiltinMirrors() []Mirror {
	return []Mirror{
		// ---- 前缀型中转站：覆盖 releases / archive / raw / git clone ----
		{ID: "ghfast-top", Name: "ghfast.top", URL: "https://ghfast.top/", Kind: KindPrefix, Enabled: true, Weight: 1, Note: "韩国首尔节点"},
		{ID: "gh-proxy-com", Name: "gh-proxy.com", URL: "https://gh-proxy.com/", Kind: KindPrefix, Enabled: true, Weight: 1, Note: "韩国首尔节点，大文件表现好"},
		{ID: "ghproxy-vip", Name: "ghproxy.vip", URL: "https://ghproxy.vip/", Kind: KindPrefix, Enabled: true, Weight: 0.9, Note: "多线路，支持 LFS"},
		{ID: "gh-proxy-org", Name: "gh-proxy.org", URL: "https://gh-proxy.org/", Kind: KindPrefix, Enabled: true, Weight: 0.9, Note: "GH-Proxy 主站，多线路"},
		{ID: "gh-proxy-hk", Name: "hk.gh-proxy.org", URL: "https://hk.gh-proxy.org/", Kind: KindPrefix, Enabled: true, Weight: 0.8, Note: "GH-Proxy 香港节点"},
		{ID: "cdn-ghproxy", Name: "cdn.gh-proxy.org", URL: "https://cdn.gh-proxy.org/", Kind: KindPrefix, Enabled: true, Weight: 0.8, Note: "GH-Proxy CDN 节点"},
		{ID: "edgeone-ghproxy", Name: "edgeone.gh-proxy.org", URL: "https://edgeone.gh-proxy.org/", Kind: KindPrefix, Enabled: true, Weight: 0.8, Note: "GH-Proxy EdgeOne 节点"},
		{ID: "ghps-cc", Name: "ghps.cc", URL: "https://ghps.cc/", Kind: KindPrefix, Enabled: true, Weight: 0.8, Note: "社区公益中转"},
		{ID: "yylx", Name: "git.yylx.win", URL: "https://git.yylx.win/", Kind: KindPrefix, Enabled: true, Weight: 0.7, Note: "社区公益中转"},
		{ID: "wget-la", Name: "wget.la", URL: "https://wget.la/", Kind: KindPrefix, Enabled: true, Weight: 0.7, Note: "社区公益中转，大文件友好"},
		{ID: "gh-dpik", Name: "gh.dpik.top", URL: "https://gh.dpik.top/", Kind: KindPrefix, Enabled: true, Weight: 0.7, Note: "社区公益中转"},
		{ID: "hub-conversun", Name: "hub.conversun.com", URL: "https://hub.conversun.com/", Kind: KindPrefix, Enabled: true, Weight: 0.7, Note: "社区公益中转"},
		{ID: "ghproxy-it", Name: "ghproxy.it", URL: "https://ghproxy.it/", Kind: KindPrefix, Enabled: true, Weight: 0.7, Note: "意大利节点"},
		{ID: "github-boki", Name: "github.boki.moe", URL: "https://github.boki.moe/", Kind: KindPrefix, Enabled: true, Weight: 0.7, Note: "社区公益中转"},
		{ID: "gh-monlor", Name: "gh.monlor.com", URL: "https://gh.monlor.com/", Kind: KindPrefix, Enabled: true, Weight: 0.7, Note: "社区公益中转"},
		{ID: "github-ednovas", Name: "github.ednovas.xyz", URL: "https://github.ednovas.xyz/", Kind: KindPrefix, Enabled: true, Weight: 0.7, Note: "社区公益中转"},
		{ID: "ghpxy-hwinzniej", Name: "ghpxy.hwinzniej.top", URL: "https://ghpxy.hwinzniej.top/", Kind: KindPrefix, Enabled: true, Weight: 0.6, Note: "社区公益中转"},
		{ID: "cdn-crashmc", Name: "cdn.crashmc.com", URL: "https://cdn.crashmc.com/", Kind: KindPrefix, Enabled: true, Weight: 0.7, Note: "社区 CDN 中转"},
		{ID: "down-npee", Name: "down.npee.cn", URL: "https://down.npee.cn/", Kind: KindPrefix, Enabled: true, Weight: 0.6, Note: "社区公益中转"},
		{ID: "hub-glowp", Name: "hub.glowp.xyz", URL: "https://hub.glowp.xyz/", Kind: KindPrefix, Enabled: true, Weight: 0.7, Note: "社区公益中转"},
		{ID: "gh-catmak", Name: "gh.catmak.name", URL: "https://gh.catmak.name/", Kind: KindPrefix, Enabled: true, Weight: 0.7, Note: "社区公益中转"},
		{ID: "g-blfrp", Name: "g.blfrp.cn", URL: "https://g.blfrp.cn/", Kind: KindPrefix, Enabled: true, Weight: 0.7, Note: "社区公益中转"},

		// ---- 路径抓取型源（xget 形态，仅 raw 文件）----
		{ID: "xget-xi-xu", Name: "xget.xi-xu.me", URL: "https://xget.xi-xu.me/gh", Kind: KindPathFetch, Enabled: true, Weight: 0.7, Note: "路径抓取型中转，仅处理 raw 文件"},
		// ---- raw CDN ----
		{ID: "jsdelivr", Name: "jsDelivr CDN", URL: "https://fastly.jsdelivr.net/", Kind: KindRawCDN, Enabled: true, Weight: 1, Note: "全球 CDN，raw 文件最快"},

		// ---- Git 仓库镜像 ----
		{ID: "kkgithub", Name: "kkgithub.com", URL: "https://kkgithub.com/", Kind: KindGitClone, Enabled: true, Weight: 0.8, Note: "香港节点，GitHub 全站镜像"},

		// ---- 官方直连：始终保留作为兜底 ----
		{ID: "origin", Name: "GitHub 官方直连", URL: "https://github.com/", Kind: KindDirect, Enabled: true, Weight: 0.5, Note: "兜底源，网络通畅时最快"},
	}
}

// retiredBuiltinMirrorIDs 是 1.3.0 下线的出厂源（2026-10-19 两轮实测均不可用）。
//
// 保留这张表而不是"直接删掉"：老装机配置里仍然存着这些源，
// normalize 会按 ID 把它们剔除（从未安装过的新配置则根本不会出现）。
var retiredBuiltinMirrorIDs = map[string]bool{
	"ghproxy-net":       true, // 两轮 502
	"githubfast":        true, // 两轮 403
	"moeyy":             true, // 两轮超时
	"hub-gitmirror":     true, // 域名解析失败
	"gh-llkk":           true, // 连接超时
	"moeyy-xyz":         true, // 404
	"ghp-999":           true, // 域名解析失败
	"gh-cdn-zz":         true, // 域名解析失败
	"gh-ciiiii":         true, // 域名解析失败
	"gh-aas123":         true, // 连接被重置
	"gh-api999":         true, // 域名解析失败
	"gh-dd320":          true, // 域名解析失败
	"ghps-hk":           true, // 域名解析失败
	"ghps-us":           true, // TLS 握手失败
	"ght-999":           true, // 域名解析失败
	"ghproxy-cxkpro":    true, // 两轮 429
	"gitproxy-mrhjx":    true, // 两轮 429
	"felicity":          true, // 两轮 429
	"github-dpik":       true, // 两轮 429
	"cors-isteed":       true, // 两轮 403
	"memory-echoes":     true, // 两轮 429
	"gh-ddlc":           true, // 两轮 429
	"gh-b52m":           true, // 读超时
	"gh-h233":           true, // 两轮 403
	"rapidgit-jjda":     true, // 503
	"gh-jasonzeng":      true, // 域名解析失败
	"github-tbedu":      true, // 域名解析失败
	"github-geekery":    true, // 404
	"ghfile-geekertao":  true, // 两轮 429
	"ghp-keleyaa":       true, // 两轮 429
	"gh-chjina":         true, // 连接失败
	"gh-xxooo":          true, // 域名解析失败
	"gh-idayer":         true, // 两轮 429
	"raw-ihtw":          true, // 域名解析失败
	"gh-zwy":            true, // 两轮 403
	"ghproxy-monkeyray": true, // 两轮 500/404
	"raw-kkgithub":      true, // 读超时
	"gitclone":          true, // 404（服务已停）
}

// RetiredMirror 判断某个出厂源 ID 是否已下线。
func RetiredMirror(id string) bool { return retiredBuiltinMirrorIDs[id] }

// BuiltinDockerUpstreams 返回出厂内置的 Docker 上游列表。
//
// 依次为社区公益镜像站，最后 "official" 表示 Docker Hub 官方源（registry-1.docker.io），
// 作为兜底。请求按顺序尝试，上游不可用时自动切换。
// 1.3.0：只用实测能连上的上游。2026-10-19 逐个探测 /v2/ 与
// /v2/library/alpine/manifests/3.20：
//   - 200 / 401 都算存活（401 是标准认证质询，daemon 会自行换 token）；
//   - DNS 失败、TLS 握手失败、连接超时、/v2/ 返回 404 的一律下线；
//   - ghcr.nju.edu.cn 只镜像 ghcr.io，对 docker.io 拉取无意义，一并下线。
//
// official 必须保留在末尾作为兜底（本机直连 Docker Hub 慢/超时是环境问题，
// 不代表兜底链可以去掉）。
func BuiltinDockerUpstreams() []string {
	return []string{
		"https://docker.1ms.run",
		"https://docker.m.daocloud.io",
		"https://docker.xuanyuan.me",
		"https://docker.1panel.live",
		"https://dockerproxy.net",
		"https://m.daocloud.io",
		"https://hub.rat.dev",
		"official",
	}
}

// retiredDockerUpstreams 是 1.3.0 下线的出厂 Docker 上游（实测不可用）。
//
// 老装机配置里仍存着这些地址，normalize 会按地址把它们剔除。
var retiredDockerUpstreams = map[string]bool{
	"https://dhub.kubespeed.xyz":      true, // 域名解析失败
	"https://dockermirror.com":        true, // TLS 握手失败
	"https://docker.nastool.de":       true, // 连接超时
	"https://dockerhub.jobtechzy.com": true, // 域名解析失败
	"https://registry.cyou":           true, // /v2/ 返回 404
	"https://ghcr.nju.edu.cn":         true, // 只镜像 ghcr.io，对 docker.io 无用
}

// RetiredDockerUpstream 判断某个出厂 Docker 上游是否已下线。
func RetiredDockerUpstream(u string) bool {
	return retiredDockerUpstreams[strings.TrimRight(strings.TrimSpace(u), "/")]
}

// GHHost 描述一个需要加速的 GitHub 域名及其属性。
type GHHost struct {
	// Host 是域名本身。
	Host string
	// Category 是域名分类，决定路由到哪一类镜像源。
	Category string
	// TLS 表示该域名是否强制 HTTPS。
	TLS bool
}

// 域名分类常量，与镜像源的 Kind 一一对应。
const (
	CatWeb   = "web"   // 网页与 API
	CatRaw   = "raw"   // 裸文件
	CatClone = "clone" // git 仓库传输
)

// GitHubHosts 列出全部受管的 GitHub 域名。
//
// 这些域名会在代理层被识别并重定向到加速通道，
// 同时也会作为 DNS 优选的目标。
func GitHubHosts() []GHHost {
	return []GHHost{
		{Host: "github.com", Category: CatWeb, TLS: true},
		{Host: "www.github.com", Category: CatWeb, TLS: true},
		{Host: "api.github.com", Category: CatWeb, TLS: true},
		{Host: "gist.github.com", Category: CatWeb, TLS: true},
		{Host: "codeload.github.com", Category: CatWeb, TLS: true},
		{Host: "raw.githubusercontent.com", Category: CatRaw, TLS: true},
		{Host: "gist.githubusercontent.com", Category: CatRaw, TLS: true},
		{Host: "objects.githubusercontent.com", Category: CatRaw, TLS: true},
		{Host: "release-assets.githubusercontent.com", Category: CatRaw, TLS: true},
		{Host: "github-releases.githubusercontent.com", Category: CatRaw, TLS: true},
		{Host: "cam.githubusercontent.com", Category: CatRaw, TLS: true},
		{Host: "avatars.githubusercontent.com", Category: CatRaw, TLS: true},
		{Host: "user-images.githubusercontent.com", Category: CatRaw, TLS: true},
		{Host: "private-user-images.githubusercontent.com", Category: CatRaw, TLS: true},
		{Host: "media.githubusercontent.com", Category: CatRaw, TLS: true},
	}
}

// HostSet 返回受管域名的集合，便于 O(1) 判断。
func HostSet() map[string]GHHost {
	hs := GitHubHosts()
	out := make(map[string]GHHost, len(hs))
	for _, h := range hs {
		out[h.Host] = h
	}
	return out
}

// DirectPreferredHosts 列出应当优先直连的域名。
//
// 原因：这些接口对请求来源与认证态敏感，
// 经第三方镜像中转容易被 GitHub 判定为异常流量而返回 403/401，
// 直连的成功率反而更高。程序仅在直连失败时才会使用镜像。
func DirectPreferredHosts() map[string]bool {
	return map[string]bool{
		"api.github.com":  true,
		"github.com":      true, // 网页与登录态相关请求
		"www.github.com":  true,
		"gist.github.com": true,
	}
}

// HostIsDirectPreferred 判断某域名是否应优先直连。
func HostIsDirectPreferred(host string) bool {
	return DirectPreferredHosts()[host]
}

// DomainProbeTargets 返回 DNS 优选时使用的探测目标，
// 每一项都附带一个体积可控的真实文件路径用于吞吐测速。
func DomainProbeTargets() []ProbeTarget {
	return []ProbeTarget{
		{Host: "github.com", Path: "/favicon.ico", Category: CatWeb},
		{Host: "api.github.com", Path: "/meta", Category: CatWeb},
		{Host: "raw.githubusercontent.com", Path: "/github/gitignore/main/Go.gitignore", Category: CatRaw},
		{Host: "codeload.github.com", Path: "/github/gitignore/tar.gz/refs/heads/main", Category: CatRaw},
		{Host: "objects.githubusercontent.com", Path: "/", Category: CatRaw},
	}
}

// ProbeTarget 描述一次测速探测的目标。
type ProbeTarget struct {
	Host     string `json:"host"`
	Path     string `json:"path"`
	Category string `json:"category"`
}

// DefaultUserAgent 是代理转发时使用的 UA，尽量贴近真实浏览器以避免被判为异常流量。
const DefaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 " +
	"(KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"

// ProbeUserAgent 是测速探测使用的 UA，标识自身便于镜像站统计。
const ProbeUserAgent = "ghpp-probe/1.0"

// ScoreHalfLife 是测速得分的半衰期，越久远的成绩权重越低。
const ScoreHalfLife = 10 * time.Minute
