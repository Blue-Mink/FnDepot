package config

import (
	"reflect"
	"testing"
)

func TestGitHubFallbackPrefixes_StaticWithoutSmart(t *testing.T) {
	// 未注册钩子：选定在前、其余按声明顺序（实测延迟序）、直连兜底
	got := GitHubFallbackPrefixes("gh-proxy", Config{})
	wantSelectedFirst := GitHubMirrorPrefix("gh-proxy", Config{})
	if got[0] != wantSelectedFirst {
		t.Fatalf("selected mirror should be first, got %v", got)
	}
	if len(got) < 2 || got[len(got)-1] != "" {
		t.Fatalf("direct (\"\") should be last resort, got %v", got)
	}
}

func TestGitHubFallbackPrefixes_DirectFirst(t *testing.T) {
	got := GitHubFallbackPrefixes("direct", Config{})
	if got[0] != "" {
		t.Fatalf("direct should be first, got %v", got)
	}
}

func TestGitHubFallbackPrefixes_AutoUsesRankOrder(t *testing.T) {
	// 模拟监测：ranker 给出与健康度相关的顺序（与声明顺序不同）
	ordered := []string{"yylx", "gh-proxy-hk", "gh-proxy", "cors-isteed", "gh-ddlc"}
	RegisterGitHubSmart(GitHubSmart{
		Rank: func(cfg Config) []string { return ordered },
		Best: func(cfg Config) string { return "https://yylx.win/" },
		Degraded: func(key string) bool { return key == "gh-ddlc" },
	})
	defer RegisterGitHubSmart(GitHubSmart{})

	got := GitHubFallbackPrefixes("auto", Config{})
	byKey := map[string]bool{"yylx": true}
	wantFirst := "https://git.yylx.win/"
	if got[0] != wantFirst {
		t.Fatalf("auto chain should start with best mirror, got %v", got)
	}
	_ = byKey
	if got[len(got)-1] != "" {
		t.Fatalf("auto chain should end with direct, got %v", got)
	}
	// auto 链里不应出现重复前缀
	seen := map[string]bool{}
	for _, p := range got {
		if p != "" && seen[p] {
			t.Fatalf("duplicate prefix %q in %v", p, got)
		}
		seen[p] = true
	}
}

func TestGitHubFallbackPrefixes_DegradedSelectedSinks(t *testing.T) {
	RegisterGitHubSmart(GitHubSmart{
		Rank: func(cfg Config) []string {
			return []string{"yylx", "gh-proxy-hk", "gh-proxy", "cors-isteed", "gh-ddlc"}
		},
		Best:     func(cfg Config) string { return "https://git.yylx.win/" },
		Degraded: func(key string) bool { return key == "gh-ddlc" },
	})
	defer RegisterGitHubSmart(GitHubSmart{})

	got := GitHubFallbackPrefixes("gh-ddlc", Config{})
	// 降级源不应在链首
	if got[0] == "https://gh.ddlc.top/" {
		t.Fatalf("degraded mirror must not be first: %v", got)
	}
	// 但仍保留在链里（可能恢复）
	found := false
	for _, p := range got {
		if p == "https://gh.ddlc.top/" {
			found = true
		}
	}
	if !found {
		t.Fatalf("degraded mirror should still be in chain: %v", got)
	}
}

func TestGitHubMirrorPrefix_AutoAndDegraded(t *testing.T) {
	RegisterGitHubSmart(GitHubSmart{
		Rank:     func(cfg Config) []string { return nil },
		Best:     func(cfg Config) string { return "https://git.yylx.win/" },
		Degraded: func(key string) bool { return key == "gh-ddlc" },
	})
	defer RegisterGitHubSmart(GitHubSmart{})

	if got := GitHubMirrorPrefix("auto", Config{}); got != "https://git.yylx.win/" {
		t.Fatalf("auto should resolve to best stable mirror, got %q", got)
	}
	if got := GitHubMirrorPrefix("gh-ddlc", Config{}); got != "https://git.yylx.win/" {
		t.Fatalf("degraded selection should resolve to best stable mirror, got %q", got)
	}
	if got := GitHubMirrorPrefix("gh-proxy", Config{}); got != "https://gh-proxy.com/" {
		t.Fatalf("healthy selection should keep its own URL, got %q", got)
	}
	if got := GitHubMirrorPrefix("custom", Config{CustomGitHubMirror: "https://my.mirror/"}); got != "https://my.mirror/" {
		t.Fatalf("custom should keep custom URL, got %q", got)
	}
	if got := GitHubMirrorPrefix("direct", Config{}); got != "" {
		t.Fatalf("direct should resolve to empty prefix, got %q", got)
	}
}

func TestGitHubFallbackPrefixes_CustomFirst(t *testing.T) {
	RegisterGitHubSmart(GitHubSmart{
		Rank: func(cfg Config) []string {
			return []string{"yylx", "gh-proxy-hk", "gh-proxy", "cors-isteed", "gh-ddlc"}
		},
		Best:     func(cfg Config) string { return "https://git.yylx.win/" },
		Degraded: func(key string) bool { return false },
	})
	defer RegisterGitHubSmart(GitHubSmart{})

	got := GitHubFallbackPrefixes("custom", Config{CustomGitHubMirror: "https://my.mirror/"})
	if got[0] != "https://my.mirror/" {
		t.Fatalf("custom mirror should be first, got %v", got)
	}
	if !reflect.DeepEqual([]string{got[len(got)-1]}, []string{""}) {
		t.Fatalf("chain should end with direct, got %v", got)
	}
}

// ---- Docker 镜像加速智能顺序（与 GitHub 同构） ----

const (
	dkRankOrdered = "docker-1ms"
	dkRankURL     = "docker.1ms.run/"
	dkDegradedKey = "ratdev"
	dkDegradedURL = "hub.rat.dev/"
)

func TestDockerFallbackPrefixes_StaticWithoutSmart(t *testing.T) {
	// 未注册钩子：选定在前、其余按声明顺序、直连兜底
	got := DockerFallbackPrefixes("daocloud", Config{})
	if got[0] != "m.daocloud.io/" {
		t.Fatalf("selected mirror should be first, got %v", got)
	}
	if got[len(got)-1] != "" {
		t.Fatalf("direct (\"\") should be last resort, got %v", got)
	}
	// nju-ghcr 无 URL，不应进入前缀链
	for _, p := range got {
		if p == "" {
			continue
		}
		if !isRealDockerMirrorURL(p) {
			t.Fatalf("unexpected prefix %q in %v", p, got)
		}
	}
}

func isRealDockerMirrorURL(u string) bool {
	for _, m := range DockerMirrorOptions() {
		if m.URL == u {
			return true
		}
	}
	return false
}

func TestDockerFallbackPrefixes_DirectFirst(t *testing.T) {
	got := DockerFallbackPrefixes("direct", Config{})
	if got[0] != "" {
		t.Fatalf("direct should be first, got %v", got)
	}
}

func TestDockerFallbackPrefixes_AutoUsesRankOrder(t *testing.T) {
	ordered := []string{dkRankOrdered, "daocloud", "1panel", dkDegradedKey}
	RegisterDockerSmart(DockerSmart{
		Rank:     func(cfg Config) []string { return ordered },
		Best:     func(cfg Config) string { return dkRankURL },
		Degraded: func(key string) bool { return key == dkDegradedKey },
	})
	defer RegisterDockerSmart(DockerSmart{})

	got := DockerFallbackPrefixes("auto", Config{})
	if got[0] != dkRankURL {
		t.Fatalf("auto chain should start with best mirror, got %v", got)
	}
	if got[len(got)-1] != "" {
		t.Fatalf("auto chain should end with direct, got %v", got)
	}
	seen := map[string]bool{}
	for _, p := range got {
		if p != "" && seen[p] {
			t.Fatalf("duplicate prefix %q in %v", p, got)
		}
		seen[p] = true
	}
}

func TestDockerFallbackPrefixes_DegradedSelectedSinks(t *testing.T) {
	RegisterDockerSmart(DockerSmart{
		Rank:     func(cfg Config) []string { return []string{dkRankOrdered, "daocloud", dkDegradedKey} },
		Best:     func(cfg Config) string { return dkRankURL },
		Degraded: func(key string) bool { return key == dkDegradedKey },
	})
	defer RegisterDockerSmart(DockerSmart{})

	got := DockerFallbackPrefixes(dkDegradedKey, Config{})
	if got[0] == dkDegradedURL {
		t.Fatalf("degraded mirror must not be first: %v", got)
	}
	found := false
	for _, p := range got {
		if p == dkDegradedURL {
			found = true
		}
	}
	if !found {
		t.Fatalf("degraded mirror should still be in chain: %v", got)
	}
}

func TestDockerMirrorPrefix_AutoAndDegraded(t *testing.T) {
	RegisterDockerSmart(DockerSmart{
		Rank:     func(cfg Config) []string { return nil },
		Best:     func(cfg Config) string { return dkRankURL },
		Degraded: func(key string) bool { return key == dkDegradedKey },
	})
	defer RegisterDockerSmart(DockerSmart{})

	if got := DockerMirrorPrefix("auto", Config{}); got != dkRankURL {
		t.Fatalf("auto should resolve to best stable mirror, got %q", got)
	}
	if got := DockerMirrorPrefix(dkDegradedKey, Config{}); got != dkRankURL {
		t.Fatalf("degraded selection should resolve to best stable mirror, got %q", got)
	}
	if got := DockerMirrorPrefix("daocloud", Config{}); got != "m.daocloud.io/" {
		t.Fatalf("healthy selection should keep its own URL, got %q", got)
	}
	if got := DockerMirrorPrefix("custom", Config{CustomDockerMirror: "my.mirror/"}); got != "my.mirror/" {
		t.Fatalf("custom should keep custom URL, got %q", got)
	}
	if got := DockerMirrorPrefix("direct", Config{}); got != "" {
		t.Fatalf("direct should resolve to empty prefix, got %q", got)
	}
}

func TestDockerFallbackPrefixes_CustomFirst(t *testing.T) {
	RegisterDockerSmart(DockerSmart{
		Rank:     func(cfg Config) []string { return []string{dkRankOrdered, "daocloud"} },
		Best:     func(cfg Config) string { return dkRankURL },
		Degraded: func(key string) bool { return false },
	})
	defer RegisterDockerSmart(DockerSmart{})

	got := DockerFallbackPrefixes("custom", Config{CustomDockerMirror: "my.mirror/"})
	if got[0] != "my.mirror/" {
		t.Fatalf("custom mirror should be first, got %v", got)
	}
	if got[len(got)-1] != "" {
		t.Fatalf("chain should end with direct, got %v", got)
	}
}

func TestDockerFallbackPrefixes_NJUGhostMirrorChain(t *testing.T) {
	// nju-ghcr 无 URL（host 改写型）：不进前缀链，docker.io 镜像走真实
	// 镜像回退链、直连沉底
	got := DockerFallbackPrefixes("nju-ghcr", Config{})
	if got[0] != "m.daocloud.io/" {
		t.Fatalf("NJU selection should lead with first real mirror, got %v", got)
	}
	if got[len(got)-1] != "" {
		t.Fatalf("chain should end with direct, got %v", got)
	}
}

// ---- KSpeeder 本地 registry 型镜像 ----

func TestKSpeederRefs(t *testing.T) {
	m, ok := DockerMirrorByKey("kspeeder")
	if !ok {
		t.Fatal("kspeeder mirror not registered")
	}
	cases := []struct {
		canonical string
		want      []string
	}{
		{"docker.io/busybox:latest", []string{"127.0.0.1:5443/library/busybox:latest"}},          // 官方单段补 library/
		{"docker.io/linuxserver/radarr:10.8.3", []string{"127.0.0.1:5443/linuxserver/radarr:10.8.3"}}, // 命名空间原样
		{"docker.io/busybox@sha256:abcd1234", []string{"127.0.0.1:5443/library/busybox@sha256:abcd1234"}},
		{"docker.io/xream/sub-store:2.36.35", []string{"127.0.0.1:5443/xream/sub-store:2.36.35"}},
		{"ghcr.io/paperless-ngx/paperless-ngx:3.1.1", nil}, // KSpeeder 不代理 ghcr
		{"quay.io/coreos/etcd:v3.5", nil},                  // 其它源不支持
	}
	for _, c := range cases {
		got := RewriteRefsForMirror(m, c.canonical)
		if len(got) != len(c.want) || (len(got) > 0 && got[0] != c.want[0]) {
			t.Fatalf("kspeederRefs(%q) = %v, want %v", c.canonical, got, c.want)
		}
	}
}

func TestKSpeederPrefixAndChain(t *testing.T) {
	if got := DockerMirrorPrefix("kspeeder", Config{}); got != KSpeederPrefix {
		t.Fatalf("kspeeder single-prefix = %q, want %q", got, KSpeederPrefix)
	}
	// 选定 kspeeder：前缀链只含 URL 型镜像 + 直连兜底（kspeeder ref 走 rewrite 槽位）
	got := DockerFallbackPrefixes("kspeeder", Config{})
	if got[len(got)-1] != "" {
		t.Fatalf("chain should end with direct, got %v", got)
	}
	for _, p := range got {
		if p == KSpeederPrefix {
			t.Fatalf("kspeeder address must not appear as a URL-prefix chain entry: %v", got)
		}
	}
}
