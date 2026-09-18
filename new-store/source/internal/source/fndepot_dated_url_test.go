package source

import "testing"

func TestDateFromReleaseURL(t *testing.T) {
	cases := []struct {
		url  string
		want string
	}{
		{
			url:  "https://github.com/Brian099/fn_fpk_packages/releases/download/2026.09.16-1052/musicwave_v1.0.24.fpk",
			want: "2026-09-16T10:52:00+08:00",
		},
		{
			url:  "https://github.com/shuangji66/Fndepot/releases/download/2026.9.17/Komga-1.27.0-all.fpk",
			want: "2026-09-17T00:00:00+08:00",
		},
		{
			url:  "https://github.com/x/y/releases/download/2026-08-01/app.fpk",
			want: "2026-08-01T00:00:00+08:00",
		},
		{
			url:  "https://github.com/x/y/releases/download/20260715_0930/app.fpk",
			want: "2026-07-15T09:30:00+08:00",
		},
		{
			// 文件名里的日期数字不应被误当成标签（标签段是 latest）
			url:  "https://github.com/x/y/releases/download/latest/app-2026.01.02.fpk",
			want: "",
		},
		{
			url:  "https://example.com/download/app.fpk",
			want: "",
		},
		{
			// 非法日期（13 月）拒绝
			url:  "https://github.com/x/y/releases/download/2026.13.01/app.fpk",
			want: "",
		},
	}
	for _, c := range cases {
		if got := DateFromReleaseURL(c.url); got != c.want {
			t.Errorf("DateFromReleaseURL(%q) = %q, want %q", c.url, got, c.want)
		}
	}
}
