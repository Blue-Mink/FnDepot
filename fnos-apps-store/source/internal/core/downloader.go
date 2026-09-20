package core

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

type DownloadRequest struct {
	URLs     []string
	FileName string
	AppName  string
}

type Downloader struct {
	httpClient  *http.Client
	mu          sync.RWMutex
	downloadDir string
	tmpDir      string
}

func NewDownloader(downloadDir string) *Downloader {
	if downloadDir == "" {
		downloadDir = os.TempDir()
	}
	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
	}
	return &Downloader{
		httpClient:  &http.Client{Transport: transport},
		downloadDir: downloadDir,
		tmpDir:      os.TempDir(),
	}
}

// DownloadDir 返回当前 FPK 下载目录（设置页展示用）。
func (d *Downloader) DownloadDir() string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.downloadDir
}

// SetDownloadDir 运行时切换 FPK 下载目录（设置页「FPK 下载目录」保存时调用）。
// 空路径忽略。已有缓存留在旧目录（不会自动迁移）。
func (d *Downloader) SetDownloadDir(dir string) {
	if strings.TrimSpace(dir) == "" {
		return
	}
	d.mu.Lock()
	d.downloadDir = strings.TrimSpace(dir)
	d.mu.Unlock()
}

func (d *Downloader) dir() string {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.downloadDir
}

// staleTmpAge is how old a temp file must be before cleanup may reap it. A
// younger file may belong to an in-flight download — deleting it out from
// under os.Rename was conversun/fnos-apps#245.
const staleTmpAge = time.Hour

// CleanupStaleTmpFiles reaps ABANDONED download temp files (older than
// staleTmpAge). It is safe to run while a download is in flight.
func (d *Downloader) CleanupStaleTmpFiles() error {
	entries, err := os.ReadDir(d.dir())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !strings.HasSuffix(entry.Name(), ".fpk.tmp") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if time.Since(info.ModTime()) < staleTmpAge {
			continue
		}
		_ = os.Remove(filepath.Join(d.dir(), entry.Name()))
	}
	return nil
}

func (d *Downloader) Download(ctx context.Context, req DownloadRequest, progress func(downloaded, total int64)) (string, error) {
	if req.FileName == "" {
		return "", errors.New("file name is required")
	}

	if err := os.MkdirAll(d.dir(), 0o755); err != nil {
		return "", fmt.Errorf("create download dir: %w", err)
	}

	if err := checkTmpSpace(d.tmpDir); err != nil {
		return "", err
	}

	prefixedName := req.AppName + "-" + req.FileName
	finalPath := filepath.Join(d.dir(), prefixedName)

	// 缓存复用：安装向导预取（fetchWizard）已完整下载过同一 FPK 时直接复用，
	// 避免大应用（100MB+）被下载两次。文件名含版本（如 jellyfin_12.1_x86.fpk），
	// 版本变化 → 文件名变化 → 自然失效；24h TTL 兜底同文件名的静默重发布。
	// 安装/更新完成后管道会删除该文件，不会无限堆积。
	if st, err := os.Stat(finalPath); err == nil && st.Size() > 10*1024 && time.Since(st.ModTime()) < 24*time.Hour {
		if progress != nil {
			progress(st.Size(), st.Size())
		}
		return finalPath, nil
	}

	urls := req.URLs

	if len(urls) == 0 {
		return "", errors.New("download urls are empty")
	}

	var lastErr error
	for _, url := range urls {
		// A unique temp file per attempt: the deterministic finalPath+".tmp"
		// let any second actor (OS /tmp reaper, stale cleanup, retry) delete
		// the in-flight file under os.Rename (conversun/fnos-apps#245). The
		// ".fpk.tmp" suffix is kept so CleanupStaleTmpFiles still matches.
		tmp, err := os.CreateTemp(d.dir(), prefixedName+".*.fpk.tmp")
		if err != nil {
			return "", fmt.Errorf("create temp file: %w", err)
		}
		tmpPath := tmp.Name()
		if err := tmp.Close(); err != nil {
			_ = os.Remove(tmpPath)
			return "", fmt.Errorf("close temp file: %w", err)
		}

		if err := d.downloadFromURL(ctx, url, tmpPath, progress); err != nil {
			lastErr = err
			_ = os.Remove(tmpPath)
			continue
		}

		if err := os.Rename(tmpPath, finalPath); err != nil {
			_ = os.Remove(tmpPath)
			return "", fmt.Errorf("rename %q to %q: %w", tmpPath, finalPath, err)
		}
		return finalPath, nil
	}

	if lastErr == nil {
		lastErr = errors.New("download failed")
	}
	return "", lastErr
}

func (d *Downloader) downloadFromURL(ctx context.Context, url, dstPath string, progress func(downloaded, total int64)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %q: %s", url, resp.Status)
	}

	f, err := os.OpenFile(dstPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	total := resp.ContentLength
	buf := make([]byte, 128*1024)
	var downloaded int64
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, err := f.Write(buf[:n]); err != nil {
				return err
			}
			downloaded += int64(n)
			if progress != nil {
				progress(downloaded, total)
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			return readErr
		}
	}

	if err := f.Sync(); err != nil {
		return err
	}

	if err := validateFpk(dstPath); err != nil {
		return err
	}
	return nil
}

// validateFpk proves the downloaded bytes are an fpk: a gzip stream wrapping
// a tar whose root carries a manifest entry.
//
// Size alone cannot be the gate in either direction. Docker-mode fpks ship
// no binaries and legitimately land under 10 KiB (astrbot 4.27.4 is 8483
// bytes, conversun/fnos-apps#284), while a mirror answering 200 with a
// full-size HTML error page defeats any size floor. Archive structure is
// the actual contract the installer relies on.
func validateFpk(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("downloaded file is not a valid fpk archive: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return errors.New("downloaded fpk has no manifest entry — likely corrupted or an error page")
		}
		if err != nil {
			return fmt.Errorf("downloaded fpk is truncated: %w", err)
		}
		if filepath.Base(hdr.Name) == "manifest" {
			return nil
		}
	}
}

func checkTmpSpace(tmpDir string) error {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(tmpDir, &stat); err != nil {
		return fmt.Errorf("statfs %q: %w", tmpDir, err)
	}

	available := stat.Bavail * uint64(stat.Bsize)
	const minRequired = 64 * 1024 * 1024
	if available < minRequired {
		return fmt.Errorf("insufficient free space in %s: %d bytes available", tmpDir, available)
	}
	return nil
}
