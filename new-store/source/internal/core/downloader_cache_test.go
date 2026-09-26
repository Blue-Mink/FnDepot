package core

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// 缓存复用：安装向导预取已留下完整 FPK 时，Download 直接命中缓存、
// 不发起任何网络请求（故意用不可达 URL 验证）。
func TestDownload_CacheReuse(t *testing.T) {
	dir := t.TempDir()
	d := NewDownloader(dir)
	name := "test_1.0.fpk"
	finalPath := filepath.Join(dir, "app-"+name)
	if err := os.WriteFile(finalPath, bytes.Repeat([]byte("x"), 20*1024), 0o644); err != nil {
		t.Fatal(err)
	}

	var progressHits int
	got, err := d.Download(context.Background(), DownloadRequest{
		URLs:     []string{"http://127.0.0.1:1/never.fpk"},
		FileName: name,
		AppName:  "app",
	}, func(downloaded, total int64) {
		progressHits++
		if downloaded != total {
			t.Errorf("cache-hit progress should be (size, size), got (%d, %d)", downloaded, total)
		}
	})
	if err != nil {
		t.Fatalf("cache hit should succeed without network: %v", err)
	}
	if got != finalPath {
		t.Fatalf("want cached path %q, got %q", finalPath, got)
	}
	if progressHits != 1 {
		t.Fatalf("expected one progress call on cache hit, got %d", progressHits)
	}
}

// TTL：超过 24h 的缓存文件不复用（走真实下载路径；坏 URL 下应报错）。
func TestDownload_CacheStaleMiss(t *testing.T) {
	dir := t.TempDir()
	d := NewDownloader(dir)
	name := "test_1.0.fpk"
	finalPath := filepath.Join(dir, "app-"+name)
	if err := os.WriteFile(finalPath, bytes.Repeat([]byte("x"), 20*1024), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-25 * time.Hour)
	if err := os.Chtimes(finalPath, old, old); err != nil {
		t.Fatal(err)
	}

	_, err := d.Download(context.Background(), DownloadRequest{
		URLs:     []string{"http://127.0.0.1:1/never.fpk"},
		FileName: name,
		AppName:  "app",
	}, nil)
	if err == nil {
		t.Fatal("stale cache must be re-downloaded (expected error from unreachable URL)")
	}
}

// 过小文件（<10KB，可能是残留/损坏）不复用。
func TestDownload_CacheTooSmallMiss(t *testing.T) {
	dir := t.TempDir()
	d := NewDownloader(dir)
	name := "test_1.0.fpk"
	finalPath := filepath.Join(dir, "app-"+name)
	if err := os.WriteFile(finalPath, []byte("tiny"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := d.Download(context.Background(), DownloadRequest{
		URLs:     []string{"http://127.0.0.1:1/never.fpk"},
		FileName: name,
		AppName:  "app",
	}, nil)
	if err == nil {
		t.Fatal("tiny leftover must not be treated as a valid cache")
	}
}
