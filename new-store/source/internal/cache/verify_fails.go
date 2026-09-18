package cache

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// 源列表自动同步时，死链/非 FnDepot 仓库的验证会吃光单源预算。
// 记录验证失败时间，skipTTL 内不再重复验证（实测首跑要过 ~110 个
// 无效仓库，60s/源 × 并发 4 ≈ 27 分钟，阻塞整个目录检查）。
const verifyFailSkipTTL = 6 * time.Hour

const verifyFailsFile = "verify_fails.json"

// verifyFailMaxAge 是保存时清理的保留上限。
const verifyFailMaxAge = 24 * time.Hour

// LoadVerifyFails 读取验证失败记忆（url -> 最近一次失败时间）。
func (s *Store) LoadVerifyFails() map[string]time.Time {
	s.mu.RLock()
	cacheDir := s.cacheDir
	s.mu.RUnlock()

	raw, err := os.ReadFile(filepath.Join(cacheDir, verifyFailsFile))
	if err != nil {
		return map[string]time.Time{}
	}
	m := map[string]time.Time{}
	if err := json.Unmarshal(raw, &m); err != nil {
		return map[string]time.Time{}
	}
	return m
}

// RecordVerifyFail 记录一次源验证失败并持久化（顺带清理过期条目）。
func (s *Store) RecordVerifyFail(u string) {
	m := s.LoadVerifyFails()
	now := time.Now()
	m[u] = now
	for k, t := range m {
		if now.Sub(t) > verifyFailMaxAge {
			delete(m, k)
		}
	}
	s.mu.Lock()
	cacheDir := s.cacheDir
	s.mu.Unlock()
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return
	}
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return
	}
	tmp := filepath.Join(cacheDir, verifyFailsFile+".tmp")
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, filepath.Join(cacheDir, verifyFailsFile))
}

// VerifyFailSkip 报告该 URL 是否应跳过本次验证（TTL 内失败过）。
func VerifyFailSkip(fails map[string]time.Time, u string, now time.Time) bool {
	t, ok := fails[u]
	return ok && now.Sub(t) < verifyFailSkipTTL
}
