// Package app 负责把配置、镜像池、代理引擎、DNS 优选、决策器等模块编排成一个可运行的服务。
package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ghpp/ghpp/internal/config"
	"github.com/ghpp/ghpp/internal/decide"
	"github.com/ghpp/ghpp/internal/dnsopt"
	"github.com/ghpp/ghpp/internal/hostsfile"
	"github.com/ghpp/ghpp/internal/km"
	"github.com/ghpp/ghpp/internal/logbus"
	"github.com/ghpp/ghpp/internal/mirror"
	"github.com/ghpp/ghpp/internal/netutil"
	"github.com/ghpp/ghpp/internal/proxy"
)

// App 是应用的总控制器。
//
// 它持有全部子系统，并驱动后台任务：
//   - 定期镜像测速
//   - 定期 DNS 优选与 hosts 刷新
//   - 定期自动决策
//   - 得分衰减与会话清理
type App struct {
	cfg *config.Config
	log *logbus.Bus

	pool     *mirror.Pool
	engine   *proxy.Engine
	optimize *dnsopt.Optimizer
	hostsMgr *hostsfile.Manager
	decider  *decide.Decider
	// kspeeder 探测本机 kspeeder 依赖应用（1.2.0 起不再捆绑引擎）。
	kspeeder *km.Manager

	// ca 是 HTTPS 中间人所需的根证书，初始化失败时为 nil。
	ca    *proxy.CA
	caErr error

	// catrust* 缓存「本地 CA 是否已装入系统信任」的检测结论（30 秒 TTL）。
	// 系统代理 / 证书安装 / CONNECT 分流共用这一份状态，避免反复解析系统 bundle。
	catrustMu  sync.Mutex
	catrustVal bool
	catrustFP  string
	catrustAt  time.Time

	mu      sync.RWMutex
	started bool

	cancel context.CancelFunc
	wg     sync.WaitGroup

	// 最近一次全量测速的结果，供决策与界面展示。
	lastProbes []mirror.Result

	// 最近一次 hosts 刷新时间。
	lastHostsSync time.Time
	// 最近一次自动决策时间。
	lastDecide time.Time

	// 最近一次加速源轻量监测时间（自动优先监测节拍）。
	lastMirrorMonitor time.Time
	// 最近一次 Docker 上游探活时间。
	lastDockerProbe time.Time
	// 轻量监测是否正在后台执行。
	monitoring bool

	// 当前实际生效的模式（可能是自动决策的产物，而非用户设定的模式）。
	effectiveMode config.Mode
	effectiveWhy  string
}

// New 创建应用。
func New(cfg *config.Config, log *logbus.Bus) *App {
	pool := mirror.NewPool()

	// 加载或生成根证书，用于可选的 HTTPS 中间人加速。
	// 失败不阻断启动，只是 HTTPS 网页加速不可用，代理与 DNS 优选照常工作。
	caDir := config.ResolveEtcDir(cfg.DataDir)
	ca, caErr := proxy.LoadOrCreateCA(caDir)
	if caErr != nil {
		log.Logf("warn", "根证书初始化失败，HTTPS 中间人加速不可用: %v", caErr)
	}

	// kspeeder 依赖应用探测器：检测独立安装的 kspeeder 应用是否
	// 安装/运行，其 registry 端口活着时作为 Docker 上游第一优先。
	a := &App{
		cfg:      cfg,
		log:      log,
		pool:     pool,
		ca:       ca,
		caErr:    caErr,
		kspeeder: km.New(cfg.KSpeeder.RegistryURL, cfg.KSpeeder.AdminURL, cfg.KSpeeder.ManifestPath, cfg.KSpeeder.PortConf, log.Logf),
	}

	// 日志总线同时作为各模块的日志出口。
	a.engine = proxy.NewEngine(proxy.Options{
		Config: func() *config.Config { return a.CurrentConfig() },
		Pool:   pool,
		Logf:   log.Logf,
		CA:     ca,
		// kspeeder 应用运行时，其 registry 作为 Docker 上游的第一优先。
		DockerExtraUpstream: a.KSpeederURL,
		// CONNECT 分流用：系统代理场景下 CA 未入系统信任时降级纯隧道。
		CAInSystemTrust: a.CAInSystemTrust,
	})
	if ca != nil {
		// NewEngine 通过 Options 传入 CA，这里再显式同步一次，
		// 保证 caPtr 与 a.ca 始终指向同一份证书。
		a.engine.SetCA(ca)
	}
	a.optimize = dnsopt.NewOptimizer(log.Logf)
	a.hostsMgr = hostsfile.NewManager(cfg.Hosts.FilePath, log.Logf)
	a.decider = decide.New(pool, log.Logf)
	// 让决策器能拿到优选出的 IP，从而公平比较 hosts 通路的真实表现。
	a.decider.SetBestIPSource(func(host string) (string, bool) {
		c, ok := a.optimize.Best(host)
		if !ok || !c.OK {
			return "", false
		}
		return c.IP, true
	})
	a.effectiveMode = cfg.Mode

	return a
}

// CA 返回根证书，可能为 nil（初始化失败时）。
func (a *App) CA() *proxy.CA { return a.ca }

// CAError 返回根证书初始化时的错误。
func (a *App) CAError() error { return a.caErr }

// ReplaceCA 用新证书替换本地 CA 并热生效。
// mode 为 "import" 时使用用户提供的 PEM；为 "regenerate" 时重新生成。
func (a *App) ReplaceCA(mode string, certPEM, keyPEM []byte) error {
	caDir := config.ResolveEtcDir(a.CurrentConfig().DataDir)

	var (
		ca  *proxy.CA
		err error
	)
	switch mode {
	case "import":
		ca, err = proxy.ImportCA(caDir, certPEM, keyPEM)
	case "regenerate":
		ca, err = proxy.RegenerateCA(caDir)
	default:
		err = fmt.Errorf("不支持的操作: %s", mode)
	}
	if err != nil {
		return err
	}

	a.mu.Lock()
	a.ca = ca
	a.caErr = nil
	a.mu.Unlock()

	// 热替换，新连接立即使用新证书。
	a.engine.SetCA(ca)
	// 新指纹生效，系统信任里的旧 CA 不再算数——立即作废缓存的检测结论。
	a.InvalidateCATrust()
	a.log.Logf("info", "根证书已更新（%s），客户端需重新安装并信任新证书", mode)
	return nil
}

// CAInSystemTrust 判断当前本地 CA 是否已装入 NAS 系统信任。
//
// 结论按当前 CA 指纹缓存 30 秒（见 catrustTTL）；CA 被替换或执行过安装/移除
// 动作时必须先调用 InvalidateCATrust。检测不确定时恒返回 false（fail-safe）。
func (a *App) CAInSystemTrust() bool {
	a.mu.RLock()
	ca := a.ca
	a.mu.RUnlock()
	if ca == nil {
		return false
	}
	fp := ca.Fingerprint()

	a.catrustMu.Lock()
	defer a.catrustMu.Unlock()
	if a.catrustFP == fp && time.Since(a.catrustAt) < catrustTTL {
		return a.catrustVal
	}
	a.catrustVal = caInSystemTrustFor(ca)
	a.catrustFP = fp
	a.catrustAt = time.Now()
	return a.catrustVal
}

// InvalidateCATrust 作废信任检测缓存，下次 CAInSystemTrust 强制重新检查。
func (a *App) InvalidateCATrust() {
	a.catrustMu.Lock()
	a.catrustFP = ""
	a.catrustAt = time.Time{}
	a.catrustMu.Unlock()
}

// InstallSystemCA 把当前本地 CA 装入系统信任（幂等）。
//
// 返回 "already"（已在系统库）或 "installed"（本次新装）。
// 失败时返回错误，由调用方决定如何呈现（系统代理功能本身保持可用，
// 由 CONNECT 隧道降级兜底）。
func (a *App) InstallSystemCA() (string, error) {
	a.mu.RLock()
	ca := a.ca
	a.mu.RUnlock()
	if ca == nil {
		return "", fmt.Errorf("本地 CA 不可用: %v", a.caErr)
	}
	action, err := installSystemCAFor(ca)
	if err == nil {
		a.InvalidateCATrust()
	}
	return action, err
}

// RemoveSystemCA 从系统信任移除本地 CA（幂等），供卸载流程调用。
func (a *App) RemoveSystemCA() {
	removeSystemCAFile()
	a.InvalidateCATrust()
}

// CurrentConfig 返回当前配置快照，供各模块读取。
func (a *App) CurrentConfig() *config.Config {
	return a.cfg.Snapshot()
}

// Config 返回底层配置对象，供 API 层执行写操作。
func (a *App) Config() *config.Config { return a.cfg }

// Log 返回日志总线。
func (a *App) Log() *logbus.Bus { return a.log }

// Pool 返回镜像源池。
func (a *App) Pool() *mirror.Pool { return a.pool }

// Engine 返回代理引擎。
func (a *App) Engine() *proxy.Engine { return a.engine }

// KSpeederURL 返回本机 kspeeder 应用的 registry 地址（未运行时为空串）。
func (a *App) KSpeederURL() string {
	if a.kspeeder.RegistryUp() {
		return a.kspeeder.Registry()
	}
	return ""
}

// KSpeederStatus 返回 kspeeder 依赖应用的检测状态（供 API 层展示）。
func (a *App) KSpeederStatus() km.Status {
	st := a.kspeeder.Status()
	if st.Mode == km.ModeRunning {
		st.EngineVersion = a.kspeeder.EngineVersion(context.Background())
		if st.EngineVersion != "" && km.VersionBelow(st.EngineVersion, km.RecommendedEngineMin) {
			st.EngineBelowRecommended = true
		}
	}
	return st
}

// KSpeederNodes 返回 kspeeder 引擎加速节点的运行态（供 API 层展示）。
func (a *App) KSpeederNodes(ctx context.Context) ([]km.NodeInfo, error) {
	return a.kspeeder.Nodes(ctx)
}

// migrateLegacyKSpeeder 清理 1.1.x 内置引擎的遗留（1.2.0 起不再自托管）：
// ① 杀掉仍在运行的旧引擎进程（必须同时匹配 iStoreEnhance 与旧数据目录，
// 避免误伤独立 kspeeder 应用的引擎）；② 归档旧数据目录，避免混淆。
// 1.1.x 内置引擎的数据目录固定为 ${DataDir}/kspeeder。
func (a *App) migrateLegacyKSpeeder(cfg *config.Config) {
	legacyDir := filepath.Join(cfg.DataDir, "kspeeder")

	killLegacy := func(pid int) {
		if pid <= 0 {
			return
		}
		cmdline, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
		if err != nil {
			return
		}
		cl := string(cmdline)
		if !strings.Contains(cl, "iStoreEnhance") || !strings.Contains(cl, legacyDir) {
			return
		}
		if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
			return
		}
		a.log.Logf("info", "KSpeeder: 已结束 1.1.x 遗留的内置引擎进程（PID %d）", pid)
		for i := 0; i < 50 && pidAlive(pid); i++ {
			time.Sleep(100 * time.Millisecond)
		}
		if pidAlive(pid) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	}

	// pid 文件优先（1.1.x 的 km 管理器写在此目录）。
	if data, err := os.ReadFile(filepath.Join(legacyDir, "dogdev-kspeeder.pid")); err == nil {
		if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil {
			killLegacy(pid)
		}
	}
	// 兜底：扫 /proc 找同时包含 iStoreEnhance 与旧数据目录的进程
	// （覆盖 pid 文件缺失、进程被收养等场景）。
	if entries, err := os.ReadDir("/proc"); err == nil {
		for _, e := range entries {
			if pid, err := strconv.Atoi(e.Name()); err == nil {
				killLegacy(pid)
			}
		}
	}

	// 归档旧数据目录（旧节点配置、测速状态、引擎日志）。
	if fi, err := os.Stat(legacyDir); err == nil && fi.IsDir() {
		dst := legacyDir + "-legacy-" + time.Now().Format("20060102-150405")
		if err := os.Rename(legacyDir, dst); err == nil {
			a.log.Logf("info", "KSpeeder: 1.1.x 内置引擎数据目录已归档为 %s", dst)
		} else {
			a.log.Logf("warn", "KSpeeder: 旧引擎数据目录归档失败: %v", err)
		}
	}
}

func pidAlive(pid int) bool { return syscall.Kill(pid, 0) == nil }

// Optimizer 返回 DNS 优选器。
func (a *App) Optimizer() *dnsopt.Optimizer { return a.optimize }

// HostsManager 返回 hosts 管理器。
func (a *App) HostsManager() *hostsfile.Manager { return a.hostsMgr }

// Decider 返回决策器。
func (a *App) Decider() *decide.Decider { return a.decider }

// Start 启动全部子系统与后台任务。
func (a *App) Start() error {
	a.mu.Lock()
	if a.started {
		a.mu.Unlock()
		return errors.New("服务已在运行")
	}
	a.started = true
	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel
	a.mu.Unlock()

	// 重置启动时刻，保证重启后运行时长从本次启动算起。
	startedAt = time.Now()

	cfg := a.CurrentConfig()

	// 启动代理监听。
	if err := a.engine.Start(cfg.Proxy.Listen); err != nil {
		return err
	}

	// 1.2.0 起不再自托管引擎：清理 1.1.x 内置引擎的残留进程与数据目录。
	a.migrateLegacyKSpeeder(cfg)

	// 首轮测速与决策放到后台，避免阻塞启动。
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		// 稍作延迟，等待网络栈就绪。
		select {
		case <-time.After(2 * time.Second):
		case <-ctx.Done():
			return
		}
		a.RefreshAll(ctx)
		a.loop(ctx)
	}()

	// 定期清理会话与陈旧统计。
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				a.pool.ScoreHalfLifeDecay(now)
				a.engine.DockerPoolDecay(now)
			}
		}
	}()

	// 启动自检：系统代理已开但本地 CA 未入系统信任 → 新 shell 的 HTTPS 会走
	// 纯隧道降级（可用但不加速）。只记日志不自动装：扩大系统信任必须是用户的
	// 显式动作（控制台「应用加速」页有一键安装入口）。
	if systemProxyFileExists() && a.ca != nil && !a.CAInSystemTrust() {
		a.log.Logf("warn", "系统代理已开启但本地 CA 未装入系统信任：新 shell 的 HTTPS 将走透传隧道（可用但不加速）。请在控制台「应用加速」页点「安装本地 CA 到系统信任」启用全速加速")
	}

	a.log.Logf("info", "Dog-dev 已启动：代理 %s，控制台 %s，模式 %s",
		cfg.Proxy.Listen, cfg.Server.Listen, cfg.Mode)
	return nil
}

// Stop 停止全部子系统。
func (a *App) Stop(ctx context.Context) error {
	a.mu.Lock()
	if !a.started {
		a.mu.Unlock()
		return nil
	}
	a.started = false
	cancel := a.cancel
	a.mu.Unlock()

	if cancel != nil {
		cancel()
	}

	// 等待后台任务退出，但不超过上下文期限。
	done := make(chan struct{})
	go func() {
		a.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
	}

	// 停止时清理 hosts 托管区，避免留下失效的解析记录。
	if a.CurrentConfig().Hosts.Enabled {
		if err := a.hostsMgr.Clear(); err != nil {
			a.log.Logf("warn", "停止时清理 hosts 失败: %v", err)
		}
	}

	// 停引擎前把配置落盘：防抖窗口（300ms）内的改动（如刚切完模式就停止）
	// 一并刷出，避免丢最后一次配置。
	if ferr := a.cfg.Flush(); ferr != nil {
		a.log.Logf("warn", "停止时配置落盘失败: %v", ferr)
	}

	// 停代理（1.2.0 起本应用不再有自托管引擎需要停止）。
	err := a.engine.Stop(ctx)
	a.log.Logf("info", "Dog-dev 已停止")
	return err
}

// LastDockerProbe 返回最近一次 Docker 上游自动监测（探活）完成时间。
func (a *App) LastDockerProbe() time.Time {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.lastDockerProbe
}

// DockerProbeAll 对全部 Docker 上游做一轮 /v2/ 探活并记录完成时间。
// 自动监测节拍与 Docker 页手动「探活」按钮共用，保证「最近监测」口径一致。
func (a *App) DockerProbeAll(ctx context.Context) {
	if a.engine == nil {
		return
	}
	a.engine.DockerProbeAll(ctx)
	a.mu.Lock()
	a.lastDockerProbe = time.Now()
	a.mu.Unlock()
}

// Running 返回服务是否在运行。
func (a *App) Running() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.started
}

// loop 驱动周期性的后台任务。
func (a *App) loop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if ctx.Err() != nil {
				return
			}
			a.tickMonitor(ctx)
			a.tickFullRefresh(ctx)
		}
	}
}

// tickMonitor 驱动「自动优先监测」：按 MirrorMonitorMinutes 节拍
// 对 GitHub 加速源做轻量测速、对 Docker 上游做 /v2/ 探活。
//
// 轻量监测独立于全量评估（不触发 DNS 优选与自动决策），
// 让界面与调度始终保持"最优节点在前、失效节点沉底"的新鲜状态。
func (a *App) tickMonitor(ctx context.Context) {
	cfg := a.CurrentConfig()
	interval := time.Duration(cfg.Auto.MirrorMonitorMinutes) * time.Minute
	now := time.Now()

	if now.Sub(a.lastMirrorMonitor) < interval {
		return
	}
	if !a.acquireMonitor() {
		return // 上一轮监测还没跑完，跳过本次
	}
	a.mu.Lock()
	a.lastMirrorMonitor = now
	a.mu.Unlock()

	go func() {
		defer a.releaseMonitor()
		cctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
		defer cancel()

		if probes := a.SweepMirrorsLight(cctx); probes != nil {
			a.mu.Lock()
			a.lastProbes = probes
			a.mu.Unlock()
		}

		// Docker 上游探活（与 GitHub 源同一节拍，失败互不影响）。
		// 仅当 Docker 页「自动监测」开启时执行；最近一次探活时间
		// 经 /api/docker 的 monitor 字段回给界面展示。
		if a.engine != nil {
			if c := a.CurrentConfig(); c.Docker.AutoMonitor {
				a.DockerProbeAll(cctx)
			}
		}
	}()
}

// tickFullRefresh 驱动全量评估：按 ProbeIntervalMinutes 节拍执行
// 镜像全量测速 + DNS 优选 + 自动决策。
func (a *App) tickFullRefresh(ctx context.Context) {
	cfg := a.CurrentConfig()
	interval := time.Duration(cfg.Auto.ProbeIntervalMinutes) * time.Minute
	now := time.Now()

	// 镜像全量测速与决策。
	if now.Sub(a.lastDecide) >= interval {
		a.RefreshAll(ctx)
	}

	// hosts 刷新。
	if cfg.Hosts.Enabled {
		hostsInterval := time.Duration(cfg.Hosts.RefreshMinutes) * time.Minute
		if now.Sub(a.lastHostsSync) >= hostsInterval {
			a.SyncHosts(ctx)
		}
	}
}

// acquireMonitor 原子地获取监测执行权；已有监测在跑时返回 false。
func (a *App) acquireMonitor() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.monitoring {
		return false
	}
	a.monitoring = true
	return true
}

// releaseMonitor 释放监测执行权。
func (a *App) releaseMonitor() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.monitoring = false
}

// RefreshAll 执行一轮完整的刷新：镜像测速 -> DNS 优选 -> 自动决策。
func (a *App) RefreshAll(ctx context.Context) {
	cfg := a.CurrentConfig()

	a.log.Logf("info", "开始全量刷新：镜像测速中…")
	probes := a.SweepMirrors(ctx)

	a.mu.Lock()
	a.lastProbes = probes
	// 全量测速同样刷新了源得分，等同一次更彻底的监测。
	a.lastMirrorMonitor = time.Now()
	a.mu.Unlock()

	// hosts 优选与代理测速相互独立，始终执行一次，
	// 这样决策器才能公平比较三条通路的真实表现。
	// 是否真正写入 hosts 由 Hosts.Enabled 决定。
	a.SyncHosts(ctx)

	// 仅在自动模式下才做通路评估，手动模式尊重用户选择。
	if cfg.Mode == config.ModeAuto {
		verdict := a.decider.Decide(ctx, cfg, probes)
		a.mu.Lock()
		a.effectiveMode = verdict.EffectiveMode
		a.effectiveWhy = verdict.Reason
		a.lastDecide = time.Now()
		a.mu.Unlock()
	} else {
		a.mu.Lock()
		a.effectiveMode = cfg.Mode
		a.effectiveWhy = "用户手动指定模式"
		a.lastDecide = time.Now()
		a.mu.Unlock()
	}
}

// SweepMirrors 对所有启用的镜像源执行一轮全量测速。
//
// 全量测速前清除冷却状态，让被摘除的源有机会回归并重新参与竞争。
func (a *App) SweepMirrors(ctx context.Context) []mirror.Result {
	return a.sweepMirrorsCore(ctx, 256*1024, true)
}

// SweepMirrorsLight 执行一轮轻量监测：小载荷、不清冷却。
//
// 用于「自动优先监测」节拍——只刷新得分让"最优在前、失效沉底"保持新鲜，
// 不给被冷却摘除的源续命（恢复由得分半衰期衰减自然驱动），
// 因此成本远低于全量测速，可每几分钟跑一次。
func (a *App) SweepMirrorsLight(ctx context.Context) []mirror.Result {
	return a.sweepMirrorsCore(ctx, 32*1024, false)
}

// sweepMirrorsCore 是两类测速的公共实现。
func (a *App) sweepMirrorsCore(ctx context.Context, maxBytes int64, resetCooldown bool) []mirror.Result {
	cfg := a.CurrentConfig()
	mirrors := cfg.EnabledMirrors()

	if resetCooldown {
		// 全量测速前清除冷却状态，让被摘除的源有机会回归。
		a.pool.ResetCooldown()
	}

	targets := config.DomainProbeTargets()

	// Sweep 内部会根据源类型与目标分类自动匹配，无需在此预组合。
	// 若另一轮测速正在进行，Sweep 返回 nil——本次视为"未执行"。
	results := a.pool.Sweep(ctx, mirrors, targets, maxBytes)
	if results == nil {
		return nil
	}

	okCount := 0
	for _, r := range results {
		if r.OK {
			okCount++
			continue
		}
		msg := r.Err
		if msg == "" {
			msg = "无响应"
		}
		if len(msg) > 80 {
			msg = msg[:80]
		}
		a.log.Logf("debug", "镜像监测 %s 失败：%s", r.MirrorID, msg)
	}
	a.log.Logf("info", "镜像测速完成：%d/%d 个源可用", okCount, len(results))
	return results
}

// SweepOne 只对指定源测速，用于界面上的单源测试按钮。
func (a *App) SweepOne(ctx context.Context, id string) ([]mirror.Result, error) {
	cfg := a.CurrentConfig()
	var target config.Mirror
	found := false
	for _, m := range cfg.Mirrors {
		if m.ID == id {
			target = m
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("未找到镜像源 %s", id)
	}

	targets := config.DomainProbeTargets()
	filtered := make([]config.ProbeTarget, 0, len(targets))
	for _, t := range targets {
		if mirror.Supports(target, t.Category) {
			filtered = append(filtered, t)
		}
	}
	if len(filtered) == 0 {
		filtered = targets
	}

	results := a.pool.Sweep(ctx, []config.Mirror{target}, filtered, 256*1024)
	return results, nil
}

// SyncHosts 执行 DNS 优选并把结果写入 hosts。
//
// 当用户尚未显式关闭 hosts 能力时（Enabled 为 true），本方法会完成
// 优选并写入系统 hosts 文件；若用户关闭了该开关，则跳过。
func (a *App) SyncHosts(ctx context.Context) {
	cfg := a.CurrentConfig()
	if !cfg.Hosts.Enabled {
		a.log.Logf("debug", "hosts 加速未启用，跳过同步")
		return
	}

	if err := ctx.Err(); err != nil {
		return
	}

	a.log.Logf("info", "开始 DNS 优选…")
	timeout := time.Duration(cfg.Hosts.ProbeTimeoutMS) * time.Millisecond
	best := a.optimize.OptimizeAll(ctx, cfg.Hosts.CandidateLimit, timeout)

	entries := make([]hostsfile.Entry, 0, len(best))
	for host, candidates := range best {
		if len(candidates) == 0 || !candidates[0].OK {
			a.log.Logf("debug", "域名 %s 无可用候选 IP", host)
			continue
		}
		top := candidates[0]
		entries = append(entries, hostsfile.Entry{
			IP:   top.IP,
			Host: host,
			Note: fmt.Sprintf("%s 延迟 %dms", host, top.TotalMS),
		})
	}

	if len(entries) == 0 {
		a.log.Logf("warn", "DNS 优选未获得可用结果，hosts 保持不变")
		return
	}

	// 检查写入权限，提前给出明确提示而不是等写入失败。
	if !a.hostsMgr.Writable() {
		a.log.Logf("warn", "没有权限写入 %s，DNS 优选结果仅用于展示。"+
			"请以 root 身份运行，或手动把结果添加到 hosts 文件。", a.hostsMgr.Path())
	}

	if err := a.hostsMgr.Apply(entries); err != nil {
		a.log.Logf("error", "写入 hosts 失败: %v", err)
		return
	}

	a.mu.Lock()
	a.lastHostsSync = time.Now()
	a.mu.Unlock()

	a.hostsMgr.FlushDNS()
}

// SyncDNSOnly 只执行 DNS 优选而不写入 hosts。
//
// 用于用户没有 hosts 写入权限的场景：结果仍可在界面上查看，
// 用户可手动采纳，或改用代理模式。
func (a *App) SyncDNSOnly(ctx context.Context) map[string][]dnsopt.Candidate {
	cfg := a.CurrentConfig()
	a.log.Logf("info", "开始 DNS 优选（仅探测，不写 hosts）…")
	timeout := time.Duration(cfg.Hosts.ProbeTimeoutMS) * time.Millisecond
	return a.optimize.OptimizeAll(ctx, cfg.Hosts.CandidateLimit, timeout)
}

// ClearHosts 清除 hosts 托管区。
func (a *App) ClearHosts() error {
	return a.hostsMgr.Clear()
}

// SetWatchdogAutoRestart 切换看门狗自动重启开关并立即同步标记文件。
//
// 配置写入 config.yaml 后，同步把 ${DataDir}/watchdog_enabled 改为 "1"/"0"，
// 这样看门狗在下次进程异常退出时立即按新值判断，无需重启 Go 进程。
func (a *App) SetWatchdogAutoRestart(enabled bool) error {
	if err := a.cfg.Update(func(c *config.Config) error {
		c.Watchdog.AutoRestart = enabled
		c.Watchdog.AutoRestartSet = true
		return nil
	}); err != nil {
		return err
	}
	return config.SyncWatchdogFlag(a.cfg.DataDir, enabled)
}

// Status 汇总当前运行状态，供 API 返回给前端。
type Status struct {
	Running       bool          `json:"running"`
	EffectiveMode config.Mode   `json:"effective_mode"`
	ConfigMode    config.Mode   `json:"config_mode"`
	Reason        string        `json:"reason"`
	ProxyAddr     string        `json:"proxy_addr"`
	Uptime        string        `json:"uptime"`
	LastSweep     time.Time     `json:"last_sweep"`
	LastHostsSync time.Time     `json:"last_hosts_sync"`
	LastDecide    time.Time     `json:"last_decide"`
	// MirrorMonitorMin 是加速源自动优先监测的间隔（分钟）。
	MirrorMonitorMin int `json:"mirror_monitor_min"`
	// LastMirrorMonitor 是最近一次自动监测完成的时间。
	LastMirrorMonitor time.Time `json:"last_mirror_monitor"`
	HostsFile     string        `json:"hosts_file"`
	HostsWritable bool          `json:"hosts_writable"`
	Metrics       MetricsStatus `json:"metrics"`
	Mirrors       MirrorBrief   `json:"mirrors"`
	// LocalIPs 是本机所有可用于局域网通信的 IPv4，供前端「接入方式」切换 LAN 地址。
	LocalIPs []string `json:"local_ips"`
	// ExternalHost 是用户保存的外网接入地址（域名或 IP，可带端口），空表示未配置。
	ExternalHost string `json:"external_host"`
	// SystemProxyEnabled 表示是否已开启系统级 HTTP 代理（写 /etc/profile.d/ghpp-proxy.sh）。
	// 开启后飞牛本机新 login shell 启动的软件默认走加速器，无需软件自身配置。
	SystemProxyEnabled bool `json:"system_proxy_enabled"`
	// HostsEnabled 表示 hosts 加速是否已开启（cfg.Hosts.Enabled），
	// 前端「应用加速」页据此展示状态徽章。
	HostsEnabled bool `json:"hosts_enabled"`
	// DockerEnabled 表示 Docker 加速是否已开启（cfg.Docker.Enabled），
	// 前端「应用加速」页据此展示状态徽章。
	DockerEnabled bool `json:"docker_enabled"`
}

// MetricsStatus 是流量与命中指标。
type MetricsStatus struct {
	TotalRequests int64 `json:"total_requests"`
	Accelerated   int64 `json:"accelerated"`
	Failed        int64 `json:"failed"`
	Failovers     int64 `json:"failovers"`
	BytesIn       int64 `json:"bytes_in"`
	BytesOut      int64 `json:"bytes_out"`
	SavedMS       int64 `json:"saved_ms"`
	WebCount      int64 `json:"web_count"`
	RawCount      int64 `json:"raw_count"`
	CloneCount    int64 `json:"clone_count"`
	DockerCount   int64 `json:"docker_count"`
	// SavedMB 是节省流量的可读形式，单位 MB。
	SavedMB float64 `json:"saved_mb"`
}

// MirrorBrief 是镜像源的汇总信息。
type MirrorBrief struct {
	Total     int    `json:"total"`
	Enabled   int    `json:"enabled"`
	Healthy   int    `json:"healthy"`
	BestID    string `json:"best_id"`
	BestName  string `json:"best_name"`
	BestLatMS int64  `json:"best_latency_ms"`
}

// startedAt 记录启动时刻，用于计算运行时长。
var startedAt = time.Now()

// Status 构造当前状态快照。
func (a *App) Status() Status {
	cfg := a.CurrentConfig()
	m := a.engine.Metrics()

	a.mu.RLock()
	effMode := a.effectiveMode
	reason := a.effectiveWhy
	lastSweep := time.Time{}
	lastHosts := a.lastHostsSync
	lastDecide := a.lastDecide
	lastMirrorMon := a.lastMirrorMonitor
	monitorMin := cfg.Auto.MirrorMonitorMinutes
	probes := a.lastProbes
	a.mu.RUnlock()

	if len(probes) > 0 {
		lastSweep = a.pool.LastSweep()
	}

	stats := a.pool.Stats()

	brief := MirrorBrief{Total: len(cfg.Mirrors)}
	var bestLat int64
	for _, m := range cfg.Mirrors {
		if !m.Enabled {
			continue
		}
		brief.Enabled++
		st, ok := stats[m.ID]
		if ok && st.OK {
			brief.Healthy++
			if bestLat == 0 || (st.LatencyMS > 0 && st.LatencyMS < bestLat) {
				bestLat = st.LatencyMS
				brief.BestID = m.ID
				brief.BestName = m.Name
			}
		}
	}
	brief.BestLatMS = bestLat

	if effMode == "" {
		effMode = cfg.Mode
	}

	acc := m.Accelerated.Load()
	total := m.TotalRequests.Load()

	return Status{
		Running:       a.Running(),
		EffectiveMode: effMode,
		ConfigMode:    cfg.Mode,
		Reason:        reason,
		ProxyAddr:     a.engine.Addr(),
		Uptime:        time.Since(startedAt).Round(time.Second).String(),
		LastSweep:     lastSweep,
		LastHostsSync: lastHosts,
		LastDecide:    lastDecide,
		MirrorMonitorMin:  monitorMin,
		LastMirrorMonitor: lastMirrorMon,
		HostsFile:     a.hostsMgr.Path(),
		HostsWritable: a.hostsMgr.Writable(),
		Metrics: MetricsStatus{
			TotalRequests: total,
			Accelerated:   acc,
			Failed:        m.Failed.Load(),
			Failovers:     m.Failovers.Load(),
			BytesIn:       m.BytesIn.Load(),
			BytesOut:      m.BytesOut.Load(),
			SavedMS:       m.SavedMS.Load(),
			WebCount:      m.CategoryWeb.Load(),
			RawCount:      m.CategoryRaw.Load(),
			CloneCount:    m.CategoryClone.Load(),
			DockerCount:   m.CategoryDocker.Load(),
			SavedMB:       float64(m.BytesOut.Load()) / 1024 / 1024,
		},
		Mirrors:      brief,
		LocalIPs:     netutil.LocalIPs(),
		ExternalHost: a.cfg.External.Host,
		// 系统代理实际状态以文件是否存在为准（不依赖 config 字段，避免配置与文件不一致时误报）。
		SystemProxyEnabled: systemProxyFileExists(),
		HostsEnabled:       a.cfg.Hosts.Enabled,
		DockerEnabled:      a.cfg.Docker.Enabled,
	}
}

// SetExternalHost 保存用户配置的外网接入地址（域名或 IP，可带端口）。
//
// 写入 config.yaml 的 external.host 字段，前端「接入方式」切换到「外网」时
// 会用此地址生成接入示例。空串表示清空，下次切换外网模式前端会引导重新输入。
func (a *App) SetExternalHost(host string) error {
	return a.cfg.Update(func(c *config.Config) error {
		c.External.Host = strings.TrimSpace(host)
		return nil
	})
}

// SysProxyResult 是系统代理设置的结果，供 API 呈现给用户。
type SysProxyResult struct {
	Enabled     bool   `json:"enabled"`
	CAInstalled bool   `json:"ca_installed"` // 本次是否新装了本地 CA 到系统信任
	CATrusted   bool   `json:"ca_trusted"`   // 当前 CA 是否已在系统信任中
	Note        string `json:"note"`
}

// SetSystemProxy 开启或关闭系统级 HTTP 代理。
//
// 开启：往 /etc/profile.d/ghpp-proxy.sh 写入 http_proxy / https_proxy 指向
// 127.0.0.1:<代理端口>，让飞牛本机所有新 login shell 启动的软件默认走加速器，
// 无需软件自身支持配置代理。关闭：删除该文件。
//
// 同步把状态写入 config.yaml 的 system_proxy.enabled，便于启动期与前端展示。
// 已运行的进程不受影响，需重启对应软件才生效——UI 必须给出提示。
//
// installCA（仅开启时有效）：同时把本地 CA 装入系统信任，让 git/ssh 的 HTTPS
// 也走 MITM 全速加速。用户显式拒绝（控制台勾选框取消）时尊重其选择，
// CONNECT 侧的隧道降级（见 proxy.Engine.mitmAllowed）保证流量可用不加速。
func (a *App) SetSystemProxy(enabled, installCA bool) (*SysProxyResult, error) {
	if err := a.cfg.Update(func(c *config.Config) error {
		c.SystemProxy.Enabled = enabled
		c.SystemProxy.EnabledSet = true
		return nil
	}); err != nil {
		return nil, err
	}
	if err := syncSystemProxyFile(enabled, a.cfg.Proxy.Listen); err != nil {
		return nil, err
	}

	res := &SysProxyResult{Enabled: enabled}
	if !enabled {
		res.CATrusted = a.CAInSystemTrust()
		res.Note = "系统代理已关闭"
		return res, nil
	}

	if installCA {
		if a.ca == nil {
			res.Note = "系统代理已开启。本地 CA 不可用，HTTPS 加速受限（透传隧道）。"
			return res, nil
		}
		if a.CAInSystemTrust() {
			res.CATrusted = true
			res.Note = "系统代理已开启，本地 CA 已在系统信任中，git/ssh HTTPS 加速全速生效。"
			return res, nil
		}
		action, err := a.InstallSystemCA()
		if err != nil {
			res.Note = "系统代理已开启，但本地 CA 装入系统信任失败：" + err.Error() +
				"（新 shell 的 HTTPS 走透传隧道，可用但不加速，可稍后在控制台重试）"
			a.log.Logf("warn", "系统代理联动安装 CA 失败: %v", err)
			return res, nil
		}
		res.CAInstalled = action == "installed"
		res.CATrusted = true
		res.Note = "系统代理已开启，本地 CA 已装入系统信任，git/ssh HTTPS 加速全速生效。"
		return res, nil
	}

	res.CATrusted = a.CAInSystemTrust()
	if res.CATrusted {
		res.Note = "系统代理已开启"
	} else {
		res.Note = "系统代理已开启（本地 CA 未装入系统信任），新 shell 的 HTTPS 走透传隧道" +
			"（可用但不加速）；可在控制台点「安装本地 CA 到系统信任」启用全速加速。"
	}
	return res, nil
}

// systemProxyFilePath 是系统级 HTTP 代理环境变量注入文件的固定路径。
//
// 选 /etc/profile.d/ 是因为飞牛基于 Debian，所有 login shell（含 SSH 登录、
// systemd 服务里 source 了 profile 的）都会自动 source 该目录下 .sh 文件，
// 不需要改 ~/.bashrc 或单个软件配置。
const systemProxyFilePath = "/etc/profile.d/ghpp-proxy.sh"

// syncSystemProxyFile 按开关状态创建或删除 /etc/profile.d/ghpp-proxy.sh。
//
// 文件内容导出 http_proxy/https_proxy/HTTP_PROXY/HTTPS_PROXY 与 no_proxy，
// no_proxy 排除本机与私网段，避免 NAS 内部通信也走代理。
// 写入失败（权限不足或 /etc/profile.d 不存在）时返回错误，由 API 层提示用户。
func syncSystemProxyFile(enabled bool, proxyListen string) error {
	if !enabled {
		// 关闭：删除文件。文件不存在视为成功，避免首次关闭报错。
		err := os.Remove(systemProxyFilePath)
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("删除系统代理文件失败：%w（请确认加速器以 root 运行）", err)
		}
		return nil
	}

	// 端口取自代理服务的实际监听地址；异常时回退 37710。
	port := "37710"
	if _, p, err := net.SplitHostPort(proxyListen); err == nil && p != "" {
		port = p
	}

	// NAS 本机 shell 走 loopback，地址固定 127.0.0.1。
	proxyURL := "http://127.0.0.1:" + port
	// no_proxy 排除本机与私网段，避免 NAS 内部通信（emby/jellyfin 等本地服务）也走代理。
	noProxy := "localhost,127.0.0.1,::1,192.168.0.0/16,10.0.0.0/8,172.16.0.0/12,*.local"

	content := "# GitHub++ 加速器系统代理（自动生成，请勿手动编辑）\n" +
		"# 开启后所有新 login shell 默认走加速器；仅对 GitHub 相关域名加速，\n" +
		"# 其他流量透明转发不影响正常联网。关闭请到 GitHub++ 控制台「应用加速」页。\n" +
		"export http_proxy=" + proxyURL + "\n" +
		"export https_proxy=" + proxyURL + "\n" +
		"export HTTP_PROXY=" + proxyURL + "\n" +
		"export HTTPS_PROXY=" + proxyURL + "\n" +
		"export no_proxy=" + noProxy + "\n" +
		"export NO_PROXY=" + noProxy + "\n"

	// 写入临时文件再原子重命名，避免半写入态被 shell 读到。
	tmp := systemProxyFilePath + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0644); err != nil {
		return fmt.Errorf("写入系统代理文件失败：%w（请确认加速器以 root 运行且 /etc/profile.d 可写）", err)
	}
	return os.Rename(tmp, systemProxyFilePath)
}

// systemProxyFileExists 判断系统级 HTTP 代理是否已开启（文件存在即视为开启）。
//
// 用文件存在性而非 config.SystemProxy.Enabled，避免配置与实际文件不一致时误报。
func systemProxyFileExists() bool {
	_, err := os.Stat(systemProxyFilePath)
	return err == nil
}

// Metrics 返回指标状态，供界面轮询。
func (a *App) Metrics() MetricsStatus {
	return a.Status().Metrics
}
