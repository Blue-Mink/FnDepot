package core

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// serveRange 起一个支持 HTTP Range 的测试服务器：
//   - 带 Range: bytes=N- 且 N 有效 → 206 + body[N:]
//   - 否则 → 200 + 全量 body
// gotRange 记录是否收到过 Range 请求（验证确实走了续传而非重下）。
func serveRange(t *testing.T, body []byte, gotRange *bool) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if rng := r.Header.Get("Range"); rng != "" {
			*gotRange = true
			spec := strings.TrimPrefix(rng, "bytes=")
			if startStr, _, ok := strings.Cut(spec, "-"); ok {
				if start, err := strconv.ParseInt(startStr, 10, 64); err == nil && start > 0 && start < int64(len(body)) {
					w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, int64(len(body))-1, len(body)))
					w.WriteHeader(http.StatusPartialContent)
					_, _ = w.Write(body[start:])
					return
				}
			}
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestDownloadResumesFromPart：预置半截 .part（模拟中断），再下载 →
// 应发 Range 从断点续传，最终拼出完整且合法的 fpk。
func TestDownloadResumesFromPart(t *testing.T) {
	dir := t.TempDir()
	body := fpkBody(t)
	gotRange := false
	srv := serveRange(t, body, &gotRange)
	d := NewDownloader(dir)

	partPath := filepath.Join(dir, "app-x.fpk.part")
	if err := os.WriteFile(partPath, body[:len(body)/2], 0o644); err != nil {
		t.Fatal(err)
	}

	finalPath, err := d.Download(context.Background(), DownloadRequest{
		URLs: []string{srv.URL}, FileName: "x.fpk", AppName: "app",
	}, nil)
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	got, err := os.ReadFile(finalPath)
	if err != nil {
		t.Fatalf("read final: %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Errorf("final = %d bytes, want the %d-byte body (resume should complete it)", len(got), len(body))
	}
	if !gotRange {
		t.Errorf("server did not receive a Range request; download did not resume from .part")
	}
	if _, err := os.Stat(partPath); !os.IsNotExist(err) {
		t.Errorf(".part should be renamed to final, but still exists")
	}
}

// TestDownloadServerWithoutRangeStartsOver：服务端不支持 Range（恒 200）时，
// 已有「脏」.part 应被截断重下（不产生拼接错误）。
func TestDownloadServerWithoutRangeStartsOver(t *testing.T) {
	dir := t.TempDir()
	body := fpkBody(t)
	d := NewDownloader(dir)

	partPath := filepath.Join(dir, "app-x.fpk.part")
	if err := os.WriteFile(partPath, []byte("garbage-garbage-garbage"), 0o644); err != nil {
		t.Fatal(err)
	}

	// 不支持 Range 的服务器（恒 200 全量）。
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	}))
	t.Cleanup(plain.Close)

	finalPath, err := d.Download(context.Background(), DownloadRequest{
		URLs: []string{plain.URL}, FileName: "x.fpk", AppName: "app",
	}, nil)
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	got, err := os.ReadFile(finalPath)
	if err != nil {
		t.Fatalf("read final: %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Errorf("final content wrong after 200 restart (garbage .part should be truncated)")
	}
}

// TestDownloadResumeAcrossMirrors：镜像1 只能给前半（206 到一半即 EOF），
// 镜像2 给全量 → 回退到镜像2 后仍能拿到完整合法 fpk。
func TestDownloadResumeAcrossMirrors(t *testing.T) {
	dir := t.TempDir()
	body := fpkBody(t)
	d := NewDownloader(dir)

	// 镜像1：恒返回前半（206），模拟只能部分交付的慢镜像。
	half := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes 0-%d/%d", int64(len(body)/2)-1, len(body)))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(body[:len(body)/2])
	}))
	t.Cleanup(half.Close)

	// 镜像2：全量（200）。
	full := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	}))
	t.Cleanup(full.Close)

	finalPath, err := d.Download(context.Background(), DownloadRequest{
		URLs: []string{half.URL, full.URL}, FileName: "x.fpk", AppName: "app",
	}, nil)
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	got, err := os.ReadFile(finalPath)
	if err != nil {
		t.Fatalf("read final: %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Errorf("final = %d bytes, want %d (fallback to full mirror should complete)", len(got), len(body))
	}
}
