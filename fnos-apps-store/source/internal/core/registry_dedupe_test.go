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

	out, _ := dedupeIdenticalApps([]AppInfo{a, b, c, d, e})
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
	out, _ := dedupeIdenticalApps(apps)
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
	if out, _ := dedupeIdenticalApps(apps); len(out) != 2 {
		t.Fatalf("different hashes must both be kept, got %d", len(out))
	}
}

// TestMerge_HiddenDuplicateNotRoutable 被内容去重隐藏的转载条目必须同时从
// 注册表映射移除：否则按裸 AppName 的 Get 会随机命中隐藏条目，把安装/向导
// 路由到列表里看不到的源（2026-09-19 真机 Vaultwarden 实锤：官方条目被
// shuangji66 同哈希转载抢路由，下载了转载 FPK 而非官方云通道）。
func TestMerge_HiddenDuplicateNotRoutable(t *testing.T) {
	r := NewRegistry()
	remote := []source.RemoteApp{
		{AppName: "Vaultwarden", DisplayName: "Vaultwarden", Version: "1.32.4",
			Source: "fnos-official", PanelSourceID: "900", FpkURL: "cloud://vaultwarden"},
		{AppName: "vaultwarden", DisplayName: "Vaultwarden", Version: "1.37.3",
			Source: "fnos-apps", FpkURL: "https://x/vaultwarden.fpk",
			SHA256: "same-pack", SizeBytes: 500, DownloadCount: 10},
		// 与内置目录同一份包（同哈希）的转载：应被去重隐藏
		{AppName: "Vaultwarden", DisplayName: "Vaultwarden", Version: "1.37.3",
			Source: "shuangji66的应用源", FpkURL: "https://x/mirror.fpk",
			SHA256: "same-pack"},
	}
	r.Merge(nil, remote, nil)

	// 列表只剩 2 条（转载被隐藏）
	if got := len(r.List()); got != 2 {
		t.Fatalf("list len = %d, want 2: %+v", got, r.List())
	}

	// 官方 AppName 的 Get 必须稳定命中官方条目，而不是随机撞进隐藏转载
	for i := 0; i < 50; i++ {
		app, ok := r.Get("Vaultwarden")
		if !ok {
			t.Fatal("Vaultwarden not found")
		}
		if app.Source != "fnos-official" || app.PanelSourceID != "900" {
			t.Fatalf("Get(Vaultwarden) -> source=%q panelID=%q, want official", app.Source, app.PanelSourceID)
		}
	}
	// 内置小写条目不受影响
	app, ok := r.Get("vaultwarden")
	if !ok || app.Source != "fnos-apps" {
		t.Fatalf("Get(vaultwarden) -> %q ok=%v, want fnos-apps", app.Source, ok)
	}
}

// TestGet_SameNameDeterministic 同名多条（不同哈希，都会展示在列表折叠前）
// 时，Get 的裸 appname 回退必须按卡片折叠同一优先级确定性裁决——官方源
// 恒胜（2026-09-19 Vaultwarden 真机：旧实现按 map 随机序，官方卡片被
// shuangji66 转载随机抢路由，装了转载 FPK 而非官方云通道）。
func TestGet_SameNameDeterministic(t *testing.T) {
	r := NewRegistry()
	remote := []source.RemoteApp{
		{AppName: "Vaultwarden", DisplayName: "Vaultwarden", Version: "1.37.3",
			Source: "shuangji66的应用源", FpkURL: "https://x/m.fpk",
			SHA256: "mirror-sha"},
		{AppName: "Vaultwarden", DisplayName: "Vaultwarden", Version: "1.32.4",
			Source: "fnos-official", PanelSourceID: "900", FpkURL: "cloud://v"},
	}
	r.Merge(nil, remote, nil)
	for i := 0; i < 100; i++ {
		app, ok := r.Get("Vaultwarden")
		if !ok {
			t.Fatal("Vaultwarden not found")
		}
		if app.Source != "fnos-official" || app.PanelSourceID != "900" {
			t.Fatalf("iteration %d: Get -> source=%q, want fnos-official", i, app.Source)
		}
	}
}

// TestGetFold_CaseInsensitive FPK manifest 的 appname 常为小写（"gitea"），
// 注册表条目可能首字母大写（"Gitea"）。GetFold 必须大小写不敏感命中，
// 否则「已安装」检查被绕过、直接安装静默重装正在运行的应用（2026-09-20 真机实锤）。
func TestGetFold_CaseInsensitive(t *testing.T) {
	r := NewRegistry()
	remote := []source.RemoteApp{
		{AppName: "Gitea", DisplayName: "Gitea", Version: "1.27.3",
			Source: "fnos-apps", FpkURL: "https://x/g.fpk"},
	}
	r.Merge(nil, remote, nil)
	app, ok := r.GetFold("gitea")
	if !ok || app.AppName != "Gitea" {
		t.Fatalf("GetFold(\"gitea\") = ok:%v app:%+v, want Gitea entry", ok, app)
	}
	if _, ok := r.GetFold("GITEA"); !ok {
		t.Fatal("GetFold(\"GITEA\") should also match")
	}
	if _, ok := r.GetFold("ghost-app"); ok {
		t.Fatal("GetFold must not match a nonexistent appname")
	}
}

// TestSetOfficialMeta 官方应用开发者/发布者回填（面板 app/detail 增量同步）：
// 只改官方条目、不碰第三方；LastResult 与 r.apps 双写一致。
func TestSetOfficialMeta(t *testing.T) {
	r := NewRegistry()
	remote := []source.RemoteApp{
		{AppName: "transmission", DisplayName: "Transmission", Version: "4.0.5",
			Source: "fnos-official", PanelSourceID: "1", FpkURL: "cloud://t"},
		{AppName: "openlist", DisplayName: "OpenList", Version: "4.2.5",
			Source: "某第三方源", FpkURL: "https://x/o.fpk", Maintainer: "521xueweihan"},
	}
	r.Merge(nil, remote, nil)
	r.SetOfficialMeta("TRANSMISSION", "飞牛科技", "https://fnnas.com", "fnOS", "https://fnnas.com")

	app, ok := r.Get("transmission")
	if !ok || app.Maintainer != "飞牛科技" || app.MaintainerURL != "https://fnnas.com" ||
		app.Distributor != "fnOS" || app.DistributorURL != "https://fnnas.com" {
		t.Fatalf("official meta not applied: %+v", app)
	}
	// List（详情页/列表数据源）也要能看到
	found := false
	for _, a := range r.List() {
		if a.AppName == "transmission" && a.Maintainer == "飞牛科技" {
			found = true
		}
	}
	if !found {
		t.Fatal("List() does not reflect SetOfficialMeta")
	}
	// 第三方条目不受影响
	other, _ := r.Get("openlist")
	if other.Maintainer != "521xueweihan" || other.Distributor != "" {
		t.Fatalf("third-party entry was touched: %+v", other)
	}
}

// TestSetOfficialDescription 官方应用描述回填（面板 app/list 无 desc，
// 走 app/detail 增量同步）：只改官方条目、空 desc 不覆盖、第三方不动。
func TestSetOfficialDescription(t *testing.T) {
	r := NewRegistry()
	remote := []source.RemoteApp{
		{AppName: "transmission", DisplayName: "Transmission", Version: "4.0.5",
			Source: "fnos-official", PanelSourceID: "1", FpkURL: "cloud://t"},
		{AppName: "openlist", DisplayName: "OpenList", Version: "4.2.5",
			Source: "某第三方源", FpkURL: "https://x/o.fpk", Description: "原有描述"},
	}
	r.Merge(nil, remote, nil)

	// 空 desc 是 no-op
	r.SetOfficialDescription("transmission", "")
	if app, _ := r.Get("transmission"); app.Description != "" {
		t.Fatal("empty desc must not overwrite")
	}

	r.SetOfficialDescription("TRANSMISSION", "BitTorrent 下载工具")
	app, ok := r.Get("transmission")
	if !ok || app.Description != "BitTorrent 下载工具" {
		t.Fatalf("official desc not applied: %+v", app)
	}
	found := false
	for _, a := range r.List() {
		if a.AppName == "transmission" && a.Description == "BitTorrent 下载工具" {
			found = true
		}
	}
	if !found {
		t.Fatal("List() does not reflect SetOfficialDescription")
	}
	// 第三方不受影响
	if other, _ := r.Get("openlist"); other.Description != "原有描述" {
		t.Fatalf("third-party entry was touched: %+v", other)
	}
}

// TestGet_InstalledWinsOverOfficial 已安装条目优先于未安装的官方条目
// （与卡片折叠规则一致：已装状态优先，避免「已装第三方包、目录官方卡」
// 时安装/更新操作路由到官方源而撞上 daemon 的「已安装」拒绝）。
func TestGet_InstalledWinsOverOfficial(t *testing.T) {
	r := NewRegistry()
	remote := []source.RemoteApp{
		{AppName: "vaultwarden", DisplayName: "Vaultwarden", Version: "1.37.3",
			Source: "shuangji66的应用源", FpkURL: "https://x/m.fpk"},
		{AppName: "vaultwarden", DisplayName: "Vaultwarden", Version: "1.32.4",
			Source: "fnos-official", PanelSourceID: "900", FpkURL: "cloud://v"},
	}
	r.Merge(nil, remote, nil)
	// 都未安装时官方恒胜
	if app, ok := r.Get("vaultwarden"); !ok || app.Source != "fnos-official" {
		t.Fatalf("precondition: uninstalled winner should be official, got ok=%v src=%q", ok, app.Source)
	}
	// 把第三方条目标为已安装（测试在同包内直改 map；真实场景由本地
	// manifest 扫描/ReconcileInstalled 驱动同一字段）
	key := "vaultwarden@shuangji66的应用源"
	ent, ok := r.apps[key]
	if !ok {
		t.Fatalf("mirror entry %q not in registry", key)
	}
	ent.Installed = true
	ent.InstalledVersion = "1.37.3"
	r.apps[key] = ent
	for i := 0; i < 100; i++ {
		app, ok := r.Get("vaultwarden")
		if !ok {
			t.Fatal("vaultwarden not found")
		}
		if !app.Installed || app.Source != "shuangji66的应用源" {
			t.Fatalf("iteration %d: Get -> installed=%v source=%q, want installed shuangji66 entry", i, app.Installed, app.Source)
		}
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
