package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newMooURLs 是修订 2 合并进来的前缀源 URL（去尾斜杠）。
var newMooURLs = map[string]bool{
	"https://gh-proxy.org":                            true,
	"https://hk.gh-proxy.org":                         true,
	"https://cdn.gh-proxy.org":                        true,
	"https://ghproxy.cxkpro.top":                      true,
	"https://git.yylx.win":                            true,
	"https://gitproxy.mrhjx.cn":                       true,
	"https://gh.felicity.ac.cn":                       true,
	"https://wget.la":                                 true,
	"https://github.dpik.top":                         true,
	"https://cors.isteed.cc":                          true,
	"https://gh.dpik.top":                             true,
	"https://github-proxy.memory-echoes.cn":           true,
	"https://hub.conversun.com":                       true,
	"https://gh.ddlc.top":                             true,
}

// legacyMirrors 模拟修订 2 之前的出厂清单（当前清单减去新增项）。
func legacyMirrors() []Mirror {
	all := BuiltinMirrors()
	legacy := make([]Mirror, 0, len(all))
	for _, m := range all {
		url := m.URL
		for i := len(url) - 1; i >= 0 && url[i] == '/'; i-- {
			url = url[:i+1]
		}
		if !newMooURLs[url] {
			legacy = append(legacy, m)
		}
	}
	return legacy
}

// TestBuiltinRevMigratesLegacyConfig：旧配置（修订 0）加载后补齐新出厂源，
// 且修订号落盘。
func TestBuiltinRevMigratesLegacyConfig(t *testing.T) {
	dir := t.TempDir()
	etc := filepath.Join(dir, "etc")
	if err := os.MkdirAll(etc, 0o755); err != nil {
		t.Fatal(err)
	}
	legacy := legacyMirrors()
	if len(legacy) == 0 {
		t.Fatal("legacyMirrors 不应为空")
	}
	cfg := Default()
	cfg.DataDir = dir
	cfg.Mirrors = legacy
	cfg.BuiltinRev = 0
	cfg.Server.Password = "legacy-pw" // 非空，排除"空密码回写"这条落盘路径
	if err := os.MkdirAll(ResolveEtcDir(dir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	cfg2, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	total := len(BuiltinMirrors())
	if len(cfg2.Mirrors) != total {
		t.Fatalf("迁移后源数量 = %d，期望 %d", len(cfg2.Mirrors), total)
	}
	if cfg2.BuiltinRev != BuiltinRevision {
		t.Fatalf("BuiltinRev = %d，期望 %d", cfg2.BuiltinRev, BuiltinRevision)
	}
	// 修订号必须已写回磁盘。
	data, err := os.ReadFile(filepath.Join(ResolveEtcDir(dir), "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !containsRev(data) {
		t.Fatalf("config.yaml 未持久化 builtin_rev=%d:\n%s", BuiltinRevision, string(data))
	}
}

// TestBuiltinRevMigratesDockerUpstreams：修订落后且 Docker 上游还是
// 旧清单（Moo 合并前）的配置，加载后应补齐缺失的出厂上游、
// "official" 保持在末尾，且修订号落盘。
func TestBuiltinRevMigratesDockerUpstreams(t *testing.T) {
	dir := t.TempDir()
	cfg := Default()
	cfg.DataDir = dir
	cfg.Server.Password = "pw"
	cfg.BuiltinRev = BuiltinRevision - 1
	// 修订 2 之前的出厂 Docker 上游清单（9 社区 + official）。
	oldDocker := []string{
		"https://docker.1ms.run",
		"https://docker.m.daocloud.io",
		"https://docker.xuanyuan.me",
		"https://dhub.kubespeed.xyz",
		"https://docker.1panel.live",
		"https://dockermirror.com",
		"https://docker.nastool.de",
		"https://dockerproxy.net",
		"https://dockerhub.jobtechzy.com",
		"official",
	}
	cfg.Docker.Upstreams = oldDocker
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	cfg2, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	want := BuiltinDockerUpstreams()
	if len(cfg2.Docker.Upstreams) != len(want) {
		t.Fatalf("迁移后 Docker 上游数 = %d，期望 %d\n实际: %v", len(cfg2.Docker.Upstreams), len(want), cfg2.Docker.Upstreams)
	}
	for i, u := range want {
		if cfg2.Docker.Upstreams[i] != u {
			t.Fatalf("第 %d 个上游 = %q，期望 %q\n实际: %v", i, cfg2.Docker.Upstreams[i], u, cfg2.Docker.Upstreams)
		}
	}
	if cfg2.BuiltinRev != BuiltinRevision {
		t.Fatalf("BuiltinRev = %d，期望 %d", cfg2.BuiltinRev, BuiltinRevision)
	}
}

// TestBuiltinRevDockerKeepsUserEntries：迁移不得丢弃用户自加的上游，
// 且按 URL 去重（尾斜杠差异不算新源）。
func TestBuiltinRevDockerKeepsUserEntries(t *testing.T) {
	dir := t.TempDir()
	cfg := Default()
	cfg.DataDir = dir
	cfg.Server.Password = "pw"
	cfg.BuiltinRev = BuiltinRevision - 1
	cfg.Docker.Upstreams = []string{
		"https://docker.1ms.run",
		"https://my.mirror.example/", // 用户自定义，带尾斜杠
		"https://m.daocloud.io/",     // 与出厂 m.daocloud.io 同源，尾斜杠差异
		"official",
	}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	cfg2, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	up := cfg2.Docker.Upstreams
	// 出厂 14 条（含 official）+ 1 条用户自定义 = 15；m.daocloud.io 不得重复。
	want := len(BuiltinDockerUpstreams()) + 1
	if len(up) != want {
		t.Fatalf("迁移后上游数 = %d，期望 %d\n实际: %v", len(up), want, up)
	}
	foundUser, foundMoo := false, 0
	for i, u := range up {
		switch {
		case strings.TrimRight(u, "/") == "https://my.mirror.example":
			foundUser = true
		case strings.TrimRight(u, "/") == "https://m.daocloud.io":
			foundMoo++
		}
		if i == len(up)-1 && u != "official" {
			t.Fatalf("末尾必须是 official，实际 %q", u)
		}
	}
	if !foundUser {
		t.Fatalf("用户自定义上游丢失: %v", up)
	}
	if foundMoo != 1 {
		t.Fatalf("m.daocloud.io 出现 %d 次（期望 1，尾斜杠差异应去重）: %v", foundMoo, up)
	}
}

// TestBuiltinRevRespectsUserDeletion：迁移完成后用户删除的出厂源
// 不会被再次复活。
func TestBuiltinRevRespectsUserDeletion(t *testing.T) {
	dir := t.TempDir()
	cfg := Default()
	cfg.DataDir = dir
	cfg.Server.Password = "pw"
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	cfg2, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg2.BuiltinRev != BuiltinRevision {
		t.Fatalf("新配置 BuiltinRev = %d，期望 %d", cfg2.BuiltinRev, BuiltinRevision)
	}
	// 用户删掉一个出厂源后保存，再次加载不应复活。
	kept := make([]Mirror, 0, len(cfg2.Mirrors)-1)
	deleted := 0
	for _, m := range cfg2.Mirrors {
		if m.ID == "wget-la" {
			deleted++
			continue
		}
		kept = append(kept, m)
	}
	if deleted != 1 {
		t.Fatalf("期望删掉 wget-la，实际删掉 %d 条", deleted)
	}
	cfg2.Mirrors = kept
	if err := cfg2.Save(); err != nil {
		t.Fatal(err)
	}
	cfg3, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg3.Mirrors) != len(cfg2.Mirrors) {
		t.Fatalf("用户删除后数量 = %d，期望保持 %d（不该复活）", len(cfg3.Mirrors), len(cfg2.Mirrors))
	}
	for _, m := range cfg3.Mirrors {
		if m.ID == "wget-la" {
			t.Fatal("wget-la 被复活了")
		}
	}
}

// containsRev 检查配置是否持久化了当前出厂修订号。
func containsRev(data []byte) bool {
	return contains(string(data), fmt.Sprintf("builtin_rev: %d", BuiltinRevision))
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
