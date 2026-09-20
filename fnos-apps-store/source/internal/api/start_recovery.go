package api

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"time"

	"fnos-store/internal/core"
	"fnos-store/internal/platform"
)

// Bounds for the post-start confirmation window. fnOS's appcenter-cli start
// is not transactional: on a busy daemon it returns a transient "code 10500"
// envelope while the service is still coming up — the app logs attached to
// conversun/fnos-apps#264, #260, #258, #253, #251 and #246 show the apps
// listening seconds after the store had already declared the install failed.
// The window is bounded so a genuinely dead app still fails, just 30s later.
const (
	startRecoveryTimeout  = 30 * time.Second
	startRecoveryInterval = 2 * time.Second
	startRecoveryDialWait = 1 * time.Second
)

// Seam variables, overridden by tests so the recovery loop runs hermetically —
// a fake clock, instant sleeps that advance it, and a dialer that never
// touches a real socket. Same pattern as verifyWait.
var (
	startRecoveryNow   = time.Now
	startRecoverySleep = func(ctx context.Context, d time.Duration) error {
		return verifyWait(ctx, d)
	}
	startRecoveryDial = net.DialTimeout
)

// appStatus reads the app's status through the CLI mutex, one acquisition per
// call — the same convention verifyInstalled uses for each Check. The caller
// of startAndConfirm holds NO lock (runWithVirtualProgress has already
// returned, releasing WithCLI), so this cannot self-deadlock; what WOULD
// deadlock is calling WithCLI from inside the startApp closure itself, since
// cliMu is not reentrant.
func (p *installPipeline) appStatus(appname string) (string, error) {
	var status string
	err := p.queue.WithCLI(func() error {
		var e error
		status, e = p.ac.Status(appname)
		return e
	})
	return status, err
}

// startAndConfirm runs the post-install start step for a fresh install and
// reports the outcome on the stream. Returns false when the pipeline must
// abort.
//
// A start error is not automatically fatal for a serviced app: when the CLI
// reports an error envelope (ErrCLIFailure), the app gets a bounded window to
// disprove it — via `appcenter-cli status` or by accepting a TCP connection
// on its service port. Only if neither signal fires within the window is the
// original start error surfaced, byte-for-byte as before this recovery
// existed.
func (p *installPipeline) startAndConfirm(ctx context.Context, stream *sseStream, app core.AppInfo) bool {
	err := runWithVirtualProgress(ctx, stream, "starting", "正在启动...", func() error {
		return p.startApp(app.AppName)
	})
	if err == nil {
		return true
	}

	// Apps with no service port (e.g. nvidia-driver — a root-installed
	// kernel driver with ctl_stop=false) have no startable service, so
	// appcenter-cli start answers code 10332. That is the app's nature,
	// not an install failure: the payload is already on disk. Surface it
	// as a note rather than failing the whole operation (#226).
	if app.ServicePort == 0 {
		_ = stream.sendProgress(progressPayload{Step: "starting", Message: "该应用为驱动/工具类（无服务端口），无需启动"})
		return true
	}

	// Only a CLI-envelope failure is worth waiting out: the daemon may still
	// be bringing the app up. An exec-level error means the CLI never ran,
	// so there is nothing transient to recover from — fail immediately.
	if !errors.Is(err, platform.ErrCLIFailure) {
		_ = stream.sendError(err.Error())
		return false
	}

	log.Printf("startAndConfirm: %s start reported a CLI failure (%v); probing for up to %s before failing", app.AppName, err, startRecoveryTimeout)
	_ = stream.sendProgress(progressPayload{Step: "starting", Message: "启动返回瞬时错误，正在确认应用实际状态..."})

	if waitForActuallyRunning(ctx, p.appStatus, app.AppName, app.ServicePort,
		startRecoveryTimeout, startRecoveryNow, startRecoverySleep, startRecoveryDial) {
		log.Printf("startAndConfirm: %s is actually running despite the start error; treating the install as successful", app.AppName)
		_ = stream.sendProgress(progressPayload{Step: "starting", Message: "启动返回瞬时错误，已确认应用实际在运行"})
		return true
	}

	// A genuinely dead app keeps the pre-recovery behavior exactly: the
	// original start error is what the user sees.
	_ = stream.sendError(err.Error())
	return false
}

// waitForActuallyRunning polls until the app proves itself running — the CLI
// status says "running" OR the service port accepts a TCP connection — or the
// timeout expires. Both signals are probed every round because each alone can
// lag: the control plane is eventually consistent on a busy daemon (the 10500
// reports), and a port can answer before appcenter notices, or vice versa.
//
// status is routed through the operation queue by the caller; now/sleep/dial
// are seams so tests run without real time or sockets. A status error is not
// fatal — the CLI is unreliable by premise here — it just means "not proven
// yet". Returns false on timeout or ctx cancellation.
func waitForActuallyRunning(
	ctx context.Context,
	status func(appname string) (string, error),
	appname string,
	port int,
	timeout time.Duration,
	now func() time.Time,
	sleep func(context.Context, time.Duration) error,
	dial func(network, addr string, d time.Duration) (net.Conn, error),
) bool {
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	deadline := now().Add(timeout)
	for {
		if s, err := status(appname); err == nil && s == "running" {
			return true
		}
		if conn, err := dial("tcp", addr, startRecoveryDialWait); err == nil {
			_ = conn.Close()
			return true
		}
		if !now().Before(deadline) {
			return false
		}
		if err := sleep(ctx, startRecoveryInterval); err != nil {
			return false
		}
	}
}
