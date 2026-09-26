package source

import "testing"

// looseV1Sample 覆盖社区源的三种"宽松写法"（真实仓库实测）：
//   - isdocker 布尔（X520qq/18926922221/weifang11233/wy77888/FnDepot）
//   - labels 字符串数组（zyp1690/FnDepot）
//   - changelog {版本: 文本} 对象（moxyis/FnDepot）
const looseV1Sample = `{
  "fnlogpush": {
    "display_name": "日志哨兵",
    "version": "1.2.0",
    "labels": ["工具", "系统"],
    "isdocker": false,
    "download_url": "https://cdn.example.com/fnlogpush.fpk",
    "changelog": "v1.2.0\n- DND 组级锁"
  },
  "fpk-napcatqq": {
    "display_name": "NapCatQQ",
    "version": "4.18.28",
    "changelog": {"V4.9.80": "1、新增 pnpm-lock.yaml", "V4.18.28": "2、修复登录"},
    "size": "0.3"
  },
  "bitcomet": {
    "display_name": "BitComet",
    "version": "2.20.0-2",
    "labels": "下载",
    "isdocker": true,
    "download_url": "https://cdn.example.com/bitcomet.fpk"
  }
}`

func TestDecodeFndepotApps_LooseV1Types(t *testing.T) {
	m := decodeBody(t, looseV1Sample)
	if len(m) != 3 {
		t.Fatalf("宽松 V1 应解析 3 个应用，实际 %d", len(m))
	}
	// labels 数组形态
	if len(m["fnlogpush"].Labels) != 2 || m["fnlogpush"].Labels[0] != "工具" {
		t.Errorf("labels 数组解析异常: %v", m["fnlogpush"].Labels)
	}
	// isdocker 布尔形态
	if !bool(m["bitcomet"].IsDockerV1) || bool(m["fnlogpush"].IsDockerV1) {
		t.Errorf("isdocker 布尔解析异常: %v / %v", bool(m["bitcomet"].IsDockerV1), bool(m["fnlogpush"].IsDockerV1))
	}
	// changelog 对象形态：取最高版本
	if got := string(m["fpk-napcatqq"].Changelog); got != "2、修复登录" {
		t.Errorf("changelog 对象应取最高版本条目，实际 %q", got)
	}
	// labels 字符串形态仍兼容
	if len(m["bitcomet"].Labels) != 1 || m["bitcomet"].Labels[0] != "下载" {
		t.Errorf("labels 字符串解析异常: %v", m["bitcomet"].Labels)
	}

	// 翻译验证：changelog 对象条目走仓库内约定（无 download_url）
	ra, ok := translateFndepotApp("fpk-napcatqq", m["fpk-napcatqq"],
		"https://raw.githubusercontent.com/moxyis/FnDepot/HEAD/fnpack.json", "moxyis",
		"https://raw.githubusercontent.com/moxyis/FnDepot/HEAD")
	if !ok {
		t.Fatal("changelog 对象条目应可翻译")
	}
	if ra.Changelog != "2、修复登录" {
		t.Errorf("RemoteApp.Changelog = %q", ra.Changelog)
	}
	if ra.FpkURL != "https://raw.githubusercontent.com/moxyis/FnDepot/HEAD/fpk-napcatqq/fpk-napcatqq.fpk" {
		t.Errorf("FpkURL = %s", ra.FpkURL)
	}
}
