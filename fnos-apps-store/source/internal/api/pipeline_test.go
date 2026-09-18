package api

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"fnos-store/internal/config"
	"fnos-store/internal/core"
	"fnos-store/internal/platform"
)

// stubAppCenter is a scripted platform.AppCenter for verifyInstalled tests.
// Only Check and List participate in the assertions; the other methods are
// no-ops satisfying the interface.
type stubAppCenter struct {
	checkScript []stubCheckResult
	listResult  []platform.InstalledApp
	listErr     error

	appVolIdx   int
	appVolFound bool
	appVolErr   error
	volumes     []platform.VolumeInfo

	setVolCalls    []int
	setVolErr      error
	upgradeBlocked bool
	// daemonInstallDown models the daemon's INSTALL channel being unreachable,
	// independently of upgradeBlocked — they are separate probes so a change to
	// one cannot reroute the other onto install-local.
	daemonInstallDown bool
	// curVol is the volume the stub reports from DefaultVolume(). A successful
	// SetDefaultVolume updates it; setVolIgnored models the real fnOS defect
	// where the setter exits 0 but the value never changes.
	curVol        int
	setVolIgnored bool
	getVolErr     error

	nCheck           int32
	nList            int32
	nInstallFpk      int32
	nInstallLocal    int32
	nUpgradeFpk      int32
	nInstallWizard   int32
	upgradeErr       error
	installWizardErr error
	wizard           *platform.AppWizard
	lastParams       []platform.WizardParam

	startErr     error
	statusScript []string // per-call Status values; the last entry repeats

	nStart  int32
	nStatus int32
}

type stubCheckResult struct {
	installed bool
	err       error
}

func (s *stubAppCenter) Check(appname string) (bool, error) {
	idx := int(atomic.AddInt32(&s.nCheck, 1)) - 1
	if idx >= len(s.checkScript) {
		// Script exhausted: repeat the last entry so long loops don't panic.
		idx = len(s.checkScript) - 1
	}
	r := s.checkScript[idx]
	return r.installed, r.err
}

func (s *stubAppCenter) List() ([]platform.InstalledApp, error) {
	atomic.AddInt32(&s.nList, 1)
	return s.listResult, s.listErr
}

// Status replays statusScript one entry per call (clamping to the last
// entry, like Check); an empty script keeps the legacy "", nil answer.
func (s *stubAppCenter) Status(string) (string, error) {
	idx := int(atomic.AddInt32(&s.nStatus, 1)) - 1
	if len(s.statusScript) == 0 {
		return "", nil
	}
	if idx >= len(s.statusScript) {
		idx = len(s.statusScript) - 1
	}
	return s.statusScript[idx], nil
}
func (s *stubAppCenter) InstallFpk(string, int) error {
	atomic.AddInt32(&s.nInstallFpk, 1)
	return nil
}

func (s *stubAppCenter) InstallLocal(string, int, bool) error {
	atomic.AddInt32(&s.nInstallLocal, 1)
	return nil
}
func (s *stubAppCenter) Uninstall(context.Context, string) error { return nil }
func (s *stubAppCenter) Start(string) error {
	atomic.AddInt32(&s.nStart, 1)
	return s.startErr
}
func (s *stubAppCenter) Stop(string) error { return nil }
func (s *stubAppCenter) StartConfirmed(context.Context, string) error {
	atomic.AddInt32(&s.nStart, 1)
	return s.startErr
}
func (s *stubAppCenter) StopConfirmed(context.Context, string) error { return nil }
func (s *stubAppCenter) DefaultVolume() (int, error) {
	if s.getVolErr != nil {
		return 0, s.getVolErr
	}
	if s.curVol == 0 {
		return 1, nil
	}
	return s.curVol, nil
}
func (s *stubAppCenter) ListVolumes() ([]platform.VolumeInfo, error) { return s.volumes, nil }
func (s *stubAppCenter) FetchWizard(_ context.Context, _ string) (*platform.AppWizard, error) {
	return s.wizard, nil
}

func (s *stubAppCenter) InstallFpkWithWizard(_ context.Context, _ string, _ int, params []platform.WizardParam) error {
	atomic.AddInt32(&s.nInstallWizard, 1)
	s.lastParams = params
	return s.installWizardErr
}

func (s *stubAppCenter) UpgradeFpk(_ context.Context, _ string, _ []platform.WizardParam) error {
	atomic.AddInt32(&s.nUpgradeFpk, 1)
	return s.upgradeErr
}

func (s *stubAppCenter) UpgradeCapability() platform.UpgradeCapability {
	if s.upgradeBlocked {
		return platform.UpgradeCapability{Allowed: false, PlatformVersion: "1.2.0203", Reason: "该 fnOS 版本更新会删除应用数据"}
	}
	return platform.UpgradeCapability{Allowed: true, PlatformVersion: "test"}
}

func (s *stubAppCenter) DaemonInstallAvailable() bool {
	return !s.daemonInstallDown
}

func (s *stubAppCenter) AppInstallVolume(string) (int, bool, error) {
	return s.appVolIdx, s.appVolFound, s.appVolErr
}
func (s *stubAppCenter) SetDefaultVolume(v int) error {
	s.setVolCalls = append(s.setVolCalls, v)
	if s.setVolErr != nil {
		return s.setVolErr
	}
	if !s.setVolIgnored {
		s.curVol = v
	}
	return nil
}

// TestVerifyInstalled locks in the retry + List() fallback contract for
// GitHub issue conversun/fnos-apps#181. Cases cover the happy path, the
// race-recovery path that motivates the fix, the List() fallback (accept
// only running/stopped, reject unknown), a hard CLI error that MUST NOT
// retry, and ctx cancellation.
func TestVerifyInstalled(t *testing.T) {
	const appName = "plexmediaserver"

	// Skip real sleeps but keep ctx-cancellation semantics.
	origWait := verifyWait
	verifyWait = func(ctx context.Context, _ time.Duration) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			return nil
		}
	}
	t.Cleanup(func() { verifyWait = origWait })

	cases := []struct {
		name        string
		checkScript []stubCheckResult
		list        []platform.InstalledApp
		listErr     error
		manifest    bool
		preCancel   bool
		wantErr     bool
		wantErrSub  string
		wantCtxErr  bool
		wantNCheck  int32
		wantNList   int32
	}{
		{
			name:        "happy_first_try",
			checkScript: []stubCheckResult{{installed: true}},
			wantNCheck:  1,
			wantNList:   0,
		},
		{
			name: "race_recovers_at_4",
			checkScript: []stubCheckResult{
				{installed: false}, {installed: false}, {installed: false},
				{installed: true},
			},
			wantNCheck: 4,
			wantNList:  0,
		},
		{
			name:        "extended_budget_attempts",
			checkScript: []stubCheckResult{{installed: false}},
			wantErr:     true,
			wantNCheck:  12,
			wantNList:   1,
		},
		{
			name: "list_fallback_hit_running",
			checkScript: []stubCheckResult{
				{installed: false}, {installed: false}, {installed: false}, {installed: false},
				{installed: false}, {installed: false}, {installed: false}, {installed: false},
			},
			list:       []platform.InstalledApp{{AppName: "plexmediaserver", Status: "running"}},
			wantNCheck: 12,
			wantNList:  1,
		},
		{
			name: "list_fallback_hit_stopped",
			checkScript: []stubCheckResult{
				{installed: false}, {installed: false}, {installed: false}, {installed: false},
				{installed: false}, {installed: false}, {installed: false}, {installed: false},
			},
			list:       []platform.InstalledApp{{AppName: "plexmediaserver", Status: "stopped"}},
			wantNCheck: 12,
			wantNList:  1,
		},
		{
			name:        "fs_fallback_hit_after_check_and_list_miss",
			checkScript: []stubCheckResult{{installed: false}},
			manifest:    true,
			wantNCheck:  12,
			wantNList:   1,
		},
		{
			name: "list_fallback_reject_unknown_status",
			checkScript: []stubCheckResult{
				{installed: false}, {installed: false}, {installed: false}, {installed: false},
				{installed: false}, {installed: false}, {installed: false}, {installed: false},
			},
			list:       []platform.InstalledApp{{AppName: "plexmediaserver", Status: "unknown"}},
			wantErr:    true,
			wantErrSub: "验证失败",
			wantNCheck: 12,
			wantNList:  1,
		},
		{
			name: "list_fallback_wrong_appname",
			checkScript: []stubCheckResult{
				{installed: false}, {installed: false}, {installed: false}, {installed: false},
				{installed: false}, {installed: false}, {installed: false}, {installed: false},
			},
			list:       []platform.InstalledApp{{AppName: "other-app", Status: "running"}},
			wantErr:    true,
			wantErrSub: "验证失败",
			wantNCheck: 12,
			wantNList:  1,
		},
		{
			name: "list_fallback_miss_empty",
			checkScript: []stubCheckResult{
				{installed: false}, {installed: false}, {installed: false}, {installed: false},
				{installed: false}, {installed: false}, {installed: false}, {installed: false},
			},
			list:       nil,
			wantErr:    true,
			wantErrSub: "验证失败",
			wantNCheck: 12,
			wantNList:  1,
		},
		{
			name:        "fs_fallback_miss",
			checkScript: []stubCheckResult{{installed: false}},
			wantErr:     true,
			wantErrSub:  "重试 12 次共 51.5s",
			wantNCheck:  12,
			wantNList:   1,
		},
		{
			name:        "hard_error_but_manifest_present_succeeds",
			checkScript: []stubCheckResult{{installed: false, err: errors.New("cli exit 2")}},
			manifest:    true,
			wantNCheck:  1,
			wantNList:   0,
		},
		{
			name:        "hard_error_no_manifest_fails_fast",
			checkScript: []stubCheckResult{{installed: false, err: errors.New("cli exit 2")}},
			wantErr:     true,
			wantErrSub:  "cli exit 2",
			wantNCheck:  1,
			wantNList:   0,
		},
		{
			name: "ctx_already_canceled",
			checkScript: []stubCheckResult{
				{installed: false}, {installed: false},
			},
			preCancel:  true,
			wantErr:    true,
			wantCtxErr: true,
			wantNCheck: 0,
			wantNList:  0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			appsDir := t.TempDir()
			if tc.manifest {
				appDir := filepath.Join(appsDir, appName)
				if err := os.MkdirAll(appDir, 0o755); err != nil {
					t.Fatalf("create app dir: %v", err)
				}
				if err := os.WriteFile(filepath.Join(appDir, "manifest"), []byte("appname = "+appName+"\n"), 0o600); err != nil {
					t.Fatalf("create manifest: %v", err)
				}
			}

			stub := &stubAppCenter{
				checkScript: tc.checkScript,
				listResult:  tc.list,
				listErr:     tc.listErr,
			}
			p := &installPipeline{
				queue:   NewOperationQueue(),
				ac:      stub,
				appsDir: appsDir,
			}

			ctx, cancel := context.WithCancel(context.Background())
			if tc.preCancel {
				cancel()
			} else {
				t.Cleanup(cancel)
			}

			err := p.verifyInstalled(ctx, appName)

			if tc.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.wantErrSub != "" && err != nil {
				if !strings.Contains(err.Error(), tc.wantErrSub) {
					t.Errorf("error %q does not contain %q", err.Error(), tc.wantErrSub)
				}
			}
			if tc.wantCtxErr && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
				t.Errorf("expected ctx error, got %v", err)
			}
			if got := atomic.LoadInt32(&stub.nCheck); got != tc.wantNCheck {
				t.Errorf("nCheck = %d, want %d", got, tc.wantNCheck)
			}
			if got := atomic.LoadInt32(&stub.nList); got != tc.wantNList {
				t.Errorf("nList = %d, want %d", got, tc.wantNList)
			}
		})
	}
}

// TestResolveVolumeFor locks the update-pinning contract: an update targets the
// app's CURRENT volume and fails closed when it cannot be determined, while a
// fresh install uses the default volume. This is the core guard against the
// cross-volume relocation data loss in conversun/fnos-apps#189.
func TestResolveVolumeFor(t *testing.T) {
	t.Run("update pins to the app's current volume", func(t *testing.T) {
		stub := &stubAppCenter{appVolIdx: 2, appVolFound: true}
		p := &installPipeline{queue: NewOperationQueue(), ac: stub}
		got, err := p.resolveVolumeFor("update", "emby")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != 2 {
			t.Errorf("volume = %d, want 2", got)
		}
	})

	t.Run("update fails closed when current volume is unresolvable", func(t *testing.T) {
		stub := &stubAppCenter{appVolFound: false}
		p := &installPipeline{queue: NewOperationQueue(), ac: stub}
		_, err := p.resolveVolumeFor("update", "emby")
		if err == nil {
			t.Fatalf("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "已中止更新") {
			t.Errorf("error %q missing abort reason", err.Error())
		}
	})

	t.Run("install uses the default volume, not the pin", func(t *testing.T) {
		stub := &stubAppCenter{
			appVolIdx:   2,
			appVolFound: true,
			volumes:     []platform.VolumeInfo{{Index: 1, Path: "/vol1"}},
		}
		p := &installPipeline{queue: NewOperationQueue(), ac: stub}
		got, err := p.resolveVolumeFor("install", "emby")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != 1 {
			t.Errorf("volume = %d, want 1 (DefaultVolume)", got)
		}
	})

	// fnOS 1.2.0203 returns a default volume of 0 from the CLI even though its
	// own database holds a valid index, and vol0 does not exist. Installing
	// onto it always fails, so surface the one lever the user can actually
	// pull instead of a generic downstream error.
	t.Run("install rejects a default volume that is not mounted", func(t *testing.T) {
		stub := &stubAppCenter{
			curVol:  9,
			volumes: []platform.VolumeInfo{{Index: 1, Path: "/vol1"}, {Index: 2, Path: "/vol2"}},
		}
		p := &installPipeline{queue: NewOperationQueue(), ac: stub}
		_, err := p.resolveVolumeFor("install", "emby")
		if err == nil {
			t.Fatal("expected error for an unmounted default volume, got nil")
		}
		if !strings.Contains(err.Error(), "设置") {
			t.Errorf("error %q should point the user at settings", err.Error())
		}
	})
}

// TestPreflightInstall locks the fail-closed guard that runs BEFORE the
// destructive install-local step: a missing or full target volume aborts the
// operation so the app is never left uninstalled with orphaned data.
func TestPreflightInstall(t *testing.T) {
	dir := t.TempDir()
	fpk := filepath.Join(dir, "app.fpk")
	if err := os.WriteFile(fpk, make([]byte, 1000), 0o600); err != nil {
		t.Fatalf("write fpk: %v", err)
	}

	t.Run("passes when target volume is mounted with ample space", func(t *testing.T) {
		stub := &stubAppCenter{volumes: []platform.VolumeInfo{{Index: 1, Path: "/vol1", FreeBytes: 1 << 40}}}
		p := &installPipeline{queue: NewOperationQueue(), ac: stub}
		if err := p.preflightInstall(1, fpk); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("aborts when target volume is not mounted", func(t *testing.T) {
		stub := &stubAppCenter{volumes: []platform.VolumeInfo{{Index: 1, Path: "/vol1", FreeBytes: 1 << 40}}}
		p := &installPipeline{queue: NewOperationQueue(), ac: stub}
		err := p.preflightInstall(9, fpk)
		if err == nil {
			t.Fatalf("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "不可用") {
			t.Errorf("error %q missing unavailable reason", err.Error())
		}
	})

	t.Run("aborts when target volume lacks free space", func(t *testing.T) {
		stub := &stubAppCenter{volumes: []platform.VolumeInfo{{Index: 1, Path: "/vol1", FreeBytes: 100}}}
		p := &installPipeline{queue: NewOperationQueue(), ac: stub}
		err := p.preflightInstall(1, fpk)
		if err == nil {
			t.Fatalf("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "空间不足") {
			t.Errorf("error %q missing space reason", err.Error())
		}
	})
}

// TestPrecheckServicePort locks the install-time port probe behind
// conversun/fnos-apps#295: an install whose published port is taken must fail
// BEFORE any download, naming the port, with the wizard's answer outranking
// the catalog default.
func TestPrecheckServicePort(t *testing.T) {
	p := &installPipeline{queue: NewOperationQueue(), ac: &stubAppCenter{}}

	t.Run("passes when the catalog port is free", func(t *testing.T) {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("reserve port: %v", err)
		}
		freePort := ln.Addr().(*net.TCPAddr).Port
		ln.Close()
		app := core.AppInfo{AppName: "paperless-ngx", ServicePort: freePort}
		if err := p.precheckServicePort(app, nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("fails naming the port when the catalog port is taken", func(t *testing.T) {
		ln, err := net.Listen("tcp", ":0")
		if err != nil {
			t.Fatalf("bind: %v", err)
		}
		defer ln.Close()
		busy := ln.Addr().(*net.TCPAddr).Port
		app := core.AppInfo{AppName: "paperless-ngx", ServicePort: busy}
		err = p.precheckServicePort(app, nil)
		if err == nil {
			t.Fatalf("expected error for busy port %d, got nil", busy)
		}
		if !strings.Contains(err.Error(), fmt.Sprintf("%d", busy)) {
			t.Errorf("error %q does not name the busy port", err.Error())
		}
	})

	t.Run("wizard answer outranks the catalog default", func(t *testing.T) {
		ln, err := net.Listen("tcp", ":0")
		if err != nil {
			t.Fatalf("bind: %v", err)
		}
		defer ln.Close()
		busy := ln.Addr().(*net.TCPAddr).Port

		ln2, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("reserve: %v", err)
		}
		freePort := ln2.Addr().(*net.TCPAddr).Port
		ln2.Close()

		app := core.AppInfo{AppName: "paperless-ngx", ServicePort: freePort}
		params := []platform.WizardParam{{Key: "wizard_port", Value: fmt.Sprintf("%d", busy)}}
		if err := p.precheckServicePort(app, params); err == nil {
			t.Fatalf("expected error for busy wizard port %d, got nil", busy)
		}
	})

	t.Run("skips the probe when no port is known", func(t *testing.T) {
		if err := p.precheckServicePort(core.AppInfo{AppName: "x"}, nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

// TestSetDefaultVolume locks that the update volume pin drives the documented
// default-volume lever and surfaces CLI failures so the caller can fail closed
// before the destructive install-local (conversun/fnos-apps#189).
func TestSetDefaultVolume(t *testing.T) {
	t.Run("propagates the target volume to the CLI", func(t *testing.T) {
		stub := &stubAppCenter{}
		p := &installPipeline{queue: NewOperationQueue(), ac: stub}
		if err := p.setDefaultVolume(3); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(stub.setVolCalls) != 1 || stub.setVolCalls[0] != 3 {
			t.Fatalf("SetDefaultVolume calls = %v, want [3]", stub.setVolCalls)
		}
	})

	t.Run("surfaces a CLI failure", func(t *testing.T) {
		stub := &stubAppCenter{setVolErr: errors.New("cli boom")}
		p := &installPipeline{queue: NewOperationQueue(), ac: stub}
		if err := p.setDefaultVolume(1); err == nil {
			t.Fatal("expected error, got nil")
		}
	})

	// THE regression that motivated the read-back. On fnOS 1.2.0203 the real
	// `appcenter-cli default-volume <n>` exits 0, prints the OLD value, and
	// changes nothing. Trusting the setter's nil error let an update walk into
	// install-local's uninstall step with an unpinned (invalid) volume, so the
	// reinstall failed and the app was destroyed (conversun/fnos-apps#189).
	t.Run("fails closed when the setter is silently ignored", func(t *testing.T) {
		stub := &stubAppCenter{curVol: 0, setVolIgnored: true}
		p := &installPipeline{queue: NewOperationQueue(), ac: stub}
		err := p.setDefaultVolume(2)
		if err == nil {
			t.Fatal("expected error when default-volume set is a no-op, got nil")
		}
		if !strings.Contains(err.Error(), "未生效") {
			t.Errorf("error %q should report the pin did not take effect", err.Error())
		}
	})

	// Single-volume systems get NO exemption here. Relocation is impossible
	// with one volume, but the pin verification is not what protects them —
	// on fnOS 1.2.0203 the update destroys the app regardless (error 10237),
	// so the refusal now lives in requireSafeUpgrade(). Skipping verification
	// here would only have let those users reach the destroyer sooner.
	t.Run("single volume does not exempt the pin verification", func(t *testing.T) {
		stub := &stubAppCenter{
			curVol:        -1, // broken getter, as measured on fnOS 1.2.0203
			setVolIgnored: true,
			volumes:       []platform.VolumeInfo{{Index: 1, Path: "/vol1"}},
		}
		p := &installPipeline{queue: NewOperationQueue(), ac: stub}
		if err := p.setDefaultVolume(1); err == nil {
			t.Fatal("expected the pin verification to still fail closed")
		}
	})

	t.Run("fails closed when the value cannot be read back", func(t *testing.T) {
		stub := &stubAppCenter{getVolErr: errors.New("read boom")}
		p := &installPipeline{queue: NewOperationQueue(), ac: stub}
		if err := p.setDefaultVolume(2); err == nil {
			t.Fatal("expected error when read-back fails, got nil")
		}
	})
}

// TestResolveVolume locks the fresh-install volume choice on a system whose
// fnOS default-volume getter is broken. Measured on fnOS 1.2.0203: the getter
// returns 0 while the daemon's own DB holds 1, and vol0 does not exist.
//
// Dead-ending the user there is bad UX — the Settings default reads "系统默认",
// which looks correct and gives no hint it is the thing failing. valfar7 hit
// exactly that on 1.7.14 (conversun/fnos-apps#189). When only one volume is
// mounted there is no ambiguity, so fall back to it instead of demanding a
// choice the user cannot know they must make.
func TestResolveVolume(t *testing.T) {
	t.Run("uses the configured volume when set", func(t *testing.T) {
		stub := &stubAppCenter{curVol: 9, volumes: []platform.VolumeInfo{{Index: 1, Path: "/vol1"}}}
		p := &installPipeline{
			queue:     NewOperationQueue(),
			ac:        stub,
			configMgr: config.NewManager(t.TempDir()),
		}
		cfg, err := p.configMgr.LoadConfig()
		if err != nil {
			t.Fatal(err)
		}
		cfg.InstallVolume = 2
		if err := p.configMgr.SaveConfig(cfg); err != nil {
			t.Fatal(err)
		}
		got, err := p.resolveVolume()
		if err != nil || got != 2 {
			t.Fatalf("resolveVolume() = (%d, %v), want (2, nil)", got, err)
		}
	})

	t.Run("accepts a usable fnOS default", func(t *testing.T) {
		stub := &stubAppCenter{
			curVol:  2,
			volumes: []platform.VolumeInfo{{Index: 1, Path: "/vol1"}, {Index: 2, Path: "/vol2"}},
		}
		p := &installPipeline{queue: NewOperationQueue(), ac: stub}
		got, err := p.resolveVolume()
		if err != nil || got != 2 {
			t.Fatalf("resolveVolume() = (%d, %v), want (2, nil)", got, err)
		}
	})

	// THE regression: getter says vol0, only /vol1 exists.
	t.Run("falls back to the only mounted volume when the default is unusable", func(t *testing.T) {
		stub := &stubAppCenter{curVol: -1, volumes: []platform.VolumeInfo{{Index: 1, Path: "/vol1"}}}
		p := &installPipeline{queue: NewOperationQueue(), ac: stub}
		got, err := p.resolveVolume()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != 1 {
			t.Errorf("volume = %d, want 1 (the only mounted volume)", got)
		}
	})

	// With several volumes there is no safe guess — ask, and say where to look.
	t.Run("asks the user when several volumes exist and the default is unusable", func(t *testing.T) {
		stub := &stubAppCenter{
			curVol:  -1,
			volumes: []platform.VolumeInfo{{Index: 1, Path: "/vol1"}, {Index: 2, Path: "/vol2"}},
		}
		p := &installPipeline{queue: NewOperationQueue(), ac: stub}
		_, err := p.resolveVolume()
		if err == nil {
			t.Fatal("expected an error when the choice is ambiguous, got nil")
		}
		if !strings.Contains(err.Error(), "应用安装位置") {
			t.Errorf("error %q should name the settings field to change", err.Error())
		}
	})

	t.Run("falls back when the getter itself fails", func(t *testing.T) {
		stub := &stubAppCenter{
			getVolErr: errors.New("cli boom"),
			volumes:   []platform.VolumeInfo{{Index: 1, Path: "/vol1"}},
		}
		p := &installPipeline{queue: NewOperationQueue(), ac: stub}
		got, err := p.resolveVolume()
		if err != nil || got != 1 {
			t.Fatalf("resolveVolume() = (%d, %v), want (1, nil)", got, err)
		}
	})
}

// TestVerifyPayloadLanded locks the filesystem proof that an install/update
// actually produced files at the shipped version. The appcenter control plane
// cannot be trusted for this: `check` returned "Installed" for an app whose
// directory had been deleted, and an update reported 操作完成 while the on-disk
// manifest still held the OLD version (conversun/fnos-apps#189).
func TestVerifyPayloadLanded(t *testing.T) {
	const appName = "filebrowser"

	// newApp lays out the /var/apps/<app> shape the checker reads: a manifest
	// plus a target symlink into the "volume" directory holding the payload.
	// newAppRev lays out an app whose manifest carries BOTH version and
	// fpk_version, the shape a repackaged (-rN) build actually ships.
	newAppRev := func(t *testing.T, version, fpkVersion string) string {
		t.Helper()
		root := t.TempDir()
		appsDir := filepath.Join(root, "apps")
		volDir := filepath.Join(root, "vol1", "@appcenter", appName)
		if err := os.MkdirAll(filepath.Join(appsDir, appName), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(volDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(volDir, appName), []byte("binary"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(volDir, filepath.Join(appsDir, appName, "target")); err != nil {
			t.Fatal(err)
		}
		manifest := "appname         = " + appName + "\nversion         = " + version + "\n"
		if fpkVersion != "" {
			manifest += "fpk_version     = " + fpkVersion + "\n"
		}
		if err := os.WriteFile(filepath.Join(appsDir, appName, "manifest"), []byte(manifest), 0o644); err != nil {
			t.Fatal(err)
		}
		return appsDir
	}

	newApp := func(t *testing.T, version string, payload bool) (appsDir string, volDir string) {
		t.Helper()
		root := t.TempDir()
		appsDir = filepath.Join(root, "apps")
		volDir = filepath.Join(root, "vol1", "@appcenter", appName)
		if err := os.MkdirAll(filepath.Join(appsDir, appName), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(volDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if payload {
			if err := os.WriteFile(filepath.Join(volDir, appName), []byte("binary"), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.Symlink(volDir, filepath.Join(appsDir, appName, "target")); err != nil {
			t.Fatal(err)
		}
		manifest := "appname         = " + appName + "\nversion         = " + version + "\n"
		if err := os.WriteFile(filepath.Join(appsDir, appName, "manifest"), []byte(manifest), 0o644); err != nil {
			t.Fatal(err)
		}
		return appsDir, volDir
	}

	t.Run("accepts a real install at the shipped version", func(t *testing.T) {
		appsDir, _ := newApp(t, "2.63.19", true)
		p := &installPipeline{queue: NewOperationQueue(), ac: &stubAppCenter{}, appsDir: appsDir}
		if err := p.verifyPayloadLanded(appName, 0, "2.63.19"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	// The exact false-success observed on the VM: install-local exited 0, the
	// control plane said Installed, but nothing was replaced.
	t.Run("rejects an update whose version never changed", func(t *testing.T) {
		appsDir, _ := newApp(t, "2.63.18", true)
		p := &installPipeline{queue: NewOperationQueue(), ac: &stubAppCenter{}, appsDir: appsDir}
		err := p.verifyPayloadLanded(appName, 0, "2.63.19")
		if err == nil {
			t.Fatal("expected error when the version did not change, got nil")
		}
		if !strings.Contains(err.Error(), "2.63.19") {
			t.Errorf("error %q should name the expected version", err.Error())
		}
	})

	// The store itself was found in this state: process alive, directory gone.
	t.Run("rejects an empty install directory", func(t *testing.T) {
		appsDir, _ := newApp(t, "2.63.19", false)
		p := &installPipeline{queue: NewOperationQueue(), ac: &stubAppCenter{}, appsDir: appsDir}
		err := p.verifyPayloadLanded(appName, 0, "2.63.19")
		if err == nil {
			t.Fatal("expected error for an empty install dir, got nil")
		}
		if !strings.Contains(err.Error(), "为空") {
			t.Errorf("error %q should report the empty payload", err.Error())
		}
	})

	t.Run("rejects a missing install directory", func(t *testing.T) {
		appsDir, volDir := newApp(t, "2.63.19", true)
		if err := os.RemoveAll(volDir); err != nil {
			t.Fatal(err)
		}
		p := &installPipeline{queue: NewOperationQueue(), ac: &stubAppCenter{}, appsDir: appsDir}
		if err := p.verifyPayloadLanded(appName, 0, "2.63.19"); err == nil {
			t.Fatal("expected error for a missing install dir, got nil")
		}
	})

	// A repackaged build ships fpk_version=1.9.3-r2 while the manifest's own
	// version stays 1.9.3. runStandard passes app.FpkVersion as wantVersion, so
	// comparing it against manifest `version` would reject a PERFECTLY GOOD
	// install. 29 of the 145 catalogued apps carry a -rN suffix (1panel, alist,
	// gitea, gopeed, embyserver ...), so this is a fifth of the catalog.
	t.Run("accepts a revision package by fpk_version", func(t *testing.T) {
		appsDir := newAppRev(t, "1.9.3", "1.9.3-r2")
		p := &installPipeline{queue: NewOperationQueue(), ac: &stubAppCenter{}, appsDir: appsDir}
		if err := p.verifyPayloadLanded(appName, 0, "1.9.3-r2"); err != nil {
			t.Fatalf("revision package must be accepted, got: %v", err)
		}
	})

	t.Run("still rejects a revision package that did not upgrade", func(t *testing.T) {
		appsDir := newAppRev(t, "1.9.3", "1.9.3-r1")
		p := &installPipeline{queue: NewOperationQueue(), ac: &stubAppCenter{}, appsDir: appsDir}
		err := p.verifyPayloadLanded(appName, 0, "1.9.3-r2")
		if err == nil {
			t.Fatal("expected error when fpk_version did not change, got nil")
		}
		if !strings.Contains(err.Error(), "1.9.3-r2") {
			t.Errorf("error %q should name the expected version", err.Error())
		}
	})

	t.Run("falls back to version when no fpk_version is shipped", func(t *testing.T) {
		appsDir := newAppRev(t, "2.63.19", "")
		p := &installPipeline{queue: NewOperationQueue(), ac: &stubAppCenter{}, appsDir: appsDir}
		if err := p.verifyPayloadLanded(appName, 0, "2.63.19"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	// Relocation is the data-orphaning half of #189: the app lands on a volume
	// other than the one we pinned, leaving its data behind on the old one.
	t.Run("rejects a payload that landed on the wrong volume", func(t *testing.T) {
		appsDir, volDir := newApp(t, "2.63.19", true)
		// Resolve first: on macOS t.TempDir() sits under a /var -> /private/var
		// symlink, and verifyPayloadLanded compares the RESOLVED payload path.
		resolvedVol, err := filepath.EvalSymlinks(volDir)
		if err != nil {
			t.Fatal(err)
		}
		volRoot := filepath.Dir(filepath.Dir(resolvedVol)) // .../vol1
		stub := &stubAppCenter{volumes: []platform.VolumeInfo{{Index: 2, Path: volRoot}}}
		p := &installPipeline{queue: NewOperationQueue(), ac: stub, appsDir: appsDir}
		err = p.verifyPayloadLanded(appName, 1, "2.63.19")
		if err == nil {
			t.Fatal("expected error when the app landed on another volume, got nil")
		}
		if !strings.Contains(err.Error(), "vol2") {
			t.Errorf("error %q should name the actual volume", err.Error())
		}
	})
}

// TestUpgradeGuardBlocksDestructivePath locks the refusal added after a user
// lost apps on fnOS 1.2.0203 (conversun/fnos-apps#189).
//
// fnOS has no data-preserving upgrade command: `install-fpk` refuses an
// already-installed app outright, and `install-local` implements an upgrade as
// uninstall-then-reinstall whose reinstall fails with error 10237 on that
// build. Reproduced twice on a live box — gopeed went from running to gone,
// program directory AND @appdata deleted.
//
// The guard therefore has to fire BEFORE anything is downloaded or installed,
// because nothing downstream can undo the uninstall.
func TestUpgradeGuardBlocksDestructivePath(t *testing.T) {
	newPipeline := func(blocked bool) (*installPipeline, *stubAppCenter) {
		stub := &stubAppCenter{
			upgradeBlocked: blocked,
			appVolIdx:      1,
			appVolFound:    true,
			volumes:        []platform.VolumeInfo{{Index: 1, Path: "/vol1"}},
			checkScript:    []stubCheckResult{{installed: true}},
		}
		return &installPipeline{queue: NewOperationQueue(), ac: stub}, stub
	}

	t.Run("update is refused before any destructive call", func(t *testing.T) {
		p, stub := newPipeline(true)
		if err := p.requireSafeUpgrade(); err == nil {
			t.Fatal("expected the update to be refused on an unsafe build")
		}
		if n := atomic.LoadInt32(&stub.nInstallLocal); n != 0 {
			t.Errorf("InstallLocal called %d times; must never run when blocked", n)
		}
		if n := atomic.LoadInt32(&stub.nInstallFpk); n != 0 {
			t.Errorf("InstallFpk called %d times; must never run when blocked", n)
		}
	})

	t.Run("refusal explains the manual route", func(t *testing.T) {
		p, _ := newPipeline(true)
		err := p.requireSafeUpgrade()
		if err == nil {
			t.Fatal("expected an error")
		}
		if !strings.Contains(err.Error(), "删除") {
			t.Errorf("error %q should explain the data-loss risk", err.Error())
		}
	})

	// Fresh installs stay available: there is no existing app to destroy, and
	// install-fpk on a not-installed app works fine on the affected build.
	t.Run("safe builds are unaffected", func(t *testing.T) {
		p, _ := newPipeline(false)
		if err := p.requireSafeUpgrade(); err != nil {
			t.Fatalf("update must proceed on a safe build, got: %v", err)
		}
	})
}

// TestUpdateUsesDaemonUpgradeNotInstallLocal locks the routing that keeps an
// update from destroying the app.
//
// fnOS has two upgrade mechanisms with opposite outcomes: the daemon's RPC
// upgrade preserves @appdata and can roll back, while install-local is
// uninstall-then-reinstall and on fnOS 1.2.0203 always loses the app. So an
// update must reach UpgradeFpk, and a FAILED update must NOT retry through
// InstallFpk — falling back would turn a clean failure into data loss
// (conversun/fnos-apps#189).
func TestUpdateUsesDaemonUpgradeNotInstallLocal(t *testing.T) {
	t.Run("update routes to UpgradeFpk", func(t *testing.T) {
		stub := &stubAppCenter{}
		p := &installPipeline{queue: NewOperationQueue(), ac: stub}
		if err := p.upgradeFpk(context.Background(), "/tmp/x.fpk"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if n := atomic.LoadInt32(&stub.nUpgradeFpk); n != 1 {
			t.Errorf("UpgradeFpk called %d times, want 1", n)
		}
		if n := atomic.LoadInt32(&stub.nInstallFpk); n != 0 {
			t.Errorf("InstallFpk called %d times; the destructive path must not run for updates", n)
		}
	})

	t.Run("a failed upgrade does not fall back to install-local", func(t *testing.T) {
		stub := &stubAppCenter{upgradeErr: errSimulatedUpgrade}
		p := &installPipeline{queue: NewOperationQueue(), ac: stub}
		if err := p.upgradeFpk(context.Background(), "/tmp/x.fpk"); err == nil {
			t.Fatal("expected the upgrade error to surface")
		}
		if n := atomic.LoadInt32(&stub.nInstallFpk); n != 0 {
			t.Errorf("InstallFpk called %d times after a failed upgrade; that would destroy the app", n)
		}
		if n := atomic.LoadInt32(&stub.nInstallLocal); n != 0 {
			t.Errorf("InstallLocal called %d times after a failed upgrade", n)
		}
	})
}

// TestChooseInstallRoute locks the channel every operation installs/upgrades
// through: updates always take the daemon's data-preserving upgrade, fresh
// installs take the daemon's install channel when reachable (avoiding
// install-local's code-10237 chown failures, #227/#228), and only a box whose
// daemon is unreachable falls back to install-local for a fresh install.
func TestChooseInstallRoute(t *testing.T) {
	cases := []struct {
		name     string
		opName   string
		daemonUp bool
		want     installRoute
	}{
		{"update uses the daemon upgrade even when the daemon is down", "update", false, routeDaemonUpgrade},
		{"update uses the daemon upgrade when the daemon is up", "update", true, routeDaemonUpgrade},
		{"fresh install uses the daemon install when the daemon is up", "install", true, routeDaemonInstall},
		{"fresh install falls back to install-local only when the daemon is down", "install", false, routeInstallLocal},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := chooseInstallRoute(c.opName, c.daemonUp); got != c.want {
				t.Errorf("chooseInstallRoute(%q, %v) = %v, want %v", c.opName, c.daemonUp, got, c.want)
			}
		})
	}
}

var errSimulatedUpgrade = errors.New("simulated upgrade failure")

// TestDockerPullCandidates locks the per-image fallback chain behind the fix
// for conversun/fnos-apps#267, #266, #257, #248: the selected mirror's shape
// comes first, every other real mirror follows with the ref re-prefixed by
// ITS OWN multi-registry capability (strip the old prefix, apply the new one),
// and the bare direct ref closes the list.
func TestDockerPullCandidates(t *testing.T) {
	t.Run("daocloud multi-registry ref fans out across every mirror", func(t *testing.T) {
		cfg := config.Config{DockerMirror: "daocloud"}
		got := dockerPullCandidates("m.daocloud.io/docker.io/xream/sub-store:2.36.35", cfg)
		want := []string{
			"m.daocloud.io/docker.io/xream/sub-store:2.36.35", // selected, multi-registry: unchanged
			"docker.1ms.run/xream/sub-store:2.36.35",          // single-registry mirrors strip docker.io/
			"docker.m.daocloud.io/xream/sub-store:2.36.35",
			"hub.rat.dev/xream/sub-store:2.36.35",
			"docker.1panel.live/xream/sub-store:2.36.35",
			"dockerproxy.net/xream/sub-store:2.36.35",
			"registry.cyou/xream/sub-store:2.36.35",
			"127.0.0.1:5443/xream/sub-store:2.36.35", // local KSpeeder rewrite slots in before direct
			"docker.io/xream/sub-store:2.36.35",      // direct, always last
		}
		if len(got) != len(want) {
			t.Fatalf("len = %d, want %d: %v", len(got), len(want), got)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("candidates[%d] = %q, want %q", i, got[i], want[i])
			}
		}
	})

	t.Run("single-registry selection starts with its own normalized shape", func(t *testing.T) {
		cfg := config.Config{DockerMirror: "docker-1ms"}
		got := dockerPullCandidates("docker.1ms.run/docker.io/xream/sub-store:2.36.35", cfg)
		if len(got) == 0 {
			t.Fatal("no candidates")
		}
		if got[0] != "docker.1ms.run/xream/sub-store:2.36.35" {
			t.Errorf("first candidate = %q, want the selected mirror's normalized ref", got[0])
		}
		if got[1] != "m.daocloud.io/docker.io/xream/sub-store:2.36.35" {
			t.Errorf("second candidate = %q, want daocloud's multi-registry shape", got[1])
		}
		if got[len(got)-1] != "docker.io/xream/sub-store:2.36.35" {
			t.Errorf("last candidate = %q, want the direct ref", got[len(got)-1])
		}
		seen := map[string]bool{}
		for _, c := range got {
			if seen[c] {
				t.Errorf("duplicate candidate %q in %v", c, got)
			}
			seen[c] = true
		}
	})

	t.Run("ghcr ref reaches the NJU rewrite mirror before direct", func(t *testing.T) {
		cfg := config.Config{DockerMirror: "daocloud"}
		got := dockerPullCandidates("m.daocloud.io/ghcr.io/paperless-ngx/paperless-ngx:3.1.1", cfg)
		if len(got) < 3 {
			t.Fatalf("len = %d, want at least 3: %v", len(got), got)
		}
		if got[0] != "m.daocloud.io/ghcr.io/paperless-ngx/paperless-ngx:3.1.1" {
			t.Errorf("first candidate = %q, want the selected mirror's shape", got[0])
		}
		// Single-registry mirrors cannot proxy ghcr and collapse onto the
		// direct ref, so NJU's rewrite is the only real second source.
		if got[1] != "ghcr.nju.edu.cn/paperless-ngx/paperless-ngx:3.1.1" {
			t.Errorf("second candidate = %q, want the NJU rewrite shape", got[1])
		}
		if got[len(got)-1] != "ghcr.io/paperless-ngx/paperless-ngx:3.1.1" {
			t.Errorf("last candidate = %q, want the direct ghcr ref", got[len(got)-1])
		}
	})

	t.Run("NJU selection leads ghcr with its rewrite and docker.io with mirror chain", func(t *testing.T) {
		cfg := config.Config{DockerMirror: "nju-ghcr"}
		ghcr := dockerPullCandidates("ghcr.io/paperless-ngx/paperless-ngx:3.1.1", cfg)
		if len(ghcr) == 0 || ghcr[0] != "ghcr.nju.edu.cn/paperless-ngx/paperless-ngx:3.1.1" {
			t.Errorf("ghcr first candidate = %v, want the NJU rewrite shape", ghcr)
		}
		// NJU 无 URL（host 改写型）不进前缀链：docker.io 镜像走真实镜像
		// 回退链（声明序，无监测钩子时），直连 docker.io 沉底兜底。
		dockerio := dockerPullCandidates("docker.io/xream/sub-store:2.36.35", cfg)
		if len(dockerio) == 0 || dockerio[0] != "m.daocloud.io/docker.io/xream/sub-store:2.36.35" {
			t.Errorf("docker.io first candidate = %v, want the first real mirror (NJU serves no docker.io)", dockerio)
		}
		if dockerio[len(dockerio)-1] != "docker.io/xream/sub-store:2.36.35" {
			t.Errorf("docker.io last candidate = %v, want the direct ref", dockerio)
		}
		for _, c := range dockerio {
			if c == "ghcr.nju.edu.cn/docker.io/xream/sub-store:2.36.35" {
				t.Errorf("candidate %q misapplies the ghcr rewrite to a docker.io ref", c)
			}
		}
	})

	t.Run("KSpeeder selection leads with its local registry ref", func(t *testing.T) {
		cfg := config.Config{DockerMirror: "kspeeder"}
		got := dockerPullCandidates("docker.io/busybox:latest", cfg)
		if len(got) == 0 || got[0] != "127.0.0.1:5443/library/busybox:latest" {
			t.Errorf("first candidate = %v, want the kspeeder local ref", got)
		}
		if got[len(got)-1] != "docker.io/busybox:latest" {
			t.Errorf("last candidate = %v, want the direct ref", got)
		}
		// ghcr 镜像：kspeeder 无改写，链首应为真实镜像
		ghcr := dockerPullCandidates("ghcr.io/x/y:1", cfg)
		if len(ghcr) == 0 || strings.HasPrefix(ghcr[0], "127.0.0.1:5443/") {
			t.Errorf("ghcr first candidate = %v, kspeeder must not handle ghcr", ghcr)
		}
	})

	t.Run("auto mode leads with healthy kspeeder, skips degraded", func(t *testing.T) {
		healthy := config.Config{DockerMirror: "auto"}
		got := dockerPullCandidates("docker.io/busybox:latest", healthy)
		if len(got) == 0 || got[0] != "127.0.0.1:5443/library/busybox:latest" {
			t.Errorf("auto healthy: first candidate = %v, want kspeeder local ref first", got)
		}

		config.RegisterDockerSmart(config.DockerSmart{
			Rank:     func(cfg config.Config) []string { return nil },
			Best:     func(cfg config.Config) string { return "m.daocloud.io/" },
			Degraded: func(key string) bool { return key == "kspeeder" },
		})
		defer config.RegisterDockerSmart(config.DockerSmart{})

		got = dockerPullCandidates("docker.io/busybox:latest", healthy)
		for _, c := range got {
			if strings.HasPrefix(c, "127.0.0.1:5443/") {
				t.Fatalf("auto degraded: kspeeder ref must be excluded from chain: %v", got)
			}
		}
	})

	t.Run("kspeeder-prefixed compose ref strips back to docker.io canonical", func(t *testing.T) {
		cfg := config.Config{DockerMirror: "kspeeder"}
		cases := map[string]string{
			"127.0.0.1:5443/library/busybox:latest":       "docker.io/busybox:latest",
			"127.0.0.1:5443/linuxserver/radarr:10.8.3":    "docker.io/linuxserver/radarr:10.8.3",
			// ${DOCKER_MIRROR}docker.io/x/y 模板形态：rest 已是完整 canonical，
			// 不能再补 docker.io/（双重化会让全链失效）
			"127.0.0.1:5443/docker.io/wisdomsky/cloudflared-web:2026.9.1": "docker.io/wisdomsky/cloudflared-web:2026.9.1",
		}
		for in, want := range cases {
			if got := stripDockerMirrorPrefix(in, cfg); got != want {
				t.Errorf("stripDockerMirrorPrefix(%q) = %q, want %q", in, got, want)
			}
		}
	})

	t.Run("kspeeder template-shaped compose ref yields clean chain", func(t *testing.T) {
		// auto + kspeeder 最稳：${DOCKER_MIRROR} 模板替换后的 ref 走完整链
		cfg := config.Config{DockerMirror: "auto"}
		got := dockerPullCandidates("127.0.0.1:5443/docker.io/wisdomsky/cloudflared-web:2026.9.1", cfg)
		if len(got) == 0 || got[0] != "127.0.0.1:5443/wisdomsky/cloudflared-web:2026.9.1" {
			t.Errorf("first candidate = %v, want the clean kspeeder ref", got)
		}
		for _, c := range got {
			if strings.Contains(c, "docker.io/docker.io/") {
				t.Fatalf("doubled docker.io leaked into chain: %v", got)
			}
		}
		if got[len(got)-1] != "docker.io/wisdomsky/cloudflared-web:2026.9.1" {
			t.Errorf("last candidate = %v, want the clean direct ref", got)
		}
	})
}

// TestPullRetryPredicate locks which pull failures advance to the next mirror
// candidate and which abort the whole chain: registry denials (allowlist,
// access denied) are per-mirror and must fall through, while local fatal
// conditions (cancellation, disk full, OOM kill) make every further attempt
// pointless.
func TestPullRetryPredicate(t *testing.T) {
	cases := []struct {
		name      string
		output    string
		err       error
		wantAbort bool
	}{
		{"allowlist denial continues to the next mirror", "denied: 这镜像不在白名单. this image is not in the allowlist.", nil, false},
		{"pull access denied continues to the next mirror", "Error response from daemon: pull access denied for m.daocloud.io/docker.io/xream/sub-store", nil, false},
		{"context canceled aborts the chain", "context canceled", nil, true},
		{"disk full aborts the chain", "write /var/lib/docker/tmp: no space left on device", nil, true},
		{"oom kill aborts the chain", "signal: killed", nil, true},
		{"canceled ctx error aborts the chain", "", context.Canceled, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isPullAbortError(tc.output, tc.err); got != tc.wantAbort {
				t.Errorf("isPullAbortError(%q, %v) = %v, want %v", tc.output, tc.err, got, tc.wantAbort)
			}
		})
	}
}

// TestStartFailureRecovery locks the transient-start recovery contract for
// conversun/fnos-apps#264, #260, #258, #253, #251 and #246. On a busy daemon
// `appcenter-cli start` returns a transient "code 10500" envelope while the
// app is still coming up — the attached logs show the apps listening seconds
// after the store declared the install failed. The start step must therefore
// give a serviced app a bounded window to prove itself (CLI status OR a TCP
// dial on its service port) instead of failing the install — while:
//   - a genuinely dead app still fails, after the window, with the ORIGINAL error;
//   - a portless app keeps the 1.8.3 tolerance (#226) and never polls;
//   - a non-envelope (exec-level) error fails immediately, never swallowed.
func TestStartFailureRecovery(t *testing.T) {
	const (
		appName = "msf"
		port    = 7777
	)

	// Mirrors the real LinuxAppCenter.run wrapping (appcenter_linux.go:41).
	cliErr := fmt.Errorf("appcenter-cli start %s: %w: Something wrong with appcenter: code 10500", appName, platform.ErrCLIFailure)
	errDial := errors.New("dial tcp 127.0.0.1: connect: connection refused")

	cases := []struct {
		name         string
		startErr     error
		servicePort  int
		statusScript []string
		dialOK       bool

		wantOK       bool   // startAndConfirm lets the install proceed
		wantErrEvent bool   // the SSE stream carries step=error
		wantBodySub  string // substring the SSE body must contain
		wantNStart   int32
		wantNStatus  int32
		wantNDial    int32
	}{
		{
			name:         "transient_10500_recovers",
			startErr:     cliErr,
			servicePort:  port,
			statusScript: []string{"starting", "running"}, // running on the 2nd poll
			dialOK:       false,                           // the port never answers; status alone proves it
			wantOK:       true,
			wantBodySub:  "已确认应用实际在运行",
			wantNStart:   1,
			wantNStatus:  2,
			wantNDial:    1,
		},
		{
			name:         "port_listen_recovers",
			startErr:     cliErr,
			servicePort:  port,
			statusScript: []string{"starting"}, // the control plane never catches up
			dialOK:       true,                 // but the service port accepts a connection
			wantOK:       true,
			wantBodySub:  "已确认应用实际在运行",
			wantNStart:   1,
			wantNStatus:  1,
			wantNDial:    1,
		},
		{
			name:         "dead_app_still_fails",
			startErr:     cliErr,
			servicePort:  port,
			statusScript: []string{"starting"},
			dialOK:       false,
			wantOK:       false,
			wantErrEvent: true,
			wantBodySub:  "code 10500", // the ORIGINAL start error, not a timeout invention
			wantNStart:   1,
			// Probes at t=0,2,...,30 within the 30s window: bounded, then fails.
			wantNStatus: 16,
			wantNDial:   16,
		},
		{
			name:        "serviceportless_skips_poll",
			startErr:    cliErr,
			servicePort: 0, // #226 tolerance from 1.8.3: a note, no polling at all
			wantOK:      true,
			wantBodySub: "无需启动",
			wantNStart:  1,
			wantNStatus: 0,
			wantNDial:   0,
		},
		{
			name:         "non_clifailure_not_swallowed",
			startErr:     errors.New("appcenter-cli start msf: exit status 1"), // plain exec error
			servicePort:  port,
			wantOK:       false,
			wantErrEvent: true,
			wantBodySub:  "exit status 1",
			wantNStart:   1,
			wantNStatus:  0, // immediate failure: no recovery window for hard errors
			wantNDial:    0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Fake clock: the injected sleep advances it instantly, so the
			// whole 30s recovery window replays in microseconds — no real
			// sleeping, no real sockets.
			fakeNow := time.Now()
			var nDial int32

			origNow, origSleep, origDial := startRecoveryNow, startRecoverySleep, startRecoveryDial
			startRecoveryNow = func() time.Time { return fakeNow }
			startRecoverySleep = func(ctx context.Context, d time.Duration) error {
				select {
				case <-ctx.Done():
					return ctx.Err()
				default:
				}
				fakeNow = fakeNow.Add(d)
				return nil
			}
			startRecoveryDial = func(network, addr string, _ time.Duration) (net.Conn, error) {
				atomic.AddInt32(&nDial, 1)
				if want := fmt.Sprintf("127.0.0.1:%d", port); network != "tcp" || addr != want {
					return nil, fmt.Errorf("unexpected dial %s %s, want tcp %s", network, addr, want)
				}
				if !tc.dialOK {
					return nil, errDial
				}
				c1, _ := net.Pipe() // hermetic in-memory conn, closed by the helper
				return c1, nil
			}
			t.Cleanup(func() {
				startRecoveryNow, startRecoverySleep, startRecoveryDial = origNow, origSleep, origDial
			})

			stub := &stubAppCenter{startErr: tc.startErr, statusScript: tc.statusScript}
			p := &installPipeline{queue: NewOperationQueue(), ac: stub}

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/install", nil)
			stream := &sseStream{w: rec, r: req, flusher: rec, appname: appName}

			ok := p.startAndConfirm(context.Background(), stream, core.AppInfo{AppName: appName, ServicePort: tc.servicePort})

			if ok != tc.wantOK {
				t.Fatalf("startAndConfirm = %v, want %v", ok, tc.wantOK)
			}
			body := rec.Body.String()
			if tc.wantBodySub != "" && !strings.Contains(body, tc.wantBodySub) {
				t.Errorf("SSE body missing %q:\n%s", tc.wantBodySub, body)
			}
			if hasErr := strings.Contains(body, `"step":"error"`); hasErr != tc.wantErrEvent {
				t.Errorf("error event present = %v, want %v:\n%s", hasErr, tc.wantErrEvent, body)
			}
			if got := atomic.LoadInt32(&stub.nStart); got != tc.wantNStart {
				t.Errorf("Start calls = %d, want %d", got, tc.wantNStart)
			}
			if got := atomic.LoadInt32(&stub.nStatus); got != tc.wantNStatus {
				t.Errorf("Status calls = %d, want %d", got, tc.wantNStatus)
			}
			if got := atomic.LoadInt32(&nDial); got != tc.wantNDial {
				t.Errorf("dial calls = %d, want %d", got, tc.wantNDial)
			}
		})
	}
}
