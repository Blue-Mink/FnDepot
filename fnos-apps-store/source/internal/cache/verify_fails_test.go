package cache

import (
	"testing"
	"time"
)

func TestVerifyFailSkipTTL(t *testing.T) {
	s := NewStore(t.TempDir())
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}

	u := "https://github.com/dead-repo/FnDepot"
	s.RecordVerifyFail(u)

	fails := s.LoadVerifyFails()
	now := time.Now()
	if !VerifyFailSkip(fails, u, now) {
		t.Fatal("fresh failure must be skipped within TTL")
	}
	if VerifyFailSkip(fails, "https://github.com/other/FnDepot", now) {
		t.Fatal("unknown URL must not be skipped")
	}
	// 超过 TTL 的失败不跳过（允许重试）
	if VerifyFailSkip(fails, u, now.Add(verifyFailSkipTTL+time.Minute)) {
		t.Fatal("stale failure must be retried after TTL")
	}
}

func TestVerifyFailRoundTripAndPrune(t *testing.T) {
	s := NewStore(t.TempDir())
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	s.RecordVerifyFail("https://github.com/a/FnDepot")
	// 手工塞一个超龄条目，保存时应被清理
	raw := s.LoadVerifyFails()
	raw["https://github.com/ancient/FnDepot"] = time.Now().Add(-30 * 24 * time.Hour)
	// 通过再次记录触发保存（内部清理）
	s.RecordVerifyFail("https://github.com/b/FnDepot")

	out := s.LoadVerifyFails()
	if _, ok := out["https://github.com/ancient/FnDepot"]; ok {
		t.Fatal("ancient entry must be pruned on save")
	}
	if _, ok := out["https://github.com/a/FnDepot"]; !ok {
		t.Fatal("recent entry must survive")
	}
}
