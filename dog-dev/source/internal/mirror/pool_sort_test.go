package mirror

import (
	"testing"
	"time"

	"github.com/ghpp/ghpp/internal/config"
)

type sortItem struct {
	name    string
	enabled bool
	tested  bool
	ok      bool
	score   float64
	weight  float64
}

func (it *sortItem) info() DisplayInfo {
	return DisplayInfo{Enabled: it.enabled, Tested: it.tested, OK: it.ok, Score: it.score, Weight: it.weight}
}

// 健康按分降序 → 未测（权重降序）→ 失效沉底 → 停用殿后。
func TestSortDisplaySegments(t *testing.T) {
	items := []sortItem{
		{name: "disabled-old", enabled: false, tested: true, ok: true, score: 90},
		{name: "healthy-low", enabled: true, tested: true, ok: true, score: 40},
		{name: "untested-b", enabled: true, weight: 0.5},
		{name: "healthy-high", enabled: true, tested: true, ok: true, score: 80},
		{name: "failed", enabled: true, tested: true, ok: false, score: 0},
		{name: "untested-a", enabled: true, weight: 1},
	}
	ranks := SortDisplay(items, func(it sortItem) DisplayInfo { return it.info() })
	ordered := Reorder(items, func(it sortItem) DisplayInfo { return it.info() })

	wantOrder := []string{"healthy-high", "healthy-low", "untested-a", "untested-b", "failed", "disabled-old"}
	for i, w := range wantOrder {
		if ordered[i].name != w {
			t.Fatalf("位置 %d = %s，期望 %s（实际顺序 %v）", i, ordered[i].name, w, names(ordered))
		}
	}

	// 名次只发给启用项，顺序与展示一致。
	wantRank := map[string]int{"healthy-high": 1, "healthy-low": 2, "untested-a": 3, "untested-b": 4, "failed": 5, "disabled-old": 0}
	for i, it := range items {
		if ranks[i] != wantRank[it.name] {
			t.Errorf("%s 名次 = %d，期望 %d", it.name, ranks[i], wantRank[it.name])
		}
	}
}

// 冷却中的源（OK=false 但历史得分尚存）必须沉底而不是顶着高分排前。
func TestSortDisplayCooldownSinks(t *testing.T) {
	items := []sortItem{
		{name: "cooling", enabled: true, tested: true, ok: false, score: 60},
		{name: "steady", enabled: true, tested: true, ok: true, score: 30},
	}
	ordered := Reorder(items, func(it sortItem) DisplayInfo { return it.info() })
	if ordered[0].name != "steady" {
		t.Fatalf("冷却源未沉底，顺序 = %v", names(ordered))
	}
}

func names(items []sortItem) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.name
	}
	return out
}

// Stats 快照：冷却源 OK=false 且 InCooldown=true。
func TestStatsCooldownFlag(t *testing.T) {
	p := NewPool()
	p.applyProbe(config.Mirror{ID: "m1"}, Result{MirrorID: "m1", OK: true, LatencyMS: 100, ThroughputKBps: 2048, At: time.Now()})

	// 模拟冷却：手动置冷却截止时刻。
	p.mu.Lock()
	p.stats["m1"].cooldown = time.Now().Add(time.Minute)
	p.mu.Unlock()

	st := p.Stats()["m1"]
	if !st.InCooldown || st.OK {
		t.Fatalf("冷却源应 InCooldown=true 且 OK=false，实际 %+v", st)
	}
}
