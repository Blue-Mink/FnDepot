//go:build linux

package platform

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// recordedRequest captures what the fake daemon received.
type recordedRequest struct {
	method string
	path   string
	body   string
}

// startFakeDaemon serves handler over a real unix socket in t.TempDir and
// points the package's daemon client at it for the duration of the test.
func startFakeDaemon(t *testing.T, handler http.Handler) {
	t.Helper()
	ln, err := net.Listen("unix", filepath.Join(t.TempDir(), "daemon.sock"))
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	srv := httptest.NewUnstartedServer(handler)
	srv.Listener = ln
	srv.Start()

	old := daemonSocket
	daemonSocket = ln.Addr().String()
	t.Cleanup(func() {
		srv.Close()
		daemonSocket = old
	})
}

// writeFakeCLI installs a fake appcenter-cli that appends its args to marker
// and exits 0 with benign output, so tests can prove whether the legacy CLI
// path ran.
func writeFakeCLI(t *testing.T, marker string) string {
	t.Helper()
	script := fmt.Sprintf("#!/bin/sh\necho \"$@\" >> %q\necho ok\n", marker)
	path := filepath.Join(t.TempDir(), "appcenter-cli")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake cli: %v", err)
	}
	return path
}

// The daemon accepts exactly one uninstall request shape — measured on fnOS
// 1.2.0203 against /var/run/com.trim.app.center.sock: a POST whose body names
// the app and pins wizard_delete_data to "false" (preserve user data, the
// same default the native Web UI sends).
func TestUninstallPostsExactDaemonBody(t *testing.T) {
	// Given a daemon that records the uninstall task request and completes it
	var got recordedRequest
	mux := http.NewServeMux()
	mux.HandleFunc("/rpc/v1/uninstall/task", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		got = recordedRequest{method: r.Method, path: r.URL.Path, body: string(b)}
		_, _ = w.Write([]byte(`{"code":0,"data":{"taskId":"t-1"}}`))
	})
	mux.HandleFunc("/rpc/v1/common/status", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"code":0,"data":{"status":2}}`))
	})
	startFakeDaemon(t, mux)

	marker := filepath.Join(t.TempDir(), "cli-ran")
	a := &LinuxAppCenter{CLIPath: writeFakeCLI(t, marker)}

	// When the store uninstalls picoclaw
	err := a.Uninstall(context.Background(), "picoclaw")

	// Then the daemon received exactly the measured request, the CLI never
	// ran, and no error surfaced
	if err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if got.method != http.MethodPost {
		t.Errorf("method = %s, want POST (GET is the appcenter-cli bug this replaces)", got.method)
	}
	wantBody := `{"appname":"picoclaw","wizard_delete_data":"false"}`
	if got.body != wantBody {
		t.Errorf("body = %s, want %s", got.body, wantBody)
	}
	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
		t.Error("CLI fallback ran even though the daemon accepted the task")
	}
}

// A non-zero code in a 200 body is the daemon REFUSING the uninstall. That is
// a business error: the CLI cannot do better, so the store must surface it as
// a DaemonError and never silently fall back.
func TestUninstallDaemonBusinessErrorDoesNotFallBack(t *testing.T) {
	// Given a daemon that refuses the uninstall with code 12345
	mux := http.NewServeMux()
	mux.HandleFunc("/rpc/v1/uninstall/task", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"code":12345,"msg":"app busy"}`))
	})
	startFakeDaemon(t, mux)

	marker := filepath.Join(t.TempDir(), "cli-ran")
	a := &LinuxAppCenter{CLIPath: writeFakeCLI(t, marker)}

	// When the store uninstalls
	err := a.Uninstall(context.Background(), "wolgoweb")

	// Then the daemon's code and message surface, and the CLI never ran
	var de *DaemonError
	if !errors.As(err, &de) {
		t.Fatalf("error = %v, want a *DaemonError", err)
	}
	if de.Code != 12345 || !strings.Contains(de.Msg, "app busy") {
		t.Errorf("DaemonError = %+v, want code 12345 with the daemon's message", de)
	}
	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
		t.Error("CLI fallback ran on a daemon business error")
	}
}

// On a build where the daemon socket is absent the store must still be able
// to uninstall, so a transport failure falls back to the legacy CLI.
func TestUninstallFallsBackToCLIWhenDaemonUnreachable(t *testing.T) {
	// Given no daemon listening at the configured socket
	old := daemonSocket
	daemonSocket = filepath.Join(t.TempDir(), "no-such-daemon.sock")
	t.Cleanup(func() { daemonSocket = old })

	marker := filepath.Join(t.TempDir(), "cli-ran")
	a := &LinuxAppCenter{CLIPath: writeFakeCLI(t, marker)}

	// When the store uninstalls
	err := a.Uninstall(context.Background(), "picoclaw")

	// Then the legacy CLI ran the uninstall and reported success
	if err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	b, readErr := os.ReadFile(marker)
	if readErr != nil {
		t.Fatalf("CLI fallback did not run: %v", readErr)
	}
	if got := strings.TrimSpace(string(b)); got != "uninstall picoclaw" {
		t.Errorf("CLI invoked as %q, want %q", got, "uninstall picoclaw")
	}
}

// Once the daemon has ACCEPTED an uninstall task the CLI must never run: a
// failed task means the daemon already took destructive action, and a CLI
// retry could double-uninstall.
func TestUninstallFailedDaemonTaskDoesNotFallBack(t *testing.T) {
	// Given a daemon that accepts the task but reports it failed
	mux := http.NewServeMux()
	mux.HandleFunc("/rpc/v1/uninstall/task", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"code":0,"data":{"taskId":"t-9"}}`))
	})
	mux.HandleFunc("/rpc/v1/common/status", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"code":0,"data":{"status":3,"message":"boom"}}`))
	})
	startFakeDaemon(t, mux)

	marker := filepath.Join(t.TempDir(), "cli-ran")
	a := &LinuxAppCenter{CLIPath: writeFakeCLI(t, marker)}

	// When the store uninstalls
	err := a.Uninstall(context.Background(), "metatube")

	// Then the task failure surfaces and the CLI never ran
	if err == nil || !strings.Contains(err.Error(), "卸载") {
		t.Fatalf("error = %v, want the failed uninstall task", err)
	}
	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
		t.Error("CLI fallback ran after the daemon accepted the task")
	}
}
