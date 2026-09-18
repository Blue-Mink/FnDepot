package core

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fpkBody builds a minimal VALID fpk: a gzip stream wrapping a tar whose
// root carries a manifest entry. The downloader validates archive structure,
// not size — docker-mode fpks legitimately ship under 10 KiB.
func fpkBody(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	hdr := &tar.Header{Name: "manifest", Mode: 0o644, Size: int64(len("appname = demo\n"))}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte("appname = demo\n")); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// serveBody starts a test server that answers every GET with body.
func serveBody(t *testing.T, body []byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func tmpFilesIn(t *testing.T, dir string) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, "*.tmp"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	return matches
}

// The temp file must be UNIQUE per download, not the deterministic
// finalPath+".tmp": any second actor using that path (an OS /tmp reaper, a
// stale-cleanup pass, a retry) could delete the in-flight file out from under
// os.Rename (conversun/fnos-apps#245).
func TestDownloadUsesUniqueTempFile(t *testing.T) {
	// Given a stale file squatting on the old deterministic temp path
	dir := t.TempDir()
	body := fpkBody(t)
	srv := serveBody(t, body)
	d := NewDownloader(dir)
	req := DownloadRequest{URLs: []string{srv.URL}, FileName: "x.fpk", AppName: "app"}
	staleTmp := filepath.Join(dir, "app-x.fpk.tmp")
	if err := os.WriteFile(staleTmp, []byte("someone else's temp file"), 0o644); err != nil {
		t.Fatal(err)
	}

	// When a download runs
	finalPath, err := d.Download(context.Background(), req, nil)

	// Then the payload landed AND the squatting file was never touched
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	got, err := os.ReadFile(finalPath)
	if err != nil {
		t.Fatalf("read final: %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Errorf("final content = %d bytes, want the %d-byte server body", len(got), len(body))
	}
	stale, err := os.ReadFile(staleTmp)
	if err != nil {
		t.Fatalf("deterministic temp path was consumed by the download: %v", err)
	}
	if string(stale) != "someone else's temp file" {
		t.Errorf("stale temp file content = %q, want it untouched", stale)
	}
}

// Two sequential downloads of the same app must both succeed, with the second
// overwriting the first and no temp debris left behind.
func TestSequentialDownloadsDoNotCollide(t *testing.T) {
	// Given two versions of the same fpk served back to back
	dir := t.TempDir()
	first := serveBody(t, fpkBody(t))
	second := serveBody(t, append(fpkBody(t), "v2"...))
	d := NewDownloader(dir)

	// When both are downloaded under the same final name
	req := DownloadRequest{URLs: []string{first.URL}, FileName: "x.fpk", AppName: "app"}
	if _, err := d.Download(context.Background(), req, nil); err != nil {
		t.Fatalf("first Download: %v", err)
	}
	req.URLs = []string{second.URL}
	finalPath, err := d.Download(context.Background(), req, nil)
	if err != nil {
		t.Fatalf("second Download: %v", err)
	}

	// Then the final file holds the second payload and no temp files remain
	got, _ := os.ReadFile(finalPath)
	if !bytes.HasSuffix(got, []byte("v2")) {
		t.Errorf("final file does not hold the second download's payload")
	}
	if left := tmpFilesIn(t, dir); len(left) != 0 {
		t.Errorf("temp files left behind: %v", left)
	}
}

// Walking the mirror list must not leak a temp file per failed URL.
func TestDownloadFailureLeavesNoTmpFiles(t *testing.T) {
	// Given two URLs that both fail (one 500, one serving a corrupt tiny body)
	dir := t.TempDir()
	fail := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(fail.Close)
	tiny := serveBody(t, []byte("too small"))
	d := NewDownloader(dir)

	// When the download walks both and gives up
	_, err := d.Download(context.Background(), DownloadRequest{
		URLs: []string{fail.URL, tiny.URL}, FileName: "x.fpk", AppName: "app",
	}, nil)

	// Then it failed and left no temp debris from either attempt
	if err == nil {
		t.Fatal("Download succeeded, want failure")
	}
	if left := tmpFilesIn(t, dir); len(left) != 0 {
		t.Errorf("temp files left behind: %v", left)
	}
}

// CleanupStaleTmpFiles runs while downloads may be in flight, so it must only
// reap files old enough to be genuinely abandoned — never a fresh temp file
// an active download is writing (conversun/fnos-apps#245).
func TestCleanupStaleTmpFilesKeepsFreshFiles(t *testing.T) {
	// Given a fresh in-flight temp file and an abandoned hour-old-plus one
	dir := t.TempDir()
	d := NewDownloader(dir)
	fresh := filepath.Join(dir, "app-a.fpk.tmp")
	stale := filepath.Join(dir, "app-b.fpk.tmp")
	for _, p := range []string{fresh, stale} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}

	// When cleanup runs
	if err := d.CleanupStaleTmpFiles(); err != nil {
		t.Fatalf("CleanupStaleTmpFiles: %v", err)
	}

	// Then the fresh file survived and the stale one was reaped
	if _, err := os.Stat(fresh); err != nil {
		t.Errorf("fresh in-flight temp file was deleted: %v", err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("stale temp file survived cleanup")
	}
}

// A mirror or portal can answer 200 with a full-size HTML error page. Size
// alone must not pass as an fpk — the archive must actually be one
// (conversun/fnos-apps#284 was the flip side: a valid 8483-byte docker fpk
// rejected for being "too small").
func TestDownloadRejectsErrorPageDespiteSize(t *testing.T) {
	// Given an 11 KiB HTML body — comfortably past the old 10 KiB floor
	dir := t.TempDir()
	page := bytes.Repeat([]byte("<html><body>upstream rate limited, try later</body></html>\n"), 200)
	if len(page) <= 10*1024 {
		t.Fatalf("test body must exceed the old size floor, got %d", len(page))
	}
	srv := serveBody(t, page)
	d := NewDownloader(dir)

	// When it is downloaded
	_, err := d.Download(context.Background(), DownloadRequest{
		URLs: []string{srv.URL}, FileName: "x.fpk", AppName: "app",
	}, nil)

	// Then the non-archive body is rejected
	if err == nil {
		t.Fatal("Download accepted an HTML error page, want rejection")
	}
}

// Docker-mode fpks carry no binaries and legitimately land under the old
// 10 KiB floor; a structurally valid archive must be accepted.
func TestDownloadAcceptsSmallValidFpk(t *testing.T) {
	dir := t.TempDir()
	srv := serveBody(t, fpkBody(t)) // ~200 bytes
	d := NewDownloader(dir)

	finalPath, err := d.Download(context.Background(), DownloadRequest{
		URLs: []string{srv.URL}, FileName: "x.fpk", AppName: "app",
	}, nil)
	if err != nil {
		t.Fatalf("Download rejected a valid small fpk: %v", err)
	}
	if _, err := os.Stat(finalPath); err != nil {
		t.Errorf("final fpk missing: %v", err)
	}
}

// A corrupt mirror first, GitHub direct second: the failure on URL #1 must
// not stop the fallback from being tried and succeeding.
func TestDownloadFallsBackAfterCorruptBody(t *testing.T) {
	dir := t.TempDir()
	corrupt := serveBody(t, bytes.Repeat([]byte("not gzip at all"), 1024))
	good := serveBody(t, fpkBody(t))
	d := NewDownloader(dir)

	finalPath, err := d.Download(context.Background(), DownloadRequest{
		URLs: []string{corrupt.URL, good.URL}, FileName: "x.fpk", AppName: "app",
	}, nil)
	if err != nil {
		t.Fatalf("Download did not fall back to the good URL: %v", err)
	}
	if got, _ := os.ReadFile(finalPath); len(got) == 0 {
		t.Error("final fpk is empty")
	}
}
