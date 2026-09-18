package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"fnos-store/internal/cache"
	"fnos-store/internal/config"
	"fnos-store/internal/source"
)

// SourceEntry 是外部源管理视图（列表 API 返回项）。
type SourceEntry struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	URL         string    `json:"url"`
	Author      string    `json:"author,omitempty"`
	Homepage    string    `json:"homepage,omitempty"`
	AppCount    int       `json:"app_count"`
	Error       string    `json:"error,omitempty"`
	LastFetched time.Time `json:"last_fetched,omitempty"`
}

func (s *Server) handleListSources(w http.ResponseWriter, r *http.Request) {
	entries := s.ListSources()
	writeJSON(w, http.StatusOK, map[string]any{"sources": entries})
}

type addSourceRequest struct {
	URL  string `json:"url"`
	Name string `json:"name"`
}

// sourcesMu 串行化「读配置→追加源→保存」，批量并发加源时防止丢失更新。
var sourcesMu sync.Mutex

// addOneSource 添加单个外部源的核心逻辑：验证 + 去重 + 持久化。
// ctx 约束验证阶段的抓取（批量同步源列表时按源设超时）。
func (s *Server) addOneSource(ctx context.Context, rawURL, name string) (config.CustomSource, bool, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return config.CustomSource{}, false, fmt.Errorf("源地址不能为空")
	}

	// 立即验证：可抓取且是 FnDepot V1/V2 结构（可能需数秒，含 GitHub 仓库解析与镜像回退）
	cs, err := source.NewFNDepotSourceCtx(ctx, rawURL, s.configMgr)
	if err != nil {
		return config.CustomSource{}, false, err
	}

	name = strings.TrimSpace(name)
	if name == "" {
		name = cs.Meta().Name // source_info.name → 仓库 owner → 仓库名
	}

	entry := config.CustomSource{ID: cs.ID(), Name: name, URL: cs.Meta().URL}
	sourcesMu.Lock()
	defer sourcesMu.Unlock()
	cfg := s.configMgr.Get()
	for _, existing := range cfg.Sources {
		if strings.EqualFold(strings.TrimSpace(existing.URL), entry.URL) || existing.ID == entry.ID {
			return entry, false, fmt.Errorf("该应用源已添加")
		}
	}
	cfg.Sources = append(cfg.Sources, entry)
	if err := s.configMgr.SaveConfig(cfg); err != nil {
		return config.CustomSource{}, false, err
	}
	s.rebuildCustomSources()
	return entry, true, nil
}

// handleAddSource 添加外部应用源：立即抓取+解析验证，成功后持久化并刷新注册表。
func (s *Server) handleAddSource(w http.ResponseWriter, r *http.Request) {
	if s.configMgr == nil {
		writeAPIError(w, http.StatusInternalServerError, "config not available")
		return
	}
	var req addSourceRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid json")
		return
	}

	entry, ok, err := s.addOneSource(r.Context(), req.URL, req.Name)
	if !ok {
		if strings.Contains(err.Error(), "已添加") {
			writeAPIError(w, http.StatusConflict, err.Error())
		} else {
			writeAPIError(w, http.StatusUnprocessableEntity, err.Error())
		}
		return
	}
	// 后台刷新注册表（新源的应用并入目录），不阻塞响应。
	go s.refreshRegistryDebounced(context.Background())

	writeJSON(w, http.StatusOK, map[string]any{"source": SourceEntry{
		ID:   entry.ID,
		Name: entry.Name,
		URL:  entry.URL,
	}})
}

type addSourceBatchRequest struct {
	Items []addSourceRequest `json:"items"`
}

type batchSourceResult struct {
	URL   string `json:"url"`
	OK    bool   `json:"ok"`
	Name  string `json:"name,omitempty"`
	Error string `json:"error,omitempty"`
}

// handleBatchAddSources 批量添加外部源（前端多行输入框一次提交）。
// 逐条验证与去重；单条失败不影响其他条。
func (s *Server) handleBatchAddSources(w http.ResponseWriter, r *http.Request) {
	if s.configMgr == nil {
		writeAPIError(w, http.StatusInternalServerError, "config not available")
		return
	}
	var req addSourceBatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if len(req.Items) == 0 {
		writeAPIError(w, http.StatusBadRequest, "items 不能为空")
		return
	}
	if len(req.Items) > 150 {
		writeAPIError(w, http.StatusRequestEntityTooLarge, "单次最多添加 150 个源")
		return
	}

	results := make([]batchSourceResult, 0, len(req.Items))
	added := 0
	for _, item := range req.Items {
		if strings.TrimSpace(item.URL) == "" {
			continue // 跳过空行
		}
		entry, ok, err := s.addOneSource(r.Context(), item.URL, item.Name)
		if ok {
			added++
			results = append(results, batchSourceResult{URL: strings.TrimSpace(item.URL), OK: true, Name: entry.Name})
		} else {
			results = append(results, batchSourceResult{URL: strings.TrimSpace(item.URL), Error: err.Error()})
		}
	}

	if added > 0 {
		go s.refreshRegistryDebounced(context.Background())
	}
	writeJSON(w, http.StatusOK, map[string]any{"added": added, "results": results})
}

// handleRemoveSource 删除外部应用源并刷新注册表。
func (s *Server) handleRemoveSource(w http.ResponseWriter, r *http.Request) {
	if s.configMgr == nil {
		writeAPIError(w, http.StatusInternalServerError, "config not available")
		return
	}
	id := r.PathValue("id")
	if id == "" {
		writeAPIError(w, http.StatusBadRequest, "缺少源 ID")
		return
	}

	cfg := s.configMgr.Get()
	kept := make([]config.CustomSource, 0, len(cfg.Sources))
	removed := false
	for _, entry := range cfg.Sources {
		if entry.ID == id {
			removed = true
			continue
		}
		kept = append(kept, entry)
	}
	cfg.Sources = kept
	if !removed {
		writeAPIError(w, http.StatusNotFound, "应用源不存在")
		return
	}
	if err := s.configMgr.SaveConfig(cfg); err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.rebuildCustomSources()
	go s.refreshRegistryDebounced(context.Background())
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleSyncSource 手动同步单个外部源：立即抓取全部目录（内置+外部源），
// 返回该源最新的应用数与状态。
func (s *Server) handleSyncSource(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeAPIError(w, http.StatusBadRequest, "缺少源 ID")
		return
	}
	cfg := s.configMgr.Get()
	found := false
	for _, entry := range cfg.Sources {
		if entry.ID == id {
			found = true
			break
		}
	}
	if !found {
		writeAPIError(w, http.StatusNotFound, "应用源不存在")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 180*time.Second)
	defer cancel()
	_ = s.refreshRegistry(ctx)

	for _, e := range s.ListSources() {
		if e.ID == id {
			writeJSON(w, http.StatusOK, map[string]any{"source": e})
			return
		}
	}
	writeAPIError(w, http.StatusInternalServerError, "同步完成但未找到源状态")
}

// ── 内置应用源列表自动同步 ────────────────────────────────────────────────
// 从社区源列表（每行一个 GitHub 仓库地址）自动发现并添加新的 FnDepot 应用源，
// 内置 710850609/FnDepot 的 repo_list.txt，可在设置里换地址或整体关闭。

type sourceListSyncResult struct {
	Fetched    int      `json:"fetched"`                 // 列表条目数
	Already    int      `json:"already"`                 // 已添加过
	Added      int      `json:"added"`                   // 本次新增
	Failed     int      `json:"failed"`                  // 校验失败/超时
	AddedNames []string `json:"added_names,omitempty"`
	Errors     []string `json:"errors,omitempty"`
}

var sourceListMu sync.Mutex // 防并发的源列表同步互相踩

// parseSourceList 解析源列表文本：每行一个 URL，支持 # 注释与行内注释。
func parseSourceList(body string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if i := strings.IndexAny(line, " \t"); i > 0 {
			line = strings.TrimSpace(line[:i])
		}
		line = strings.TrimSuffix(line, "/")
		if !strings.HasPrefix(line, "http://") && !strings.HasPrefix(line, "https://") {
			continue
		}
		key := strings.ToLower(line)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, line)
	}
	return out
}

// fetchSourceList 抓取源列表文本；GitHub raw 地址走与下载一致的镜像链。
func (s *Server) fetchSourceList(ctx context.Context, cfg config.Config) (string, error) {
	listURL := strings.TrimSpace(cfg.SourceListURL)
	if listURL == "" {
		listURL = config.DefaultSourceListURL
	}
	candidates := []string{listURL}
	if strings.Contains(listURL, "raw.githubusercontent.com") {
		for _, prefix := range config.GitHubFallbackPrefixes(cfg.Mirror, cfg) {
			if prefix != "" {
				candidates = append(candidates, prefix+listURL)
			}
		}
	}
	client := &http.Client{}
	var lastErr error
	for _, c := range candidates {
		cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		req, err := http.NewRequestWithContext(cctx, "GET", c, nil)
		if err == nil {
			req.Header.Set("User-Agent", "fnos-store/1.x")
			resp, doErr := client.Do(req)
			if doErr == nil {
				body, rerr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
				resp.Body.Close()
				if rerr == nil && resp.StatusCode == http.StatusOK {
					cancel()
					return string(body), nil
				}
				lastErr = fmt.Errorf("HTTP %s", resp.Status)
				if rerr != nil {
					lastErr = rerr
				}
			} else {
				lastErr = doErr
			}
		}
		cancel()
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
	}
	return "", fmt.Errorf("源列表获取失败: %w", lastErr)
}

// syncSourceList 抓取源列表并自动添加新源。
// refreshOnAdd=false 供定时调度内部调用（外层 refreshRegistry 会抓取新源）；
// 手动触发传 true（添加后立刻刷新目录）。
func (s *Server) syncSourceList(ctx context.Context, refreshOnAdd bool) sourceListSyncResult {
	sourceListMu.Lock()
	defer sourceListMu.Unlock()

	var res sourceListSyncResult
	cfg := s.configMgr.Get()
	body, err := s.fetchSourceList(ctx, cfg)
	if err != nil {
		res.Errors = append(res.Errors, err.Error())
		return res
	}
	urls := parseSourceList(body)
	res.Fetched = len(urls)

	cfg = s.configMgr.Get()
	var newURLs []string
	for _, u := range urls {
		dup := false
		for _, e := range cfg.Sources {
			if strings.EqualFold(strings.TrimSuffix(strings.TrimSpace(e.URL), "/"), u) {
				dup = true
				break
			}
		}
		if dup {
			res.Already++
		} else {
			newURLs = append(newURLs, u)
		}
	}

	// 并发校验+添加：并发 4、单源预算 20s（GitHub raw 404 秒回，20s 足够
	// 覆盖镜像链；60s 会让 ~110 个死链仓库耗时 27 分钟并阻塞整个目录
	// 检查，2026-09-18 测试机实测）。TTL 内验证失败过的仓库直接跳过。
	var verifyFails map[string]time.Time
	if s.cacheStore != nil {
		verifyFails = s.cacheStore.LoadVerifyFails()
	}
	now := time.Now()
	var wg sync.WaitGroup
	var mu sync.Mutex
	sem := make(chan struct{}, 4)
	for _, u := range newURLs {
		if verifyFails != nil && cache.VerifyFailSkip(verifyFails, u, now) {
			res.Already++ // 近期失败过：跳过（不计错误，防 UI 刷屏）
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(u string) {
			defer wg.Done()
			defer func() { <-sem }()
			sctx, cancel := context.WithTimeout(ctx, 20*time.Second)
			defer cancel()
			entry, ok, addErr := s.addOneSource(sctx, u, "")
			mu.Lock()
			defer mu.Unlock()
			switch {
			case ok:
				res.Added++
				res.AddedNames = append(res.AddedNames, entry.Name)
			case addErr != nil && strings.Contains(addErr.Error(), "已添加"):
				res.Already++
			default:
				res.Failed++
				msg := ""
				if addErr != nil {
					msg = addErr.Error()
				}
				res.Errors = append(res.Errors, u+": "+msg)
				if s.cacheStore != nil {
					s.cacheStore.RecordVerifyFail(u)
				}
			}
		}(u)
	}
	wg.Wait()

	if res.Added > 0 {
		log.Printf("source list: 自动新增 %d 个应用源: %v", res.Added, res.AddedNames)
		if refreshOnAdd {
			_ = s.refreshRegistry(ctx)
		}
	}
	return res
}

// handleSyncSourceList 手动触发源列表同步（立即发现+添加新源）。
func (s *Server) handleSyncSourceList(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Minute)
	defer cancel()
	res := s.syncSourceList(ctx, true)
	writeJSON(w, http.StatusOK, res)
}
