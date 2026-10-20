package api

import (
	"net/http"
	"runtime"
)

// 关于本软件的静态信息。作者、发布者与仓库信息在此维护。
const (
	aboutName   = "Dog-dev 加速器"
	aboutAuthor = "MisiteQ"
	// aboutAuthorURL 是开发者项目主页（关于页「开发者」一行点击跳转）。
	aboutAuthorURL = "https://github.com/MisiteQ"
	aboutGitHub    = "https://github.com/MisiteQ"
	aboutRepo      = "https://github.com/MisiteQ/github-plus-plus"
	aboutLanguage  = "Go"
	aboutLicense   = "MIT"
	// 发布者：Dog-dev 版本的打包与发布方。
	aboutPublisher    = "Blue-Mink"
	aboutPublisherURL = "https://github.com/Blue-Mink/dog-dev"
	aboutDesc         = "为飞牛 fnOS 打造的 GitHub 加速应用，基于开源项目 GitHub++ 二次开发：智能测速自动选择最优加速通道，" +
		"支持 GitHub 反代加速、DNS 优选 hosts 加速、HTTPS 网页加速与 Docker 镜像拉取加速，" +
		"内置 iOS 风格 Web 控制台，Go 单二进制开箱即用。"
)

// handleAbout 返回"关于"信息：作者、技术栈、仓库地址与致谢。
func (s *Server) handleAbout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "不支持的方法")
		return
	}

	writeOK(w, map[string]any{
		"name":          aboutName,
		"version":       Version,
		"author":        aboutAuthor,
		"author_url":    aboutAuthorURL,
		"github":        aboutGitHub,
		"repo":          aboutRepo,
		"publisher":     aboutPublisher,
		"publisher_url": aboutPublisherURL,
		"language":      aboutLanguage,
		"go_version":    runtime.Version(),
		"license":       aboutLicense,
		"description":   aboutDesc,

		// 安全设计说明：每一条都与代码实际行为逐项核对过（1.3.0）。
		"safety": []map[string]string{
			{"point": "仅监听自身端口", "detail": "加速代理与控制台只监听配置声明的端口（默认 37710 / 37717），不修改系统防火墙规则，不做端口转发。"},
			{"point": "不劫持系统流量", "detail": "不启用透明网关模式，不改动 iptables / NAT / DNS 服务，也不接管路由；除本机 hosts 托管区（见下条）外，不改变 NAS 与局域网其他设备的联网路径。"},
			{"point": "hosts 仅动托管区", "detail": "hosts 优选默认开启，只在系统 hosts 文件的『Dog-dev 加速托管区』标记内写入记录，其他条目原样保留；可在设置页关闭，停止服务时自动清理托管区。"},
			{"point": "加速随开关启停", "detail": "代理型加速（HTTP 代理、Docker 镜像）只有客户端主动指向 NAS 后才经过本服务；系统代理默认关闭，开启时会写入 /etc/profile.d/ghpp-proxy.sh 并可用同一开关撤销。"},
			{"point": "上游自动降级", "detail": "镜像源或上游不可用时自动切换到下一通道直至官方直连，不会因加速器自身故障导致网络不可用。"},
			{"point": "不上报任何数据", "detail": "不包含统计、遥测或云端账号逻辑；配置、日志与证书全部留在本机，卸载应用后数据目录可由用户自行删除。"},
			{"point": "控制台默认免登录", "detail": "1.3.0 起控制台默认不要求登录（监听 0.0.0.0:37717），同一局域网内的设备可直接打开并修改配置；如需恢复登录门，在配置文件中设置 server.auth_enabled: true 即可（用户名密码沿用原设置）。"},
		},

		// 致谢：本系统设计参考了以下优秀的开源项目。
		"references": []map[string]string{
			{"name": "hunshcn/gh-proxy", "url": "https://github.com/hunshcn/gh-proxy", "note": "GitHub 加速的经典实现，release/archive/raw/gist 的 URL 识别与改写规则参考了该项目"},
			{"name": "jsDelivr", "url": "https://github.com/jsdelivr/jsdelivr", "note": "免费开源的全球 CDN，作为 raw 文件的优选加速通道之一"},
			{"name": "DaoCloud 公益镜像", "url": "https://github.com/DaoCloud/public-image-mirror", "note": "Docker Hub 与 ghcr/gcr/quay 的免费镜像服务，作为 Docker 加速的内置上游之一"},
			{"name": "ghproxy 系列镜像站", "url": "https://github.com/topics/github-proxy", "note": "ghproxy.net、ghfast.top 等社区公益中转站，作为内置加速源"},
			{"name": "Go 语言", "url": "https://go.dev/", "note": "本项目使用 Go 编写，单二进制、交叉编译、内存占用低，适合 NAS 常驻运行"},
		},

		// 免费开源资源说明。
		"open_source": "本软件的默认加速源均为社区免费公益服务；本地 CA 证书由程序自动生成（免费，无需购买），" +
			"用户也可以导入自己的证书与自定义加速源。",
	})
}
