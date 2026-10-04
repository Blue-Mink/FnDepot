package config

import "time"

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
const BuiltinRevision = 5

// BuiltinMirrors 返回出厂内置的 GitHub 加速源列表。
//
// 这些源均为社区公开的公益中转服务，可用性随时间波动，
// 因此程序会在运行期持续测速并按实测结果动态排序，
// 用户也可以在控制台中增删自己的私有源。
func BuiltinMirrors() []Mirror {
	return []Mirror{
		// ---- 前缀型中转站：覆盖 releases / archive / raw / git clone ----
		{ID: "ghproxy-net", Name: "ghproxy.net", URL: "https://ghproxy.net/", Kind: KindPrefix, Enabled: true, Weight: 1, Note: "日本节点，覆盖面广"},
		{ID: "ghfast-top", Name: "ghfast.top", URL: "https://ghfast.top/", Kind: KindPrefix, Enabled: true, Weight: 1, Note: "韩国首尔节点"},
		{ID: "gh-proxy-com", Name: "gh-proxy.com", URL: "https://gh-proxy.com/", Kind: KindPrefix, Enabled: true, Weight: 1, Note: "韩国首尔节点，大文件表现好"},
		{ID: "githubfast", Name: "githubfast.com", URL: "https://githubfast.com/", Kind: KindPrefix, Enabled: true, Weight: 1, Note: "韩国首尔节点"},
		{ID: "ghproxy-vip", Name: "ghproxy.vip", URL: "https://ghproxy.vip/", Kind: KindPrefix, Enabled: true, Weight: 0.9, Note: "多线路，支持 LFS"},
		{ID: "moeyy", Name: "github.moeyy.xyz", URL: "https://github.moeyy.xyz/", Kind: KindPrefix, Enabled: true, Weight: 0.9, Note: "美国节点"},
		{ID: "hub-gitmirror", Name: "hub.gitmirror.com", URL: "https://hub.gitmirror.com/", Kind: KindPrefix, Enabled: true, Weight: 0.9, Note: "适合大体积 Release"},
		{ID: "gh-llkk", Name: "gh.llkk.cc", URL: "https://gh.llkk.cc/", Kind: KindPrefix, Enabled: true, Weight: 0.8, Note: "国内 CDN"},
		{ID: "moeyy-xyz", Name: "moeyy.xyz", URL: "https://moeyy.xyz/", Kind: KindPrefix, Enabled: true, Weight: 0.8, Note: "多线路社区中转"},
		{ID: "ghps-cc", Name: "ghps.cc", URL: "https://ghps.cc/", Kind: KindPrefix, Enabled: true, Weight: 0.8, Note: "社区公益中转"},
		{ID: "ghp-999", Name: "ghp.99988866.xyz", URL: "https://ghp.99988866.xyz/", Kind: KindPrefix, Enabled: true, Weight: 0.8, Note: "社区公益中转"},
		{ID: "gh-cdn-zz", Name: "gh.cdn.zz.zz", URL: "https://gh.cdn.zz.zz/", Kind: KindPrefix, Enabled: true, Weight: 0.7, Note: "国内 CDN 线路"},
		{ID: "gh-ciiiii", Name: "gh.ciiiii.com", URL: "https://gh.ciiiii.com/", Kind: KindPrefix, Enabled: true, Weight: 0.7, Note: "社区公益中转"},
		{ID: "gh-aas123", Name: "gh.aas123.com", URL: "https://gh.aas123.com/", Kind: KindPrefix, Enabled: true, Weight: 0.7, Note: "社区公益中转"},
		{ID: "gh-api999", Name: "gh.api.99988866.com", URL: "https://gh.api.99988866.com/", Kind: KindPrefix, Enabled: true, Weight: 0.7, Note: "社区公益中转"},
		{ID: "gh-dd320", Name: "gh.dd320.org", URL: "https://gh.dd320.org/", Kind: KindPrefix, Enabled: true, Weight: 0.7, Note: "社区公益中转"},
		{ID: "ghps-hk", Name: "ghps.hk", URL: "https://ghps.hk/", Kind: KindPrefix, Enabled: true, Weight: 0.7, Note: "香港线路"},
		{ID: "ghps-us", Name: "ghps.us", URL: "https://ghps.us/", Kind: KindPrefix, Enabled: true, Weight: 0.7, Note: "美国线路"},
		{ID: "ght-999", Name: "ght.99988866.com", URL: "https://ght.99988866.com/", Kind: KindPrefix, Enabled: true, Weight: 0.7, Note: "社区公益中转"},
		// ---- Moo 应用实测源（2026-10-02 去重合并，前缀形态均已验证）----
		{ID: "gh-proxy-org", Name: "gh-proxy.org", URL: "https://gh-proxy.org/", Kind: KindPrefix, Enabled: true, Weight: 0.9, Note: "GH-Proxy 主站，多线路"},
		{ID: "gh-proxy-hk", Name: "hk.gh-proxy.org", URL: "https://hk.gh-proxy.org/", Kind: KindPrefix, Enabled: true, Weight: 0.8, Note: "GH-Proxy 香港节点"},
		{ID: "cdn-ghproxy", Name: "cdn.gh-proxy.org", URL: "https://cdn.gh-proxy.org/", Kind: KindPrefix, Enabled: true, Weight: 0.8, Note: "GH-Proxy CDN 节点"},
		{ID: "ghproxy-cxkpro", Name: "ghproxy.cxkpro.top", URL: "https://ghproxy.cxkpro.top/", Kind: KindPrefix, Enabled: true, Weight: 0.7, Note: "社区公益中转"},
		{ID: "yylx", Name: "git.yylx.win", URL: "https://git.yylx.win/", Kind: KindPrefix, Enabled: true, Weight: 0.7, Note: "社区公益中转"},
		{ID: "gitproxy-mrhjx", Name: "gitproxy.mrhjx.cn", URL: "https://gitproxy.mrhjx.cn/", Kind: KindPrefix, Enabled: true, Weight: 0.7, Note: "社区公益中转"},
		{ID: "felicity", Name: "gh.felicity.ac.cn", URL: "https://gh.felicity.ac.cn/", Kind: KindPrefix, Enabled: true, Weight: 0.6, Note: "高校公益镜像"},
		{ID: "wget-la", Name: "wget.la", URL: "https://wget.la/", Kind: KindPrefix, Enabled: true, Weight: 0.7, Note: "社区公益中转，大文件友好"},
		{ID: "github-dpik", Name: "github.dpik.top", URL: "https://github.dpik.top/", Kind: KindPrefix, Enabled: true, Weight: 0.6, Note: "社区公益中转"},
		{ID: "cors-isteed", Name: "cors.isteed.cc", URL: "https://cors.isteed.cc/", Kind: KindPrefix, Enabled: true, Weight: 0.7, Note: "社区公益中转（带 CORS）"},
		{ID: "gh-dpik", Name: "gh.dpik.top", URL: "https://gh.dpik.top/", Kind: KindPrefix, Enabled: true, Weight: 0.7, Note: "社区公益中转"},
		{ID: "memory-echoes", Name: "github-proxy.memory-echoes.cn", URL: "https://github-proxy.memory-echoes.cn/", Kind: KindPrefix, Enabled: true, Weight: 0.7, Note: "社区公益中转"},
		{ID: "hub-conversun", Name: "hub.conversun.com", URL: "https://hub.conversun.com/", Kind: KindPrefix, Enabled: true, Weight: 0.7, Note: "社区公益中转"},
		{ID: "gh-ddlc", Name: "gh.ddlc.top", URL: "https://gh.ddlc.top/", Kind: KindPrefix, Enabled: true, Weight: 0.6, Note: "社区公益中转"},
		// ---- 用户提供的社区前缀源（2026-10-03 全量合入，25 条）----
		{ID: "edgeone-ghproxy", Name: "edgeone.gh-proxy.org", URL: "https://edgeone.gh-proxy.org/", Kind: KindPrefix, Enabled: true, Weight: 0.8, Note: "GH-Proxy EdgeOne 节点"},
		{ID: "gh-b52m", Name: "gh.b52m.cn", URL: "https://gh.b52m.cn/", Kind: KindPrefix, Enabled: true, Weight: 0.7, Note: "社区公益中转"},
		{ID: "gh-h233", Name: "gh.h233.eu.org", URL: "https://gh.h233.eu.org/", Kind: KindPrefix, Enabled: true, Weight: 0.6, Note: "社区公益中转"},
		{ID: "rapidgit-jjda", Name: "rapidgit.jjda.de5.net", URL: "https://rapidgit.jjda.de5.net/", Kind: KindPrefix, Enabled: true, Weight: 0.6, Note: "社区公益中转"},
		{ID: "ghproxy-it", Name: "ghproxy.it", URL: "https://ghproxy.it/", Kind: KindPrefix, Enabled: true, Weight: 0.7, Note: "意大利节点"},
		{ID: "github-boki", Name: "github.boki.moe", URL: "https://github.boki.moe/", Kind: KindPrefix, Enabled: true, Weight: 0.7, Note: "社区公益中转"},
		{ID: "gh-jasonzeng", Name: "gh.jasonzeng.dev", URL: "https://gh.jasonzeng.dev/", Kind: KindPrefix, Enabled: true, Weight: 0.6, Note: "社区公益中转"},
		{ID: "gh-monlor", Name: "gh.monlor.com", URL: "https://gh.monlor.com/", Kind: KindPrefix, Enabled: true, Weight: 0.7, Note: "社区公益中转"},
		{ID: "github-tbedu", Name: "github.tbedu.top", URL: "https://github.tbedu.top/", Kind: KindPrefix, Enabled: true, Weight: 0.6, Note: "社区公益中转"},
		{ID: "github-geekery", Name: "github.geekery.cn", URL: "https://github.geekery.cn/", Kind: KindPrefix, Enabled: true, Weight: 0.7, Note: "社区公益中转"},
		{ID: "github-ednovas", Name: "github.ednovas.xyz", URL: "https://github.ednovas.xyz/", Kind: KindPrefix, Enabled: true, Weight: 0.7, Note: "社区公益中转"},
		{ID: "ghfile-geekertao", Name: "ghfile.geekertao.top", URL: "https://ghfile.geekertao.top/", Kind: KindPrefix, Enabled: true, Weight: 0.7, Note: "社区公益中转"},
		{ID: "ghp-keleyaa", Name: "ghp.keleyaa.com", URL: "https://ghp.keleyaa.com/", Kind: KindPrefix, Enabled: true, Weight: 0.7, Note: "社区公益中转"},
		{ID: "gh-chjina", Name: "gh.chjina.com", URL: "https://gh.chjina.com/", Kind: KindPrefix, Enabled: true, Weight: 0.7, Note: "社区公益中转"},
		{ID: "ghpxy-hwinzniej", Name: "ghpxy.hwinzniej.top", URL: "https://ghpxy.hwinzniej.top/", Kind: KindPrefix, Enabled: true, Weight: 0.6, Note: "社区公益中转"},
		{ID: "cdn-crashmc", Name: "cdn.crashmc.com", URL: "https://cdn.crashmc.com/", Kind: KindPrefix, Enabled: true, Weight: 0.7, Note: "社区 CDN 中转"},
		{ID: "gh-xxooo", Name: "gh.xxooo.cf", URL: "https://gh.xxooo.cf/", Kind: KindPrefix, Enabled: true, Weight: 0.6, Note: "社区公益中转"},
		{ID: "gh-idayer", Name: "gh.idayer.com", URL: "https://gh.idayer.com/", Kind: KindPrefix, Enabled: true, Weight: 0.7, Note: "社区公益中转"},
		{ID: "down-npee", Name: "down.npee.cn", URL: "https://down.npee.cn/", Kind: KindPrefix, Enabled: true, Weight: 0.6, Note: "社区公益中转"},
		{ID: "raw-ihtw", Name: "raw.ihtw.moe", URL: "https://raw.ihtw.moe/", Kind: KindPrefix, Enabled: true, Weight: 0.6, Note: "社区公益中转"},
		{ID: "gh-zwy", Name: "gh.zwy.one", URL: "https://gh.zwy.one/", Kind: KindPrefix, Enabled: true, Weight: 0.7, Note: "社区公益中转"},
		{ID: "ghproxy-monkeyray", Name: "ghproxy.monkeyray.net", URL: "https://ghproxy.monkeyray.net/", Kind: KindPrefix, Enabled: true, Weight: 0.6, Note: "社区公益中转"},
		{ID: "hub-glowp", Name: "hub.glowp.xyz", URL: "https://hub.glowp.xyz/", Kind: KindPrefix, Enabled: true, Weight: 0.7, Note: "社区公益中转"},
		{ID: "gh-catmak", Name: "gh.catmak.name", URL: "https://gh.catmak.name/", Kind: KindPrefix, Enabled: true, Weight: 0.7, Note: "社区公益中转"},
		{ID: "g-blfrp", Name: "g.blfrp.cn", URL: "https://g.blfrp.cn/", Kind: KindPrefix, Enabled: true, Weight: 0.7, Note: "社区公益中转"},
		// ---- 路径抓取型源（xget 形态，仅 raw 文件）----
		{ID: "xget-xi-xu", Name: "xget.xi-xu.me", URL: "https://xget.xi-xu.me/gh", Kind: KindPathFetch, Enabled: true, Weight: 0.7, Note: "路径抓取型中转，仅处理 raw 文件"},
		{ID: "jsdelivr", Name: "jsDelivr CDN", URL: "https://fastly.jsdelivr.net/", Kind: KindRawCDN, Enabled: true, Weight: 1, Note: "全球 CDN，raw 文件最快"},
		{ID: "raw-kkgithub", Name: "raw.kkgithub.com", URL: "https://raw.kkgithub.com/", Kind: KindRawCDN, Enabled: true, Weight: 0.8, Note: "香港节点"},

		// ---- Git 仓库镜像 ----
		{ID: "gitclone", Name: "gitclone.com", URL: "https://gitclone.com/", Kind: KindGitClone, Enabled: true, Weight: 0.9, Note: "国内 clone 镜像，首次较慢后续走缓存"},
		{ID: "kkgithub", Name: "kkgithub.com", URL: "https://kkgithub.com/", Kind: KindGitClone, Enabled: true, Weight: 0.8, Note: "香港节点，GitHub 全站镜像"},

		// ---- 官方直连：始终保留作为兜底 ----
		{ID: "origin", Name: "GitHub 官方直连", URL: "https://github.com/", Kind: KindDirect, Enabled: true, Weight: 0.5, Note: "兜底源，网络通畅时最快"},
	}
}

// BuiltinDockerUpstreams 返回出厂内置的 Docker 上游列表。
//
// 依次为社区公益镜像站，最后 "official" 表示 Docker Hub 官方源（registry-1.docker.io），
// 作为兜底。请求按顺序尝试，上游不可用时自动切换。
func BuiltinDockerUpstreams() []string {
	return []string{
		"https://docker.1ms.run",
		"https://docker.m.daocloud.io",
		"https://docker.xuanyuan.me",
		"https://dhub.kubespeed.xyz",
		"https://docker.1panel.live",
		"https://dockermirror.com",
		"https://docker.nastool.de",
		"https://dockerproxy.net",
		"https://dockerhub.jobtechzy.com",
		// ---- Moo 应用内置源（2026-10-02 去重合并）----
		"https://registry.cyou",
		"https://m.daocloud.io",
		"https://hub.rat.dev",
		"https://ghcr.nju.edu.cn",
		"official",
	}
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
