package config

import (
	"os"
	"path/filepath"
	"testing"
)

// kspeeder 下载链接的默认值与迁移：
// 旧的出厂默认地址在升级后要自动跟进到当前发布版，用户自定义的地址一律不动。

func writeConfigWithDownloadURL(t *testing.T, dir, url string) {
	t.Helper()
	cfg := Default()
	cfg.DataDir = dir
	cfg.KSpeeder.DownloadURL = url
	cfg.Server.Password = "keep-me" // 非空，避开"空密码回填默认值"的路径
	if err := os.MkdirAll(ResolveEtcDir(dir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
}

func TestDefaultKSpeederDownloadURLIsReleased(t *testing.T) {
	cfg := Default()
	if cfg.KSpeeder.DownloadURL != kspeederAppDownloadURL {
		t.Fatalf("默认下载地址 = %q，期望 %q", cfg.KSpeeder.DownloadURL, kspeederAppDownloadURL)
	}
	// 下载包名要指向已发布的 0.8.2（与 km.RecommendedEngineMin 对齐，
	// 装了更低版本的包等于没有分段下载/节点竞速）。
	if got := filepath.Base(kspeederAppDownloadURL); got != "kspeeder-0.8.2-fnos-amd64.fpk" {
		t.Fatalf("下载包名 = %q，期望 kspeeder-0.8.2-fnos-amd64.fpk", got)
	}
}

func TestKSpeederLegacyDownloadURLMigrates(t *testing.T) {
	for legacy := range legacyKSpeederDownloadURLs {
		dir := t.TempDir()
		writeConfigWithDownloadURL(t, dir, legacy)
		cfg, err := Load(dir)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.KSpeeder.DownloadURL != kspeederAppDownloadURL {
			t.Fatalf("旧默认值 %q 未迁移，加载后仍是 %q", legacy, cfg.KSpeeder.DownloadURL)
		}
	}
}

func TestKSpeederCustomDownloadURLKept(t *testing.T) {
	const custom = "https://mirror.example.com/kspeeder-0.8.0-fnos-amd64.fpk"
	dir := t.TempDir()
	writeConfigWithDownloadURL(t, dir, custom)
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.KSpeeder.DownloadURL != custom {
		t.Fatalf("用户自定义地址被改动：%q", cfg.KSpeeder.DownloadURL)
	}
}
