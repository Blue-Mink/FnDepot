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
	"strconv"
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
		// 兼容两种临时产物：旧的 .fpk.tmp（唯一临时文件）与断点续传的
		// .part（确定性部分文件）。都按 mtime 判定（活跃下载 mtime 新鲜）。
		name := entry.Name()
		if !strings.HasSuffix(name, ".fpk.tmp") && !strings.HasSuffix(name, ".part") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if time.Since(info.ModTime()) < staleTmpAge {
			continue
		}
		_ = os.Remove(filepath.Join(d.dir(), name))
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
	// 断点续传：确定性 .part 文件，跨镜像/重试复用。链上各 URL 是同一文件
	// 的镜像（内容一致），故续传安全。单写者由上层队列保证（同一应用同时
	// 只有一个安装/更新操作），不会被第二方删除（conversun/fnos-apps#245）。
	partPath := finalPath + ".part"
	for _, url := range urls {
		if err := d.downloadFromURL(ctx, url, partPath, progress); err != nil {
			lastErr = err
			continue // 不删 .part：下一个镜像 / 重试可从断点继续
		}

		if err := os.Rename(partPath, finalPath); err != nil {
			return "", fmt.Errorf("rename %q to %q: %w", partPath, finalPath, err)
		}
		return finalPath, nil
	}

	if lastErr == nil {
		lastErr = errors.New("download failed")
	}
	return "", lastErr
}

// downloadFromURL 下载 url 到 partPath，支持断点续传（HTTP Range）。
//
// 断点续传语义：
//   - partPath 已有 startBytes 字节 → 发 Range: bytes=startBytes-。
//   - 206（Partial Content）→ 从 startBytes 偏移继续写（WriteAt）。
//   - 200（服务端不支持 Range / 文件已变）→ 截断从头下载。
//   - 416（Range 不满足，.part 已完整或失效）→ 截断从头下载。
//
// 每次写入后刷新 mtime，使 CleanupStaleTmpFiles 的按 mtime 判定不会误删
// 进行中的 .part（活跃下载 mtime 始终新鲜）。
func (d *Downloader) downloadFromURL(ctx context.Context, url, partPath string, progress func(downloaded, total int64)) error {
	// 断点：若 .part 已存在，从断点继续。
	var startBytes int64
	if st, err := os.Stat(partPath); err == nil && st.Size() > 0 {
		startBytes = st.Size()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	if startBytes > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", startBytes))
	}

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var total int64
	writeFlags := os.O_CREATE | os.O_WRONLY
	switch resp.StatusCode {
	case http.StatusPartialContent: // 206 续传：追加到 .part 末尾
		writeFlags |= os.O_APPEND
		// Content-Range: bytes start-end/total
		if cr := resp.Header.Get("Content-Range"); cr != "" {
			if parts := strings.Split(cr, "/"); len(parts) == 2 {
				if t, perr := strconv.ParseInt(parts[1], 10, 64); perr == nil && t > 0 {
					total = t
				}
			}
		}
		if total == 0 && resp.ContentLength > 0 {
			total = startBytes + resp.ContentLength
		}
	case http.StatusOK: // 200 不支持续传或文件已变 → 从头
		startBytes = 0
		writeFlags |= os.O_TRUNC
		total = resp.ContentLength
	case http.StatusRequestedRangeNotSatisfiable: // 416 → 从头
		startBytes = 0
		writeFlags |= os.O_TRUNC
		total = resp.ContentLength
	default:
		return fmt.Errorf("download %q: %s", url, resp.Status)
	}

	f, err := os.OpenFile(partPath, writeFlags, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	buf := make([]byte, 128*1024)
	downloaded := startBytes
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			// O_APPEND：续传追加到末尾 / 新鲜写从头，避免 WriteAt 部分写风险。
			if _, werr := f.Write(buf[:n]); werr != nil {
				return werr
			}
			downloaded += int64(n)
			// 刷新 mtime：防止 stale 清理误删进行中的 .part。
			_ = os.Chtimes(partPath, time.Now(), time.Now())
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

	if err := validateFpk(partPath); err != nil {
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
