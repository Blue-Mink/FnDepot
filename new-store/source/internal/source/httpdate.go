package source

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// github 文件/资产 URL 的日期探测走 GitHub API（比 HEAD 更准）：
//   - raw 文件（raw.githubusercontent.com 或 github.com/.../raw/）
//     → contents API 的 last_modified = 该文件最后一次提交的真实时间
//   - release 资产（github.com/.../releases/download/<tag>/...）
//     → releases API 的 published_at = release 发布时间
//
// 原因：GitHub raw 的 HEAD 响应没有 Last-Modified 头（只有 etag），
// 公共镜像还会剥掉头 —— 实测 2026-09-18 测试机 34 条 github 链接
// HEAD 全部拿不到日期。API 未授权限流 60 次/小时/IP：探测结果按 URL
// 缓存 7 天（调用方负责），首跑约 30 余次，后续增量极少。
var (
	githubRawRE    = regexp.MustCompile(`^https?://raw\.githubusercontent\.com/([^/]+)/([^/]+)/([^/]+)/(.+)$`)
	githubRawGitRE = regexp.MustCompile(`^https?://github\.com/([^/]+)/([^/]+)/raw/([^/]+)/(.+)$`)
	githubRelRE    = regexp.MustCompile(`^https?://github\.com/([^/]+)/([^/]+)/releases/download/([^/]+)/(.+)$`)
)

// IsGitHubFileURL 报告该下载链接是否指向 GitHub 仓库文件/release 资产
// （即存在可查询日期的端点）。
func IsGitHubFileURL(rawURL string) bool {
	_, _, ok := githubMetaURLs(rawURL)
	return ok
}

// escapePath 逐段转义，保留分隔符斜杠（contents API 需要）。
func escapePath(p string) string {
	segs := strings.Split(p, "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	return strings.Join(segs, "/")
}

// githubMetaURLs 把 GitHub 文件/资产 URL 翻译为可查日期的地址（主 + 兜底）：
//   - raw 文件：主 = 网页 Atom feed（/commits/<branch>/<path>.atom，第一条
//     entry 的 <updated> = 该文件最后一次提交时间；网页端，不受 API 限流——
//     共享 NAT 出口的未授权 API 配额（60/h）经常被挤占，实测 2026-09-18
//     测试机出口 182.91.126.84 整点 403）。兜底 = contents API。
//   - release 资产：只有 releases API（published_at）可用。
//
// 非 GitHub 文件 URL 返回 ok=false。
func githubMetaURLs(rawURL string) (primary, fallback string, ok bool) {
	if m := githubRawRE.FindStringSubmatch(rawURL); m != nil {
		owner, repo, branch, p := m[1], m[2], m[3], m[4]
		return fmt.Sprintf("https://github.com/%s/%s/commits/%s/%s.atom",
			owner, repo, url.PathEscape(branch), escapePath(p)),
			fmt.Sprintf("https://api.github.com/repos/%s/%s/contents/%s?ref=%s",
				owner, repo, escapePath(p), url.QueryEscape(branch)), true
	}
	if m := githubRawGitRE.FindStringSubmatch(rawURL); m != nil {
		owner, repo, branch, p := m[1], m[2], m[3], m[4]
		return fmt.Sprintf("https://github.com/%s/%s/commits/%s/%s.atom",
			owner, repo, url.PathEscape(branch), escapePath(p)),
			fmt.Sprintf("https://api.github.com/repos/%s/%s/contents/%s?ref=%s",
				owner, repo, escapePath(p), url.QueryEscape(branch)), true
	}
	if m := githubRelRE.FindStringSubmatch(rawURL); m != nil {
		owner, repo, tag := m[1], m[2], m[3]
		return fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/tags/%s",
			owner, repo, url.PathEscape(tag)), "", true
	}
	return "", "", false
}

// (?s) 让 .*? 跨行（Atom XML 每个标签独占一行）。
var atomEntryUpdatedRE = regexp.MustCompile(`(?s)<entry>.*?<updated>([^<]+)</updated>`)

// probeMetaURL 请求一个日期端点，兼容 Atom feed（<entry><updated>）与
// GitHub API JSON（last_modified/published_at）。
func probeMetaURL(ctx context.Context, client *http.Client, metaURL string, browserLike bool) (time.Time, bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, metaURL, nil)
	if err != nil {
		return time.Time{}, false
	}
	if browserLike {
		req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) fnos-store/1.x")
	} else {
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("User-Agent", "fnos-store/1.x")
	}
	resp, err := client.Do(req)
	if err != nil {
		return time.Time{}, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return time.Time{}, false // 403/429 限流按失败处理，下轮重试
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 256<<10))
	if err != nil {
		return time.Time{}, false
	}
	// Atom feed：第一条 entry 的 <updated>
	if m := atomEntryUpdatedRE.FindSubmatch(body); m != nil {
		if t, perr := time.Parse(time.RFC3339, strings.TrimSpace(string(m[1]))); perr == nil {
			return t, true
		}
	}
	// GitHub API JSON
	var meta struct {
		LastModified string `json:"last_modified"`
		PublishedAt  string `json:"published_at"`
	}
	if err := json.Unmarshal(body, &meta); err == nil {
		raw := meta.LastModified
		if raw == "" {
			raw = meta.PublishedAt
		}
		if raw != "" {
			if t, perr := time.Parse(time.RFC3339, raw); perr == nil {
				return t, true
			}
		}
	}
	return time.Time{}, false
}

// probeGithubMeta 按 主→兜底 顺序取日期。
func probeGithubMeta(ctx context.Context, client *http.Client, rawURL string) (time.Time, bool) {
	primary, fallback, ok := githubMetaURLs(rawURL)
	if !ok {
		return time.Time{}, false
	}
	if t, ok2 := probeMetaURL(ctx, client, primary, true); ok2 {
		return t, true
	}
	if fallback != "" {
		if t, ok2 := probeMetaURL(ctx, client, fallback, false); ok2 {
			return t, true
		}
	}
	return time.Time{}, false
}

// lastModifiedRE 兜底解析非 RFC1123 的 Last-Modified（部分 CDN 直接给
// "2006-01-02 15:04:05" 墙钟格式，时区未知，按 +08:00 理解，与
// DateFromReleaseURL 的墙钟惯例一致）。
var lastModifiedFallbackFormats = []string{
	time.RFC1123,
	time.RFC1123Z,
	"2006-01-02 15:04:05",
	"2006-01-02T15:04:05",
	"2006-01-02",
}

// ParseLastModified 解析 Last-Modified 响应头为 UTC 时间。
// 非 RFC1123 的墙钟格式按 +08:00 理解（国内源/CDN 惯例）。
func ParseLastModified(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false
	}
	if t, err := time.Parse(time.RFC1123, value); err == nil {
		return t.UTC(), true
	}
	loc := time.FixedZone("CST", 8*3600)
	for _, layout := range lastModifiedFallbackFormats {
		if t, err := time.ParseInLocation(layout, value, loc); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// ProbeFileMeta 探测下载链接的文件元数据。
//   - GitHub 文件/release 链接：先走 GitHub API（last_modified/published_at，
//     精确时间；raw HEAD 没有 Last-Modified，镜像还会剥头）。
//   - 其他：按顺序对候选 URL 发 HEAD，取 Last-Modified + Content-Length。
//
// size 可为 0（服务端未提供）。全部失败返回 ok=false。
func ProbeFileMeta(ctx context.Context, client *http.Client, canonical string, candidates []string) (modTime time.Time, size int64, ok bool) {
	reqClient := client
	if reqClient == nil {
		reqClient = &http.Client{Timeout: 4 * time.Second}
	}
	if IsGitHubFileURL(canonical) {
		if t, ok2 := probeGithubMeta(ctx, reqClient, canonical); ok2 {
			return t, 0, true
		}
		// Atom/API 失败（限流/网络）→ 继续 HEAD 兜底（至少能拿大小）
	}
	for _, u := range candidates {
		if u == "" {
			continue
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodHead, u, nil)
		if err != nil {
			continue
		}
		// 部分 CDN 对 HEAD 返回 405；GET+Range:0-0 语义等价且更稳。
		req.Header.Set("Range", "bytes=0-0")
		req.Header.Set("Accept", "*/*")
		resp, err := reqClient.Do(req)
		if err != nil {
			continue
		}
		// 405 Method Not Allowed → 降级 GET Range 重试一次
		if resp.StatusCode == http.StatusMethodNotAllowed {
			resp.Body.Close()
			greq, gerr := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
			if gerr != nil {
				continue
			}
			greq.Header.Set("Range", "bytes=0-0")
			gresp, gerr := reqClient.Do(greq)
			if gerr != nil {
				continue
			}
			resp, err = gresp, nil
		}
		code := resp.StatusCode
		// Range 命中=206；全量=200；两者都可读元数据头。
		if code != http.StatusOK && code != http.StatusPartialContent {
			resp.Body.Close()
			continue
		}
		modStr := resp.Header.Get("Last-Modified")
		sizeStr := resp.Header.Get("Content-Length")
		// Range 请求命中 206 时 Content-Length 是分片长度（1 字节），
		// 全量大小在 Content-Range: bytes 0-0/TOTAL 的 TOTAL 里。
		if code == http.StatusPartialContent {
			if cr := resp.Header.Get("Content-Range"); cr != "" {
				if i := strings.LastIndex(cr, "/"); i >= 0 && cr[i+1:] != "*" {
					sizeStr = cr[i+1:]
				}
			}
		}
		var size int64
		if sizeStr != "" {
			size, _ = strconv.ParseInt(sizeStr, 10, 64)
		}
		resp.Body.Close()
		if modStr != "" {
			if t, ok2 := ParseLastModified(modStr); ok2 {
				return t, size, true
			}
			// 有 Last-Modified 但解析失败：退回 size-only 不算成功
			// （没有日期就没有本次调用的主要价值）
			continue
		}
		// 无 Last-Modified：仍返回 size（调用方可只补大小）
		return time.Time{}, size, size > 0
	}
	return time.Time{}, 0, false
}
