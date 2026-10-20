package api

import (
	"strings"
	"testing"
)

func TestBuildPAC_ContainsHostsAndProxy(t *testing.T) {
	out := buildPAC([]string{"github.com", "api.github.com", "github.com"}, "192.168.1.2:37710")

	if !strings.Contains(out, `"github.com"`) {
		t.Fatal("缺少 github.com")
	}
	if !strings.Contains(out, `"api.github.com"`) {
		t.Fatal("缺少 api.github.com")
	}
	if strings.Count(out, `"github.com"`) != 1 {
		t.Fatalf("重复域名未去重: %s", out)
	}
	if !strings.Contains(out, `PROXY 192.168.1.2:37710; DIRECT`) {
		t.Fatalf("代理地址不正确: %s", out)
	}
	if !strings.Contains(out, `return "DIRECT";`) {
		t.Fatal("缺少 DIRECT 兜底")
	}
	if !strings.Contains(out, "function FindProxyForURL") {
		t.Fatal("缺少入口函数")
	}
	if strings.Contains(out, "dnsResolve") || strings.Contains(out, "isInNet") {
		t.Fatal("PAC 不应使用阻塞或 IPv6 不可靠的函数")
	}
}

func TestBuildPAC_EmptyHosts(t *testing.T) {
	out := buildPAC(nil, "127.0.0.1:37710")
	if !strings.Contains(out, "var DOGDEV_HOSTS = [\n];") {
		t.Fatalf("空清单应生成合法空数组: %s", out)
	}
}

func TestDedupeStrings(t *testing.T) {
	got := dedupeStrings([]string{"a", "", "b", "a", "c"})
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}
