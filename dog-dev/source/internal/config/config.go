// Package config 定义 GitHub++ 加速器的配置模型与持久化逻辑。
//
// 配置以 YAML 形式保存，路径解析优先级为：
// 显式命令行参数 > 飞牛 fnOS 环境变量(TRIM_PKGVAR) > 用户目录 > 当前目录。
package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// Mode 表示加速工作模式。
type Mode string

const (
	// ModeAuto 由程序自行探测网络环境后选择最优方式。
	ModeAuto Mode = "auto"
	// ModeProxy 仅使用本地反代 + 镜像中转。
	ModeProxy Mode = "proxy"
	// ModeHosts 仅使用 DNS 优选 + hosts 注入。
	ModeHosts Mode = "hosts"
	// ModeDirect 完全旁路，不做任何加速。
	ModeDirect Mode = "direct"
)

// AllModes 列出全部合法模式，供配置校验与前端下拉框使用。
func AllModes() []Mode {
	return []Mode{ModeAuto, ModeProxy, ModeHosts, ModeDirect}
}

// IsValid 判断模式字符串是否为受支持的取值。
func (m Mode) IsValid() bool {
	for _, v := range AllModes() {
		if v == m {
			return true
		}
	}
	return false
}

// MirrorKind 区分镜像源的接入形态，决定 URL 如何改写。
type MirrorKind string

const (
	// KindPrefix 前缀型中转站，形如 https://ghproxy.com/https://github.com/...
	KindPrefix MirrorKind = "prefix"
	// KindRawCDN 专门服务 raw.githubusercontent.com 的 CDN 源。
	KindRawCDN MirrorKind = "raw"
	// KindGitClone 仅供 git clone/pull 使用的仓库镜像。
	KindGitClone MirrorKind = "git"
	// KindPathFetch 路径抓取型中转（xget 形态）：
	// /gh/owner/repo[/raw]/ref/path，仅能处理 raw 文件抓取。
	KindPathFetch MirrorKind = "pathfetch"
	// KindDirect 直连 GitHub 官方源，作为兜底选项。
	KindDirect MirrorKind = "direct"
)

// Mirror 描述一个可用的 GitHub 加速源。
type Mirror struct {
	// ID 为稳定标识，用于统计与前端引用。
	ID string `yaml:"id" json:"id"`
	// Name 是展示名称。
	Name string `yaml:"name" json:"name"`
	// URL 是源地址，语义随 Kind 变化。
	URL string `yaml:"url" json:"url"`
	// Kind 决定 URL 改写规则。
	Kind MirrorKind `yaml:"kind" json:"kind"`
	// Enabled 控制该源是否参与调度。
	Enabled bool `yaml:"enabled" json:"enabled"`
	// Weight 是人工权重，数值越高越优先，与实测得分相乘。
	Weight float64 `yaml:"weight" json:"weight"`
	// Note 保存备注，便于用户记录来源。
	Note string `yaml:"note,omitempty" json:"note,omitempty"`
}

// ProxyConfig 控制反向代理服务的行为。
type ProxyConfig struct {
	// Listen 是 HTTP 代理服务监听地址。
	Listen string `yaml:"listen" json:"listen"`
	// TLSPort 是 HTTPS 监听端口，用于配合本地 CA 做透明加速。
	TLSPort string `yaml:"tls_port" json:"tls_port"`
	// ConnectTimeoutMS 是建立上游连接的超时毫秒数。
	ConnectTimeoutMS int `yaml:"connect_timeout_ms" json:"connect_timeout_ms"`
	// ReadTimeoutMS 是等待上游首字节的超时毫秒数。
	ReadTimeoutMS int `yaml:"read_timeout_ms" json:"read_timeout_ms"`
	// FailoverThreshold 是连续失败多少次后临时摘除该源。
	FailoverThreshold int `yaml:"failover_threshold" json:"failover_threshold"`
	// CooldownSeconds 是被摘除源的冷却时长。
	CooldownSeconds int `yaml:"cooldown_seconds" json:"cooldown_seconds"`
}

// HostsConfig 控制 DNS 优选与 hosts 注入。
type HostsConfig struct {
	// Enabled 表示是否把优选结果写入系统 hosts。
	// 关闭时仍会执行优选探测，仅是不落盘。
	Enabled bool `yaml:"enabled" json:"enabled"`
	// FilePath 是目标 hosts 文件路径。
	FilePath string `yaml:"file_path" json:"file_path"`
	// RefreshMinutes 是重新测速并刷新 hosts 的间隔。
	RefreshMinutes int `yaml:"refresh_minutes" json:"refresh_minutes"`
	// CandidateLimit 是每个域名参与测速的候选 IP 数量上限。
	CandidateLimit int `yaml:"candidate_limit" json:"candidate_limit"`
	// ProbeTimeoutMS 是单个 IP 测速超时毫秒数。
	ProbeTimeoutMS int `yaml:"probe_timeout_ms" json:"probe_timeout_ms"`

	// EnabledSet 记录用户是否显式设置过 Enabled。
	//
	// 存在的意义：区分"用户主动关闭"与"配置文件来自旧版本、字段缺失"。
	// 前者不该被新默认值覆盖，后者应当采纳新默认值。
	EnabledSet bool `yaml:"enabled_set,omitempty" json:"-"`
}

// AutoConfig 定义自动模式的判定阈值。
type AutoConfig struct {
	// ProbeIntervalMinutes 是重新评估网络环境的间隔。
	ProbeIntervalMinutes int `yaml:"probe_interval_minutes" json:"probe_interval_minutes"`
	// MirrorMonitorMinutes 是加速源自动优先监测的间隔。
	//
	// 与 ProbeIntervalMinutes（全量评估：测速 + DNS 优选 + 自动决策）不同，
	// 这里的节拍只做轻量测速（小载荷、不触发 DNS 优选），让界面列表与调度
	// 始终保持"最优节点在前、失效节点沉底"，而不必等一次完整评估。
	MirrorMonitorMinutes int `yaml:"mirror_monitor_minutes" json:"mirror_monitor_minutes"`
	// DirectBetterRatio 表示直连快于加速通道多少倍时才回退直连。
	DirectBetterRatio float64 `yaml:"direct_better_ratio" json:"direct_better_ratio"`
	// MinImproveRatio 表示加速通道至少要比直连快多少倍才启用。
	MinImproveRatio float64 `yaml:"min_improve_ratio" json:"min_improve_ratio"`
}

// ServerConfig 控制 Web 控制台。
type ServerConfig struct {
	// Listen 是控制台监听地址。
	Listen string `yaml:"listen" json:"listen"`
	// Username 是控制台登录用户名。
	Username string `yaml:"username" json:"username"`
	// Password 是控制台登录密码，默认 admin123，登录后可在控制台修改。
	Password string `yaml:"password" json:"password"`
	// SessionTTLHours 是登录会话有效期小时数。
	SessionTTLHours int `yaml:"session_ttl_hours" json:"session_ttl_hours"`
}

// DockerConfig 控制 Docker 镜像拉取加速。
//
// 实现方式：代理端口同时充当 Docker Registry v2 入口，
// 把 /etc/docker/daemon.json 的 registry-mirrors 指向本机即可加速 docker pull。
type DockerConfig struct {
	// Enabled 表示是否在代理端口上启用 Docker Registry 加速。
	Enabled bool `yaml:"enabled" json:"enabled"`
	// Upstreams 是 Docker 上游列表，按优先级排序。
	// 支持镜像站地址（https://docker.1ms.run 等）与内置值 "official"（Docker 官方源）。
	// 请求会依次尝试，某个上游不可用时自动切换下一个。
	Upstreams []string `yaml:"upstreams" json:"upstreams"`
	// AutoMonitor 控制自动监测节拍是否对全部 Docker 上游做 /v2/ 探活，
	// 让"最优在前、失效沉底"的排序持续保鲜（间隔复用 auto.mirror_monitor_minutes）。
	AutoMonitor bool `yaml:"auto_monitor" json:"auto_monitor"`
	// AutoMonitorSet 记录用户是否显式设置过 AutoMonitor，
	// 用于区分"用户主动关闭"与"旧版本配置文件字段缺失"，避免旧配置被零值 false 覆盖。
	AutoMonitorSet bool `yaml:"auto_monitor_set,omitempty" json:"-"`
}

// WatchdogConfig 控制看门狗（fpk/cmd/main 中的 run_with_watchdog）的自动重启行为。
//
// 字段会同步落盘到 ${DataDir}/watchdog_enabled 标记文件（内容 "1" / "0"），
// 看门狗在子进程异常退出时读取该文件决定是否拉起新进程；这样不依赖 YAML 解析，
// bash 也能直接判断。UI 切换 auto_restart 时 API 层会同时更新配置文件与标记文件，
// 保证下次崩溃立即按新值生效。
type WatchdogConfig struct {
	// AutoRestart 为 true 时进程异常退出会被看门狗自动拉起；为 false 时看门狗自己也退出。
	AutoRestart bool `yaml:"auto_restart" json:"auto_restart"`

	// AutoRestartSet 记录用户是否显式设置过 AutoRestart。
	// 用于区分"用户主动关闭"与"旧版本配置文件字段缺失"，避免旧配置被零值 false 覆盖。
	AutoRestartSet bool `yaml:"auto_restart_set,omitempty" json:"-"`
}

// ExternalConfig 保存"外网接入地址"，供控制台「接入方式」区域切换 LAN/WAN 时使用。
//
// 飞牛 NAS 通常部署在内网，控制台从内网访问时 location.hostname 是 LAN IP，
// 但用户在外网设备上配置代理时需要外网域名/IP。让用户手动填一次保存下来，
// 之后切换「外网模式」即可一键生成外网接入示例，不必每次重新输入。
//
// 留空表示未配置，前端会引导用户输入。
type ExternalConfig struct {
	// Host 是用户配置的外网接入地址（域名或 IP，可带端口）。
	// 例：g.example.com、1.2.3.4、g.example.com:37710。
	Host string `yaml:"host" json:"host"`
}

// SystemProxyConfig 控制是否在飞牛 NAS 系统层注入 HTTP 代理环境变量。
//
// 开启时往 /etc/profile.d/ghpp-proxy.sh 写入 http_proxy / https_proxy 指向
// 127.0.0.1:<代理端口>，所有新登录 shell 启动的软件默认走加速器；
// 关闭时删除该文件。已运行的进程不受影响，需重启对应软件才生效。
//
// 注意：这会让 NAS 上所有 HTTP 流量都先经过加速器代理，加速器仅对 GitHub
// 相关域名走镜像加速、其他域名透明转发，所以一般不会拖慢正常联网；
// 但若加速器进程异常，可能影响新 shell 的网络访问，UI 必须给出风险提示。
type SystemProxyConfig struct {
	// Enabled 为 true 时写入 /etc/profile.d/ghpp-proxy.sh；为 false 时删除。
	Enabled bool `yaml:"enabled" json:"enabled"`

	// EnabledSet 标记用户是否显式设置过 Enabled，用于区分"主动关闭"与"旧配置缺失"。
	EnabledSet bool `yaml:"enabled_set,omitempty" json:"-"`
}

// KSpeederConfig 描述本机 kspeeder 依赖应用的接入点。
//
// 1.2.0 起本应用不再捆绑 iStoreEnhance 引擎：Docker 拉取加速依赖独立
// 安装的 kspeeder 应用（其 registry 端口活着时作为 Docker 上游第一优先）。
type KSpeederConfig struct {
	// RegistryURL 是显式覆盖的 registry 加速地址；留空 = 自动跟随
	// kspeeder 应用自身配置的端口（读其 ports.conf，默认 5443）。
	RegistryURL string `yaml:"registry_url" json:"registry_url"`
	// AdminURL 是显式覆盖的管理接口地址；留空 = 自动跟随
	// kspeeder 应用自身配置的端口（读其 ports.conf，默认 5003）。
	AdminURL string `yaml:"admin_url" json:"admin_url"`
	// ManifestPath 是 kspeeder 应用的 manifest 路径，
	// 用于判断是否已安装并读取应用版本。
	ManifestPath string `yaml:"manifest_path" json:"manifest_path"`
	// PortConf 是 kspeeder 应用的端口配置文件（ADMIN_PORT/PROXY_PORT）。
	PortConf string `yaml:"port_conf" json:"port_conf"`
	// DownloadURL 是 kspeeder 应用的可选下载链接（未安装时控制台展示）。
	DownloadURL string `yaml:"download_url" json:"download_url"`
}

// Config 是应用的总配置。
type Config struct {
	Mode        Mode              `yaml:"mode" json:"mode"`
	Proxy       ProxyConfig       `yaml:"proxy" json:"proxy"`
	Hosts       HostsConfig       `yaml:"hosts" json:"hosts"`
	Auto        AutoConfig        `yaml:"auto" json:"auto"`
	Server      ServerConfig      `yaml:"server" json:"server"`
	Docker      DockerConfig      `yaml:"docker" json:"docker"`
	KSpeeder    KSpeederConfig    `yaml:"kspeeder" json:"kspeeder"`
	Watchdog    WatchdogConfig    `yaml:"watchdog" json:"watchdog"`
	External    ExternalConfig    `yaml:"external" json:"external"`
	SystemProxy SystemProxyConfig `yaml:"system_proxy" json:"system_proxy"`
	Mirrors     []Mirror          `yaml:"mirrors" json:"mirrors"`

	// BuiltinRev 记录出厂源清单已合并到的修订号。
	// 升级引入新出厂源时（BuiltinRevision 递增），配置加载会把缺失的
	// 出厂源合入一次；用户删掉的出厂源不会被反复复活。
	BuiltinRev int `yaml:"builtin_rev" json:"builtin_rev"`

	// DataDir 记录配置与运行数据的存放目录，不写入 YAML。
	DataDir string `yaml:"-" json:"data_dir"`
	// path 记录配置文件自身位置，不写入 YAML。
	path string `yaml:"-" json:"-"`

	// mu 用指针持有，使得 Config 可以安全地按值拷贝快照。
	mu *sync.RWMutex `yaml:"-" json:"-"`

	// ---- 异步落盘（防抖 saver） ----
	// 高频修改（如模式切换）只更新内存并置脏，由 saver 合并后落盘：
	// 飞牛数据卷是 btrfs，tmp+rename 的 CoW 元数据开销可突发到数百毫秒，
	// 同步落盘会让控制台接口"切换很慢"。
	//
	// 以下字段用指针/共享语义，Snapshot 的按值拷贝不会复制出独立副本：
	//   - diskMu 指针在快照间共享，序列化所有 tmp+rename 磁盘写；
	//   - saverCh 等在 Snapshot 里置空，快照永远不触发 saver。
	dirty     bool
	saverCh   chan struct{}
	stopSaver chan struct{}
	saverDone chan struct{}
	diskMu    *sync.Mutex
}

// Default 返回一份带内置镜像源与合理默认值的配置。
func Default() *Config {
	return &Config{
		mu:     &sync.RWMutex{},
		diskMu: &sync.Mutex{},
		Mode:   ModeAuto,
		Proxy: ProxyConfig{
			Listen:            "0.0.0.0:37710",
			TLSPort:           "37711",
			ConnectTimeoutMS:  8000,
			ReadTimeoutMS:     30000,
			FailoverThreshold: 3,
			CooldownSeconds:   300,
		},
		Hosts: HostsConfig{
			// 默认开启优选能力：程序会持续探测最优 IP。
			// 是否真正写入系统 hosts 还会额外检查文件写入权限，
			// 没有权限时仅展示结果，不会报错中断服务。
			Enabled:        true,
			FilePath:       defaultHostsPath(),
			RefreshMinutes: 60,
			CandidateLimit: 6,
			ProbeTimeoutMS: 3000,
		},
		Auto: AutoConfig{
			ProbeIntervalMinutes: 30,
			MirrorMonitorMinutes: 5,
			DirectBetterRatio:    1.5,
			MinImproveRatio:      1.3,
		},
		Server: ServerConfig{
			Listen:          "0.0.0.0:37717",
			Username:        "admin",
			Password:        "admin123",
			SessionTTLHours: 72,
		},
		Docker: DockerConfig{
			Enabled:     true,
			AutoMonitor: true,
			Upstreams:   BuiltinDockerUpstreams(),
		},
		KSpeeder: KSpeederConfig{
			// 端口留空 = 自动跟随 kspeeder 应用自身配置（默认 5443/5003）。
			ManifestPath: "/var/apps/kspeeder/manifest",
			PortConf:     "/vol1/@appconf/kspeeder/ports.conf",
			DownloadURL:  "https://github.com/Blue-Mink/FnDepot/releases/download/v0.8.0/kspeeder-0.8.0-fnos-amd64.fpk",
		},
		Watchdog: WatchdogConfig{
			// 默认开启：进程异常退出时看门狗自动拉起，保证加速服务常驻。
			AutoRestart: true,
		},
		Mirrors: BuiltinMirrors(),
	}
}

// defaultHostsPath 依据操作系统返回系统 hosts 文件位置。
func defaultHostsPath() string {
	if isWindows() {
		root := os.Getenv("SystemRoot")
		if root == "" {
			root = `C:\Windows`
		}
		return filepath.Join(root, "System32", "drivers", "etc", "hosts")
	}
	return "/etc/hosts"
}

// ResolveDataDir 决定运行数据目录。
//
// 优先级：显式传入 > TRIM_PKGVAR(飞牛 fnOS) > 用户主目录下的 .ghpp > 当前目录。
func ResolveDataDir(explicit string) string {
	if explicit != "" {
		return explicit
	}
	if v := os.Getenv("TRIM_PKGVAR"); v != "" {
		return v
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".ghpp")
	}
	return ".ghpp"
}

// ResolveEtcDir 决定配置文件目录，飞牛环境下使用 TRIM_PKGETC。
func ResolveEtcDir(fallback string) string {
	if v := os.Getenv("TRIM_PKGETC"); v != "" {
		return v
	}
	return fallback
}

// Load 从指定目录读取配置，文件不存在时写入默认配置并返回。
func Load(dataDir string) (*Config, error) {
	etcDir := ResolveEtcDir(dataDir)
	if err := os.MkdirAll(etcDir, 0o755); err != nil {
		return nil, fmt.Errorf("创建配置目录失败: %w", err)
	}

	cfg := Default()
	cfg.DataDir = dataDir
	cfg.path = filepath.Join(etcDir, "config.yaml")

	var freshConfig bool
	diskPassword := ""
	data, err := os.ReadFile(cfg.path)
	switch {
	case err == nil:
		// 先解析出文件中实际出现的键，以便区分"用户显式设置"与"字段缺失"。
		if err := detectExplicitFields(data, cfg); err != nil {
			return nil, fmt.Errorf("解析配置文件 %s 失败: %w", cfg.path, err)
		}
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("解析配置文件 %s 失败: %w", cfg.path, err)
		}
		// 记录落盘时的原始密码，供 normalize 生成随机密码后判断是否需要回写。
		diskPassword = cfg.Server.Password
	case os.IsNotExist(err):
		freshConfig = true
	default:
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}

	// 配置迁移：旧版本的配置文件中没有 hosts.enabled 字段，
	// 此时 YAML 解析会得到零值 false，会意外覆盖新版本的默认开启行为。
	// 通过 EnabledSet 标记识别这种情况，采纳新的默认值。
	if !cfg.Hosts.EnabledSet {
		cfg.Hosts.Enabled = Default().Hosts.Enabled
		cfg.Hosts.EnabledSet = true
	}

	// watchdog.auto_restart 同样需要迁移：旧版本配置文件缺该字段时
	// YAML 解析得到零值 false，会意外关闭自动重启。通过 AutoRestartSet
	// 标记识别，未显式设置时采纳新默认值 true。
	if !cfg.Watchdog.AutoRestartSet {
		cfg.Watchdog.AutoRestart = Default().Watchdog.AutoRestart
		cfg.Watchdog.AutoRestartSet = true
	}

	// docker.auto_monitor 同理：旧版本配置文件缺该字段时 YAML 解析得到
	// 零值 false，会意外关掉 Docker 上游自动监测。未显式设置时采纳默认 true。
	if !cfg.Docker.AutoMonitorSet {
		cfg.Docker.AutoMonitor = Default().Docker.AutoMonitor
		cfg.Docker.AutoMonitorSet = true
	}

	preBuiltinRev := cfg.BuiltinRev
	cfg.normalize()

	// 首次运行落盘默认配置；配置文件里密码为空时也必须回写。
	// normalize 会把空密码回退为默认密码，若不立即持久化，历史空密码
	// 配置每次启动都会在内存里回退一次，配置文件与实际登录凭据不一致。
	// 回写后配置文件始终反映真实的登录凭据，可在控制台修改。
	// 出厂源修订号迁移（补齐新出厂源）同样必须落盘，否则每次启动
	// 都会重新合入一次，用户删除的出厂源会被反复复活。
	if freshConfig || diskPassword == "" || cfg.BuiltinRev > preBuiltinRev {
		if err := cfg.Save(); err != nil {
			return nil, err
		}
	}
	return cfg, nil
}

// WatchdogFlagPath 返回看门狗标记文件的路径（位于数据目录下）。
//
// 文件内容为 "1" 或 "0"：fpk/cmd/main 的 run_with_watchdog 在子进程异常退出后
// 读取此文件决定是否拉起新进程。bash 直接 cat 即可，无需 YAML 解析。
func WatchdogFlagPath(dataDir string) string {
	return filepath.Join(dataDir, "watchdog_enabled")
}

// SyncWatchdogFlag 把 cfg.Watchdog.AutoRestart 同步到标记文件。
//
// 在 Go 进程启动早期与 API 切换 auto_restart 时调用，保证看门狗每次判断都用最新值。
func SyncWatchdogFlag(dataDir string, enabled bool) error {
	flagPath := WatchdogFlagPath(dataDir)
	val := []byte("0")
	if enabled {
		val = []byte("1")
	}
	if err := os.MkdirAll(filepath.Dir(flagPath), 0o755); err != nil {
		return fmt.Errorf("创建数据目录失败: %w", err)
	}
	return os.WriteFile(flagPath, val, 0o644)
}

// detectExplicitFields 检查配置文件中是否有用户显式写入的字段。
//
// 目前用于判断 hosts.enabled 与 watchdog.auto_restart 是用户主动设置还是旧版本遗留的缺失项。
// 做法是先把文件内容解析成通用映射，再逐层查键是否存在。
func detectExplicitFields(data []byte, cfg *Config) error {
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return err
	}

	if hosts, ok := raw["hosts"].(map[string]any); ok {
		if _, exists := hosts["enabled"]; exists {
			cfg.Hosts.EnabledSet = true
		}
	}
	if wd, ok := raw["watchdog"].(map[string]any); ok {
		if _, exists := wd["auto_restart"]; exists {
			cfg.Watchdog.AutoRestartSet = true
		}
	}
	if dk, ok := raw["docker"].(map[string]any); ok {
		if _, exists := dk["auto_monitor"]; exists {
			cfg.Docker.AutoMonitorSet = true
		}
	}
	if sp, ok := raw["system_proxy"].(map[string]any); ok {
		if _, exists := sp["enabled"]; exists {
			cfg.SystemProxy.EnabledSet = true
		}
	}
	return nil
}

// normalize 补齐缺失字段并校正非法取值，保证运行期配置始终可用。
func (c *Config) normalize() {
	if c.mu == nil {
		c.mu = &sync.RWMutex{}
	}
	if !c.Mode.IsValid() {
		c.Mode = ModeAuto
	}
	if c.Proxy.Listen == "" {
		c.Proxy.Listen = "0.0.0.0:37710"
	}
	if c.Proxy.TLSPort == "" {
		c.Proxy.TLSPort = "37711"
	}
	if c.Proxy.ConnectTimeoutMS <= 0 {
		c.Proxy.ConnectTimeoutMS = 8000
	}
	if c.Proxy.ReadTimeoutMS <= 0 {
		c.Proxy.ReadTimeoutMS = 30000
	}
	if c.Proxy.FailoverThreshold <= 0 {
		c.Proxy.FailoverThreshold = 3
	}
	if c.Proxy.CooldownSeconds <= 0 {
		c.Proxy.CooldownSeconds = 300
	}
	if c.Hosts.FilePath == "" {
		c.Hosts.FilePath = defaultHostsPath()
	}
	if c.Hosts.RefreshMinutes <= 0 {
		c.Hosts.RefreshMinutes = 60
	}
	if c.Hosts.CandidateLimit <= 0 {
		c.Hosts.CandidateLimit = 6
	}
	if c.Hosts.ProbeTimeoutMS <= 0 {
		c.Hosts.ProbeTimeoutMS = 3000
	}
	if c.Auto.ProbeIntervalMinutes <= 0 {
		c.Auto.ProbeIntervalMinutes = 30
	}
	if c.Auto.MirrorMonitorMinutes <= 0 {
		c.Auto.MirrorMonitorMinutes = 5
	}
	if c.Auto.MirrorMonitorMinutes > 1440 {
		c.Auto.MirrorMonitorMinutes = 1440
	}
	if c.Auto.DirectBetterRatio <= 1 {
		c.Auto.DirectBetterRatio = 1.5
	}
	if c.Auto.MinImproveRatio <= 1 {
		c.Auto.MinImproveRatio = 1.3
	}
	if c.Server.Listen == "" {
		c.Server.Listen = "0.0.0.0:37717"
	}
	// kspeeder 依赖应用接入点：旧版本（1.1.x）配置里的 enabled/local_port/
	// admin_port/data_dir/bin_name 字段已无意义，YAML 解析时静默忽略。
	// registry_url / admin_url 留空 = 自动跟随 kspeeder 应用自身端口
	// （读其 ports.conf，文件缺失时回落 5443/5003）。
	if c.KSpeeder.ManifestPath == "" {
		c.KSpeeder.ManifestPath = "/var/apps/kspeeder/manifest"
	}
	if c.KSpeeder.PortConf == "" {
		c.KSpeeder.PortConf = "/vol1/@appconf/kspeeder/ports.conf"
	}
	if c.KSpeeder.DownloadURL == "" {
		c.KSpeeder.DownloadURL = "https://github.com/Blue-Mink/FnDepot/releases/download/v0.8.0/kspeeder-0.8.0-fnos-amd64.fpk"
	}
	if c.Server.Username == "" {
		c.Server.Username = "admin"
	}
	if c.Server.SessionTTLHours <= 0 {
		c.Server.SessionTTLHours = 72
	}
	if c.Server.Password == "" {
		// 密码不随机生成：随机值不看配置文件就无从得知，对 NAS 局域网
		// 场景徒增困扰。统一回退到固定默认密码，登录后可在控制台修改。
		c.Server.Password = "admin123"
	}
	// Docker 上游列表缺省时补上内置值，"official" 必须保留在末尾作为兜底。
	if len(c.Docker.Upstreams) == 0 {
		c.Docker.Upstreams = BuiltinDockerUpstreams()
	}
	if len(c.Mirrors) == 0 {
		c.Mirrors = BuiltinMirrors()
		c.BuiltinRev = BuiltinRevision
	}
	// 补全缺失的 ID，并丢弃既无 ID 也无 URL 的无效条目。
	seen := make(map[string]bool, len(c.Mirrors))
	valid := c.Mirrors[:0]
	for i := range c.Mirrors {
		m := c.Mirrors[i]
		if m.URL == "" {
			continue
		}
		if m.ID == "" {
			m.ID = fmt.Sprintf("mirror-%s", randomToken(4))
		}
		if seen[m.ID] {
			continue
		}
		if m.Weight == 0 {
			m.Weight = 1
		}
		seen[m.ID] = true
		valid = append(valid, m)
	}
	c.Mirrors = valid
	// 出厂源修订号迁移：升级引入新出厂源时，把缺失的出厂源按 URL 去重
	// 合入一次（保留用户自定义源与其权重/启用状态）。迁移完成后置位
	// 修订号，用户此后删除的出厂源不会再被复活。
	if c.BuiltinRev < BuiltinRevision {
		have := make(map[string]bool, len(c.Mirrors))
		for _, m := range c.Mirrors {
			have[strings.TrimRight(m.URL, "/")] = true
		}
		for _, bm := range BuiltinMirrors() {
			if !have[strings.TrimRight(bm.URL, "/")] {
				c.Mirrors = append(c.Mirrors, bm)
			}
		}
		// Docker 上游同样按出厂清单补齐：旧配置的清单可能早于新增上游
		// （例如修订 2 只合入了 GitHub 镜像，漏掉了 Docker 上游）。
		// 保留用户自定义上游与既有顺序，"official" 固定压尾兜底。
		if len(c.Docker.Upstreams) > 0 {
			haveD := make(map[string]bool, len(c.Docker.Upstreams))
			for _, u := range c.Docker.Upstreams {
				haveD[strings.TrimRight(u, "/")] = true
			}
			merged := make([]string, 0, len(c.Docker.Upstreams)+4)
			for _, u := range c.Docker.Upstreams {
				if u != "official" {
					merged = append(merged, u)
				}
			}
			for _, u := range BuiltinDockerUpstreams() {
				if u == "official" {
					continue
				}
				if !haveD[strings.TrimRight(u, "/")] {
					haveD[strings.TrimRight(u, "/")] = true
					merged = append(merged, u)
				}
			}
			merged = append(merged, "official")
			c.Docker.Upstreams = merged
		}
		c.BuiltinRev = BuiltinRevision
	}
}

// Save 将当前配置同步原子写入磁盘。
//
// 先清脏标记再取快照，磁盘 IO 在锁外完成，不阻塞配置读写；
// 并发写由共享的 diskMu 序列化（快照与原对象共享同一把锁）。
func (c *Config) Save() error {
	c.mu.Lock()
	c.dirty = false
	c.mu.Unlock()
	return c.Snapshot().writeToDisk()
}

// writeToDisk 执行真正的落盘（临时文件 + rename 原子替换）。
// 调用方需保证传入内容是自洽快照；diskMu 串行化同一文件的并发写。
func (c *Config) writeToDisk() error {
	if c.path == "" {
		c.path = filepath.Join(ResolveEtcDir(c.DataDir), "config.yaml")
	}
	mu := c.diskMu
	if mu == nil {
		mu = &sync.Mutex{}
	}
	mu.Lock()
	defer mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		return fmt.Errorf("创建配置目录失败: %w", err)
	}
	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("序列化配置失败: %w", err)
	}
	tmp := c.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("写入临时配置失败: %w", err)
	}
	return os.Rename(tmp, c.path)
}

// Path 返回配置文件所在路径。
func (c *Config) Path() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.path
}

// Snapshot 返回配置的深拷贝，供 API 层安全读取。
//
// 拷贝出的副本拥有独立的锁，读取时不会与原配置相互阻塞；
// saver 相关字段置空，快照永远不会触发或干扰落盘调度。
func (c *Config) Snapshot() *Config {
	c.mu.RLock()
	defer c.mu.RUnlock()
	cp := *c
	cp.Mirrors = make([]Mirror, len(c.Mirrors))
	copy(cp.Mirrors, c.Mirrors)
	cp.mu = &sync.RWMutex{}
	cp.dirty = false
	cp.saverCh = nil
	cp.stopSaver = nil
	cp.saverDone = nil
	return &cp
}

// Update 在写锁保护下修改配置，内存立即生效。
//
// 落盘是防抖异步的：置脏后唤醒 saver（300ms 合并写一次），
// 模式切换这类高频操作毫秒级返回。saver 未启动时（测试场景）
// 退回同步写盘，保持旧语义。
func (c *Config) Update(fn func(*Config) error) error {
	c.mu.Lock()
	if err := fn(c); err != nil {
		c.mu.Unlock()
		return err
	}
	c.normalize()
	c.dirty = true
	saver := c.saverCh
	c.mu.Unlock()
	if saver != nil {
		select {
		case saver <- struct{}{}:
		default:
		}
		return nil
	}
	return c.Save()
}

// StartSaver 启动异步落盘协程（应用启动时调用一次）。
func (c *Config) StartSaver() {
	c.mu.Lock()
	if c.saverCh != nil {
		c.mu.Unlock()
		return
	}
	c.saverCh = make(chan struct{}, 1)
	c.stopSaver = make(chan struct{})
	c.saverDone = make(chan struct{})
	c.mu.Unlock()
	go c.runSaver()
}

// Close 停止 saver 并做最后一次落盘（应用退出时调用）。
func (c *Config) Close() {
	c.mu.Lock()
	stop := c.stopSaver
	done := c.saverDone
	c.mu.Unlock()
	if stop == nil {
		return
	}
	close(stop)
	<-done
}

// Flush 有未落盘改动时立即同步写盘（优雅退出前调用）。
func (c *Config) Flush() error {
	c.mu.Lock()
	if !c.dirty {
		c.mu.Unlock()
		return nil
	}
	c.dirty = false
	c.mu.Unlock()
	return c.Snapshot().writeToDisk()
}

// runSaver 落盘主循环：脏标记唤醒后防抖合并写盘。
//
// 防抖窗口内的多次修改只产生一次磁盘写；停止信号到来时把
// 窗口内积压的改动一并落盘后再退出。
func (c *Config) runSaver() {
	const debounce = 300 * time.Millisecond
	defer close(c.saverDone)
	var timer *time.Timer
	for {
		select {
		case <-c.stopSaver:
			if timer != nil {
				timer.Stop()
			}
			_ = c.Flush()
			return
		case <-c.saverCh:
			if timer != nil {
				timer.Stop()
			}
			timer = time.AfterFunc(debounce, func() {
				_ = c.Flush()
			})
		}
	}
}

// EnabledMirrors 返回当前启用的镜像源副本。
func (c *Config) EnabledMirrors() []Mirror {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]Mirror, 0, len(c.Mirrors))
	for _, m := range c.Mirrors {
		if m.Enabled {
			out = append(out, m)
		}
	}
	return out
}

// randomToken 生成指定字节长度的十六进制随机串，用于密码与 ID。
func randomToken(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		// 极少数情况下系统熵源不可用，退回时间戳派生的可读串。
		return fmt.Sprintf("%x", os.Getpid())
	}
	return hex.EncodeToString(buf)
}

// RandomToken 对外暴露随机串生成，供认证模块复用。
func RandomToken(n int) string { return randomToken(n) }

// JoinHostPort 安全拼接监听地址，允许用户只填写端口号。
func JoinHostPort(addr string) string {
	if _, _, err := net.SplitHostPort(addr); err == nil {
		return addr
	}
	if !strings.Contains(addr, ":") {
		return "0.0.0.0:" + addr
	}
	return addr
}
