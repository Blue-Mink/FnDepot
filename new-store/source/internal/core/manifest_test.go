package core

import (
	"os"
	"path/filepath"
	"testing"
)

func writeManifest(t *testing.T, dir, appname, content string) {
	t.Helper()
	appDir := filepath.Join(dir, appname)
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "manifest"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// One foreign app with a malformed manifest must not abort the whole local
// scan: a single bad file used to fail refreshRegistry, which made EVERY app
// look not-installed — the store then offered 安装 on apps the daemon knew
// were installed and dead-ended users on "已安装，请使用更新功能"
// (conversun/fnos-apps#280, #281).
func TestScanInstalledSkipsMalformedManifest(t *testing.T) {
	dir := t.TempDir()

	writeManifest(t, dir, "good-app", "appname         = good-app\ndistributor     = conversun\n")
	writeManifest(t, dir, "bad-port", "appname         = bad\nservice_port    = not-a-number\n")
	writeManifest(t, dir, "foreign-app", "appname         = foreign\nservice_port    = ???\n")

	apps, err := ScanInstalled(dir)
	if err != nil {
		t.Fatalf("ScanInstalled failed on a malformed sibling manifest: %v", err)
	}
	if len(apps) != 1 || apps[0].AppName != "good-app" {
		t.Fatalf("ScanInstalled = %+v, want only good-app", apps)
	}
}

// A foreign app whose manifest PARSES fine but is missing entirely (directory
// with no manifest) is skipped silently.
func TestScanInstalledSkipsMissingManifest(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, "ours", "appname         = ours\ndistributor     = conversun\n")
	if err := os.MkdirAll(filepath.Join(dir, "no-manifest-app"), 0o755); err != nil {
		t.Fatal(err)
	}

	apps, err := ScanInstalled(dir)
	if err != nil {
		t.Fatalf("ScanInstalled: %v", err)
	}
	if len(apps) != 1 || apps[0].AppName != "ours" {
		t.Fatalf("ScanInstalled = %+v, want only ours", apps)
	}
}

// Regression (2026-09-18): the scan used to drop every manifest whose
// distributor was not "conversun" — but on real fnOS boxes installed
// manifests carry the FPK author's tag (shuangji66, fnos, Blue-Mink, …) or
// none, so the scan returned nothing, every installed app fell back to the
// daemon-reconcile path that forces "up to date", and the dock's 有更新
// badge stayed at 0 no matter what the sources published.
func TestScanInstalledIncludesForeignDistributor(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, "wb2api", "appname         = wb2api\nversion         = 1.9.2\ndistributor     = shuangji66\n")
	writeManifest(t, dir, "gitea", "appname         = gitea\nversion         = 1.27.3\ndistributor     = go-gitea\n")
	writeManifest(t, dir, "plain", "appname         = plain\nversion         = 2.0.1\n")

	apps, err := ScanInstalled(dir)
	if err != nil {
		t.Fatalf("ScanInstalled: %v", err)
	}
	if len(apps) != 3 {
		t.Fatalf("ScanInstalled = %+v, want all 3 apps regardless of distributor", apps)
	}
	byName := map[string]Manifest{}
	for _, a := range apps {
		byName[a.AppName] = a
	}
	if byName["wb2api"].Version != "1.9.2" {
		t.Errorf("wb2api version = %q, want 1.9.2 (needed for update comparison)", byName["wb2api"].Version)
	}
}
