//go:build linux

package platform

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type LinuxAppCenter struct {
	CLIPath string
}

func NewAppCenter(_ string) AppCenter {
	return NewLinuxAppCenter()
}

func NewLinuxAppCenter() *LinuxAppCenter {
	return &LinuxAppCenter{
		CLIPath: "/usr/local/bin/appcenter-cli",
	}
}

// run executes appcenter-cli and treats an error envelope in the output as a
// failure even when the process exits 0 — which it always does. See clierr.go
// for the measured evidence and why exit status cannot be trusted.
func (a *LinuxAppCenter) run(args ...string) (string, error) {
	cmd := exec.Command(a.CLIPath, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("appcenter-cli %s: %w: %s", strings.Join(args, " "), err, string(out))
	}
	text := strings.TrimSpace(string(out))
	if cliOutputFailure(text) {
		return "", fmt.Errorf("appcenter-cli %s: %w: %s",
			strings.Join(args, " "), ErrCLIFailure, cliErrorDetail(text))
	}
	return text, nil
}

// List returns the installed apps, preferring the app-center daemon:
// it is authoritative (it is what the native App Center reads), reports the
// full status vocabulary (starting/stopping/nostart, not just running/stopped)
// and carries the per-app capability bits. The CLI table parse is a fallback
// for the case the daemon socket is absent (very old builds / during updates).
func (a *LinuxAppCenter) List() ([]InstalledApp, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if apps, err := a.DaemonListInstalled(ctx); err == nil {
		return apps, nil
	}
	out, err := a.run("list")
	if err != nil {
		return nil, err
	}
	apps, err := parseListTable(out)
	if err != nil {
		return nil, err
	}
	// The CLI table has no capability column. Fill PERMISSIVE defaults:
	// zero-value AppControl would be read as "not startable / not
	// uninstallable" and hide every control button on builds that fall back
	// to the CLI. The pre-daemon UI showed the buttons everywhere except
	// nostart components, which is exactly what this encodes.
	for i := range apps {
		if apps[i].Status == "nostart" {
			continue
		}
		apps[i].Control = AppControl{IsOpen: true, IsStartStop: true, IsUninstall: true}
	}
	return apps, nil
}

func (a *LinuxAppCenter) Check(appname string) (bool, error) {
	out, err := a.run("check", appname)
	if err != nil {
		return false, err
	}
	return parseCheckOutput(out)
}

func (a *LinuxAppCenter) Status(appname string) (string, error) {
	out, err := a.run("status", appname)
	if err != nil {
		return "", err
	}
	return parseStatusOutput(out)
}

func (a *LinuxAppCenter) InstallFpk(fpkPath string, volume int) error {
	dir, err := a.extractFpk(fpkPath)
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	return a.InstallLocal(dir, volume, false)
}

// SelfUpdateLogPath is where a detached install-local writes its output.
// The store process is killed by fnOS partway through its own update, so this
// file is the only record of whether the update actually succeeded — the
// previous implementation discarded both streams, making every self-update
// failure invisible after the restart.
//
// It lives under the app's own log directory, NOT /tmp: this process runs as
// root, and a predictable name in a world-writable directory lets any local
// user pre-create a symlink and have root truncate an arbitrary file.
const SelfUpdateLogPath = "/var/log/apps/fnos-apps-store-selfupdate.log"

// InstallLocal runs install-local, which is destructive: fnOS uninstalls the
// existing app before reinstalling it. The -v/--volume flag is documented by
// the CLI itself as "(ignored during upgrades)", so it does NOT protect an
// update from being placed elsewhere — only the daemon's default-volume does.
//
// When detach is set the child runs in its own session so it survives this
// process being killed during the uninstall phase, and its output is captured
// to SelfUpdateLogPath for post-restart diagnosis.
func (a *LinuxAppCenter) InstallLocal(dir string, volume int, detach bool) error {
	args := []string{"install-local", "--dir", dir, "-v", strconv.Itoa(volume)}
	if detach {
		cmd := exec.Command(a.CLIPath, args...)
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		// O_NOFOLLOW refuses a symlink planted at the log path; 0600 keeps the
		// CLI diagnostics root-only. Best-effort: losing the log must not block
		// the update, it only costs us the post-restart failure diagnosis.
		if f, err := os.OpenFile(SelfUpdateLogPath,
			os.O_CREATE|os.O_WRONLY|os.O_TRUNC|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0o600); err == nil {
			defer f.Close()
			cmd.Stdout = f
			cmd.Stderr = f
		}
		return cmd.Start()
	}
	_, err := a.run(args...)
	return err
}

func (a *LinuxAppCenter) extractFpk(fpkPath string) (string, error) {
	dir, err := os.MkdirTemp("", "fpk-install-*")
	if err != nil {
		return "", fmt.Errorf("create temp dir: %w", err)
	}
	cmd := exec.Command("tar", "xzf", fpkPath, "-C", dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		os.RemoveAll(dir)
		return "", fmt.Errorf("extract fpk: %w: %s", err, string(out))
	}
	EnsureHooksExecutable(dir)
	return dir, nil
}

// Uninstall removes an app through the app-center daemon's uninstall task
// channel, which is the only reliable path: `appcenter-cli uninstall`'s
// pre-flight GET /rpc/v1/uninstall/info hits a daemon that only serves POST,
// so the CLI aborts 100% of the time on fnOS 1.2.0203 (conversun/fnos-apps#265).
//
// Fallback rules, in order of how much we know:
//   - A daemon business error (code != 0) is final: the daemon refused, so the
//     CLI cannot do better.
//   - A dial failure proves the request never left this process, so the legacy
//     CLI is a safe last resort.
//   - ANY other transport failure is ambiguous — the daemon may already be
//     uninstalling. Running the CLI then issues a SECOND uninstall, and the CLI
//     path does not pin wizard_delete_data=false the way submitUninstall does,
//     so the retry can take the app's @appdata with it. Refuse instead.
func (a *LinuxAppCenter) Uninstall(ctx context.Context, appname string) error {
	taskID, err := a.submitUninstall(ctx, appname)
	if err != nil {
		var de *DaemonError
		if errors.As(err, &de) {
			return fmt.Errorf("卸载失败: %w", err)
		}
		if !errors.Is(err, ErrDaemonUnreachable) {
			return fmt.Errorf("%w：卸载 %s 的请求可能已经送达 app center（%v）。请勿重复卸载，先在应用中心确认应用当前状态",
				ErrTaskOutcomeUnknown, appname, err)
		}
		if _, cliErr := a.run("uninstall", appname); cliErr != nil {
			return fmt.Errorf("%w（daemon 通道也不可用: %v）", cliErr, err)
		}
		return nil
	}
	return a.waitTask(ctx, taskID, "卸载")
}

func (a *LinuxAppCenter) Start(appname string) error {
	_, err := a.run("start", appname)
	return err
}

func (a *LinuxAppCenter) Stop(appname string) error {
	_, err := a.run("stop", appname)
	return err
}

func (a *LinuxAppCenter) DefaultVolume() (int, error) {
	out, err := a.run("default-volume")
	if err != nil {
		return 0, err
	}
	return parseVolumeOutput(out)
}

// SetDefaultVolume asks the CLI to move fnOS's default install volume.
// It reports only whether the command was accepted; on some fnOS builds the
// set is a silent no-op, so callers MUST read the value back and compare
// rather than treating a nil error as proof the volume was pinned.
func (a *LinuxAppCenter) SetDefaultVolume(volume int) error {
	_, err := a.run("default-volume", strconv.Itoa(volume))
	return err
}

// isMountPoint returns true if path is a mount point (its device differs
// from the parent directory's device). This filters out plain directories
// like /vol00 or /vol02 that sit on the root partition and are not real
// storage volumes.
func isMountPoint(path string) bool {
	var pathStat, parentStat syscall.Stat_t
	if err := syscall.Stat(path, &pathStat); err != nil {
		return false
	}
	if err := syscall.Stat(filepath.Dir(path), &parentStat); err != nil {
		return false
	}
	return pathStat.Dev != parentStat.Dev
}

func (a *LinuxAppCenter) ListVolumes() ([]VolumeInfo, error) {
	matches, err := filepath.Glob("/vol*")
	if err != nil {
		return nil, err
	}

	sort.Slice(matches, func(i, j int) bool {
		if len(matches[i]) != len(matches[j]) {
			return len(matches[i]) < len(matches[j])
		}
		return matches[i] < matches[j]
	})

	seen := make(map[int]bool)
	var volumes []VolumeInfo
	for _, m := range matches {
		info, err := os.Stat(m)
		if err != nil || !info.IsDir() {
			continue
		}
		name := filepath.Base(m)
		if !strings.HasPrefix(name, "vol") {
			continue
		}
		idx, err := strconv.Atoi(name[3:])
		if err != nil {
			continue
		}
		if !isMountPoint(m) {
			continue
		}
		if seen[idx] {
			continue
		}
		seen[idx] = true

		vol := VolumeInfo{Index: idx, Path: m}
		var stat syscall.Statfs_t
		if err := syscall.Statfs(m, &stat); err == nil {
			vol.TotalBytes = stat.Blocks * uint64(stat.Bsize)
			vol.FreeBytes = stat.Bavail * uint64(stat.Bsize)
		}
		volumes = append(volumes, vol)
	}

	sort.Slice(volumes, func(i, j int) bool {
		return volumes[i].Index < volumes[j].Index
	})
	return volumes, nil
}

// AppInstallVolume resolves the volume an app currently lives on by probing
// the app's symlinks under /var/apps/<app> in order: target, var, then meta,
// matching each resolved path against the mounted volumes. This is the
// CLI-independent source of truth used to pin an update to the app's existing
// volume instead of a re-resolved global default, which would relocate the
// app and orphan its data. found is false when the app is not installed or
// its layout cannot be mapped to a known volume.
func (a *LinuxAppCenter) AppInstallVolume(appname string) (int, bool, error) {
	volumes, err := a.ListVolumes()
	if err != nil {
		return 0, false, err
	}
	idx, found := appInstallVolume(filepath.Join("/var/apps", appname), volumes)
	return idx, found, nil
}

// appInstallVolume probes the app's symlinks in order: target
// (-> /volN/@appcenter/<app>), var (-> /volN/@appdata/<app>), then meta
// (-> /volN/@appmeta/<app>), and returns the volume of the first that
// resolves onto a mounted volume.
//
// target stays authoritative for normal apps. meta is the rescue for
// root-type apps (install_type = root, e.g. nvidia-driver): fnOS installs
// those to the SYSTEM filesystem (/usr/local/apps/...), so target and var
// resolve off-volume, and only meta — fnOS's own per-app volume association —
// still points onto a storage volume. Measured on fnOS 1.2.0203 across 60
// installed apps: 60/60 have a meta symlink, 60/60 metas resolve onto a
// /volN, and meta never contradicts an on-volume target
// (conversun/fnos-apps#254).
func appInstallVolume(appDir string, volumes []VolumeInfo) (int, bool) {
	for _, sub := range []string{"target", "var", "meta"} {
		resolved, err := filepath.EvalSymlinks(filepath.Join(appDir, sub))
		if err != nil {
			continue
		}
		if idx, ok := volumeIndexForPath(resolved, volumes); ok {
			return idx, true
		}
	}
	return 0, false
}
