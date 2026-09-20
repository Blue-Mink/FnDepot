package cache

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// ProbeEntry 记录一次文件元数据探测结果（下载链接 → Last-Modified/大小）。
// 以 URL 为键：源发布新版本时 URL 变化，旧条目自动失效。
type ProbeEntry struct {
	Date     string    `json:"date,omitempty"` // RFC3339，来自 Last-Modified；size-only 探测时为空
	Size     int64     `json:"size,omitempty"`
	ProbedAt time.Time `json:"probed_at"`
}

const probeCacheFile = "meta_probe.json"

// 超过 30 天的条目在保存时清理（防文件无限增长；7 天 TTL 由 api 层判断）。
const probeEntryMaxAge = 30 * 24 * time.Hour

func (s *Store) probePath() string {
	return filepath.Join(s.cacheDir, probeCacheFile)
}

// LoadProbeCache 读取探测缓存；文件缺失或损坏时返回空 map。
func (s *Store) LoadProbeCache() map[string]ProbeEntry {
	s.mu.RLock()
	cacheDir := s.cacheDir
	s.mu.RUnlock()

	raw, err := os.ReadFile(filepath.Join(cacheDir, probeCacheFile))
	if err != nil {
		return map[string]ProbeEntry{}
	}
	m := map[string]ProbeEntry{}
	if err := json.Unmarshal(raw, &m); err != nil {
		return map[string]ProbeEntry{}
	}
	return m
}

// SaveProbeCache 写回探测缓存，顺带清理过期条目。
func (s *Store) SaveProbeCache(m map[string]ProbeEntry) error {
	s.mu.Lock()
	cacheDir := s.cacheDir
	s.mu.Unlock()

	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return err
	}
	now := time.Now()
	for k, e := range m {
		if now.Sub(e.ProbedAt) > probeEntryMaxAge {
			delete(m, k)
		}
	}
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(cacheDir, probeCacheFile+".tmp")
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(cacheDir, probeCacheFile))
}
