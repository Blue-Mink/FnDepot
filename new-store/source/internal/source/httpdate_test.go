package source

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestParseLastModifiedRFC1123(t *testing.T) {
	got, ok := ParseLastModified("Wed, 16 Sep 2026 08:28:01 GMT")
	if !ok {
		t.Fatal("ParseLastModified failed on RFC1123 value")
	}
	want := time.Date(2026, 9, 16, 8, 28, 1, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestParseLastModifiedWallClockFallback(t *testing.T) {
	got, ok := ParseLastModified("2026-09-16 16:28:01")
	if !ok {
		t.Fatal("ParseLastModified failed on wall-clock value")
	}
	// 墙钟格式按 +08:00 理解
	if h, _, _ := got.Clock(); h != 16 || got.Hour() != 16 {
		t.Errorf("expected 16:xx wall clock, got %v", got)
	}
}

func TestParseLastModifiedGarbage(t *testing.T) {
	if _, ok := ParseLastModified(""); ok {
		t.Error("empty value must not parse")
	}
	if _, ok := ParseLastModified("not-a-date"); ok {
		t.Error("garbage value must not parse")
	}
}

// HEAD 200 + Last-Modified + Content-Length 全部返回。
func TestProbeFileMetaHeadSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			t.Errorf("expected HEAD, got %s", r.Method)
		}
		w.Header().Set("Last-Modified", "Wed, 16 Sep 2026 08:28:01 GMT")
		w.Header().Set("Content-Length", "12345")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	modTime, size, ok := ProbeFileMeta(context.Background(), srv.Client(), srv.URL, []string{srv.URL})
	if !ok {
		t.Fatal("probe failed")
	}
	if modTime.IsZero() {
		t.Error("missing Last-Modified")
	}
	if size != 12345 {
		t.Errorf("size = %d, want 12345", size)
	}
}

// 405 降级 GET Range 重试。
func TestProbeFileMetaFallsBackToGetOn405(t *testing.T) {
	var gotGet bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			http.Error(w, "no", http.StatusMethodNotAllowed)
			return
		}
		gotGet = true
		w.Header().Set("Last-Modified", "Wed, 16 Sep 2026 08:28:01 GMT")
		w.Header().Set("Content-Length", "7")
		w.WriteHeader(http.StatusPartialContent)
	}))
	defer srv.Close()

	if _, _, ok := ProbeFileMeta(context.Background(), srv.Client(), srv.URL, []string{srv.URL}); !ok {
		t.Fatal("probe failed after 405")
	}
	if !gotGet {
		t.Error("expected GET fallback after 405")
	}
}

// 第一个候选失败（404）→ 第二个候选（镜像）命中。
func TestProbeFileMetaTriesCandidatesInOrder(t *testing.T) {
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Last-Modified", "Wed, 16 Sep 2026 08:28:01 GMT")
		w.WriteHeader(http.StatusOK)
	}))
	defer good.Close()

	_, _, ok := ProbeFileMeta(context.Background(), nil, good.URL, []string{"http://127.0.0.1:1/none", good.URL})
	if !ok {
		t.Fatal("probe should have succeeded on the second candidate")
	}
}

// 全部候选 404 → ok=false（失败不缓存，由调用方下次重试）。
func TestProbeFileMetaAllFail(t *testing.T) {
	_, _, ok := ProbeFileMeta(context.Background(), nil, "http://127.0.0.1:1/none", []string{"http://127.0.0.1:1/none"})
	if ok {
		t.Fatal("probe must fail when every candidate 404s")
	}
}

// 无 Last-Modified 但有 Content-Length → 只补大小（date 为零值）。
func TestProbeFileMetaSizeOnly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "42")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	modTime, size, ok := ProbeFileMeta(context.Background(), srv.Client(), srv.URL, []string{srv.URL})
	if !ok {
		t.Fatal("size-only probe must still report ok")
	}
	if !modTime.IsZero() {
		t.Errorf("expected zero time, got %v", modTime)
	}
	if size != 42 {
		t.Errorf("size = %d, want 42", size)
	}
}

func TestGithubMetaURLsParsing(t *testing.T) {
	primary, fallback, ok := githubMetaURLs("https://raw.githubusercontent.com/hbestm/FnDepot/main/app2docker/app2docker.fpk")
	if !ok {
		t.Fatal("raw.githubusercontent must be recognized")
	}
	wantPrimary := "https://github.com/hbestm/FnDepot/commits/main/app2docker/app2docker.fpk.atom"
	if primary != wantPrimary {
		t.Errorf("primary = %q, want %q (Atom feed 优先)", primary, wantPrimary)
	}
	if fallback != "https://api.github.com/repos/hbestm/FnDepot/contents/app2docker/app2docker.fpk?ref=main" {
		t.Errorf("fallback = %q (contents API)", fallback)
	}

	primary, fallback, ok = githubMetaURLs("https://github.com/hbestm/FnDepot/raw/main/app2docker/app2docker.fpk")
	if !ok || primary != wantPrimary || fallback == "" {
		t.Errorf("github.com/raw 形解析错误: %q %q ok=%v", primary, fallback, ok)
	}

	primary, fallback, ok = githubMetaURLs("https://github.com/Blue-Mink/FnDepot/releases/download/v0.8.0/kspeeder-0.8.0-x86.fpk")
	if !ok || primary != "https://api.github.com/repos/Blue-Mink/FnDepot/releases/tags/v0.8.0" || fallback != "" {
		t.Errorf("release 资产解析错误: %q %q ok=%v", primary, fallback, ok)
	}

	if _, _, ok = githubMetaURLs("https://fndesk.imcq.top/FPK/x.fpk"); ok {
		t.Error("非 GitHub 直链必须返回 false")
	}
	if _, _, ok = githubMetaURLs("https://ghproxy.example.com/https://github.com/a/b/raw/main/x.fpk"); ok {
		t.Error("镜像前缀包裹的 URL 不是规范形（调用方传 canonical）")
	}
}

// Atom feed 解析（主通道，无限流）。
func TestProbeMetaURLAtomFeed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <updated>2025-12-31T02:28:56Z</updated>
  <entry>
    <id>x1</id>
    <updated>2026-09-16T16:28:01Z</updated>
    <title>fix</title>
  </entry>
  <entry>
    <id>x0</id>
    <updated>2025-12-31T02:28:56Z</updated>
    <title>init</title>
  </entry>
</feed>`)
	}))
	defer srv.Close()
	got, ok := probeMetaURL(context.Background(), srv.Client(), srv.URL+"/commits/main/x.fpk.atom", true)
	if !ok {
		t.Fatal("atom feed 解析失败")
	}
	// 取第一条 entry 的 updated（不是 feed 级的）
	want := time.Date(2026, 9, 16, 16, 28, 1, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// GitHub API JSON：contents 的 last_modified / releases 的 published_at。
func TestProbeMetaURLGitHubAPI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/releases" {
			fmt.Fprint(w, `{"tag_name":"v0.8.0","published_at":"2026-09-10T02:00:00Z"}`)
			return
		}
		fmt.Fprint(w, `{"name":"x.fpk","last_modified":"2026-09-16T16:28:01Z"}`)
	}))
	defer srv.Close()
	got, ok := probeMetaURL(context.Background(), srv.Client(), srv.URL+"/contents", false)
	if !ok {
		t.Fatal("contents JSON 解析失败")
	}
	if !got.Equal(time.Date(2026, 9, 16, 16, 28, 1, 0, time.UTC)) {
		t.Errorf("got %v", got)
	}
	got, ok = probeMetaURL(context.Background(), srv.Client(), srv.URL+"/releases", false)
	if !ok {
		t.Fatal("releases JSON 解析失败")
	}
	if !got.Equal(time.Date(2026, 9, 10, 2, 0, 0, 0, time.UTC)) {
		t.Errorf("got %v", got)
	}
}

// 限流（403）→ 失败，由调用方决定是否重试。
func TestProbeMetaURLRateLimited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"message":"API rate limit exceeded"}`)
	}))
	defer srv.Close()
	if _, ok := probeMetaURL(context.Background(), srv.Client(), srv.URL+"/x", false); ok {
		t.Fatal("403 must not count as success")
	}
}

// 206 + Content-Range → 全量大小（不是分片长度 1）。
func TestProbeFileMetaPartialContentSize(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Last-Modified", "Wed, 16 Sep 2026 08:28:01 GMT")
		w.Header().Set("Content-Range", "bytes 0-0/122243")
		w.Header().Set("Content-Length", "1")
		w.WriteHeader(http.StatusPartialContent)
	}))
	defer srv.Close()
	_, size, ok := ProbeFileMeta(context.Background(), srv.Client(), srv.URL, []string{srv.URL})
	if !ok || size != 122243 {
		t.Errorf("size = %d ok=%v, want 122243（从 Content-Range 取全量）", size, ok)
	}
}
