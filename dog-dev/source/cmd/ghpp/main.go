// Command ghpp 是运行在飞牛 NAS 上的 GitHub 加速器。
//
// 两个监听端口分工明确：
//
//	37710 —— 加速代理入口，客户端把请求指向这里
//	37717 —— Web 控制台，用于管理与查看状态
//
// 典型用法：
//
//	ghpp                    # 使用默认数据目录启动
//	ghpp -data /vol1/ghpp   # 指定数据目录
//	ghpp -version           # 查看版本
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ghpp/ghpp/internal/api"
	"github.com/ghpp/ghpp/internal/app"
	"github.com/ghpp/ghpp/internal/config"
	"github.com/ghpp/ghpp/internal/hostsfile"
	"github.com/ghpp/ghpp/internal/logbus"
	"github.com/ghpp/ghpp/web"
)

// 版本信息由构建脚本通过 -ldflags 注入。
var (
	version   = "dev"
	commit    = "none"
	buildTime = "unknown"
)

func main() {
	var (
		dataDir       = flag.String("data", "", "数据目录，默认为飞牛应用数据目录或 ~/.ghpp")
		proxyAddr     = flag.String("proxy", "", "覆盖代理监听地址，例如 0.0.0.0:37710")
		webAddr       = flag.String("web", "", "覆盖控制台监听地址，例如 0.0.0.0:37717")
		showVer       = flag.Bool("version", false, "打印版本信息后退出")
		resetPass     = flag.Bool("reset-password", false, "重置控制台密码后退出")
		clearHost     = flag.Bool("clear-hosts", false, "清理系统 hosts 中的加速托管区后退出，供卸载时调用")
		clearSystemCA = flag.Bool("clear-system-ca", false, "从系统信任移除本地 CA 后退出，供卸载时调用")
		ensureSystemCA = flag.Bool("ensure-system-ca", false, "系统代理开启时确保本地 CA 已装入系统信任，供安装/升级回调调用")
		noWeb         = flag.Bool("no-web", false, "仅运行代理，不启动控制台")
		gatewaySocket = flag.String("gateway-socket", "", "飞牛统一网关 unix socket 路径（平台经此转发桌面入口）")
		gatewayPrefix = flag.String("gateway-prefix", "/app/dog-dev", "飞牛统一网关访问前缀")
		setPorts      = flag.String("set-ports", "", "仅写入监听端口后退出，格式 proxy=37710,web=37717（供安装向导调用）")
	)
	flag.Parse()

	if *showVer {
		fmt.Printf("Dog-dev %s (commit %s, built %s)\n", version, commit, buildTime)
		return
	}

	// 卸载卫生：移除曾被装入系统信任的本地 CA（幂等，从未装过则什么都不做），
	// 避免系统残留对一个已删私钥的信任。不需要加载配置，直接文件系统操作。
	if *clearSystemCA {
		app.RemoveSystemCAFile()
		fmt.Println("本地 CA 已从系统信任移除（幂等，未安装则跳过）。")
		return
	}

	// 解析数据目录。
	dir := config.ResolveDataDir(*dataDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "创建数据目录 %s 失败: %v\n", dir, err)
		os.Exit(1)
	}

	// 安装/升级回调补装 CA：系统代理已开启（重装保留的 config）但系统信任里的
	// CA 被卸载流程清掉时，这里补装，避免新 shell 的 GitHub HTTPS 降级成纯隧道
	// 直连超时。失败仅打印告警、不阻断安装（应用启动后仍可用，HTTPS 走隧道兜底）。
	if *ensureSystemCA {
		if err := app.EnsureSystemCA(dir); err != nil {
			fmt.Fprintf(os.Stderr, "补装系统 CA 失败: %v\n", err)
		}
		return
	}

	// 初始化日志总线：内存保留 2000 条，同时落盘到数据目录。
	// 按大小轮转（5MB 触发，保留 3 份历史），避免日志无限增长。
	logPath := ""
	if etcDir := config.ResolveEtcDir(dir); etcDir != "" {
		logPath = etcDir + string(os.PathSeparator) + "ghpp.log"
	}
	log := logbus.NewWithRotation(2000, logPath, 5<<20, 3)
	defer log.Close()

	// 加载配置。
	cfg, err := config.Load(dir)
	if err != nil {
		log.Logf("error", "加载配置失败: %v", err)
		os.Exit(1)
	}

	// 同步看门狗自动重启开关到标记文件，供 fpk/cmd/main 的 run_with_watchdog 读取。
	// 跳过 reset-password / clear-hosts 等子命令，避免无谓写盘。
	if !*resetPass && !*clearHost && *setPorts == "" {
		if err := config.SyncWatchdogFlag(dir, cfg.Watchdog.AutoRestart); err != nil {
			log.Logf("warn", "写看门狗标记文件失败: %v", err)
		}
	}

	// 安装向导落端口：装机回调在首次启动前调用，把用户填的端口写进配置后退出。
	if *setPorts != "" {
		if err := applyPorts(cfg, *setPorts); err != nil {
			fmt.Fprintf(os.Stderr, "写入端口失败: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("已写入监听端口：代理 %s，控制台 %s\n", cfg.Proxy.Listen, cfg.Server.Listen)
		return
	}

	// 命令行参数覆盖配置。
	if *proxyAddr != "" {
		_ = cfg.Update(func(c *config.Config) error {
			c.Proxy.Listen = *proxyAddr
			return nil
		})
	}
	if *webAddr != "" {
		_ = cfg.Update(func(c *config.Config) error {
			c.Server.Listen = *webAddr
			return nil
		})
	}

	// 密码重置：用于忘记密码时通过命令行恢复访问。
	if *resetPass {
		newPass := config.RandomToken(8)
		if err := cfg.Update(func(c *config.Config) error {
			c.Server.Password = newPass
			return nil
		}); err != nil {
			fmt.Fprintf(os.Stderr, "重置密码失败: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("新的控制台密码: %s\n", newPass)
		fmt.Printf("用户名: %s\n", cfg.Server.Username)
		return
	}

	// hosts 清理：卸载应用时由生命周期脚本调用，
	// 即使服务没有正常停止过也能把系统 hosts 还原到初始状态。
	if *clearHost {
		mgr := hostsfile.NewManager(cfg.Hosts.FilePath, func(string, string, ...any) {})
		if err := mgr.Clear(); err != nil {
			fmt.Fprintf(os.Stderr, "清理 hosts 失败: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("已清理 %s 中的加速托管区\n", cfg.Hosts.FilePath)
		return
	}

	// 启动异步落盘协程：此后配置修改（模式切换等）内存立即生效、
	// 磁盘 300ms 防抖合并写，避免 btrfs rename 抖动拖慢控制台接口。
	// 必须用 Load 出来的原始对象（Snapshot 会剥离 saver 状态）。
	cfg.StartSaver()

	// 打印启动横幅，把关键信息清楚地展示给用户。
	printBanner(cfg.Snapshot(), dir, log.Path())

	// 组装应用。
	application := app.New(cfg, log)

	if err := application.Start(); err != nil {
		log.Logf("error", "启动失败: %v", err)
		os.Exit(1)
	}

	// 代理监听成功之后才写自身 PID 到 GHPH_PID_FILE 指定路径，供 fpk/cmd/main
	// 的 stop/status 管理。这是 setsid 启动方式下唯一可靠的取 PID 方式
	// （setsid 后 $! 拿到的是 setsid 而非真实服务进程）。
	// 绑定失败即退出的实例（如端口已被重复实例占用）不留 pidfile：
	// 否则会覆盖健康实例写入的 pidfile，让 stop/status 跟着死 pid 走。
	if v := os.Getenv("GHPH_PID_FILE"); v != "" {
		_ = os.WriteFile(v, []byte(fmt.Sprintf("%d", os.Getpid())), 0o644)
	}

	// 控制台服务。
	var server *api.Server
	if !*noWeb {
		api.Version = version
		server = api.NewServer(application, web.Handler())
		go func() {
			if err := server.Start(cfg.Server.Listen); err != nil {
				log.Logf("error", "控制台服务退出: %v", err)
			}
		}()
		// 飞牛统一网关入口（桌面图标走这里，与控制台端口解耦）。
		if err := server.StartGateway(*gatewaySocket, *gatewayPrefix); err != nil {
			log.Logf("warn", "统一网关入口不可用（不影响直连端口访问）: %v", err)
		}
	}

	// 等待退出信号。
	// - SIGINT/SIGTERM：优雅退出，状态码 0，让看门狗识别为"主动退出"不再拉起。
	// - SIGHUP：忽略（setsid 已让进程脱离会话，这里再保险一次），
	//   避免极端场景下终端断开误退出。看门狗在收到 SIGTERM 后会让 rc=0 退出。
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	// SIGHUP 走单独 channel，收到后仅记日志、不退出。
	hupCh := make(chan os.Signal, 1)
	signal.Notify(hupCh, syscall.SIGHUP)
	go func() {
		for range hupCh {
			log.Logf("warn", "收到 SIGHUP（终端断开），已忽略并继续运行")
		}
	}()

	sig := <-sigCh
	log.Logf("info", "收到信号 %s，开始优雅退出…", sig)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if server != nil {
		_ = server.Stop(ctx)
	}
	if err := application.Stop(ctx); err != nil {
		log.Logf("warn", "停止服务时出现错误: %v", err)
	}
	// 停止配置落盘协程并做最后一次落盘，避免退出前 300ms 内的
	// 改动（如刚切完模式就关机）丢失。
	cfg.Close()

	// 给日志刷盘留一点时间。
	time.Sleep(200 * time.Millisecond)

	// 显式返回 0，让看门狗识别为"主动退出"不再拉起；进程崩溃时 rc 非 0 才会重启。
	os.Exit(0)
}

// printBanner 输出启动信息，包含用户最需要的访问地址与初始密码。
func printBanner(cfg *config.Config, dataDir, logPath string) {
	lanIP := detectLANIP()

	fmt.Println()
	fmt.Println("  ┌─────────────────────────────────────────────────────────┐")
	fmt.Println("  │              Dog-dev 飞牛 NAS 加速器                       │")
	fmt.Println("  └─────────────────────────────────────────────────────────┘")
	fmt.Printf("   版本      : %s\n", version)
	fmt.Printf("   数据目录  : %s\n", dataDir)
	fmt.Printf("   配置文件  : %s\n", cfg.Path())
	if logPath != "" {
		fmt.Printf("   日志文件  : %s\n", logPath)
	}
	fmt.Println()
	fmt.Println("   ── 加速代理入口（客户端指向这里）──")
	fmt.Printf("   本机      : http://127.0.0.1:%s\n", portOf(cfg.Proxy.Listen))
	if lanIP != "" {
		fmt.Printf("   局域网    : http://%s:%s\n", lanIP, portOf(cfg.Proxy.Listen))
	}
	fmt.Println()
	fmt.Println("   ── Web 控制台（在这里管理）──")
	fmt.Printf("   本机      : http://127.0.0.1:%s\n", portOf(cfg.Server.Listen))
	if lanIP != "" {
		fmt.Printf("   局域网    : http://%s:%s\n", lanIP, portOf(cfg.Server.Listen))
	}
	fmt.Printf("   用户名    : %s\n", cfg.Server.Username)
	fmt.Printf("   密码      : %s\n", cfg.Server.Password)
	fmt.Println()
	fmt.Println("   提示：请登录控制台后立即修改默认密码。")
	fmt.Println()
}

// portOf 从监听地址中取出端口部分。
func portOf(addr string) string {
	for i := len(addr) - 1; i >= 0; i-- {
		if addr[i] == ':' {
			return addr[i+1:]
		}
	}
	return addr
}

// detectLANIP 探测本机在局域网中的可用 IP，用于打印访问地址。
func detectLANIP() string {
	ifaces, err := netInterfaces()
	if err != nil {
		return ""
	}
	for _, ip := range ifaces {
		if ip == "" || ip == "127.0.0.1" || ip == "::1" {
			continue
		}
		// 跳过回环与链路本地地址。
		if len(ip) >= 4 && ip[:4] == "169." {
			continue
		}
		return ip
	}
	return ""
}

// applyPorts 把安装向导填写的端口写进配置（仅供 -set-ports 使用）。
//
// 规格串形如 proxy=37710,web=37717，任一段可省略（省略则保持现有值）。
// 只改监听端口，其余配置（加速源、证书、用户设置）原样保留；
// 已存在的配置文件被升级/重装时也不会因为向导默认值被冲掉。
func applyPorts(cfg *config.Config, spec string) error {
	var proxyPort, webPort string
	for _, part := range strings.Split(spec, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			return fmt.Errorf("无法解析 %q（应为 名称=端口）", part)
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		n, err := strconv.Atoi(value)
		if err != nil || n < 1 || n > 65535 {
			return fmt.Errorf("端口 %q 不是 1-65535 之间的数字", value)
		}
		switch key {
		case "proxy":
			proxyPort = value
		case "web":
			webPort = value
		default:
			return fmt.Errorf("未知端口项 %q（仅支持 proxy / web）", key)
		}
	}
	if proxyPort != "" && proxyPort == webPort {
		return fmt.Errorf("加速代理端口与控制台端口不能相同（都是 %s）", proxyPort)
	}
	return cfg.Update(func(c *config.Config) error {
		if proxyPort != "" {
			c.Proxy.Listen = "0.0.0.0:" + proxyPort
		}
		if webPort != "" {
			c.Server.Listen = "0.0.0.0:" + webPort
		}
		return nil
	})
}
