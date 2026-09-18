package core

import (
	"testing"

	"fnos-store/internal/source"
)

func TestDedupeIdenticalApps(t *testing.T) {
	a := AppInfo{AppName: "komga", Source: "fnos-apps", SHA256: "aaa", SizeBytes: 100, DownloadCount: 50}
	// 同名（大小写不同）同哈希、元数据更少的转载 → 应被去掉
	b := AppInfo{AppName: "Komga", Source: "某外部源", SHA256: "aaa", SizeBytes: 100}
	// 同哈希但来源等级更高的条目 → 保留它（b 的替代竞争者）
	c := AppInfo{AppName: "Komga", Source: "fnOS应用中心", SHA256: "aaa", SizeBytes: 0}
	// 同名不同哈希 → 保留（不同构建）
	d := AppInfo{AppName: "Komga", Source: "另一外部源", SHA256: "bbb", SizeBytes: 200}
	// 同名无哈希 → 保留（无法证明同一性）
	e := AppInfo{AppName: "komga", Source: "第三个源"}

	out := dedupeIdenticalApps([]AppInfo{a, b, c, d, e})
	if len(out) != 3 {
		t.Fatalf("want 3 entries (winner of aaa-cluster + d + e), got %d: %+v", len(out), out)
	}
	// aaa 簇的胜者：a（fnos-apps +2，有 size +4，有下载量 +1 = 7）> c（+1+0+0=1）
	found := map[string]bool{}
	for _, app := range out {
		found[app.AppName+"|"+app.Source] = true
	}
	if !found["komga|fnos-apps"] {
		t.Errorf("expected komga|fnos-apps (highest priority) to be kept: %+v", out)
	}
	if !found["Komga|另一外部源"] || !found["komga|第三个源"] {
		t.Errorf("different-hash / no-hash entries must be kept: %+v", out)
	}
}

func TestDedupeIdenticalApps_NoHashKeepsAll(t *testing.T) {
	apps := []AppInfo{
		{AppName: "app", Source: "s1"},
		{AppName: "app", Source: "s2"},
		{AppName: "App", Source: "s3"},
	}
	out := dedupeIdenticalApps(apps)
	if len(out) != 3 {
		t.Fatalf("no sha256 -> nothing may be dropped, got %d", len(out))
	}
}

func TestDedupeIdenticalApps_SameNameSameHashDifferentVersion(t *testing.T) {
	// 同一哈希理论上就是同一版本；不同版本不同哈希各自保留
	apps := []AppInfo{
		{AppName: "app", Source: "s1", SHA256: "h1", LatestVersion: "1.0"},
		{AppName: "app", Source: "s2", SHA256: "h2", LatestVersion: "1.1"},
	}
	if out := dedupeIdenticalApps(apps); len(out) != 2 {
		t.Fatalf("different hashes must both be kept, got %d", len(out))
	}
}

// 更新判定（dock「有更新」计数依赖它）：已装 1.26.3、目录最新 1.27.0
// 的 V1 外部源应用必须判定为 update_available。
func TestMerge_ExternalAppUpdateDetection(t *testing.T) {
	r := &Registry{}
	local := []Manifest{{AppName: "Komga", Version: "1.26.3", FpkVersion: ""}}
	remote := []source.RemoteApp{{
		AppName: "Komga", DisplayName: "Komga",
		Version: "1.27.0", FpkVersion: "1.27.0", Source: "外部源",
	}}
	r.Merge(local, remote, nil)
	app, ok := r.Get("Komga")
	if !ok {
		t.Fatal("app not found in registry")
	}
	if !app.Installed {
		t.Fatal("installed app must be matched by local manifest")
	}
	if app.Status != AppStatusUpdateAvailable {
		t.Fatalf("1.26.3 installed vs 1.27.0 catalog must be update_available, got %q", app.Status)
	}
}
