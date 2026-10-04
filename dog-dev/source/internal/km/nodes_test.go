package km

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// fakeSnapshot 返回一个两节点快照：a 存活健康、b 失效；
// c 只在 States 里出现（不在 plan.Slots 中，不应出现在结果里）。
func fakeSnapshot() string {
	return `{
  "plan": {"Slots": [
    {"Node": {"NodeID": "a", "DisplayName": "节点A", "Priority": 1, "IsInternal": true}},
    {"Node": {"NodeID": "b", "Priority": 2}}
  ]},
  "status": {"States": [
    {"NodeID": "a", "Alive": true, "State": "healthy", "TestSpeed": 12000000,
     "Runtime": {"Enabled": true}, "LastTestAt": "2026-10-03T10:00:00Z"},
    {"NodeID": "b", "Alive": false, "State": "disabled", "Runtime": {"Enabled": false},
     "LastLiveErr": "boom", "LiveDisabledUntil": "0001-01-01T00:00:00Z"},
    {"NodeID": "c", "Alive": true, "State": "healthy"}
  ]}
}`
}

// newNodesTestManager 起一个假管理口，返回指向它的 Manager。
func newNodesTestManager(t *testing.T) *Manager {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/accel/snapshot" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		profile := r.URL.Query().Get("profile")
		if profile != "docker:dockerhub" && profile != "docker:ghcr" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(fakeSnapshot()))
	}))
	t.Cleanup(srv.Close)

	// 显式覆盖 admin 地址指向测试服务；registry 覆盖为空（不参与本测试）。
	return New("", srv.URL, "", "", nil)
}

func TestNodesMergeAndOrder(t *testing.T) {
	m := newNodesTestManager(t)
	nodes, err := m.Nodes(context.Background())
	if err != nil {
		t.Fatalf("Nodes 失败: %v", err)
	}
	// 两个 profile × 每个快照 2 个 plan 节点 = 4 条；States 里多出的 c 不出现。
	if len(nodes) != 4 {
		t.Fatalf("应返回 4 条（2 profile × 2 节点），实际 %d: %+v", len(nodes), nodes)
	}
	// c 不应出现（只存在于 States）。
	for _, n := range nodes {
		if n.NodeID == "c" {
			t.Fatalf("States 里不在 plan.Slots 的节点 c 不应出现")
		}
	}
	// 每个 profile 内：存活的 a 在失效的 b 前。
	for i := 0; i < len(nodes); i += 2 {
		if nodes[i].NodeID != "a" || nodes[i+1].NodeID != "b" {
			t.Fatalf("profile %s 内顺序错误: %v / %v", nodes[i].Profile, nodes[i].NodeID, nodes[i+1].NodeID)
		}
	}
	// 字段合并：a 的健康数据齐全，b 的 DisplayName 缺失时回退 NodeID。
	a := nodes[0]
	if a.Name != "节点A" || !a.Alive || a.State != "healthy" || a.TestSpeed != 12000000 ||
		!a.Enabled || !a.IsInternal || a.LastTestAt != "2026-10-03T10:00:00Z" {
		t.Fatalf("节点 a 字段合并错误: %+v", a)
	}
	b := nodes[1]
	if b.Name != "b" || b.Alive || b.State != "disabled" || b.LiveConsecutiveFail != 0 ||
		b.LastLiveErr != "boom" || b.DisabledUntil != "" {
		t.Fatalf("节点 b 字段合并错误: %+v", b)
	}
}

func TestNodesCache(t *testing.T) {
	m := newNodesTestManager(t)
	first, err := m.Nodes(context.Background())
	if err != nil || len(first) == 0 {
		t.Fatalf("首次 Nodes 失败: %v", err)
	}
	// 缓存期内再取应命中同一份数据（改缓存内容验证是否被绕过）。
	m.mu.Lock()
	m.nodesCache = []NodeInfo{{NodeID: "cached"}}
	m.mu.Unlock()
	second, err := m.Nodes(context.Background())
	if err != nil {
		t.Fatalf("二次 Nodes 失败: %v", err)
	}
	if len(second) != 1 || second[0].NodeID != "cached" {
		t.Fatalf("5 秒缓存期内应命中缓存，实际 %+v", second)
	}
}

func TestNodesAdminDown(t *testing.T) {
	m := New("", "http://127.0.0.1:1", "", "", nil) // 管理口不可达
	if _, err := m.Nodes(context.Background()); err == nil {
		t.Fatalf("管理口不可达时应报错")
	}
}

// TestResolveFollowsPortConf：registry/admin 端口跟随 kspeeder 的 ports.conf。
func TestResolveFollowsPortConf(t *testing.T) {
	dir := t.TempDir()
	portConf := filepath.Join(dir, "ports.conf")
	if err := os.WriteFile(portConf, []byte("ADMIN_PORT=5003\nPROXY_PORT=5443\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New("", "", "", portConf, nil)
	reg, adm := m.resolve()
	if reg != "https://127.0.0.1:5443" || adm != "http://127.0.0.1:5003" {
		t.Fatalf("应跟随 ports.conf，实际 %s / %s", reg, adm)
	}

	// 用户改过 kspeeder 端口后，dog-dev 应跟随新值。
	if err := os.WriteFile(portConf, []byte("ADMIN_PORT=6100\nPROXY_PORT=6200\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	m.regResolved = "" // 清缓存，重新解析
	m.mu.Unlock()
	reg, adm = m.resolve()
	if reg != "https://127.0.0.1:6200" || adm != "http://127.0.0.1:6100" {
		t.Fatalf("端口变更后应跟随新值，实际 %s / %s", reg, adm)
	}
}

// TestResolveDefaultsWithoutPortConf：ports.conf 缺失时回落默认 5443/5003。
func TestResolveDefaultsWithoutPortConf(t *testing.T) {
	m := New("", "", "", filepath.Join(t.TempDir(), "不存在"), nil)
	reg, adm := m.resolve()
	if reg != "https://127.0.0.1:5443" || adm != "http://127.0.0.1:5003" {
		t.Fatalf("ports.conf 缺失时应回落默认端口，实际 %s / %s", reg, adm)
	}
}

// TestResolveOverrideWins：配置显式地址优先于 ports.conf。
func TestResolveOverrideWins(t *testing.T) {
	dir := t.TempDir()
	portConf := filepath.Join(dir, "ports.conf")
	_ = os.WriteFile(portConf, []byte("ADMIN_PORT=9000\nPROXY_PORT=9100\n"), 0o644)
	m := New("https://127.0.0.1:7777", "http://127.0.0.1:8888", "", portConf, nil)
	reg, adm := m.resolve()
	if reg != "https://127.0.0.1:7777" || adm != "http://127.0.0.1:8888" {
		t.Fatalf("显式覆盖应生效，实际 %s / %s", reg, adm)
	}
}

// TestParsePortConf：行式解析与非法值处理。
func TestParsePortConf(t *testing.T) {
	data := []byte("ADMIN_PORT=5003\n# 注释\nPROXY_PORT=5443\nBAD=xx\n")
	if p, ok := parsePortConf(data, "ADMIN_PORT"); !ok || p != 5003 {
		t.Fatalf("ADMIN_PORT 解析错误: %d %v", p, ok)
	}
	if p, ok := parsePortConf(data, "PROXY_PORT"); !ok || p != 5443 {
		t.Fatalf("PROXY_PORT 解析错误: %d %v", p, ok)
	}
	if _, ok := parsePortConf(data, "MISSING"); ok {
		t.Fatalf("缺失键不应命中")
	}
	if p, ok := parsePortConf([]byte("X=99999"), "X"); ok || p != 0 {
		t.Fatalf("非法端口应拒绝: %d %v", p, ok)
	}
}

// TestInstalledAndVersion：manifest 存在性与应用版本读取。
func TestInstalledAndVersion(t *testing.T) {
	dir := t.TempDir()
	manifest := filepath.Join(dir, "manifest")
	if err := os.WriteFile(manifest, []byte("appname = kspeeder\nversion                    = 0.8.2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := New("", "", manifest, "", nil)
	installed, ver := m.Installed()
	if !installed || ver != "0.8.2" {
		t.Fatalf("应识别已安装且版本 0.8.2，实际 %v %q", installed, ver)
	}
	// manifest 缺失 → 未安装。
	m2 := New("", "", filepath.Join(dir, "nope"), "", nil)
	installed, ver = m2.Installed()
	if installed || ver != "" {
		t.Fatalf("manifest 缺失应判未安装，实际 %v %q", installed, ver)
	}
}

// TestVersionBelow：低版本告警判据。
func TestVersionBelow(t *testing.T) {
	cases := []struct{ v, min string; below bool }{
		{"0.8.0", "0.8.2", true},
		{"0.8.1", "0.8.2", true},
		{"0.8.2", "0.8.2", false},
		{"0.9.0", "0.8.2", false},
		{"0.8.10", "0.8.2", false},
		{"0.7.17", "0.8.2", true},
		{"", "0.8.2", false}, // 无法比较不告警
		{"unknown", "0.8.2", false},
	}
	for _, c := range cases {
		if got := VersionBelow(c.v, c.min); got != c.below {
			t.Errorf("VersionBelow(%q, %q) = %v，期望 %v", c.v, c.min, got, c.below)
		}
	}
}

// TestEngineVersion：管理口 /api/summary 读版本。
func TestEngineVersion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/summary" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		fmt.Fprint(w, `{"version":"0.8.2"}`)
	}))
	t.Cleanup(srv.Close)
	m := New("", srv.URL, "", "", nil)
	if v := m.EngineVersion(context.Background()); v != "0.8.2" {
		t.Fatalf("应读到引擎版本 0.8.2，实际 %q", v)
	}
	// 管理口不可达 → 空串（不报错）。
	m2 := New("", "http://127.0.0.1:1", "", "", nil)
	if v := m2.EngineVersion(context.Background()); v != "" {
		t.Fatalf("管理口不可达时应返回空串，实际 %q", v)
	}
}

// TestStatusModes：absent / stopped / running 三态。
func TestStatusModes(t *testing.T) {
	dir := t.TempDir()
	manifest := filepath.Join(dir, "manifest")
	_ = os.WriteFile(manifest, []byte("version = 0.8.2\n"), 0o644)

	// 未安装。
	m0 := New("", "", filepath.Join(dir, "nope"), "", nil)
	if st := m0.Status(); st.Mode != ModeAbsent || st.Installed {
		t.Fatalf("未安装应为 absent，实际 %+v", st)
	}

	// 已安装但 registry 未运行（127.0.0.1:1 必拒连）。
	m1 := New("https://127.0.0.1:1", "http://127.0.0.1:1", manifest, "", nil)
	if st := m1.Status(); st.Mode != ModeStopped || st.URL != "" {
		t.Fatalf("已安装未运行应为 stopped，实际 %+v", st)
	}

	// registry 在听 → running 且 URL 生效。
	ln, err := netListen(t, "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	m2 := New("https://"+ln.Addr().String(), "http://127.0.0.1:1", manifest, "", nil)
	st := m2.Status()
	if st.Mode != ModeRunning || st.URL != "https://"+ln.Addr().String() {
		t.Fatalf("registry 在听应为 running 且给出 URL，实际 %+v", st)
	}
}

// TestMergeSnapshotUnknownState：plan 有节点但 States 缺失时标 unknown。
func TestMergeSnapshotUnknownState(t *testing.T) {
	var snap snapResp
	_ = json.Unmarshal([]byte(`{"plan":{"Slots":[{"Node":{"NodeID":"x"}}]}}`), &snap)
	out := mergeSnapshot("docker:ghcr", snap)
	if len(out) != 1 || out[0].State != "unknown" || out[0].Name != "x" {
		t.Fatalf("States 缺失时应标 unknown 且名字回退 NodeID，实际 %+v", out)
	}
}

// 测试用：在回环上起一个 TCP 监听（模拟 registry 端口在听）。
func netListen(t *testing.T, addr string) (ln net.Listener, err error) {
	t.Helper()
	ln, err = net.Listen("tcp", addr)
	return
}
