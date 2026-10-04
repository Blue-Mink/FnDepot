// Package km 探测本机 kspeeder 依赖应用，并只读查询其管理接口。
//
// 1.2.0 起本应用不再捆绑 iStoreEnhance 引擎：Docker 拉取加速依赖独立
// 安装的 kspeeder 应用（默认 registry 端口 5443 / 管理端口 5003）。
// 本包负责：
//   - 检测 kspeeder 应用是否安装（manifest 路径）与应用版本；
//   - 探活 registry 端口（活着时作为 Docker 上游第一优先）；
//   - 从管理接口读取引擎版本与加速节点运行态，供控制台展示。
package km

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Mode 描述 kspeeder 应用的检测状态。
const (
	// ModeRunning 表示 kspeeder 应用已安装且 registry 端口在听，
	// 其地址作为 Docker 上游第一优先。
	ModeRunning = "running"
	// ModeStopped 表示已安装但 registry 未运行（需在应用中心启动）。
	ModeStopped = "stopped"
	// ModeAbsent 表示本机未安装 kspeeder 应用。
	ModeAbsent = "absent"
)

// 推荐的最低引擎版本：0.8.2 起 blob 分段下载 + 多候选节点竞速，
// 单节点传输中断（RST/520）自动换源，低版本可能出现整层拉取失败。
const RecommendedEngineMin = "0.8.2"

// Status 是 /api/kspeeder 返回的 kspeeder 应用状态。
type Status struct {
	Mode          string `json:"mode"`
	Installed     bool   `json:"installed"`
	AppVersion    string `json:"app_version,omitempty"`
	EngineVersion string `json:"engine_version,omitempty"`
	// EngineBelowRecommended 表示引擎版本低于 RecommendedEngineMin
	// （0.8.2 起才有分段下载+节点竞速，低版本单节点中断可能整层失败）。
	EngineBelowRecommended bool   `json:"engine_below_recommended,omitempty"`
	Registry               string `json:"registry"`
	RegistryUp             bool   `json:"registry_up"`
	Admin                  string `json:"admin"`
	// URL 是当前生效的本机 Docker 接入地址（不可用时为空）。
	URL    string `json:"url,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// Manager 探测 kspeeder 依赖应用。
type Manager struct {
	logf        func(level, format string, args ...any)
	registryOvr string // 显式覆盖的 registry 地址（空 = 跟随 kspeeder 实际端口）
	adminOvr    string // 显式覆盖的管理接口地址（空 = 跟随 kspeeder 实际端口）
	manifest    string
	portConf    string // kspeeder 应用的端口配置（ports.conf）

	mu                 sync.Mutex
	regResolved        string
	adminResolved      string
	resolvedAt         time.Time
	up                 bool
	upCheckedAt        time.Time
	installedCached    bool
	appVer             string
	appVerChecked      bool
	appVerCheckedAt    time.Time
	engineVer          string
	engineVerChecked   bool
	engineVerCheckedAt time.Time
	nodesCache         []NodeInfo
	nodesCachedAt      time.Time
}

// New 创建探测器。registryOverride/adminOverride 为显式覆盖地址（空串表示
// 自动跟随 kspeeder 应用自身配置的端口）；manifest 是应用中心 manifest
// 路径；portConf 是 kspeeder 应用的端口配置文件（ADMIN_PORT/PROXY_PORT）。
func New(registryOverride, adminOverride, manifest, portConf string, logf func(level, format string, args ...any)) *Manager {
	if logf == nil {
		logf = func(string, string, ...any) {}
	}
	return &Manager{
		logf:        logf,
		registryOvr: strings.TrimRight(registryOverride, "/"),
		adminOvr:    strings.TrimRight(adminOverride, "/"),
		manifest:    manifest,
		portConf:    portConf,
	}
}

// resolve 计算当前生效的 registry / 管理接口地址（缓存 60 秒）。
//
// 端口一律跟随 kspeeder 应用自己的配置：读它的 ports.conf
// （ADMIN_PORT/ADMIN 端口与 PROXY_PORT/registry 端口，用户可在该应用
// 设置里改，dog-dev 随之生效）；文件缺失时回落默认 5443/5003。
// 配置里显式设置的地址优先（覆盖自动跟随）。
func (m *Manager) resolve() (registry, admin string) {
	m.mu.Lock()
	if m.regResolved != "" && time.Since(m.resolvedAt) < 60*time.Second {
		reg, adm := m.regResolved, m.adminResolved
		m.mu.Unlock()
		return reg, adm
	}
	m.mu.Unlock()

	reg, adm := m.registryOvr, m.adminOvr
	if reg == "" || adm == "" {
		proxyPort, adminPort := 5443, 5003
		if data, err := os.ReadFile(m.portConf); err == nil {
			if p, ok := parsePortConf(data, "PROXY_PORT"); ok {
				proxyPort = p
			}
			if p, ok := parsePortConf(data, "ADMIN_PORT"); ok {
				adminPort = p
			}
		}
		if reg == "" {
			reg = fmt.Sprintf("https://127.0.0.1:%d", proxyPort)
		}
		if adm == "" {
			adm = fmt.Sprintf("http://127.0.0.1:%d", adminPort)
		}
	}
	m.mu.Lock()
	m.regResolved, m.adminResolved = reg, adm
	m.resolvedAt = time.Now()
	m.mu.Unlock()
	return reg, adm
}

// parsePortConf 从 "KEY=VALUE" 行式配置里取一个端口值。
func parsePortConf(data []byte, key string) (int, bool) {
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if v, ok := strings.CutPrefix(line, key+"="); ok {
			n, err := strconv.Atoi(strings.TrimSpace(v))
			if err == nil && n > 0 && n < 65536 {
				return n, true
			}
		}
	}
	return 0, false
}

var versionRe = regexp.MustCompile(`(?m)^version\s*=\s*([0-9][0-9A-Za-z.\-]*)`)

// Installed 判断 kspeeder 应用是否已安装，并返回应用版本（缓存 60 秒）。
func (m *Manager) Installed() (bool, string) {
	m.mu.Lock()
	if m.appVerChecked && time.Since(m.appVerCheckedAt) < 60*time.Second {
		cached := m.installedCached
		ver := m.appVer
		m.mu.Unlock()
		return cached, ver
	}
	m.mu.Unlock()

	installed := false
	ver := ""
	if data, err := os.ReadFile(m.manifest); err == nil {
		installed = true
		if loc := versionRe.FindSubmatch(data); loc != nil {
			ver = string(loc[1])
		}
	}
	m.mu.Lock()
	m.installedCached = installed
	m.appVer = ver
	m.appVerChecked = true
	m.appVerCheckedAt = time.Now()
	m.mu.Unlock()
	return installed, ver
}

// RegistryUp 探活 registry 端口（TCP 拨号，结果缓存 10 秒）。
//
// dockerExtra 每请求都会调用它，必须廉价：本机 TCP 拨号亚毫秒级，
// 真实 /v2/ 探活由 Docker 上游池的监测节拍负责。
func (m *Manager) RegistryUp() bool {
	m.mu.Lock()
	if m.up && time.Since(m.upCheckedAt) < 10*time.Second {
		m.mu.Unlock()
		return true
	}
	stale := time.Since(m.upCheckedAt) < 10*time.Second
	up := m.up
	m.mu.Unlock()
	if stale {
		return up
	}
	registryURL, _ := m.resolve()
	u, err := url.Parse(registryURL)
	up = false
	if err == nil && u.Host != "" {
		host, port, err2 := net.SplitHostPort(u.Host)
		if err2 != nil || port == "" {
			port = "5443"
		}
		if host == "" {
			host = "127.0.0.1"
		}
		conn, err2 := net.DialTimeout("tcp", net.JoinHostPort(host, port), 1500*time.Millisecond)
		if err2 == nil {
			_ = conn.Close()
			up = true
		}
	}
	m.mu.Lock()
	m.up = up
	m.upCheckedAt = time.Now()
	m.mu.Unlock()
	if !up {
		m.logf("debug", "KSpeeder: registry %s 未响应", registryURL)
	}
	return up
}

// EngineVersion 从管理接口 /api/summary 读取引擎版本（成功缓存 60 秒，
// 失败缓存 10 秒避免打爆管理口）。管理口不可达时返回空串。
func (m *Manager) EngineVersion(ctx context.Context) string {
	m.mu.Lock()
	ttl := 10 * time.Second
	if m.engineVer != "" {
		ttl = 60 * time.Second
	}
	if m.engineVerChecked && time.Since(m.engineVerCheckedAt) < ttl {
		ver := m.engineVer
		m.mu.Unlock()
		return ver
	}
	m.mu.Unlock()

	ver := ""
	_, adminURL := m.resolve()
	reqCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, adminURL+"/api/summary", nil)
	if err == nil {
		resp, err := nodeHTTPClient.Do(req)
		if err == nil {
			body, rerr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
			_ = resp.Body.Close()
			if rerr == nil && resp.StatusCode == http.StatusOK {
				var sum struct {
					Version string `json:"version"`
				}
				if json.Unmarshal(body, &sum) == nil {
					ver = sum.Version
				}
			}
		}
	}
	cancel()

	m.mu.Lock()
	m.engineVer = ver
	m.engineVerChecked = true
	m.engineVerCheckedAt = time.Now()
	m.mu.Unlock()
	return ver
}

// Registry 返回当前生效的 registry 接入地址（跟随 kspeeder 应用端口）。
func (m *Manager) Registry() string { reg, _ := m.resolve(); return reg }

// Admin 返回当前生效的管理接口地址（跟随 kspeeder 应用端口）。
func (m *Manager) Admin() string { _, adm := m.resolve(); return adm }

// Status 汇总检测状态（供 API 使用）。
func (m *Manager) Status() Status {
	installed, appVer := m.Installed()
	up := m.RegistryUp()
	registry, admin := m.resolve()
	st := Status{
		Registry:   registry,
		Admin:      admin,
		Installed:  installed,
		AppVersion: appVer,
		RegistryUp: up,
	}
	switch {
	case !installed:
		st.Mode = ModeAbsent
		st.Reason = "本机未安装 kspeeder 应用；安装并启动后，Docker 拉取将自动经其加速。"
	case !up:
		st.Mode = ModeStopped
		st.Reason = "kspeeder 应用已安装但未运行；在应用中心启动后自动生效。"
	default:
		st.Mode = ModeRunning
		st.URL = registry
	}
	return st
}

// VersionBelow 判断版本 v 是否低于 min（语义化前缀比较，容错非标准版本）。
func VersionBelow(v, min string) bool {
	v = strings.TrimPrefix(v, "v")
	min = strings.TrimPrefix(min, "v")
	pv, ok1 := parseVersion(v)
	pm, ok2 := parseVersion(min)
	if !ok1 || !ok2 {
		return false // 无法比较时不告警
	}
	for i := 0; i < 3; i++ {
		if pv[i] != pm[i] {
			return pv[i] < pm[i]
		}
	}
	return false
}

func parseVersion(v string) ([3]int, bool) {
	var out [3]int
	parts := strings.SplitN(v, ".", 3)
	ok := len(parts) >= 3
	for i := 0; i < 3; i++ {
		n, err := strconv.Atoi(strings.TrimSpace(parts[i]))
		if err != nil {
			ok = false
			break
		}
		out[i] = n
	}
	return out, ok
}

// Nodes 抓取并解析 kspeeder 引擎全部加速节点的运行态。
//
// 数据来自管理 API 的 /api/accel/snapshot（按加速域逐个拉取），
// 把 plan.Slots 的节点元数据与 status.States 的运行时状态按 NodeID 合并。
// 结果缓存 5 秒，界面 10 秒级轮询不会把管理口打爆。
func (m *Manager) Nodes(ctx context.Context) ([]NodeInfo, error) {
	m.mu.Lock()
	if now := time.Now(); now.Sub(m.nodesCachedAt) < 5*time.Second && m.nodesCache != nil {
		cached := m.nodesCache
		m.mu.Unlock()
		return cached, nil
	}
	m.mu.Unlock()

	_, adminURL := m.resolve()
	profiles := []string{"docker:dockerhub", "docker:ghcr"}
	var out []NodeInfo
	var lastErr error
	for _, profile := range profiles {
		u := fmt.Sprintf("%s/api/accel/snapshot?profile=%s", adminURL, urlQueryEscape(profile))
		reqCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
		req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, u, nil)
		if err != nil {
			lastErr = err
			cancel()
			continue
		}
		resp, err := nodeHTTPClient.Do(req)
		if err != nil {
			lastErr = err
			cancel()
			continue
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
		_ = resp.Body.Close()
		cancel()
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode != http.StatusOK {
			lastErr = fmt.Errorf("kspeeder 管理 API 返回状态码 %d", resp.StatusCode)
			continue
		}
		var snap snapResp
		if err := json.Unmarshal(body, &snap); err != nil {
			lastErr = fmt.Errorf("解析引擎快照失败: %w", err)
			continue
		}
		out = append(out, mergeSnapshot(profile, snap)...)
	}
	if len(out) == 0 {
		if lastErr == nil {
			lastErr = fmt.Errorf("kspeeder 应用未返回节点数据（未安装或未运行？）")
		}
		return nil, lastErr
	}

	m.mu.Lock()
	m.nodesCache = out
	m.nodesCachedAt = time.Now()
	m.mu.Unlock()
	return out, nil
}

// mergeSnapshot 把单个加速域的快照合并成节点列表（按"存活、健康、测速快"排序）。
func mergeSnapshot(profile string, snap snapResp) []NodeInfo {
	states := make(map[string]snapState, len(snap.Status.States))
	for _, st := range snap.Status.States {
		states[st.NodeID] = st
	}
	nodes := make([]NodeInfo, 0, len(snap.Plan.Slots))
	for _, slot := range snap.Plan.Slots {
		n := NodeInfo{
			Profile:  profile,
			Name:     slot.Node.DisplayName,
			NodeID:   slot.Node.NodeID,
			Priority: slot.Node.Priority,
			IsInternal: slot.Node.IsInternal,
		}
		if n.Name == "" {
			n.Name = slot.Node.NodeID
		}
		if st, ok := states[slot.Node.NodeID]; ok {
			n.Alive = st.Alive
			n.State = st.State
			n.Enabled = st.Runtime.Enabled
			n.TestSpeed = st.TestSpeed
			n.LiveSpeed = st.LiveSpeed
			n.LiveBytesTotal = st.LiveBytesTotal
			n.ConsecutiveFail = st.ConsecutiveFail
			n.LiveConsecutiveFail = st.LiveConsecutiveFail
			n.LastErr = st.LastErr
			n.LastLiveErr = st.LastLiveErr
			n.LastTestAt = normTime(st.LastTestAt)
			n.DisabledUntil = normTime(st.LiveDisabledUntil)
		} else {
			n.State = "unknown"
		}
		nodes = append(nodes, n)
	}
	sort.SliceStable(nodes, func(i, j int) bool {
		a, b := nodes[i], nodes[j]
		if a.Alive != b.Alive {
			return a.Alive
		}
		ah, bh := a.State == "healthy", b.State == "healthy"
		if ah != bh {
			return ah
		}
		if a.TestSpeed != b.TestSpeed {
			return a.TestSpeed > b.TestSpeed
		}
		return a.Priority < b.Priority
	})
	return nodes
}

// urlQueryEscape 做最小化的 query 转义（只处理 : 与 /）。
func urlQueryEscape(s string) string {
	s = strings.ReplaceAll(s, ":", "%3A")
	s = strings.ReplaceAll(s, "/", "%2F")
	return s
}

// NodeInfo 是引擎内一个加速节点的运行态（来自引擎管理 API 快照）。
type NodeInfo struct {
	// Profile 是节点所属加速域（docker:dockerhub / docker:ghcr）。
	Profile string `json:"profile"`
	// Name 是节点展示名（DisplayName，缺失时回退 NodeID）。
	Name string `json:"name"`
	// NodeID 是引擎内部节点标识。
	NodeID string `json:"node_id"`
	// Alive 表示节点最近是否存活。
	Alive bool `json:"alive"`
	// State 是引擎给出的状态（healthy / disabled 等）。
	State string `json:"state"`
	// Enabled 表示节点当前是否被引擎启用（Runtime.Enabled）。
	Enabled bool `json:"enabled"`
	// Priority 是引擎内优先级（数值越小越优先）。
	Priority int `json:"priority"`
	// IsInternal 表示引擎内置节点（如 d4ctech 快速通道）。
	IsInternal bool `json:"is_internal,omitempty"`
	// TestSpeed 是引擎主动测速得到的吞吐（字节/秒）。
	TestSpeed int64 `json:"test_speed_bps"`
	// LiveSpeed 是实时转发吞吐（字节/秒）。
	LiveSpeed int64 `json:"live_speed_bps"`
	// LiveBytesTotal 是实时转发累计字节数。
	LiveBytesTotal int64 `json:"live_bytes_total"`
	// ConsecutiveFail 是主动测速连续失败次数。
	ConsecutiveFail int `json:"consecutive_fail"`
	// LiveConsecutiveFail 是实时转发连续失败次数。
	LiveConsecutiveFail int `json:"live_consecutive_fail"`
	// LastErr 是最近一次主动测速失败原因。
	LastErr string `json:"last_err,omitempty"`
	// LastLiveErr 是最近一次实时转发失败原因。
	LastLiveErr string `json:"last_live_err,omitempty"`
	// LastTestAt 是最近一次测速时间（RFC3339）。
	LastTestAt string `json:"last_test_at,omitempty"`
	// DisabledUntil 是实时通道被临时禁用到的时刻（空表示未禁用）。
	DisabledUntil string `json:"disabled_until,omitempty"`
}

// snapNode 是快照 plan.Slots[i] 的结构（引擎用大写 JSON 键）。
type snapNode struct {
	Node struct {
		DisplayName string `json:"DisplayName"`
		NodeID      string `json:"NodeID"`
		Priority    int    `json:"Priority"`
		IsInternal  bool   `json:"IsInternal"`
	} `json:"Node"`
}

// snapState 是快照 status.States[i] 的结构。
type snapState struct {
	NodeID              string `json:"NodeID"`
	Alive               bool   `json:"Alive"`
	State               string `json:"State"`
	TestSpeed           int64  `json:"TestSpeed"`
	LiveSpeed           int64  `json:"LiveSpeed"`
	LiveBytesTotal      int64  `json:"LiveBytesTotal"`
	ConsecutiveFail     int    `json:"ConsecutiveFail"`
	LiveConsecutiveFail int    `json:"LiveConsecutiveFail"`
	LastErr             string `json:"LastErr"`
	LastLiveErr         string `json:"LastLiveErr"`
	LastTestAt          string `json:"LastTestAt"`
	LiveDisabledUntil   string `json:"LiveDisabledUntil"`
	Runtime             struct {
		Enabled bool `json:"Enabled"`
	} `json:"Runtime"`
}

type snapResp struct {
	Plan struct {
		Slots []snapNode `json:"Slots"`
	} `json:"plan"`
	Status struct {
		States []snapState `json:"States"`
	} `json:"status"`
}

// nodeHTTPClient 是访问 kspeeder 管理 API 的专用客户端。
var nodeHTTPClient = &http.Client{Timeout: 5 * time.Second}

var zeroTime = "0001-01-01T00:00:00Z"

func normTime(s string) string {
	if s == "" || s == zeroTime {
		return ""
	}
	return s
}
