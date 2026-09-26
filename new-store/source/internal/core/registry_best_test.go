package core

import (
	"testing"

	"fnos-store/internal/source"
)

// 内置目录收录商店自己的旧版本（conversun/fnos-apps apps.json 里的
// fnos-apps-store 1.9.5），外部源有 1.19.5。Get 的精确键命中会优先返回
// 内置旧条目；GetBest 必须跨源取版本最高者，否则自更新会降级商店。
func TestRegistryGetBestPrefersHighestVersion(t *testing.T) {
	reg := NewRegistry()
	// 内置目录条目（Source=fnos-apps，精确键=裸 appname）与外部源条目
	// 同一次 Merge 注入（Merge 会整体重建 apps map）。
	reg.Merge(nil, []source.RemoteApp{
		{AppName: "fnos-apps-store", Version: "1.9.5", Source: "fnos-apps"},
		{AppName: "fnos-apps-store", Version: "1.19.5", FpkVersion: "1.19.5", Source: "BlueMink"},
	}, nil)

	got, ok := reg.Get("fnos-apps-store")
	if !ok {
		t.Fatal("Get: 未找到 fnos-apps-store")
	}
	if got.LatestVersion != "1.9.5" {
		t.Fatalf("Get 精确键命中内置条目，期望 1.9.5，得到 %s", got.LatestVersion)
	}

	best, ok := reg.GetBest("fnos-apps-store")
	if !ok {
		t.Fatal("GetBest: 未找到 fnos-apps-store")
	}
	if best.FpkVersion != "1.19.5" {
		t.Fatalf("GetBest 应跨源取最高版本 1.19.5，得到 %s（source=%s）", best.FpkVersion, best.Source)
	}
	if best.Source != "BlueMink" {
		t.Fatalf("GetBest 应取外部源条目，得到 source=%s", best.Source)
	}
}

func TestRegistryGetBestTiePrefersExternal(t *testing.T) {
	reg := NewRegistry()
	reg.Merge(nil, []source.RemoteApp{
		{AppName: "same", Version: "2.0.0", Source: "fnos-apps"},
		{AppName: "same", Version: "2.0.0", FpkVersion: "2.0.0", Source: "Ext"},
	}, nil)

	best, ok := reg.GetBest("same")
	if !ok {
		t.Fatal("GetBest: 未找到")
	}
	if best.Source != "Ext" {
		t.Fatalf("版本平局应优先外部源，得到 source=%s", best.Source)
	}
}

func TestRegistryGetBestEmptyVersionLoses(t *testing.T) {
	reg := NewRegistry()
	reg.Merge(nil, []source.RemoteApp{
		{AppName: "x", Version: "", Source: "A"},
		{AppName: "x", Version: "1.0.0", FpkVersion: "1.0.0", Source: "B"},
	}, nil)
	best, ok := reg.GetBest("x")
	if !ok {
		t.Fatal("GetBest: 未找到")
	}
	if best.FpkVersion != "1.0.0" {
		t.Fatalf("空版本条目不应胜出，得到 version=%s source=%s", best.FpkVersion, best.Source)
	}
}
