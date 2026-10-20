package api

import (
	"strings"
	"testing"
)

// TestRenderCAPagePlaceholders 守住模板占位符：任何一个 @XXX@ 没被替换，
// 手机端就会看到生硬的占位文字（1.3.4 开发期真实踩过「免证书地址写成控制台端口」的问题）。
func TestRenderCAPagePlaceholders(t *testing.T) {
	out := renderCAPage(calInstallPageData{
		Host:        "192.168.1.2:37717",
		BareURL:     "http://192.168.1.2:37710",
		PEMURL:      "http://192.168.1.2:37717/api/cert/download",
		DERURL:      "http://192.168.1.2:37717/api/cert/download?format=der",
		QR:          "data:image/png;base64,AAA",
		Fingerprint: "39:9D:67",
		NotAfter:    "2036-10-03",
		State:       calDeviceState{Kind: "untrusted", IP: "192.168.1.76", Hosts: []string{"github.com"}, Failures: 3},
	})

	for _, bad := range []string{"@HOST@", "@BASE@", "@QR@", "@PEM_URL@", "@DER_URL@", "@STATE_TITLE@", "@FINGERPRINT@"} {
		if strings.Contains(out, bad) {
			t.Fatalf("模板占位符未被替换: %s", bad)
		}
	}
	if !strings.Contains(out, "http://192.168.1.2:37710/https://github.com") {
		t.Fatal("免证书下载地址应指向代理端口，而不是控制台端口")
	}
	if !strings.Contains(out, "这台设备还没装好证书") {
		t.Fatal("未信任状态应给出对应文案")
	}
	if !strings.Contains(out, "3 次") || !strings.Contains(out, "github.com") {
		t.Fatal("应展示被拒次数与域名")
	}
}

func TestHostOnly(t *testing.T) {
	if got := hostOnly("192.168.1.2:37717"); got != "192.168.1.2" {
		t.Fatalf("hostOnly 异常: %s", got)
	}
	if got := hostOnly("192.168.1.2"); got != "192.168.1.2" {
		t.Fatalf("无端口时应该原样返回: %s", got)
	}
}
