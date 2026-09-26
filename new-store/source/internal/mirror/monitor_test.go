package mirror

import "testing"

// TestRank_ThroughputBeatsLatency 是本修复的核心用例：
// 低延迟但低吞吐的源（gh-proxy.org：延迟 100ms / 31KB/s）必须排在
// 高延迟但高吞吐的源（cdn.gh-proxy.org：延迟 500ms / 6.5MB/s）之后，
// 否则大文件下载会选到最慢的源。
func TestRank_ThroughputBeatsLatency(t *testing.T) {
	m := New()
	m.Record("lowlat_slow", "LL", true, 100, 31000)    // 低延迟、低吞吐
	m.Record("hilat_fast", "HF", true, 500, 6500000)   // 高延迟、高吞吐
	m.Record("c", "C", false, 0, 0)                    // 未有效探测（fail）
	m.Record("d", "D", false, 10, 0)
	m.Record("e", "E", false, 10, 0)
	m.Record("e", "E", false, 10, 0)
	m.Record("e", "E", false, 10, 0)

	got := m.Rank([]string{"lowlat_slow", "hilat_fast", "unprobed", "d", "e"})
	want := []string{"hilat_fast", "lowlat_slow", "unprobed", "d", "e"}
	// 高吞吐优先 → 低吞吐 → 未探测 → fail(失败升序)
	if len(got) != len(want) {
		t.Fatalf("len mismatch: %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("rank mismatch at %d: got %v want %v", i, got, want)
		}
	}
}

// TestRank_OrderByHealth 同吞吐时退化为延迟升序（兼容旧语义，含 Docker 全 0 吞吐场景）。
func TestRank_OrderByHealth(t *testing.T) {
	m := New()
	// a、b 吞吐相同（0=未测，如 Docker），按延迟升序：b(50) < a(100)
	m.Record("a", "A", true, 100, 0)
	m.Record("b", "B", true, 50, 0)
	m.Record("d", "D", false, 10, 0)
	m.Record("e", "E", false, 10, 0)
	m.Record("e", "E", false, 10, 0)
	m.Record("e", "E", false, 10, 0)

	got := m.Rank([]string{"a", "b", "c", "d", "e"})
	want := []string{"b", "a", "c", "d", "e"} // ok(延迟升序) → 未探测 → fail(失败升序)
	if len(got) != len(want) {
		t.Fatalf("len mismatch: %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("rank mismatch at %d: got %v want %v", i, got, want)
		}
	}
}

func TestRank_RecoversAfterSuccess(t *testing.T) {
	m := New()
	m.Record("a", "A", false, 5, 0)
	m.Record("b", "B", false, 5, 0)
	m.Record("b", "B", false, 5, 0)
	m.Record("b", "B", false, 5, 0)
	m.Record("b", "B", true, 400, 0) // 恢复

	got := m.Rank([]string{"a", "b"})
	// b 恢复后成功优先（即使延迟高），a 仍失败
	if got[0] != "b" || got[1] != "a" {
		t.Fatalf("recovered mirror should rank above failed: %v", got)
	}
}

// TestBestStable_Throughput 验证 BestStable 取吞吐最高（同吞吐取延迟最低），
// 与 Rank()[0] 一致。
func TestBestStable_Throughput(t *testing.T) {
	m := New()
	if _, ok := m.BestStable(); ok {
		t.Fatal("no stats -> no best stable")
	}
	m.Record("lowlat_slow", "LL", true, 100, 31000)
	m.Record("hilat_fast", "HF", true, 500, 6500000)
	m.Record("bad", "B", false, 10, 0)
	k, ok := m.BestStable()
	if !ok || k != "hilat_fast" {
		t.Fatalf("best stable should be 'hilat_fast' (highest throughput), got %q ok=%v", k, ok)
	}
	// 与 Rank()[0] 一致
	if got := m.Rank([]string{"lowlat_slow", "hilat_fast", "bad"}); got[0] != "hilat_fast" {
		t.Fatalf("Rank()[0] should match BestStable, got %v", got)
	}
}

func TestConsecFails_ResetOnSuccess(t *testing.T) {
	m := New()
	m.Record("a", "A", false, 1, 0)
	m.Record("a", "A", false, 1, 0)
	if got := m.ConsecFails("a"); got != 2 {
		t.Fatalf("want 2 consecutive fails, got %d", got)
	}
	m.Record("a", "A", true, 50, 1000)
	if got := m.ConsecFails("a"); got != 0 {
		t.Fatalf("success should reset fails, got %d", got)
	}
	if got := m.ConsecFails("unknown"); got != 0 {
		t.Fatalf("unknown key should have 0 fails, got %d", got)
	}
}
