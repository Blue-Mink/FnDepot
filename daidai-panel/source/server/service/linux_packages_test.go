package service

import (
	"os"
	"runtime"
	"strings"
	"testing"
)

// 容器配了 PUID/PGID 之后面板以普通用户跑，apt-get / apk 必须 root。
// 原来这条路会一路跑到包管理器自己报英文的 Permission denied / dpkg 锁失败，
// 用户很容易当成面板的 bug。这里锁住「提前拦下并说清楚」这个行为。
func TestEnsureLinuxPackageManagerPrivilege(t *testing.T) {
	err := EnsureLinuxPackageManagerPrivilege()

	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		if err != nil {
			t.Fatalf("root / Windows 下不应拦截，err=%v", err)
		}
		return
	}

	if err == nil {
		t.Fatal("非 root 运行时必须拦下 Linux 系统依赖操作")
	}
	// 报错必须自带出路，否则用户只知道「不行」不知道「怎么办」。
	for _, want := range []string{
		"非 root",
		"可选做法：",
		"Node.js / Python 依赖不受此限制",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("提示里缺少 %q，实际=%q", want, err.Error())
		}
	}

	// 出路必须按部署形态给：Docker 里才提 docker exec，裸机部署提的是 systemd 的 User=。
	// 给二进制部署的用户一段 docker exec 指引等于没给（他机器上既没容器也没 compose）。
	inDocker := false
	if _, statErr := os.Stat("/.dockerenv"); statErr == nil {
		inDocker = true
	}
	if inDocker && !strings.Contains(err.Error(), "docker exec -u 0") {
		t.Fatalf("容器内应给出 docker exec 出路，实际=%q", err.Error())
	}
	if !inDocker && strings.Contains(err.Error(), "docker exec -u 0") {
		t.Fatalf("非容器部署不应给出 docker exec 出路，实际=%q", err.Error())
	}
}

func TestBuildLinuxPackageCommandChecksPrivilegeBeforeTouchingMirror(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("这条用例只在非 root 环境下有意义")
	}

	mirrorCalled := false
	ensureMirror := func(LinuxPackageManager, string) error {
		mirrorCalled = true
		return nil
	}

	for _, action := range []string{"install", "remove"} {
		cmd, err := BuildLinuxPackageCommand(
			LinuxPackageManager{Name: "apk", Binary: "apk"},
			action, "curl", false, "alpine", ensureMirror,
		)
		if err == nil {
			t.Fatalf("action=%s 非 root 时必须直接返回错误", action)
		}
		if cmd != nil {
			t.Fatalf("action=%s 被拦下时不应返回命令", action)
		}
	}

	// 权限闸必须排在换镜像源之前：注定要失败的操作没有理由先去改用户的 sources.list。
	if mirrorCalled {
		t.Fatal("权限检查必须排在 ensureMirror 之前")
	}
}
