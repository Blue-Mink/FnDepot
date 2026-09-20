package core

import (
	"testing"

	"fnos-store/internal/source"
)

// Merge decides the update badge for every app. The scenarios below are the
// distinct shapes observed on a live fnOS box: modern packages carry
// fpk_version and take the exact comparison, while packages built before
// fpk_version existed fall back to a heuristic.
func TestMergeUpdateStatus(t *testing.T) {
	tests := []struct {
		name             string
		local            *Manifest
		remote           source.RemoteApp
		installedTags    map[string]string
		wantStatus       AppStatus
		wantRevisionFlag bool
		wantInstalledFpk string
	}{
		{
			name:             "not installed",
			local:            nil,
			remote:           source.RemoteApp{AppName: "openlist", Version: "4.2.5", FpkVersion: "4.2.5-r3", ReleaseTag: "openlist/v4.2.5-r3"},
			wantStatus:       AppStatusNotInstalled,
			wantInstalledFpk: "",
		},
		{
			// headscale: manifest version (0.29.7) disagrees with the catalog
			// version (0.29.3), but fpk_version is authoritative and shows a
			// genuine revision bump.
			name:             "fpk revision bump wins over disagreeing plain versions",
			local:            &Manifest{AppName: "headscale", Version: "0.29.7", FpkVersion: "0.29.3-r3"},
			remote:           source.RemoteApp{AppName: "headscale", Version: "0.29.3", FpkVersion: "0.29.3-r4", ReleaseTag: "headscale/v0.29.3-r4"},
			wantStatus:       AppStatusUpdateAvailable,
			wantRevisionFlag: false,
			wantInstalledFpk: "0.29.3-r3",
		},
		{
			name:             "fpk versions identical",
			local:            &Manifest{AppName: "beszel", Version: "0.18.8", FpkVersion: "0.18.8"},
			remote:           source.RemoteApp{AppName: "beszel", Version: "0.18.8", FpkVersion: "0.18.8", ReleaseTag: "beszel/v0.18.8"},
			wantStatus:       AppStatusInstalledUpToDate,
			wantInstalledFpk: "0.18.8",
		},
		{
			name:             "installed fpk ahead of catalog is not an update",
			local:            &Manifest{AppName: "beszel", Version: "0.18.9", FpkVersion: "0.18.9"},
			remote:           source.RemoteApp{AppName: "beszel", Version: "0.18.8", FpkVersion: "0.18.8", ReleaseTag: "beszel/v0.18.8"},
			wantStatus:       AppStatusInstalledUpToDate,
			wantInstalledFpk: "0.18.9",
		},
		{
			// 1panel: legacy package with no fpk_version. Equal plain versions
			// plus a revision-suffixed remote tag means the catalog is ahead.
			name:             "legacy manifest falls back to revision heuristic",
			local:            &Manifest{AppName: "1panel", Version: "1.10.34-lts"},
			remote:           source.RemoteApp{AppName: "1panel", Version: "1.10.34-lts", FpkVersion: "1.10.34-lts-r8", ReleaseTag: "1panel/v1.10.34-lts-r8"},
			wantStatus:       AppStatusUpdateAvailable,
			wantRevisionFlag: true,
			wantInstalledFpk: "",
		},
		{
			// Same legacy shape, but the store already recorded the installed
			// tag and it matches the catalog: no badge.
			name:             "legacy manifest with matching cached tag is up to date",
			local:            &Manifest{AppName: "1panel", Version: "1.10.34-lts"},
			remote:           source.RemoteApp{AppName: "1panel", Version: "1.10.34-lts", FpkVersion: "1.10.34-lts-r8", ReleaseTag: "1panel/v1.10.34-lts-r8"},
			installedTags:    map[string]string{"1panel": "1panel/v1.10.34-lts-r8"},
			wantStatus:       AppStatusInstalledUpToDate,
			wantRevisionFlag: false,
			wantInstalledFpk: "",
		},
		{
			// alist: legacy manifest whose plain version is ahead of the
			// catalog. Must not offer a downgrade.
			name:             "legacy manifest ahead of catalog is up to date",
			local:            &Manifest{AppName: "alist", Version: "3.63.1"},
			remote:           source.RemoteApp{AppName: "alist", Version: "3.63.0", FpkVersion: "3.63.0-r3", ReleaseTag: "alist/v3.63.0-r3"},
			wantStatus:       AppStatusInstalledUpToDate,
			wantRevisionFlag: false,
			wantInstalledFpk: "",
		},
		{
			name:             "legacy manifest behind catalog upstream version",
			local:            &Manifest{AppName: "prometheus", Version: "3.13.3"},
			remote:           source.RemoteApp{AppName: "prometheus", Version: "3.14.0", FpkVersion: "3.14.0", ReleaseTag: "prometheus/v3.14.0"},
			wantStatus:       AppStatusUpdateAvailable,
			wantRevisionFlag: false,
			wantInstalledFpk: "",
		},
		{
			// 2026-09-18 live case (test box): wb2api installed 1.9.2 from a
			// V2 custom source, catalog now 1.10.0, no fpk_version in the
			// installed INI manifest. Must light the badge (the dock used to
			// stay at 0 because the scan dropped non-conversun manifests).
			name:             "external source app behind catalog (no fpk_version)",
			local:            &Manifest{AppName: "wb2api", Version: "1.9.2", Distributor: "shuangji66"},
			remote:           source.RemoteApp{AppName: "wb2api", Version: "1.10.0", FpkVersion: "1.10.0", Source: "shuangji66的应用源"},
			wantStatus:       AppStatusUpdateAvailable,
			wantRevisionFlag: false,
			wantInstalledFpk: "",
		},
		{
			// Multi-digit segment: 1.9.2 < 1.10.0 (numeric, not lexicographic).
			name:             "multi-digit segment compares numerically",
			local:            &Manifest{AppName: "node", Version: "24.15.0-1"},
			remote:           source.RemoteApp{AppName: "node", Version: "24.19.0", FpkVersion: "24.19.0"},
			wantStatus:       AppStatusUpdateAvailable,
			wantRevisionFlag: false,
			wantInstalledFpk: "",
		},
		{
			// Installed version without numeric content cannot be compared:
			// stay quiet instead of badge-everything.
			name:             "non-numeric installed version stays up to date",
			local:            &Manifest{AppName: "someref", Version: "latest"},
			remote:           source.RemoteApp{AppName: "someref", Version: "1.0.0", FpkVersion: "1.0.0"},
			wantStatus:       AppStatusInstalledUpToDate,
			wantRevisionFlag: false,
			wantInstalledFpk: "",
		},
		{
			// Pre-release suffix carries the distinguishing digits: rc.37 < rc.38.
			name:             "prerelease suffix bump detected",
			local:            &Manifest{AppName: "new-api", Version: "1.0.0-rc.37", Distributor: "Blue-Mink"},
			remote:           source.RemoteApp{AppName: "new-api", Version: "1.0.0-rc.38", FpkVersion: "1.0.0-rc.38", Source: "BlueMink"},
			wantStatus:       AppStatusUpdateAvailable,
			wantRevisionFlag: false,
			wantInstalledFpk: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var local []Manifest
			if tt.local != nil {
				local = []Manifest{*tt.local}
			}

			got := NewRegistry().Merge(local, []source.RemoteApp{tt.remote}, tt.installedTags)
			if len(got) != 1 {
				t.Fatalf("Merge returned %d apps, want 1", len(got))
			}
			app := got[0]

			if app.Status != tt.wantStatus {
				t.Errorf("Status = %q, want %q", app.Status, tt.wantStatus)
			}
			if app.HasRevisionUpdate != tt.wantRevisionFlag {
				t.Errorf("HasRevisionUpdate = %v, want %v", app.HasRevisionUpdate, tt.wantRevisionFlag)
			}
			if app.InstalledFpkVersion != tt.wantInstalledFpk {
				t.Errorf("InstalledFpkVersion = %q, want %q", app.InstalledFpkVersion, tt.wantInstalledFpk)
			}
			if app.Installed != (tt.local != nil) {
				t.Errorf("Installed = %v, want %v", app.Installed, tt.local != nil)
			}
		})
	}
}

// A remote app the box does not have installed must never inherit another
// app's local manifest, and apps absent from the catalog must not appear.
func TestMergeOnlyReturnsCatalogApps(t *testing.T) {
	local := []Manifest{
		{AppName: "openlist", Version: "4.2.5", FpkVersion: "4.2.5-r3"},
		{AppName: "gone-from-catalog", Version: "1.0.0", FpkVersion: "1.0.0"},
	}
	remote := []source.RemoteApp{
		{AppName: "openlist", Version: "4.2.5", FpkVersion: "4.2.5-r3", ReleaseTag: "openlist/v4.2.5-r3"},
		{AppName: "never-installed", Version: "2.0.0", FpkVersion: "2.0.0", ReleaseTag: "never-installed/v2.0.0"},
	}

	got := NewRegistry().Merge(local, remote, nil)
	if len(got) != 2 {
		t.Fatalf("Merge returned %d apps, want 2 (catalog size)", len(got))
	}

	byName := make(map[string]AppInfo, len(got))
	for _, a := range got {
		byName[a.AppName] = a
	}

	if _, ok := byName["gone-from-catalog"]; ok {
		t.Error("app missing from the catalog must not be returned")
	}
	if !byName["openlist"].Installed {
		t.Error("openlist should be marked installed")
	}
	if byName["never-installed"].Installed {
		t.Error("never-installed must not be marked installed")
	}
	if byName["never-installed"].InstalledVersion != "" {
		t.Errorf("never-installed InstalledVersion = %q, want empty", byName["never-installed"].InstalledVersion)
	}
}

// ReconcileInstalled is the daemon-truth backstop: fnOS can register an app
// whose /var/apps manifest the scan misses, which used to leave the store
// offering 安装 on an installed app — the daemon then rejected it with
// "已安装，请使用更新功能" while the update tab showed nothing, dead-ending
// the user (conversun/fnos-apps#280 daidai-panel, #281 mihomo).
func TestReconcileInstalledFromDaemon(t *testing.T) {
	r := NewRegistry()
	r.Merge([]Manifest{
		{AppName: "scanned-app", Version: "1.0.0", FpkVersion: "1.0.0", Distributor: "conversun"},
	}, []source.RemoteApp{
		{AppName: "daidai-panel", Version: "3.0.10", FpkVersion: "3.0.10"},
		{AppName: "scanned-app", Version: "1.0.0", FpkVersion: "1.0.0"},
		{AppName: "fresh-app", Version: "2.0.0", FpkVersion: "2.0.0"},
	}, nil)

	// Daemon knows daidai-panel (scan missed it) and scanned-app (agrees).
	r.ReconcileInstalled(map[string]string{
		"daidai-panel": "3.0.10",
		"scanned-app":  "1.0.0",
		"system-app":   "9.9.9", // not in catalog — ignored
	})

	byName := make(map[string]AppInfo)
	for _, a := range r.List() {
		byName[a.AppName] = a
	}

	if !byName["daidai-panel"].Installed {
		t.Error("daemon-installed daidai-panel must be marked installed")
	}
	if byName["daidai-panel"].Status != AppStatusInstalledUpToDate {
		t.Errorf("daidai-panel status = %q, want installed_up_to_date (no local version to compare)", byName["daidai-panel"].Status)
	}
	if byName["daidai-panel"].InstalledVersion != "3.0.10" {
		t.Errorf("daidai-panel InstalledVersion = %q, want the daemon-reported 3.0.10", byName["daidai-panel"].InstalledVersion)
	}
	if !byName["scanned-app"].Installed || byName["scanned-app"].InstalledFpkVersion != "1.0.0" {
		t.Error("already-scanned app must keep its manifest-derived state")
	}
	if byName["fresh-app"].Installed {
		t.Error("app unknown to the daemon must stay not-installed")
	}
}
