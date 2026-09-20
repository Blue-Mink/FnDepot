package cache

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestProbeCacheRoundTrip(t *testing.T) {
	s := NewStore(t.TempDir())
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}

	in := map[string]ProbeEntry{
		"https://example.com/a.fpk": {
			Date:     "2026-09-16T08:28:01Z",
			Size:     12345,
			ProbedAt: time.Now(),
		},
	}
	if err := s.SaveProbeCache(in); err != nil {
		t.Fatalf("SaveProbeCache: %v", err)
	}

	out := s.LoadProbeCache()
	e, ok := out["https://example.com/a.fpk"]
	if !ok {
		t.Fatal("entry missing after round trip")
	}
	if e.Date != "2026-09-16T08:28:01Z" || e.Size != 12345 {
		t.Errorf("entry = %+v, want date+size preserved", e)
	}
}

// 超过 30 天的条目在保存时清理，防文件无限增长。
func TestProbeCachePrunesOldEntries(t *testing.T) {
	s := NewStore(t.TempDir())
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	in := map[string]ProbeEntry{
		"fresh":  {Date: "2026-09-16T08:28:01Z", ProbedAt: time.Now()},
		"ancient": {Date: "2026-01-01T00:00:00Z", ProbedAt: time.Now().Add(-40 * 24 * time.Hour)},
	}
	if err := s.SaveProbeCache(in); err != nil {
		t.Fatal(err)
	}
	out := s.LoadProbeCache()
	if len(out) != 1 {
		t.Fatalf("len = %d, want 1 (ancient pruned)", len(out))
	}
	if _, ok := out["fresh"]; !ok {
		t.Error("fresh entry must survive pruning")
	}
}

// 文件缺失/损坏时返回空 map 而不是 nil（调用方可直接写回）。
func TestProbeCacheCorruptFile(t *testing.T) {
	s := NewStore(t.TempDir())
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	out := s.LoadProbeCache()
	if out == nil || len(out) != 0 {
		t.Fatalf("missing file must load as empty map, got %#v", out)
	}

	if err := os.WriteFile(filepath.Join(s.cacheDir, probeCacheFile), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out := s.LoadProbeCache(); out == nil || len(out) != 0 {
		t.Fatalf("corrupt file must load as empty map, got %#v", out)
	}
}
