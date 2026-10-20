package web

import (
	"context"
	"strings"
	"testing"
)

// 统一网关前缀：直连端口为空串，网关下为 /app/<appname>。
func TestPrefixContext(t *testing.T) {
	if got := PrefixFrom(context.Background()); got != "" {
		t.Fatalf("空上下文应无前缀，得到 %q", got)
	}
	cases := map[string]string{
		"/app/dog-dev":  "/app/dog-dev",
		"/app/dog-dev/": "/app/dog-dev",
		"app/dog-dev":   "/app/dog-dev",
		"":              "",
		"/":             "",
		"   ":           "",
	}
	for in, want := range cases {
		if got := PrefixFrom(WithPrefix(context.Background(), in)); got != want {
			t.Fatalf("前缀 %q 归一化后应为 %q，得到 %q", in, want, got)
		}
	}
}

// index.html 的站内绝对地址要补上网关前缀；无前缀时保持原样。
func TestRewriteIndex(t *testing.T) {
	src := []byte(`<link rel="icon" type="image/png" href="/favicon.png">
<script type="module" crossorigin src="/assets/index-abc.js"></script>
<link rel="stylesheet" crossorigin href="/assets/index-abc.css">`)

	plain := string(rewriteIndex(src, ""))
	if strings.Contains(plain, "/app/dog-dev") {
		t.Fatalf("无前缀时不应改写：%s", plain)
	}

	out := string(rewriteIndex(src, "/app/dog-dev"))
	for _, want := range []string{
		`href="/app/dog-dev/favicon.png"`,
		`src="/app/dog-dev/assets/index-abc.js"`,
		`href="/app/dog-dev/assets/index-abc.css"`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("改写后缺少 %q：%s", want, out)
		}
	}
	// 幂等性护栏：重复改写不应叠加前缀（网关重试/多层转发时不会翻倍）。
	if twice := string(rewriteIndex([]byte(out), "/app/dog-dev")); twice != out {
		t.Fatalf("重复改写不应叠加前缀：%s", twice)
	}
}
