//go:build linux

package platform

import (
	"os"
	"path/filepath"
	"testing"
)

// linkSubSymlink creates dst as a real directory and points appDir/sub at it,
// mimicking fnOS's /var/apps/<app>/{target,var,meta} symlinks.
func linkSubSymlink(t *testing.T, appDir, sub, dst string) {
	t.Helper()
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dst, err)
	}
	if err := os.Symlink(dst, filepath.Join(appDir, sub)); err != nil {
		t.Fatalf("symlink %s: %v", sub, err)
	}
}

// Root-type apps (install_type = root, e.g. nvidia-driver) are installed to the
// SYSTEM filesystem, so target/var resolve off-volume and only meta — fnOS's
// own per-app volume association — still lands on a storage volume. Measured
// on fnOS 1.2.0203: 60/60 apps have meta, 60/60 metas resolve onto a /volN,
// and meta never contradicts an on-volume target (conversun/fnos-apps#254).
func TestAppInstallVolumeMetaRescuesRootTypeApp(t *testing.T) {
	// Given two volumes and an app whose payload lives on the system fs
	vol1, vol2 := t.TempDir(), t.TempDir()
	vols := []VolumeInfo{{Index: 1, Path: vol1}, {Index: 2, Path: vol2}}
	appDir := t.TempDir()
	sys := t.TempDir() // stands in for /usr/local/apps — NOT under any volume

	linkSubSymlink(t, appDir, "target", filepath.Join(sys, "@appcenter", "nvidia-driver"))
	linkSubSymlink(t, appDir, "var", filepath.Join(sys, "@appdata", "nvidia-driver"))
	linkSubSymlink(t, appDir, "meta", filepath.Join(vol2, "@appmeta", "nvidia-driver"))

	// When the volume is resolved
	idx, found := appInstallVolume(appDir, vols)

	// Then meta rescues the lookup onto vol2
	if !found {
		t.Fatal("found = false, want true — root-type app must resolve via meta")
	}
	if idx != 2 {
		t.Errorf("idx = %d, want 2 (meta's volume)", idx)
	}
}

// For normal apps target stays authoritative: meta must never override it.
func TestAppInstallVolumeTargetStillWins(t *testing.T) {
	// Given an app whose target is on vol1 and meta on vol2
	vol1, vol2 := t.TempDir(), t.TempDir()
	vols := []VolumeInfo{{Index: 1, Path: vol1}, {Index: 2, Path: vol2}}
	appDir := t.TempDir()

	linkSubSymlink(t, appDir, "target", filepath.Join(vol1, "@appcenter", "emby"))
	linkSubSymlink(t, appDir, "meta", filepath.Join(vol2, "@appmeta", "emby"))

	// When the volume is resolved
	idx, found := appInstallVolume(appDir, vols)

	// Then target's volume wins
	if !found || idx != 1 {
		t.Errorf("idx = %d found = %v, want 1, true — target must stay authoritative", idx, found)
	}
}

// Probe order is target, var, meta: with target absent, var beats meta.
func TestAppInstallVolumeVarBeatsMeta(t *testing.T) {
	// Given an app with no target symlink, var on vol2 and meta on vol1
	vol1, vol2 := t.TempDir(), t.TempDir()
	vols := []VolumeInfo{{Index: 1, Path: vol1}, {Index: 2, Path: vol2}}
	appDir := t.TempDir()

	linkSubSymlink(t, appDir, "var", filepath.Join(vol2, "@appdata", "emby"))
	linkSubSymlink(t, appDir, "meta", filepath.Join(vol1, "@appmeta", "emby"))

	// When the volume is resolved
	idx, found := appInstallVolume(appDir, vols)

	// Then var wins over meta
	if !found || idx != 2 {
		t.Errorf("idx = %d found = %v, want 2, true — var must outrank meta", idx, found)
	}
}

// Data-safety guard: when NO probe lands on a known volume the lookup must
// still refuse — the fix adds a probe, it does not remove the guard.
func TestAppInstallVolumeRefusesWhenNoProbeResolves(t *testing.T) {
	// Given an app whose symlinks all point off-volume
	vol1 := t.TempDir()
	vols := []VolumeInfo{{Index: 1, Path: vol1}}
	appDir := t.TempDir()
	sys := t.TempDir()

	linkSubSymlink(t, appDir, "target", filepath.Join(sys, "@appcenter", "ghost"))
	linkSubSymlink(t, appDir, "var", filepath.Join(sys, "@appdata", "ghost"))
	linkSubSymlink(t, appDir, "meta", filepath.Join(sys, "@appmeta", "ghost"))

	// When the volume is resolved
	idx, found := appInstallVolume(appDir, vols)

	// Then it refuses to guess
	if found {
		t.Errorf("found = true (idx %d), want false — unknown volume must still abort the update", idx)
	}
}
