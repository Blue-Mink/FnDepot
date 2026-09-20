package platform

import "context"

// AppControl carries the per-app operation capability bits the app-center
// daemon reports. A zero value means "capabilities unknown" (e.g. the CLI
// fallback has no such data), and callers MUST treat zero as permissive for
// StartStop/Uninstall to preserve pre-daemon behavior.
type AppControl struct {
	IsOpen      bool // app exposes an openable UI entry
	IsStartStop bool // supports start/stop
	IsUninstall bool // supports uninstall
	Upgrade     bool // supports in-place upgrade
}

// WebService describes the openable web UI of an installed app, from the
// daemon's appServiceInfo. Type is "iframe" or "url"; a zero Port means the
// app has no openable web entry (the store must not render an "打开" button).
type WebService struct {
	Protocol string // "http" / "https" (daemon default: http)
	Host     string // daemon value; EMPTY on measured boxes — the UI fills in
	// the host the user reached the store on (window.location.hostname),
	// which is correct both for direct :38011 access and for the iframe
	// embedded in the fnOS web UI (same host).
	Port     string
	Path     string // e.g. "/"
	OpenType string // "iframe" / ...
	// ServiceName is the daemon's appServiceInfo.serviceName
	// (e.g. "Gitea.Application"). The fnOS web UI shell opens apps BY THIS
	// KEY (native App Center 打开 button: He(appServiceInfo.serviceName)), so
	// the store's in-shell "打开" passes it to the bridge openApp().
	ServiceName string
}

// HasURL reports whether the app exposes an openable web entry on its own
// port (the common case: service_port in the manifest).
func (w WebService) HasURL() bool {
	return w.Port != ""
}

// HasWebUIPath reports whether the app's web entry is served BY the fnOS web
// UI itself (port 5666): urls.port empty, urls.path set — measured for
// fn-knock /cgi/ThirdParty/fn-knock/index.cgi/ and the fndepot /app/fndepot
// micro-apps. The UI builds the URL as <store-host>:5666<path>.
func (w WebService) HasWebUIPath() bool {
	return w.Port == "" && w.Path != ""
}

// InstalledApp represents an app installed on this box, as reported by the
// app-center daemon (preferred) or appcenter-cli (fallback).
type InstalledApp struct {
	AppName     string
	Version     string
	DisplayName string
	// Status is the daemon vocabulary: "running" / "stopped" / "starting" /
	// "stopping" / "nostart" (system components with no start/stop of their
	// own, e.g. nodejs / java runtime packages).
	Status  string
	Source  string // "official" / "thirdparty"
	Icon    string // daemon icon path, e.g. /app-center-static/icon/<app>/icon.png
	Control AppControl
	Web    WebService
}

// VolumeInfo represents an available installation volume.
type VolumeInfo struct {
	Index      int
	Path       string
	TotalBytes uint64
	FreeBytes  uint64
}

// AppCenter abstracts appcenter-cli operations.
// The real implementation calls appcenter-cli on Linux;
// the mock implementation simulates it for development on macOS.
type AppCenter interface {
	// List returns all installed applications.
	List() ([]InstalledApp, error)

	// Check returns true if the given app is installed.
	Check(appname string) (bool, error)

	// Status returns the running status of an app ("running" or "stopped").
	Status(appname string) (string, error)

	// InstallFpk installs or upgrades an app from an fpk file on the given volume.
	// It extracts the fpk and uses install-local internally for upgrade support.
	InstallFpk(fpkPath string, volume int) error

	// InstallLocal installs or upgrades an app from an extracted fpk directory.
	// When detach is true, the process is launched in a new session so it
	// survives the caller being killed (used for self-update).
	InstallLocal(dir string, volume int, detach bool) error

	// Uninstall removes an installed app, preserving the app's user data.
	Uninstall(ctx context.Context, appname string) error

	// Start starts an installed app.
	Start(appname string) error

	// Stop stops a running app.
	Stop(appname string) error

	// StartConfirmed starts an app through the app-center daemon's task
	// channel: pre-flight check, task submission, then poll to completion —
	// the same flow the native App Center web UI uses, so the result is
	// CONFIRMED instead of fire-and-forget. Falls back to the CLI only when
	// the daemon socket is unreachable.
	StartConfirmed(ctx context.Context, appname string) error

	// StopConfirmed stops a running app through the daemon's task channel
	// (stop/check → stop/task → poll). The daemon refuses to stop an app
	// other apps depend on, which the fire-and-forget CLI path could not
	// report.
	StopConfirmed(ctx context.Context, appname string) error

	// DefaultVolume returns the default installation volume index.
	DefaultVolume() (int, error)

	// SetDefaultVolume sets fnOS's default installation volume. This is the
	// documented lever for install placement; install-local honors it even on
	// builds where the undocumented -v flag is ignored.
	SetDefaultVolume(volume int) error

	// ListVolumes returns all available installation volumes.
	ListVolumes() ([]VolumeInfo, error)

	// AppInstallVolume resolves the volume index an app is CURRENTLY installed
	// on, derived from its on-disk layout independently of appcenter-cli output.
	// found is false when the app is not installed or its volume cannot be
	// determined. Updates MUST pin to this volume so an app is never relocated
	// off its existing data.
	AppInstallVolume(appname string) (int, bool, error)

	// UpgradeCapability reports whether this fnOS build can update an
	// installed app without destroying it. Some builds cannot — see
	// upgrade.go.
	UpgradeCapability() UpgradeCapability

	// DaemonInstallAvailable reports whether the daemon's INSTALL channel is
	// reachable, deciding whether a FRESH install goes through the daemon or
	// falls back to install-local. Kept separate from UpgradeCapability so a
	// change to the update probe cannot silently reroute installs onto the
	// destructive path.
	DaemonInstallAvailable() bool

	// UpgradeFpk upgrades an ALREADY-INSTALLED app in place, preserving its
	// data. This is deliberately separate from InstallFpk: InstallFpk goes
	// through install-local, which implements an upgrade as
	// uninstall-then-reinstall and destroys the app when the reinstall fails.
	UpgradeFpk(ctx context.Context, fpkPath string, params []WizardParam) error

	// FetchWizard returns an app's install-time form definition without
	// installing anything, so the UI can ask the same questions the native
	// App Center does.
	FetchWizard(ctx context.Context, fpkPath string) (*AppWizard, error)

	// InstallFpkWithWizard installs a not-yet-installed app, passing the
	// user's answers to the app's own install wizard.
	InstallFpkWithWizard(ctx context.Context, fpkPath string, volume int, params []WizardParam) error
}
