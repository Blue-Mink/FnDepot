package proxy

import (
	"testing"

	"github.com/ghpp/ghpp/internal/config"
)

// newTestCA 生成一套一次性 CA 供决策矩阵测试（不接触真实数据目录）。
func newTestCA(t *testing.T) *CA {
	t.Helper()
	ca, err := LoadOrCreateCA(t.TempDir())
	if err != nil {
		t.Fatalf("生成测试 CA 失败: %v", err)
	}
	return ca
}

func newTestEngine(cfg *config.Config, ca *CA, trusted func() bool) *Engine {
	return NewEngine(Options{
		Config:          func() *config.Config { return cfg },
		Logf:            func(string, string, ...any) {},
		CA:              ca,
		CAInSystemTrust: trusted,
	})
}

func cfgWithSysProxy(enabled bool) *config.Config {
	return &config.Config{SystemProxy: config.SystemProxyConfig{Enabled: enabled}}
}

// TestMitmAllowedMatrix 验证 CONNECT 分流决策矩阵（P0-2 的 b）。
func TestMitmAllowedMatrix(t *testing.T) {
	ca := newTestCA(t)

	cases := []struct {
		name      string
		ca        *CA
		sysProxy  bool
		trustedFn func() bool // nil 表示未注入检测器
		wantMITM  bool
	}{
		{"无 CA：恒隧道", nil, false, func() bool { return true }, false},
		{"无 CA：恒隧道（系统代理开）", nil, true, func() bool { return true }, false},
		{"CA 在 + 系统代理关 + 未信任：仍 MITM（单应用代理场景，浏览器自装 CA）", ca, false, func() bool { return false }, true},
		{"CA 在 + 系统代理开 + 已信任：MITM", ca, true, func() bool { return true }, true},
		{"CA 在 + 系统代理开 + 未信任：降级隧道", ca, true, func() bool { return false }, false},
		{"CA 在 + 系统代理开 + 未注入检测器：按信任处理（旧行为）", ca, true, nil, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newTestEngine(cfgWithSysProxy(c.sysProxy), c.ca, c.trustedFn)
			if got := e.mitmAllowed(cfgWithSysProxy(c.sysProxy)); got != c.wantMITM {
				t.Fatalf("mitmAllowed = %v，期望 %v", got, c.wantMITM)
			}
		})
	}
}

// TestTunnelFallbackLogThrottle 验证「降级隧道」提示每域名 5 分钟内只打一条。
func TestTunnelFallbackLogThrottle(t *testing.T) {
	var logs []string
	e := NewEngine(Options{
		Logf: func(level, format string, args ...any) { logs = append(logs, format) },
	})

	for i := 0; i < 5; i++ {
		e.logfTunnelFallback("github.com")
	}
	if len(logs) != 1 {
		t.Fatalf("5 分钟内重复提示应只记 1 条，实际 %d 条", len(logs))
	}

	// 另一域名不受影响。
	e.logfTunnelFallback("gist.github.com")
	if len(logs) != 2 {
		t.Fatalf("不同域名应各自记一条，实际 %d 条", len(logs))
	}
}

// TestDirectWarnOnlyOnEpisodeStart 验证直连熔断 warn 只在进入新一轮冷却时记（P1-2）。
func TestDirectWarnOnlyOnEpisodeStart(t *testing.T) {
	var warns, infos []string
	e := NewEngine(Options{
		Logf: func(level, format string, args ...any) {
			switch level {
			case "warn":
				warns = append(warns, format)
			case "info":
				infos = append(infos, format)
			}
		},
	})

	const host = "gist.github.com"
	e.recordDirect(host, false) // fails=1，未达阈值
	if len(warns) != 0 {
		t.Fatalf("未达阈值不应记 warn，实际 %d 条", len(warns))
	}
	e.recordDirect(host, false) // fails=2，进入第一轮冷却 → warn
	if len(warns) != 1 {
		t.Fatalf("进入冷却应记 1 条 warn，实际 %d 条", len(warns))
	}
	// 冷却期内后台探测连续续期失败：不再重复 warn（旧行为每 ~70s 刷一条）。
	e.recordDirect(host, false)
	e.recordDirect(host, false)
	e.recordDirect(host, false)
	if len(warns) != 1 {
		t.Fatalf("轮次内续期失败不应再记 warn，实际 %d 条", len(warns))
	}

	// 恢复：应有一条 info。
	e.recordDirect(host, true)
	if len(infos) != 1 {
		t.Fatalf("恢复应记 1 条 info，实际 %d 条", len(infos))
	}
	// 恢复后再失败：新的一轮冷却 → 允许再 warn。
	e.recordDirect(host, false)
	e.recordDirect(host, false)
	if len(warns) != 2 {
		t.Fatalf("新一轮冷却应再记 1 条 warn，实际 %d 条", len(warns))
	}
}
