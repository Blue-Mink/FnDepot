package core

import "strings"

// InferCategory 为没有自带分类的应用（外部 FnDepot 源、飞牛应用中心
// 已安装应用）按项目类型自动推断分类，返回前端 CATEGORIES 的 key。
// 匹配 display_name + appname + description 的小写文本。
// 顺序即优先级：先命中先归类，system 作为兜底类放最后。
func InferCategory(displayName, appName, description string) string {
	raw := strings.ToLower(displayName + " " + appName + " " + description)
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	t := raw
	// 短关键词（ai）按整词匹配，避免 airdrop/airplane 之类误伤
	words := make([]rune, 0, len(t))
	for _, r := range t {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			words = append(words, r)
		} else {
			words = append(words, ' ')
		}
	}
	ws := strings.Fields(string(words))
	hasAIWord := false
	for _, w := range ws {
		if w == "ai" {
			hasAIWord = true
			break
		}
	}

	for _, rule := range []struct {
		key  string
		keys []string
	}{
		{"ai", []string{
			"llm", "gpt", "chat", "llama", "ollama", "deepseek", "gemini",
			"claude", "whisper", "copilot", "stable diffusion", "comfyui",
			"midjourney", "new api", "one api", "api", "tts", "asr", "embedding",
			"大模型", "智能体", "文生图", "图像生成", "语音", "绘画", "对话", "知识库",
		}},
		{"browser", []string{
			"browser", "firefox", "chromium", "safari", "浏览器",
		}},
		{"download", []string{
			"qbittorrent", "qbit", "transmission", "aria2", "aria ", "download",
			"下载", "pt站", " pt ", "网盘", "115", "webdav", "torrent", "magnet",
			"磁力", "同步", "sync", "rsync", "rclone", "alist", "cloud drive",
			"云盘", "迅雷", "seedbox",
		}},
		{"media", []string{
			"jellyfin", "emby", "plex", "navidrome", "sonic ", "komga", "media",
			"媒体", "影视", "影音", "直播", "电视", "播放器", "music", "audio",
			"video", "字幕", "电台", "radio", "nas 影音",
		}},
		{"automation", []string{
			"刮削", "整理", "重命名", "元数据", "metadata", "automation",
			"媒体管理", "影音整理", "自动备份", "auto",
		}},
		{"content", []string{
			"相册", "photo", "immich", "comic", "漫画", "电子书", "ebook",
			"书籍", "book", "library", "影音库", "内容", "导航",
		}},
		{"network", []string{
			"proxy", "代理", "vpn", "tunnel", "隧道", "wireguard", "tailscale",
			"easytier", "zerotier", "ddns", "dns", "反向", "网络", "内网", "穿透",
			"敲门", "knock", "加速", "kms", "adguard", "cloudflare", "speeder",
			"全局代理", "科学",
		}},
		{"system", []string{
			"docker", "container", "容器", "kubernetes", "k8s", "mysql",
			"mariadb", "postgres", "redis", "mongo", "数据库", "database",
			"openjdk", "java", "nodejs", "node.js", "python", "php", "runtime",
			"虚拟机", "virtual", "proxmox", "pve", "备份", "backup", "清理",
			"monitor", "监控", "beszel", "uptime", "terminal", "ssh", "gitea",
			"gitlab", "jenkins", "构建", "build", "registry", "harbor", "环境",
			"面板", "面板管理", "系统", "server", "服务端", "home assistant",
			"haos", "智能家居", "istore", "fn-vm",
		}},
	} {
		for _, k := range rule.keys {
			if strings.Contains(t, k) {
				return rule.key
			}
		}
		// ai 整词单独判断（放在 system 规则之后兜底不了 ai，故并入本循环首项）
		if rule.key == "ai" && hasAIWord {
			return rule.key
		}
	}
	return ""
}
