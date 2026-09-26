package api

import (
	"bufio"
	"context"
	"crypto/sha1"
	"fmt"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"fnos-store/internal/core"
	"fnos-store/internal/panel"
	"fnos-store/internal/platform"
	"fnos-store/internal/source"
)

// ── mock 面板服务（WS 登录 + app-center API，同端口）────────────────────────

type mockPanel struct {
	t *testing.T

	url          string
	onInstalled  func(appName string) // install/task 成功后回调（测试把它并进 fake 已装列表）

	mu             sync.Mutex
	dlCalls        []string // download/task appName 顺序
	instCalls      []string // install/task appName 顺序
	dlPoll         int
	instPoll       int
	installFailMsg string // 非空时 install/status 返回失败

	// install/info 行为注入：wizards[appName] 非空即 hasWizard=true；
	// noInstallInfo 模拟老面板（该端点 404）。
	wizards       map[string]any
	noInstallInfo bool

	// instCustomParams 记录 install/task 实际收到的 customParameters。
	instCustomParams map[string][]map[string]string
}

func newMockPanel(t *testing.T) *mockPanel {
	mp := &mockPanel{t: t, wizards: map[string]any{}, instCustomParams: map[string][]map[string]string{}}

	mux := http.NewServeMux()
	mux.HandleFunc("/websocket", mp.handleWS)
	mux.HandleFunc("/app/ticket", mp.handleTicket)
	mux.HandleFunc("/app-center/v1/app/list", mp.requireOst(mp.handleAppList))
	mux.HandleFunc("/app-center/v1/app/detail", mp.requireOst(mp.handleAppDetail))
	mux.HandleFunc("/app-center/v1/download/task", mp.requireOst(mp.handleDownloadTask))
	mux.HandleFunc("/app-center/v1/download/status", mp.requireOst(mp.handleDownloadStatus))
	mux.HandleFunc("/app-center/v1/install/task", mp.requireOst(mp.handleInstallTask))
	mux.HandleFunc("/app-center/v1/install/status", mp.requireOst(mp.handleInstallStatus))
	mux.HandleFunc("/app-center/v1/install/info", mp.requireOst(mp.handleInstallInfo))

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	mp.url = srv.URL
	return mp
}

func (mp *mockPanel) requireOst(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := r.Header.Get("Cookie")
		if !strings.Contains(c, "ost=test-ost") {
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprint(w, `{"code":40100,"msg":"unauthorized"}`)
			return
		}
		next(w, r)
	}
}

// handleWS 手工完成 WS 握手并应答 user.login（校验 ver:2 与密码）。
func (mp *mockPanel) handleWS(w http.ResponseWriter, r *http.Request) {
	key := r.Header.Get("Sec-WebSocket-Key")
	if key == "" {
		http.Error(w, "no key", http.StatusBadRequest)
		return
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "no hijack", http.StatusInternalServerError)
		return
	}
	conn, bufw, err := hj.Hijack()
	if err != nil {
		return
	}
	defer conn.Close()
	accept := base64.StdEncoding.EncodeToString(sha1Append(key, "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	_, _ = bufw.WriteString("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: " + accept + "\r\n\r\n")
	_ = bufw.Flush()

	payload, _ := mp.readClientFrame(bufw.Reader)
	var login struct {
		User     string `json:"user"`
		Password string `json:"password"`
		Ver      int    `json:"ver"`
		ReqID    string `json:"reqid"`
		Req      string `json:"req"`
	}
	_ = json.Unmarshal(payload, &login)
	resp := map[string]any{"reqid": login.ReqID}
	if login.Req == "user.login" && login.Password == "mock-pass" && login.Ver == 2 {
		resp["ticket"] = "test-ticket"
	}
	mp.writeServerFrame(conn, resp)
}

func (mp *mockPanel) handleTicket(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Ticket string `json:"ticket"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body.Ticket != "test-ticket" {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	w.Header().Set("Set-Cookie", "ost=test-ost; Path=/; HttpOnly")
	w.WriteHeader(http.StatusOK)
}

func (mp *mockPanel) handleAppList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"code": 0, "data": map[string]any{
			"total": 2,
			"list": []map[string]any{
				{"sourceID": "98", "appName": "mcsmanager", "name": "MC 服务器管理", "tags": []string{"Development Tools"}, "icon": "https://cdn/icon-mcs.png", "download": 100, "version": "2.1.3", "docker": false, "status": "noinstall", "source": "thirdparty"},
				{"sourceID": "77", "appName": "nodejs_v22", "name": "Node.js v22", "tags": []string{"Development Tools"}, "icon": "https://cdn/icon-node.png", "download": 329344, "version": "22.18.0-1", "docker": false, "status": "noinstall", "source": "thirdparty"},
			},
		},
	})
}

func (mp *mockPanel) handleAppDetail(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("appName")
	switch name {
	case "mcsmanager":
		writeJSON(w, http.StatusOK, map[string]any{
			"code": 0, "data": map[string]any{
				"sourceID": "98", "appName": "mcsmanager", "name": "MC 服务器管理",
				"version": "2.1.3", "status": "noinstall",
				"installDepApps": []map[string]any{
					{"sourceID": "77", "appName": "nodejs_v22", "name": "Node.js v22", "version": "22.18.0-1", "status": "noinstall"},
				},
				"appDetail": map[string]any{"desc": "MC server manager", "maintainer": "tester", "installSize": 1024},
			},
		})
	case "nodejs_v22":
		writeJSON(w, http.StatusOK, map[string]any{
			"code": 0, "data": map[string]any{
				"sourceID": "77", "appName": "nodejs_v22", "name": "Node.js v22",
				"version": "22.18.0-1", "status": "noinstall", "installDepApps": []any{},
			},
		})
	default:
		writeJSON(w, http.StatusNotFound, map[string]any{"code": 40400, "msg": "not found"})
	}
}

func (mp *mockPanel) handleDownloadTask(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AppName string `json:"appName"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	mp.mu.Lock()
	mp.dlCalls = append(mp.dlCalls, body.AppName)
	mp.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]any{"code": 0, "data": map[string]any{"downloadTaskId": "cloud_mock_1"}})
}

func (mp *mockPanel) handleDownloadStatus(w http.ResponseWriter, r *http.Request) {
	mp.mu.Lock()
	mp.dlPoll++
	poll := mp.dlPoll
	mp.mu.Unlock()
	status, progress := 1, 0.5
	if poll >= 2 {
		status, progress = 2, 1
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"code": 0, "data": map[string]any{
			"status": status, "message": map[int]string{1: "running", 2: "success"}[status],
			"progress": progress, "path": "/vol1/appcenter-downloads/x-tpk",
		},
	})
}

func (mp *mockPanel) handleInstallTask(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AppName          string `json:"appName"`
		SystemParameters struct {
			AgreedToProtocol  bool `json:"agreedToProtocol"`
			InstallVolumeID   int  `json:"installVolumeID"`
			DataVolumeId      int  `json:"dataVolumeId"`
			ImmediateStart    bool `json:"immediateStart"`
		} `json:"systemParameters"`
		CustomParameters []map[string]string `json:"customParameters"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	mp.mu.Lock()
	mp.instCalls = append(mp.instCalls, body.AppName)
	mp.instCustomParams[body.AppName] = body.CustomParameters
	cb := mp.onInstalled
	mp.mu.Unlock()
	if cb != nil {
		cb(body.AppName)
	}
	writeJSON(w, http.StatusOK, map[string]any{"code": 0, "data": map[string]any{"taskId": "1-mock-" + body.AppName}})
}

// handleInstallInfo 模拟 GET /app-center/v1/install/info（真面板对已下载的
// 包返回向导定义；noInstallInfo 时 404，模拟老面板）。
func (mp *mockPanel) handleInstallInfo(w http.ResponseWriter, r *http.Request) {
	mp.mu.Lock()
	missing := mp.noInstallInfo
	content := mp.wizards[r.URL.Query().Get("appName")]
	mp.mu.Unlock()
	if missing {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `404 page not found`)
		return
	}
	wizardInfo := map[string]any{"hasWizard": false}
	if content != nil {
		wizardInfo = map[string]any{"hasWizard": true, "wizardContent": content}
	}
	writeJSON(w, http.StatusOK, map[string]any{"code": 0, "data": map[string]any{"wizardInfo": wizardInfo}})
}

func (mp *mockPanel) handleInstallStatus(w http.ResponseWriter, r *http.Request) {
	mp.mu.Lock()
	mp.instPoll++
	poll := mp.instPoll
	failMsg := mp.installFailMsg
	mp.mu.Unlock()
	status := 1
	if poll >= 2 {
		status = 2
	}
	if failMsg != "" && status == 2 {
		status = 3
	}
	data := map[string]any{"status": status, "progress": 0.5, "outputText": ""}
	if status == 3 {
		data["outputText"] = failMsg
	}
	writeJSON(w, http.StatusOK, map[string]any{"code": 0, "data": data})
}

// ── WS 帧读写（测试侧）──────────────────────────────────────────────────────

func (mp *mockPanel) readClientFrame(r *bufio.Reader) ([]byte, error) {
	if _, err := r.ReadByte(); err != nil { // FIN + opcode（客户端文本帧恒 0x81）
		return nil, err
	}
	b2, err := r.ReadByte()
	if err != nil {
		return nil, err
	}
	ln := int(b2 & 0x7f)
	if ln == 126 {
		var buf [2]byte
		if _, err := io.ReadFull(r, buf[:]); err != nil {
			return nil, err
		}
		ln = int(binary.BigEndian.Uint16(buf[:]))
	} else if ln == 127 {
		var buf [8]byte
		if _, err := io.ReadFull(r, buf[:]); err != nil {
			return nil, err
		}
		ln = int(binary.BigEndian.Uint64(buf[:]))
	}
	mask := make([]byte, 4)
	if _, err := io.ReadFull(r, mask); err != nil {
		return nil, err
	}
	payload := make([]byte, ln)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, err
	}
	for i := range payload {
		payload[i] ^= mask[i%4]
	}
	return payload, nil
}

func (mp *mockPanel) writeServerFrame(conn net.Conn, payload any) {
	data, _ := json.Marshal(payload)
	hdr := make([]byte, 0, 10)
	hdr = append(hdr, 0x81)
	n := len(data)
	if n < 126 {
		hdr = append(hdr, byte(n))
	} else {
		hdr = append(hdr, 126)
		hdr = binary.BigEndian.AppendUint16(hdr, uint16(n))
	}
	_, _ = conn.Write(append(hdr, data...))
}

func sha1Append(key, magic string) []byte {
	h := sha1.New()
	h.Write([]byte(key + magic))
	return h.Sum(nil)
}

// ── fake AppCenter（List 可注入，其余 no-op）────────────────────────────────

type fakeAppCenter struct {
	mu   sync.Mutex
	apps []platform.InstalledApp
}

func (f *fakeAppCenter) List() ([]platform.InstalledApp, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]platform.InstalledApp(nil), f.apps...), nil
}

func (f *fakeAppCenter) add(appName string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.apps = append(f.apps, platform.InstalledApp{AppName: appName, Version: "x", Status: "running"})
}
func (f *fakeAppCenter) Check(string) (bool, error) { return false, nil }
func (f *fakeAppCenter) Status(string) (string, error) {
	return "running", nil
}
func (f *fakeAppCenter) InstallFpk(string, int) error                 { return nil }
func (f *fakeAppCenter) InstallLocal(string, int, bool) error         { return nil }
func (f *fakeAppCenter) Uninstall(context.Context, string) error      { return nil }
func (f *fakeAppCenter) Start(string) error                           { return nil }
func (f *fakeAppCenter) Stop(string) error                            { return nil }
func (f *fakeAppCenter) StartConfirmed(context.Context, string) error { return nil }
func (f *fakeAppCenter) StopConfirmed(context.Context, string) error  { return nil }
func (f *fakeAppCenter) DefaultVolume() (int, error)                  { return 1, nil }
func (f *fakeAppCenter) SetDefaultVolume(int) error                   { return nil }
func (f *fakeAppCenter) ListVolumes() ([]platform.VolumeInfo, error)  { return nil, nil }
func (f *fakeAppCenter) AppInstallVolume(string) (int, bool, error)   { return 0, false, nil }
func (f *fakeAppCenter) UpgradeCapability() platform.UpgradeCapability {
	return platform.UpgradeCapability{}
}
func (f *fakeAppCenter) DaemonInstallAvailable() bool                 { return true }
func (f *fakeAppCenter) UpgradeFpk(context.Context, string, []platform.WizardParam) error {
	return nil
}
func (f *fakeAppCenter) FetchWizard(context.Context, string) (*platform.AppWizard, error) {
	return nil, nil
}
func (f *fakeAppCenter) InstallFpkWithWizard(context.Context, string, int, []platform.WizardParam) error {
	return nil
}

// ── 端到端：带依赖的官方应用 cloud 安装 ──────────────────────────────────────

// TestPanelInstallWithDependency 验证完整面板通道：
// WS 登录 → 依赖先行 cloud 安装 → 主应用 cloud 安装 → 安装后验证。
func TestPanelInstallWithDependency(t *testing.T) {
	mp := newMockPanel(t)

	registry := core.NewRegistry()
	registry.Merge(nil, []source.RemoteApp{
		{
			AppName: "mcsmanager", DisplayName: "MC 服务器管理", Version: "2.1.3",
			Source: source.OfficialSourceID, PanelSourceID: "98", AppType: "fpk",
		},
		{
			// 同名依赖在第三方源里也存在 → 依赖弹窗应列出（same_name_apps）
			AppName: "nodejs_v22", DisplayName: "Node.js v22", Version: "22.18.0-1",
			Source: "BlueMink", AppType: "fpk",
		},
	}, nil)

	fake := &fakeAppCenter{}
	mp.onInstalled = fake.add
	server := &Server{
		registry:     registry,
		queue:        NewOperationQueue(),
		panelClient:  panel.NewClient(mp.url, "mockuser", "mock-pass"),
		officialSource: source.NewOfficialSource(panel.NewClient(mp.url, "mockuser", "mock-pass")),
		ac:           fake,
		appsDir:      t.TempDir(),
	}

	req := httptest.NewRequest(http.MethodPost,
		"/api/apps/mcsmanager/install?panel="+url.QueryEscape(`{"volumeID":1}`), nil)
	req.SetPathValue("appname", "mcsmanager")
	rec := httptest.NewRecorder()

	server.handleInstall(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, `"step":"done"`) {
		t.Fatalf("install should finish with done, got:\n%s", body)
	}
	// 依赖先于主应用；依赖第一次下载是向导预取（真面板里缓存复用，
	// 不重复传输），第二次才是安装流程本体。
	dl := mp.dlCalls
	if len(dl) != 3 || dl[0] != "nodejs_v22" || dl[1] != "nodejs_v22" || dl[2] != "mcsmanager" {
		t.Fatalf("download order = %v, want [nodejs_v22 nodejs_v22 mcsmanager]", dl)
	}
	inst := mp.instCalls
	if len(inst) != 2 || inst[0] != "nodejs_v22" || inst[1] != "mcsmanager" {
		t.Fatalf("install order = %v, want [nodejs_v22 mcsmanager]", inst)
	}
	// SSE 里应能看到依赖安装提示
	if !strings.Contains(body, "正在安装依赖 Node.js v22") {
		t.Errorf("expected dependency progress event, got:\n%s", body)
	}
}

// TestPanelInstallSkippedDependency 用户选择跳过依赖（用已有同名应用）时，
// 依赖不应触发任何下载/安装。
func TestPanelInstallSkippedDependency(t *testing.T) {
	mp := newMockPanel(t)

	registry := core.NewRegistry()
	registry.Merge(nil, []source.RemoteApp{
		{AppName: "mcsmanager", DisplayName: "MC 服务器管理", Version: "2.1.3",
			Source: source.OfficialSourceID, PanelSourceID: "98", AppType: "fpk"},
	}, nil)

	fake := &fakeAppCenter{}
	mp.onInstalled = fake.add
	server := &Server{
		registry:       registry,
		queue:          NewOperationQueue(),
		panelClient:    panel.NewClient(mp.url, "mockuser", "mock-pass"),
		officialSource: source.NewOfficialSource(panel.NewClient(mp.url, "mockuser", "mock-pass")),
		ac:             fake,
		appsDir:        t.TempDir(),
	}

	req := httptest.NewRequest(http.MethodPost,
		"/api/apps/mcsmanager/install?panel="+url.QueryEscape(`{"volumeID":1,"deps":[{"appName":"nodejs_v22","action":"skip"}]}`), nil)
	req.SetPathValue("appname", "mcsmanager")
	rec := httptest.NewRecorder()

	server.handleInstall(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, `"step":"done"`) {
		t.Fatalf("install should finish with done, got:\n%s", body)
	}
	if len(mp.dlCalls) != 1 || mp.dlCalls[0] != "mcsmanager" {
		t.Fatalf("dep should be skipped; downloads = %v", mp.dlCalls)
	}
	if !strings.Contains(body, "按你的选择跳过依赖 Node.js v22") {
		t.Errorf("expected skip note, got:\n%s", body)
	}
}

// TestPanelInstallFail 面板安装失败时 SSE 以 error 收尾（依赖失败不装主应用）。
func TestPanelInstallFail(t *testing.T) {
	mp := newMockPanel(t)
	mp.installFailMsg = "磁盘空间不足"

	registry := core.NewRegistry()
	registry.Merge(nil, []source.RemoteApp{
		{AppName: "mcsmanager", DisplayName: "MC 服务器管理", Version: "2.1.3",
			Source: source.OfficialSourceID, PanelSourceID: "98", AppType: "fpk"},
	}, nil)

	server := &Server{
		registry:       registry,
		queue:          NewOperationQueue(),
		panelClient:    panel.NewClient(mp.url, "mockuser", "mock-pass"),
		officialSource: source.NewOfficialSource(panel.NewClient(mp.url, "mockuser", "mock-pass")),
		ac:             &fakeAppCenter{},
		appsDir:        t.TempDir(),
	}

	req := httptest.NewRequest(http.MethodPost, "/api/apps/mcsmanager/install", nil)
	req.SetPathValue("appname", "mcsmanager")
	rec := httptest.NewRecorder()

	server.handleInstall(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, `"step":"error"`) {
		t.Fatalf("install should fail with error step, got:\n%s", body)
	}
	if !strings.Contains(body, "磁盘空间不足") {
		t.Errorf("error message should carry panel output, got:\n%s", body)
	}
}

// TestHandlePanelDetail 依赖弹窗数据：实时依赖状态 + 商店目录同名条目。
func TestHandlePanelDetail(t *testing.T) {
	mp := newMockPanel(t)

	registry := core.NewRegistry()
	registry.Merge(nil, []source.RemoteApp{
		{AppName: "mcsmanager", DisplayName: "MC 服务器管理", Version: "2.1.3",
			Source: source.OfficialSourceID, PanelSourceID: "98", AppType: "fpk"},
		{AppName: "nodejs_v22", DisplayName: "Node.js v22", Version: "22.18.0-1",
			Source: "BlueMink", AppType: "fpk"},
	}, nil)

	server := &Server{
		registry:       registry,
		queue:          NewOperationQueue(),
		panelClient:    panel.NewClient(mp.url, "mockuser", "mock-pass"),
		officialSource: source.NewOfficialSource(panel.NewClient(mp.url, "mockuser", "mock-pass")),
		appsDir:        t.TempDir(),
	}

	req := httptest.NewRequest(http.MethodGet, "/api/apps/mcsmanager/panel-detail", nil)
	req.SetPathValue("appname", "mcsmanager")
	rec := httptest.NewRecorder()
	server.handlePanelDetail(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var d panelAppDetailResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &d); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(d.App.InstallDepApps) != 1 || d.App.InstallDepApps[0].AppName != "nodejs_v22" {
		t.Fatalf("deps = %+v", d.App.InstallDepApps)
	}
	same, ok := d.SameNameApps["nodejs_v22"]
	if !ok || len(same) != 1 || !strings.Contains(same[0], "BlueMink") {
		t.Fatalf("same_name_apps = %+v, want nodejs_v22 from BlueMink", d.SameNameApps)
	}
}

// TestPanelClientLoginContract 锁定 WS 登录契约：ver 缺失（老 bug）时面板不
// 签发 ticket，客户端必须报错而不是拿到空会话。
func TestPanelClientLoginContract(t *testing.T) {
	mp := newMockPanel(t)
	c := panel.NewClient(mp.url, "mockuser", "mock-pass")

	apps, err := c.AppList(context.Background())
	if err != nil {
		t.Fatalf("AppList: %v", err)
	}
	if len(apps) != 2 {
		t.Fatalf("apps = %d, want 2", len(apps))
	}
	if apps[0].SourceID != "98" || apps[0].AppName != "mcsmanager" {
		t.Fatalf("first app = %+v", apps[0])
	}

	// 密码错误：mock 面板不签发 ticket
	bad := panel.NewClient(mp.url, "mockuser", "wrong")
	if _, err := bad.AppList(context.Background()); err == nil {
		t.Fatal("wrong password should fail login")
	}
}

// TestPanelInstallWizardAutoFill 依赖带向导时按 install/info 的 initValue
// 自动填充 customParameters（{key,value} 格式）；无向导的主应用发空数组。
func TestPanelInstallWizardAutoFill(t *testing.T) {
	mp := newMockPanel(t)
	mp.wizards["nodejs_v22"] = []map[string]any{
		{"items": []map[string]any{
			{"type": "tips", "helpText": "欢迎安装"},
			{"type": "text", "field": "wizard_app_port", "label": "端口", "initValue": "3000",
				"rules": []map[string]any{{"required": true}}},
		}},
	}

	registry := core.NewRegistry()
	registry.Merge(nil, []source.RemoteApp{
		{AppName: "mcsmanager", DisplayName: "MC 服务器管理", Version: "2.1.3",
			Source: source.OfficialSourceID, PanelSourceID: "98", AppType: "fpk"},
	}, nil)

	fake := &fakeAppCenter{}
	mp.onInstalled = fake.add
	server := &Server{
		registry:       registry,
		queue:          NewOperationQueue(),
		panelClient:    panel.NewClient(mp.url, "mockuser", "mock-pass"),
		officialSource: source.NewOfficialSource(panel.NewClient(mp.url, "mockuser", "mock-pass")),
		ac:             fake,
		appsDir:        t.TempDir(),
	}

	req := httptest.NewRequest(http.MethodPost,
		"/api/apps/mcsmanager/install?panel="+url.QueryEscape(`{"volumeID":1}`), nil)
	req.SetPathValue("appname", "mcsmanager")
	rec := httptest.NewRecorder()
	server.handleInstall(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, `"step":"done"`) {
		t.Fatalf("install should finish with done, got:\n%s", body)
	}
	got := mp.instCustomParams["nodejs_v22"]
	if len(got) != 1 || got[0]["key"] != "wizard_app_port" || got[0]["value"] != "3000" {
		t.Fatalf("dep customParameters = %v, want [{wizard_app_port 3000}]", got)
	}
	if len(mp.instCustomParams["mcsmanager"]) != 0 {
		t.Fatalf("main customParameters = %v, want empty", mp.instCustomParams["mcsmanager"])
	}
}

// TestPanelInstallWizardOldPanel 老面板没有 install/info 端点（404）时，
// 依赖安装不传向导参数照常进行，而不是整个安装失败。
func TestPanelInstallWizardOldPanel(t *testing.T) {
	mp := newMockPanel(t)
	mp.noInstallInfo = true

	registry := core.NewRegistry()
	registry.Merge(nil, []source.RemoteApp{
		{AppName: "mcsmanager", DisplayName: "MC 服务器管理", Version: "2.1.3",
			Source: source.OfficialSourceID, PanelSourceID: "98", AppType: "fpk"},
	}, nil)

	fake := &fakeAppCenter{}
	mp.onInstalled = fake.add
	server := &Server{
		registry:       registry,
		queue:          NewOperationQueue(),
		panelClient:    panel.NewClient(mp.url, "mockuser", "mock-pass"),
		officialSource: source.NewOfficialSource(panel.NewClient(mp.url, "mockuser", "mock-pass")),
		ac:             fake,
		appsDir:        t.TempDir(),
	}

	req := httptest.NewRequest(http.MethodPost,
		"/api/apps/mcsmanager/install?panel="+url.QueryEscape(`{"volumeID":1}`), nil)
	req.SetPathValue("appname", "mcsmanager")
	rec := httptest.NewRecorder()
	server.handleInstall(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, `"step":"done"`) {
		t.Fatalf("install should finish with done on old panel, got:\n%s", body)
	}
	if len(mp.instCustomParams["nodejs_v22"]) != 0 {
		t.Fatalf("old panel: customParameters = %v, want empty", mp.instCustomParams["nodejs_v22"])
	}
}


