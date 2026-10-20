package certtrack

import "testing"

func TestUntrustedThenTrusted(t *testing.T) {
	Reset()
	defer Reset()

	MarkUntrusted("192.168.1.76:5678", "github.com")
	MarkUntrusted("192.168.1.76", "api.github.com")

	e, ok := Lookup("192.168.1.76")
	if !ok {
		t.Fatal("应该登记该设备")
	}
	if e.Trusted {
		t.Fatal("握手失败后不应被标为已信任")
	}
	if e.Failures != 2 {
		t.Fatalf("失败次数应为 2，实际 %d", e.Failures)
	}
	if len(e.Hosts) != 2 || e.Hosts[0] != "github.com" {
		t.Fatalf("被拒域名记录异常: %v", e.Hosts)
	}

	// 装好证书后握手成功：状态翻转为已信任，失败计数与被拒域名清空。
	MarkTrusted("192.168.1.76")
	e, _ = Lookup("192.168.1.76")
	if !e.Trusted || e.Failures != 0 || len(e.Hosts) != 0 {
		t.Fatalf("信任后状态未清理: %+v", e)
	}

	// 再次失败（例如换了浏览器）要能如实反映。
	MarkUntrusted("192.168.1.76", "github.com")
	e, _ = Lookup("192.168.1.76")
	if e.Trusted || e.Failures != 1 {
		t.Fatalf("重新失败后状态异常: %+v", e)
	}
}

func TestHostsDedupAndCap(t *testing.T) {
	Reset()
	defer Reset()
	for _, h := range []string{"a", "a", "b", "c", "d"} {
		MarkUntrusted("10.0.0.5", h)
	}
	e, _ := Lookup("10.0.0.5")
	if len(e.Hosts) != hostLimit {
		t.Fatalf("被拒域名最多保留 %d 个，实际 %v", hostLimit, e.Hosts)
	}
	// 只保留最近的三个。
	if e.Hosts[len(e.Hosts)-1] != "d" {
		t.Fatalf("应保留最近的域名，实际 %v", e.Hosts)
	}
	e.Failures = 0 // 仅确认字段可写，避免误用
}

func TestNormalizeIgnoresInvalid(t *testing.T) {
	Reset()
	defer Reset()
	MarkUntrusted("", "github.com")
	MarkUntrusted("not-an-ip", "github.com")
	if got := List(); len(got) != 0 {
		t.Fatalf("空/非法地址不应登记: %+v", got)
	}
	MarkUntrusted("::1", "github.com")
	if got := List(); len(got) != 1 || got[0].IP != "::1" {
		t.Fatalf("IPv6 地址应被登记: %+v", got)
	}
}
