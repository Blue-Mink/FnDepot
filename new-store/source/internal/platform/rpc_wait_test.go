//go:build linux

package platform

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// waitFakeDaemon serves /rpc/v1/common/status, replaying the scripted responses
// in order and repeating the last one once the script runs out.
func waitFakeDaemon(t *testing.T, statusScript []string) *int {
	t.Helper()
	var mu sync.Mutex
	calls := new(int)
	mux := http.NewServeMux()
	mux.HandleFunc(routeCommonStatus, func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		n := *calls
		*calls++
		mu.Unlock()
		if n >= len(statusScript) {
			n = len(statusScript) - 1
		}
		_, _ = w.Write([]byte(statusScript[n]))
	})
	startFakeDaemon(t, mux)
	return calls
}

// Losing sight of a task is NOT the task failing. The daemon answers an unknown
// taskId with code 0 and status 5 (measured on fnOS 1.2.0505), which used to
// fall through to the generic branch and surface as the bare, actionless string
// "升级失败: 状态 5". The operation may in fact have SUCCEEDED — the daemon reaps
// finished tasks, and an fnOS system update restarts it outright — so this must
// be reported as an unknown outcome, never as a failure.
func TestWaitTaskUnknownTaskIsIndeterminate(t *testing.T) {
	// Given a daemon that no longer knows the task
	waitFakeDaemon(t, []string{`{"code":0,"data":{"status":5,"taskId":""}}`})
	a := NewLinuxAppCenter()

	// When the task is awaited
	err := a.waitTask(context.Background(), "task-1", "升级")

	// Then the outcome is indeterminate, not a failure
	if !errors.Is(err, ErrTaskOutcomeUnknown) {
		t.Fatalf("err = %v, want ErrTaskOutcomeUnknown", err)
	}
	if strings.Contains(err.Error(), "状态 5") {
		t.Errorf("err = %q still reports the raw status instead of an actionable message", err)
	}
}

// A status poll is observational: failing to READ it says nothing about the
// task. StageFpk already rides out the daemon's transient 10050 / TRPC blips;
// the task poll covers far more wall-clock time and needs it at least as much.
func TestWaitTaskRidesOutTransientPollFailures(t *testing.T) {
	// Given a daemon that blips twice, then reports success
	calls := waitFakeDaemon(t, []string{
		`{"code":10050,"msg":"failed to get volume info: TRPC read timeout"}`,
		`{"code":10050,"msg":"failed to get volume info: TRPC read timeout"}`,
		`{"code":0,"data":{"status":2}}`,
	})
	a := NewLinuxAppCenter()

	// When the task is awaited
	if err := a.waitTask(context.Background(), "task-1", "升级"); err != nil {
		t.Fatalf("waitTask: %v — a transient status poll must not fail the task", err)
	}

	// Then it polled through the blips rather than aborting on the first
	if *calls != 3 {
		t.Errorf("status polled %d times, want 3", *calls)
	}
}

// A status the daemon reports that we do not recognise is still a definite
// terminal answer from the daemon — unlike an unknown task — so it must fail
// rather than be papered over as indeterminate.
func TestWaitTaskUnrecognisedStatusFails(t *testing.T) {
	// Given a daemon reporting an unmodelled terminal status
	waitFakeDaemon(t, []string{`{"code":0,"data":{"status":3,"message":"install hook failed"}}`})
	a := NewLinuxAppCenter()

	// When the task is awaited
	err := a.waitTask(context.Background(), "task-1", "安装")

	// Then it is a failure carrying the daemon's own detail
	if err == nil {
		t.Fatal("err = nil, want a failure")
	}
	if errors.Is(err, ErrTaskOutcomeUnknown) {
		t.Errorf("err = %v, must not be indeterminate — the daemon gave a definite answer", err)
	}
	if !strings.Contains(err.Error(), "install hook failed") {
		t.Errorf("err = %q, want the daemon's own detail", err)
	}
}

// Cancelling the observer (e.g. the user closes the tab) does not cancel the
// daemon's work, so it cannot be reported as a failure either.
func TestWaitTaskCancellationIsIndeterminate(t *testing.T) {
	// Given a task that never finishes
	waitFakeDaemon(t, []string{`{"code":0,"data":{"status":1}}`})
	a := NewLinuxAppCenter()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// When the caller has already gone away
	err := a.waitTask(ctx, "task-1", "升级")

	// Then the outcome is unknown, not failed
	if !errors.Is(err, ErrTaskOutcomeUnknown) {
		t.Fatalf("err = %v, want ErrTaskOutcomeUnknown", err)
	}
}

// removeStagedPackage runs as root, so it must only ever touch something that
// actually looks like the daemon's staging directory.
func TestRemoveStagedPackageOnlyTouchesStagingPaths(t *testing.T) {
	root := t.TempDir()

	staged := filepath.Join(root, "appcenter-downloads", "openlist-4.2.5-tpk")
	if err := os.MkdirAll(staged, 0o755); err != nil {
		t.Fatal(err)
	}
	// Paths that must survive: wrong parent, wrong suffix, and a relative path.
	survivors := []string{
		filepath.Join(root, "appcenter-downloads", "not-staging"),
		filepath.Join(root, "elsewhere", "openlist-4.2.5-tpk"),
	}
	for _, p := range survivors {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	removeStagedPackage(staged)
	if _, err := os.Stat(staged); !os.IsNotExist(err) {
		t.Errorf("staged dir still present, want removed")
	}

	for _, p := range survivors {
		removeStagedPackage(p)
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s was removed; only .../appcenter-downloads/*-tpk may be touched", p)
		}
	}

	// Empty and relative paths must be no-ops rather than panics or surprises.
	removeStagedPackage("")
	removeStagedPackage("appcenter-downloads/rel-tpk")
}
