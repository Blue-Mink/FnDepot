package source

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

const v2Sample = `{
  "schema_version": "2",
  "source_info": {"name": "测试源", "author": "tester", "homepage": "https://example.com"},
  "apps": {
    "demo.app": {
      "display_name": "Demo",
      "desc": "<p>演示<b>应用</b></p>",
      "platform": "all",
      "categories": ["影音娱乐"],
      "icon_url": "icons/demo.png",
      "homepage": "https://demo.example.com",
      "service_port": "8080",
      "releases": {
        "1.2.0": {"updated_at": "2026-09-01", "packages": {
          "x86": {"download_url": "releases/demo-1.2.0-x86.fpk"},
          "arm": {"download_url": "releases/demo-1.2.0-arm.fpk"}
        }},
        "1.10.3": {"updated_at": "2026-09-10", "packages": {
          "x86": {"download_url": "https://cdn.example.com/demo-1.10.3-x86.fpk"}
        }},
        "1.0.0": {"updated_at": "2026-08-01", "packages": {"all": {"download_url": "releases/demo-all.fpk"}}}
      }
    },
    "x86only.app": {
      "display_name": "X86Only",
      "platform": "x86",
      "releases": {"1.0.0": {"packages": {"x86": {"download_url": "https://cdn.example.com/x86only.fpk"}}}}
    },
    "empty.app": {
      "display_name": "Empty",
      "releases": {}
    }
  }
}`

const v1Sample = `{
  "legacy.app": {
    "display_name": "Legacy",
    "releases": {"2.0.0": {"packages": {"all": {"download_url": "https://cdn.example.com/legacy.fpk"}}}}
  }
}`

// v1FlatSample 是真实 V1 平铺格式（Blue-Mink/FnDepot 同款）：
// 无 schema_version、无 releases，单 version + download_url，
// labels 中文字符串、isdocker 字符串、service_port 数字。
const v1FlatSample = `{
  "global-radio": {
    "display_name": "全球电台",
    "version": "1.3.0",
    "platform": "x86",
    "desc": "在线电台应用",
    "labels": "娱乐",
    "author": "molixia",
    "author_url": "https://github.com/moli-xia",
    "isdocker": "true",
    "install_type": "用户空间",
    "size": "0.10",
    "download_url": "https://github.com/B/R/releases/download/v1.3.0/gr.fpk",
    "changelog": "1.3.0: 网关双入口",
    "icon_url": "./global-radio/ICON.PNG",
    "readme_url": "./global-radio/README.md",
    "homepage": "https://github.com/moli-xia/global-radio",
    "service_port": 32678,
    "download_count": 1234
  },
  "fn-knock": {
    "display_name": "敲门knock",
    "version": "2.4.14",
    "platform": "all",
    "labels": "安全，工具",
    "isdocker": "false",
    "download_url": "https://github.com/B/R/releases/download/v2.4.14/fnk.fpk",
    "icon_url": "./fn-knock/ICON.PNG",
    "service_port": 7999
  },
  "arm-only.app": {
    "display_name": "ArmOnly",
    "version": "1.0.0",
    "platform": "arm",
    "download_url": "https://cdn.example.com/arm.fpk"
  }
}`

func decodeBody(t *testing.T, body string) map[string]fndepotAppEntry {
	t.Helper()
	m, _ := decodeFndepotApps([]byte(body)) // 第二返回值=是否V2，成功与否看长度
	if len(m) == 0 {
		t.Fatalf("decodeFndepotApps 未识别该结构")
	}
	return m
}

func TestDecodeFndepotApps_V2(t *testing.T) {
	m := decodeBody(t, v2Sample)
	if len(m) != 3 {
		t.Fatalf("V2 应解析 3 个应用，实际 %d", len(m))
	}
	if m["demo.app"].DisplayName != "Demo" {
		t.Errorf("display_name = %q", m["demo.app"].DisplayName)
	}
}

func TestDecodeFndepotApps_V1(t *testing.T) {
	m := decodeBody(t, v1Sample)
	if len(m) != 1 || m["legacy.app"].DisplayName != "Legacy" {
		t.Fatalf("V1 解析失败: %v", m)
	}
}

func TestDecodeFndepotApps_Invalid(t *testing.T) {
	cases := []string{
		`{}`,
		`{"schema_version":"2"}`,
		`{"schema_version":"2","apps":{}}`,
		`{"schema_version":"3","apps":{"a.app":{"releases":{}}}}`,
		`[1,2,3]`,
		`not json`,
	}
	for i, c := range cases {
		if m, _ := decodeFndepotApps([]byte(c)); len(m) != 0 {
			t.Errorf("case %d 应解析为空，实际 %d 项: %s", i, len(m), c)
		}
	}
	// 缺 schema_version 的 V2 形文档：V1 回退可能收下 source_info/apps 两个
	// 幻影键，但它们无 releases，翻译阶段必然跳过——不产生任何应用。
	m, _ := decodeFndepotApps([]byte(`{"source_info":{"name":"x"},"apps":{"a.app":{"releases":{}}}}`))
	for _, e := range m {
		if len(e.Releases) != 0 {
			t.Errorf("幻影条目不应带 releases: %+v", e)
		}
	}
}

func TestTranslateFndepotApp_VersionAndArch(t *testing.T) {
	m := decodeBody(t, v2Sample)
	entry := m["demo.app"]

	// 当前机器为 x86：应取 1.10.3（最高版，x86 包，绝对 URL 保持原样）
	ra, ok := translateFndepotApp("demo.app", entry, "https://cdn.example.com/source.json", "测试源")
	if !ok {
		t.Fatal("demo.app 应可翻译")
	}
	if ra.Version != "1.10.3" {
		t.Errorf("最高版本应选 1.10.3（1.10 > 1.2），实际 %s", ra.Version)
	}
	if ra.FpkURL != "https://cdn.example.com/demo-1.10.3-x86.fpk" {
		t.Errorf("FpkURL = %s", ra.FpkURL)
	}
	if ra.ServicePort != 8080 {
		t.Errorf("ServicePort = %d", ra.ServicePort)
	}
	if ra.Category != "media" {
		t.Errorf("Category = %q，期望 media", ra.Category)
	}
	if ra.Source != "测试源" {
		t.Errorf("Source = %q", ra.Source)
	}
	if got := ra.Description; got != "演示 应用" {
		t.Errorf("stripHTML desc = %q", got)
	}
	if ra.HomepageURL != "https://demo.example.com" {
		t.Errorf("HomepageURL = %q", ra.HomepageURL)
	}
}

func TestTranslateFndepotApp_RelativeURL(t *testing.T) {
	// 1.2.0 只有相对 URL：构造只有 1.2.0 的条目验证相对解析
	m := decodeBody(t, v2Sample)
	entry := m["demo.app"]
	entry.Releases = map[string]fndepotRelease{
		"1.2.0": entry.Releases["1.2.0"],
		"1.0.0": entry.Releases["1.0.0"],
	}
	delete(entry.Releases, "1.10.3")
	ra, ok := translateFndepotApp("demo.app", entry, "https://cdn.example.com/v2/fnpack.json", "S")
	if !ok {
		t.Fatal("应可翻译")
	}
	if ra.Version != "1.2.0" {
		t.Fatalf("版本 = %s", ra.Version)
	}
	if ra.FpkURL != "https://cdn.example.com/v2/releases/demo-1.2.0-x86.fpk" {
		t.Errorf("相对 FpkURL 解析错误: %s", ra.FpkURL)
	}
	if ra.IconURL != "https://cdn.example.com/v2/icons/demo.png" {
		t.Errorf("相对 IconURL 解析错误: %s", ra.IconURL)
	}

	// 只剩 all 包时回退 all
	entry.Releases = map[string]fndepotRelease{"1.0.0": entry.Releases["1.0.0"]}
	ra, ok = translateFndepotApp("demo.app", entry, "https://cdn.example.com/v2/fnpack.json", "S")
	if !ok {
		t.Fatal("all 包应回退选中")
	}
	if ra.FpkURL != "https://cdn.example.com/v2/releases/demo-all.fpk" {
		t.Errorf("all 包 FpkURL = %s", ra.FpkURL)
	}
}

func TestTranslateFndepotApp_Skips(t *testing.T) {
	m := decodeBody(t, v2Sample)
	if _, ok := translateFndepotApp("empty.app", m["empty.app"], "https://x.com/s.json", "S"); ok {
		t.Error("无 releases 的拆分模式应用应被跳过")
	}
	if _, ok := translateFndepotApp("bad name!", fndepotAppEntry{}, "https://x.com/s.json", "S"); ok {
		t.Error("非法 appname 应被跳过")
	}
	if _, ok := translateFndepotApp("x86only.app", m["x86only.app"], "https://x.com/s.json", "S"); fndepotCurrentArch() == "arm" && !ok {
		t.Error("arm 机器上 x86-only 应用应被跳过")
	}
}

func TestPlatformOK(t *testing.T) {
	yes, _ := json.Marshal("all")
	many, _ := json.Marshal([]string{"arm", "x86"})
	if !fndepotPlatformOK(yes, "arm") {
		t.Error("platform=all 应放行")
	}
	if !fndepotPlatformOK(many, "arm") {
		t.Error("platform=[arm,x86] 在 arm 应放行")
	}
	one, _ := json.Marshal("x86")
	if fndepotPlatformOK(one, "arm") {
		t.Error("platform=x86 在 arm 应拒绝")
	}
	if !fndepotPlatformOK(nil, "x86") {
		t.Error("无 platform 应放行")
	}
}

func TestResolveJSONURL(t *testing.T) {
	// GitHub 仓库根地址 → raw + 分支回退
	got, fb := resolveJSONURL(mustParseURL(t, "https://github.com/owner/repo/"))
	want := "https://raw.githubusercontent.com/owner/repo/HEAD/fnpack.json"
	if got != want {
		t.Errorf("repo JSON URL = %s，期望 %s", got, want)
	}
	if len(fb) != 2 || fb[0] != "https://raw.githubusercontent.com/owner/repo/main/fnpack.json" || fb[1] != "https://raw.githubusercontent.com/owner/repo/master/fnpack.json" {
		t.Errorf("fallback = %v", fb)
	}
	// JSON 直链原样返回
	got, fb = resolveJSONURL(mustParseURL(t, "https://cdn.example.com/v2/fnpack.json"))
	if got != "https://cdn.example.com/v2/fnpack.json" || len(fb) != 0 {
		t.Errorf("直链应原样: %s %v", got, fb)
	}
}

func mustParseURL(t *testing.T, s string) *url.URL {
	t.Helper()
	u, err := url.Parse(s)
	if err != nil {
		t.Fatalf("parse %s: %v", s, err)
	}
	return u
}

func TestResolveAgainst(t *testing.T) {
	if got := resolveAgainst("https://cdn.example.com/v2/fnpack.json", "releases/a.fpk"); got != "https://cdn.example.com/v2/releases/a.fpk" {
		t.Errorf("relative = %s", got)
	}
	if got := resolveAgainst("https://cdn.example.com/v2/fnpack.json", "https://other.example.com/a.fpk"); got != "https://other.example.com/a.fpk" {
		t.Errorf("absolute 应保持: %s", got)
	}
	if got := resolveAgainst("https://cdn.example.com/v2/", "/abs.fpk"); got != "https://cdn.example.com/abs.fpk" {
		t.Errorf("root-relative = %s", got)
	}
}

func TestCompareVersions(t *testing.T) {
	cases := [][3]string{
		{"1.10.0", "1.9.9", "1.10.0"},
		{"v2.0.0", "1.9.9", "v2.0.0"},
		{"1.0.0", "1.0.0-beta", "1.0.0"},
		{"1.0", "1.0.0", "1.0.0"},
	}
	for _, c := range cases {
		if max := firstGreater(c[0], c[1]); max != c[2] {
			t.Errorf("max(%s,%s) = %s，期望 %s", c[0], c[1], max, c[2])
		}
	}
	// 排序
	vs := []string{"1.2.0", "1.10.3", "1.0.0"}
	sortByVersionDesc(vs)
	if vs[0] != "1.10.3" || vs[1] != "1.2.0" || vs[2] != "1.0.0" {
		t.Errorf("sortByVersionDesc = %v", vs)
	}
}

func firstGreater(a, b string) string {
	if compareFndepotVersions(a, b) >= 0 {
		return a
	}
	return b
}

func TestMapFndepotCategory(t *testing.T) {
	if mapFndepotCategory([]string{"AI赋能"}) != "ai" {
		t.Error("AI赋能 → ai")
	}
	if mapFndepotCategory([]string{"未知分类"}) != "" {
		t.Error("未知分类应返回空")
	}
	if mapFndepotCategory(nil) != "" {
		t.Error("nil 应返回空")
	}
}

func TestSourceIDStable(t *testing.T) {
	if fndepotSourceID("https://a.com/x.json") != fndepotSourceID("https://a.com/x.json") {
		t.Error("ID 应稳定")
	}
	if fndepotSourceID("https://a.com/x.json") == fndepotSourceID("https://a.com/y.json") {
		t.Error("不同地址 ID 应不同")
	}
}

// TestNewFNDepotSource_EndToEnd 用 httptest 模拟一个 V2 源，验证
// NewFNDepotSource + FetchApps 全链路（含元信息提取与源名回退）。
func TestNewFNDepotSource_EndToEnd(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/fnpack.json" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(v2Sample))
	}))
	defer srv.Close()

	s, err := NewFNDepotSource(srv.URL+"/fnpack.json", nil)
	if err != nil {
		t.Fatalf("NewFNDepotSource: %v", err)
	}
	if s.Name() != "测试源" {
		t.Errorf("源名应取 source_info.name，实际 %q", s.Name())
	}
	if s.Meta().Author != "tester" || s.Meta().Homepage != "https://example.com" {
		t.Errorf("Meta = %+v", s.Meta())
	}

	apps, err := s.FetchApps(context.Background())
	if err != nil {
		t.Fatalf("FetchApps: %v", err)
	}
	// demo.app（all/x86 可用）+ x86only.app（x86 机器上可用）；empty.app 跳过
	if fndepotCurrentArch() == "x86" && len(apps) != 2 {
		t.Errorf("x86 机器应得 2 个应用，实际 %d: %v", len(apps), names(apps))
	}
	for _, a := range apps {
		if a.Source != "测试源" {
			t.Errorf("%s Source = %q", a.AppName, a.Source)
		}
	}
}

// TestNewFNDepotSource_Invalid 非法/不可达源应报错。
func TestNewFNDepotSource_Invalid(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/bad.json" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"hello":"world"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	if _, err := NewFNDepotSource(srv.URL+"/bad.json", nil); err == nil {
		t.Error("非 FnDepot 结构的 JSON 应报错")
	}
	if _, err := NewFNDepotSource("ftp://nope/x.json", nil); err == nil {
		t.Error("非法 scheme 应报错")
	}
	if _, err := NewFNDepotSource("", nil); err == nil {
		t.Error("空地址应报错")
	}
}

func TestFetchCandidates_MirrorChain(t *testing.T) {
	s := &FNDepotSource{} // configMgr 为 nil → 默认镜像 gh-proxy
	head := "https://raw.githubusercontent.com/o/r/HEAD/fnpack.json"
	main := "https://raw.githubusercontent.com/o/r/main/fnpack.json"
	master := "https://raw.githubusercontent.com/o/r/master/fnpack.json"
	out := s.fetchCandidates(head, []string{main, master})
	if len(out) < 3 {
		t.Fatalf("候选过少: %v", out)
	}
	// 直连 HEAD 在最前
	if out[0] != head {
		t.Errorf("首候选应为直连 HEAD: %s", out[0])
	}
	// 镜像链包含默认镜像前缀
	hasMirror := false
	for _, c := range out {
		if strings.HasPrefix(c, "https://gh-proxy.com/"+head) {
			hasMirror = true
		}
	}
	if !hasMirror {
		t.Errorf("候选链应包含镜像前缀: %v", out)
	}
	// 分支回退在最后
	if out[len(out)-2] != main || out[len(out)-1] != master {
		t.Errorf("末两候选应为 main/master 分支回退: %v", out[len(out)-2:])
	}
}

func TestFetchCandidates_DirectLink(t *testing.T) {
	s := &FNDepotSource{}
	out := s.fetchCandidates("https://cdn.example.com/s.json", nil)
	// 直链：原样 + 一次重试
	if len(out) != 2 || out[0] != "https://cdn.example.com/s.json" || out[1] != "https://cdn.example.com/s.json" {
		t.Errorf("JSON 直链应为原样+重试: %v", out)
	}
}

func names(apps []RemoteApp) []string {
	out := make([]string, 0, len(apps))
	for _, a := range apps {
		out = append(out, a.AppName)
	}
	return out
}

func TestDecodeFndepotApps_V1Flat(t *testing.T) {
	m := decodeBody(t, v1FlatSample)
	if len(m) != 3 {
		t.Fatalf("V1 平铺应解析 3 个应用，实际 %d", len(m))
	}
	e := m["global-radio"]
	if e.Version != "1.3.0" || e.DownloadURL == "" {
		t.Errorf("V1 平铺字段未解析: version=%q download=%q", e.Version, e.DownloadURL)
	}
	if e.Labels != "娱乐" || e.IsDockerV1 != "true" {
		t.Errorf("labels/isdocker 未解析: %q / %q", e.Labels, e.IsDockerV1)
	}
	if e.ServicePort != "32678" {
		t.Errorf("service_port(数字) 应可入 string 字段: %q", e.ServicePort)
	}
	if e.DownloadCount != 1234 {
		t.Errorf("可选 download_count 应解析: %d", e.DownloadCount)
	}
}

func TestTranslateFndepotApp_V1Flat(t *testing.T) {
	m := decodeBody(t, v1FlatSample)
	base := "https://raw.githubusercontent.com/B/R/HEAD/fnpack.json"

	// x86 机器：global-radio（x86 声明）应通过
	ra, ok := translateFndepotApp("global-radio", m["global-radio"], base, "B")
	if !ok {
		t.Fatal("V1 平铺 x86 应用应可翻译（这是 Blue-Mink 不同步的 bug 场景）")
	}
	if ra.Version != "1.3.0" {
		t.Errorf("version = %s", ra.Version)
	}
	if ra.FpkURL != "https://github.com/B/R/releases/download/v1.3.0/gr.fpk" {
		t.Errorf("FpkURL = %s", ra.FpkURL)
	}
	if ra.IconURL != "https://raw.githubusercontent.com/B/R/HEAD/global-radio/ICON.PNG" {
		t.Errorf("相对 icon 解析错误: %s", ra.IconURL)
	}
	if ra.ReadmeURL != "https://raw.githubusercontent.com/B/R/HEAD/global-radio/README.md" {
		t.Errorf("readme 解析错误: %s", ra.ReadmeURL)
	}
	if ra.AppType != "docker" {
		t.Errorf("isdocker=true 应映射 docker，实际 %q", ra.AppType)
	}
	if ra.ServicePort != 32678 {
		t.Errorf("port = %d", ra.ServicePort)
	}
	if ra.Category != "media" {
		t.Errorf("labels=娱乐 应映射 media，实际 %q", ra.Category)
	}
	if ra.Changelog != "1.3.0: 网关双入口" {
		t.Errorf("changelog = %q", ra.Changelog)
	}

	// platform=all 的 V1 应用也应通过
	ra2, ok2 := translateFndepotApp("fn-knock", m["fn-knock"], base, "B")
	if !ok2 {
		t.Fatal("V1 平铺 all 应用应可翻译")
	}
	if ra2.AppType != "" {
		t.Errorf("isdocker=false 不应是 docker: %q", ra2.AppType)
	}

	// platform=arm 的 V1 应用在 x86 机器上应被跳过
	if _, ok3 := translateFndepotApp("arm-only.app", m["arm-only.app"], base, "B"); ok3 {
		t.Error("arm-only 应用在 x86 上应被平台过滤跳过")
	}
}

func TestFndepotGitHubOwner(t *testing.T) {
	cases := map[string]string{
		"https://github.com/Blue-Mink/FnDepot":        "Blue-Mink",
		"https://github.com/Blue-Mink/FnDepot/":       "Blue-Mink",
		"https://github.com/Blue-Mink/FnDepot?x=1":    "",
		"https://raw.githubusercontent.com/a/b/c":     "",
		"https://example.com/x/y/fnpack.json":         "",
	}
	for in, want := range cases {
		if got := fndepotGitHubOwner(in); got != want {
			t.Errorf("fndepotGitHubOwner(%q) = %q，期望 %q", in, got, want)
		}
	}
}
