package api

import "testing"

func TestParseFpkManifest(t *testing.T) {
	m := parseFpkManifest("appname         = memos\nversion         = 0.30.0\nfpk_version     = 0.30.0-r3\nplatform        = x86\ndesc            = 轻量级笔记服务\n")
	if m.appName != "memos" || m.version != "0.30.0" || m.fpkVersion != "0.30.0-r3" {
		t.Fatalf("parse = %+v, want memos/0.30.0/0.30.0-r3", m)
	}
	// 垃圾输入不得误解析出 appname
	if m2 := parseFpkManifest("junk line\n# comment\nnoequals"); m2.appName != "" || m2.version != "" {
		t.Fatalf("garbage parsed: %+v", m2)
	}
	// 只有 version 没有 fpk_version 也是合法的
	m3 := parseFpkManifest("appname = x\nversion = 1.0\n")
	if m3.appName != "x" || m3.fpkVersion != "" {
		t.Fatalf("m3 = %+v", m3)
	}
}

func TestValidateFpkName(t *testing.T) {
	if err := validateFpkName("gitea-gitea_1.27.3_x86.fpk"); err != nil {
		t.Fatalf("valid name rejected: %v", err)
	}
	if err := validateFpkName("UPPER.FPK"); err != nil {
		t.Fatalf("uppercase .fpk rejected: %v", err)
	}
	for _, bad := range []string{"", "..", "../etc/passwd", "a/b.fpk", "a\\b.fpk", "notes.txt", "a..b.fpk"} {
		if err := validateFpkName(bad); err == nil {
			t.Fatalf("name %q should be rejected", bad)
		}
	}
}
