//go:build linux

package platform

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
)

// stageFakeDaemon wires the two staging routes: task hands out a fixed task
// ID, status replays the scripted responses in order (repeating the last one
// once the script runs out) and counts how many times it was polled.
func stageFakeDaemon(t *testing.T, statusScript []string) *int {
	t.Helper()
	var mu sync.Mutex
	statusCalls := new(int)
	mux := http.NewServeMux()
	mux.HandleFunc(routeDownloadTask, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"code":0,"data":{"downloadTaskId":"dt-1"}}`))
	})
	mux.HandleFunc(routeDownloadStatus, func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		n := *statusCalls
		*statusCalls++
		mu.Unlock()
		if n >= len(statusScript) {
			n = len(statusScript) - 1
		}
		_, _ = w.Write([]byte(statusScript[n]))
	})
	startFakeDaemon(t, mux)
	return statusCalls
}

// Error 10050 "failed to get volume info: TRPC read timeout" is a TRANSIENT
// daemon-internal timeout — the next poll typically succeeds — yet it used to
// kill a whole install on one blip (conversun/fnos-apps#247).
func TestStageFpkToleratesTransientPollErrors(t *testing.T) {
	// Given a daemon whose status route fails transiently twice, then succeeds
	calls := stageFakeDaemon(t, []string{
		`{"code":10050,"msg":"failed to get volume info: TRPC read timeout"}`,
		`{"code":10050,"msg":"failed to get volume info: TRPC read timeout"}`,
		`{"code":0,"data":{"status":2,"appName":"mihomo","version":"1.2.3","name":"Mihomo","packageType":"app","path":"/dl/mihomo.fpk"}}`,
	})
	a := NewLinuxAppCenter()

	// When the fpk is staged
	staged, err := a.StageFpk(context.Background(), "/tmp/mihomo.fpk")

	// Then staging rides out the blips and returns the identified package
	if err != nil {
		t.Fatalf("StageFpk: %v — transient 10050 must not abort staging", err)
	}
	if staged.AppName != "mihomo" || staged.Version != "1.2.3" {
		t.Errorf("staged = %+v, want mihomo 1.2.3", staged)
	}
	if *calls != 3 {
		t.Errorf("status polled %d times, want 3", *calls)
	}
}

// A NON-transient daemon error (any other non-zero code) is a refusal: it
// must abort staging immediately, exactly as before.
func TestStageFpkAbortsImmediatelyOnNonTransientError(t *testing.T) {
	// Given a daemon whose status route returns a business error
	calls := stageFakeDaemon(t, []string{
		`{"code":12345,"msg":"package corrupted"}`,
	})
	a := NewLinuxAppCenter()

	// When the fpk is staged
	_, err := a.StageFpk(context.Background(), "/tmp/mihomo.fpk")

	// Then the daemon's error surfaces on the first poll
	if err == nil || !strings.Contains(err.Error(), "查询暂存状态失败") {
		t.Fatalf("error = %v, want the staging-status failure", err)
	}
	var de *DaemonError
	if !errors.As(err, &de) || de.Code != 12345 {
		t.Errorf("error = %v, want DaemonError code 12345", err)
	}
	if *calls != 1 {
		t.Errorf("status polled %d times, want 1 — a non-transient error must abort at once", *calls)
	}
}

// Transient tolerance is bounded: a genuinely sick daemon must still fail
// fast instead of spinning until the 3-minute deadline.
func TestStageFpkAbortsAfterConsecutiveTransientFailures(t *testing.T) {
	// Given a daemon whose status route never stops timing out
	calls := stageFakeDaemon(t, []string{
		`{"code":10050,"msg":"failed to get volume info: TRPC read timeout"}`,
	})
	a := NewLinuxAppCenter()

	// When the fpk is staged
	_, err := a.StageFpk(context.Background(), "/tmp/mihomo.fpk")

	// Then staging gives up after the bounded number of consecutive blips.
	// 5 is the contract (the bounded-abort constant must equal it).
	if err == nil || !strings.Contains(err.Error(), "查询暂存状态失败") {
		t.Fatalf("error = %v, want the staging-status failure", err)
	}
	if *calls != 5 {
		t.Errorf("status polled %d times, want 5 (the bounded abort)", *calls)
	}
}

// The transient classification: 10050 and timeout-shaped transport errors are
// blips; every other daemon code is a refusal.
func TestIsTransientStagePollError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"daemon 10050 volume-info timeout", &DaemonError{Path: routeDownloadStatus, Code: 10050, Msg: "failed to get volume info: TRPC read timeout"}, true},
		{"wrapped TRPC read timeout", errors.New("app center daemon unreachable (/rpc/v1/download/status): Post: TRPC read timeout"), true},
		{"plain i/o timeout", errors.New("read: connection reset: i/o timeout"), true},
		{"daemon 12345 business error", &DaemonError{Path: routeDownloadStatus, Code: 12345, Msg: "package corrupted"}, false},
		{"daemon 10100 package missing", &DaemonError{Path: routeDownloadStatus, Code: daemonCodePackageMissing}, false},
		{"unrelated error", errors.New("boom"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isTransientStagePollError(tc.err); got != tc.want {
				t.Errorf("isTransientStagePollError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
