package mirror

import (
	"context"
	"errors"
	"testing"

	"github.com/ghpp/ghpp/internal/config"
)

// TestPathFetchURL：xget 形态翻译规则。
func TestPathFetchURL(t *testing.T) {
	base := "https://xget.xi-xu.me/gh"
	cases := []struct {
		name   string
		host   string
		path   string
		want   string
		wantOK bool
	}{
		{"raw 标准形态", "raw.githubusercontent.com", "/MisiteQ/github-plus-plus/main/go.mod",
			"https://xget.xi-xu.me/gh/MisiteQ/github-plus-plus/raw/main/go.mod", true},
		{"raw 带查询串", "raw.githubusercontent.com", "/o/r/main/f.txt?x=1",
			"https://xget.xi-xu.me/gh/o/r/raw/main/f.txt", true},
		{"codeload 压缩包", "codeload.github.com", "/github/gitignore/tar.gz/refs/heads/main",
			"https://xget.xi-xu.me/gh/github/gitignore/tar.gz/refs/heads/main", true},
		{"github 仓库资源", "github.com", "/o/r/releases/download/v1/a.zip",
			"https://xget.xi-xu.me/gh/o/r/releases/download/v1/a.zip", true},
		{"raw 缺剩余路径", "raw.githubusercontent.com", "/o/r", "", false},
		{"github 仓库首页无资源", "github.com", "/o/r", "", false},
		{"资源 CDN 令牌路径", "objects.githubusercontent.com", "/abc123/token/xyz", "", false},
		{"未受管 host", "gist.githubusercontent.com", "/o/r/raw/f.txt", "", false},
		{"base 带尾斜杠", "raw.githubusercontent.com", "/o/r/main/f.txt",
			"https://xget.xi-xu.me/gh/o/r/raw/main/f.txt", true},
	}
	for _, c := range cases {
		b := base
		if c.name == "base 带尾斜杠" {
			b = base + "/"
		}
		got, ok := PathFetchURL(b, c.host, c.path)
		if ok != c.wantOK {
			t.Errorf("%s: ok=%v，期望 %v（got=%q）", c.name, ok, c.wantOK, got)
			continue
		}
		if ok && got != c.want {
			t.Errorf("%s: got=%q，期望 %q", c.name, got, c.want)
		}
	}
}

// TestSweepSkipDoesNotPunish：源类型不适用的目标被跳过时，
// 不记失败、不降分（区别于真实探测失败）。
func TestSweepSkipDoesNotPunish(t *testing.T) {
	p := NewPool()
	p.probe = func(ctx context.Context, m config.Mirror, t config.ProbeTarget, maxBytes int64) (Result, error) {
		if m.Kind == config.KindPathFetch {
			return Result{}, errProbeSkip
		}
		return Result{OK: true, LatencyMS: 10, ThroughputKBps: 100}, nil
	}
	mirrors := []config.Mirror{
		{ID: "xget-test", URL: "https://xget.xi-xu.me/gh", Kind: config.KindPathFetch, Enabled: true},
		{ID: "prefix-test", URL: "https://ghproxy.net/", Kind: config.KindPrefix, Enabled: true},
	}
	targets := []config.ProbeTarget{
		{Host: "raw.githubusercontent.com", Path: "/o/r/main/f", Category: config.CatRaw},
	}
	res := p.Sweep(context.Background(), mirrors, targets, 64<<10)
	if len(res) == 0 {
		t.Fatal("Sweep 无结果")
	}
	st := p.stats["xget-test"]
	if st != nil {
		t.Fatalf("跳过的源不应进入得分表: %+v", st)
	}
	if st2 := p.stats["prefix-test"]; st2 == nil || st2.failStreak != 0 || st2.score <= 0 {
		t.Fatalf("正常源应记录成功成绩: %+v", st2)
	}
}

// TestSkipSentinelDistinct：跳过哨兵与真实错误可区分。
func TestSkipSentinelDistinct(t *testing.T) {
	if errors.Is(errProbeSkip, errors.New("连接失败")) {
		t.Fatal("哨兵不应与普通错误互判")
	}
	if errors.Is(errors.New("其他错误"), errProbeSkip) {
		t.Fatal("普通错误不应判为跳过")
	}
}
