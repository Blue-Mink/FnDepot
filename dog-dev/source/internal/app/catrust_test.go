package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ghpp/ghpp/internal/proxy"
)

// setupFakeSysTrust 把系统信任相关全局指向临时目录，并用假 update-ca-certificates
// （把证书目录里的 .crt 拼成 bundle）替换重建命令。返回 (ca, bundle 路径)。
func setupFakeSysTrust(t *testing.T) (*proxy.CA, string) {
	t.Helper()
	dir := t.TempDir()
	caDir := filepath.Join(dir, "system-ca-dir")
	bundle := filepath.Join(dir, "bundle.crt")

	fakeCmd := filepath.Join(dir, "update-ca-certificates")
	script := "#!/bin/sh\n" +
		"cat \"${FAKE_CADIR}\"/*.crt > \"${FAKE_BUNDLE}\" 2>/dev/null || true\n"
	if err := os.WriteFile(fakeCmd, []byte(script), 0o755); err != nil {
		t.Fatalf("写假命令失败: %v", err)
	}
	t.Setenv("FAKE_CADIR", caDir)
	t.Setenv("FAKE_BUNDLE", bundle)

	oldDir, oldFile, oldBundle, oldCmd := systemCADir, systemCAFile, systemCABundle, updateCACertsExe
	systemCADir = caDir
	systemCAFile = filepath.Join(caDir, "ghpp-ca.crt")
	systemCABundle = bundle
	updateCACertsExe = fakeCmd
	t.Cleanup(func() {
		systemCADir, systemCAFile, systemCABundle, updateCACertsExe = oldDir, oldFile, oldBundle, oldCmd
	})

	ca, err := proxy.LoadOrCreateCA(filepath.Join(dir, "app-ca"))
	if err != nil {
		t.Fatalf("生成测试 CA 失败: %v", err)
	}
	return ca, bundle
}

// TestCATrustDetection 验证信任检测的三种判定。
func TestCATrustDetection(t *testing.T) {
	ca, bundle := setupFakeSysTrust(t)

	// 无 bundle → 未信任（fail-safe）。
	if caInSystemTrustFor(ca) {
		t.Fatalf("无 bundle 时应判未信任")
	}
	if caInSystemTrustFor(nil) {
		t.Fatalf("nil CA 应判未信任")
	}

	// bundle 含该 CA → 信任。
	if err := os.WriteFile(bundle, ca.CertPEM(), 0o644); err != nil {
		t.Fatal(err)
	}
	if !caInSystemTrustFor(ca) {
		t.Fatalf("bundle 含当前 CA 指纹时应判已信任")
	}

	// bundle 换成另一张 CA → 未信任（认指纹不认文件存在）。
	other, err := proxy.LoadOrCreateCA(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bundle, other.CertPEM(), 0o644); err != nil {
		t.Fatal(err)
	}
	if caInSystemTrustFor(ca) {
		t.Fatalf("bundle 里是别的 CA 时应判未信任")
	}
}

// TestInstallSystemCAIdempotent 验证安装动作幂等且可复核。
func TestInstallSystemCAIdempotent(t *testing.T) {
	ca, _ := setupFakeSysTrust(t)

	action, err := installSystemCAFor(ca)
	if err != nil {
		t.Fatalf("首次安装失败: %v", err)
	}
	if action != "installed" {
		t.Fatalf("首次安装应返回 installed，实际 %q", action)
	}
	if _, err := os.Stat(systemCAFile); err != nil {
		t.Fatalf("系统证书文件应存在: %v", err)
	}
	if !caInSystemTrustFor(ca) {
		t.Fatalf("安装后应复核为已信任")
	}

	// 重复安装：幂等，直接 already。
	action, err = installSystemCAFor(ca)
	if err != nil {
		t.Fatalf("重复安装失败: %v", err)
	}
	if action != "already" {
		t.Fatalf("重复安装应返回 already，实际 %q", action)
	}
}

// TestRemoveSystemCAFile 验证移除动作幂等。
func TestRemoveSystemCAFile(t *testing.T) {
	ca, _ := setupFakeSysTrust(t)

	if _, err := installSystemCAFor(ca); err != nil {
		t.Fatalf("安装失败: %v", err)
	}
	removeSystemCAFile()
	if _, err := os.Stat(systemCAFile); !os.IsNotExist(err) {
		t.Fatalf("移除后系统证书文件应不存在，实际 err=%v", err)
	}
	if caInSystemTrustFor(ca) {
		t.Fatalf("移除后应判未信任")
	}

	// 从未安装时调用：不报错。
	removeSystemCAFile()
}

// TestAppCATrustCache 验证 App 级缓存与失效（30s TTL 内命中缓存）。
func TestAppCATrustCache(t *testing.T) {
	ca, bundle := setupFakeSysTrust(t)

	if err := os.WriteFile(bundle, ca.CertPEM(), 0o644); err != nil {
		t.Fatal(err)
	}
	a := &App{ca: ca}
	if !a.CAInSystemTrust() {
		t.Fatalf("首次检测应为已信任")
	}

	// 缓存期内 bundle 变化不影响结论（证明命中缓存）。
	if err := os.WriteFile(bundle, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	if !a.CAInSystemTrust() {
		t.Fatalf("缓存期内应保持首次结论")
	}

	// 失效后重新检测。
	a.InvalidateCATrust()
	if a.CAInSystemTrust() {
		t.Fatalf("失效后应重新检测为未信任")
	}
}
