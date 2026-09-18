package mirror

import "testing"

func TestRank_OrderByHealth(t *testing.T) {
	m := New()
	// a 成功 100ms；b 成功 50ms；c 未探测；d 失败 1 次；e 失败 3 次
	m.Record("a", "A", true, 100)
	m.Record("b", "B", true, 50)
	m.Record("d", "D", false, 10)
	m.Record("e", "E", false, 10)
	m.Record("e", "E", false, 10)
	m.Record("e", "E", false, 10)

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
	m.Record("a", "A", false, 5)
	m.Record("b", "B", false, 5)
	m.Record("b", "B", false, 5)
	m.Record("b", "B", false, 5)
	m.Record("b", "B", true, 400) // 恢复

	got := m.Rank([]string{"a", "b"})
	// b 恢复后成功优先（即使延迟高），a 仍失败
	if got[0] != "b" || got[1] != "a" {
		t.Fatalf("recovered mirror should rank above failed: %v", got)
	}
}

func TestBestStable(t *testing.T) {
	m := New()
	if _, ok := m.BestStable(); ok {
		t.Fatal("no stats -> no best stable")
	}
	m.Record("slow", "S", true, 900)
	m.Record("fast", "F", true, 100)
	m.Record("bad", "B", false, 10)
	k, ok := m.BestStable()
	if !ok || k != "fast" {
		t.Fatalf("best stable should be 'fast', got %q ok=%v", k, ok)
	}
}

func TestConsecFails_ResetOnSuccess(t *testing.T) {
	m := New()
	m.Record("a", "A", false, 1)
	m.Record("a", "A", false, 1)
	if got := m.ConsecFails("a"); got != 2 {
		t.Fatalf("want 2 consecutive fails, got %d", got)
	}
	m.Record("a", "A", true, 50)
	if got := m.ConsecFails("a"); got != 0 {
		t.Fatalf("success should reset fails, got %d", got)
	}
	if got := m.ConsecFails("unknown"); got != 0 {
		t.Fatalf("unknown key should have 0 fails, got %d", got)
	}
}
