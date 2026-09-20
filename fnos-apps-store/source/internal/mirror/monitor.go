// Package mirror 跟踪各 GitHub 加速源（镜像）的实时健康状态。
//
// 数据来源（都会汇入同一个计数器）：
//  1. 后台周期性探测（默认每 5 分钟，由 api 层启动）
//  2. 设置页「测速」按钮的手动检查
//
// 消费方：
//  1. config.GitHubFallbackPrefixes —— 下载链按健康度排序（快而稳的优先，
//     连续失败的沉底，直连兜底）
//  2. 自动切换 —— 用户显式选择的镜像连续探测失败达到阈值时，api 层把
//     配置里的 Mirror 持久化切换为当前最稳定的源，并在健康接口里记录
//     最近一次切换（UI 展示）。
package mirror

import (
	"sort"
	"sync"
	"time"
)

// Stat 是单个镜像最近的健康状态快照。
type Stat struct {
	Key         string    `json:"key"`
	Label       string    `json:"label"`
	LatencyMs   int       `json:"latency_ms"`
	Status      string    `json:"status"` // "ok" | "fail" | "" (未探测)
	LastCheck   time.Time `json:"last_check"`
	ConsecFails int       `json:"consec_fails"`
}

// SwitchInfo 记录最近一次「自动切换到稳定源」。
type SwitchInfo struct {
	From   string    `json:"from"`
	To     string    `json:"to"`
	Time   time.Time `json:"time"`
	Reason string    `json:"reason"`
}

type Monitor struct {
	mu          sync.RWMutex
	stats       map[string]*Stat
	lastProbe   time.Time
	lastSwitch  *SwitchInfo
}

func New() *Monitor {
	return &Monitor{stats: make(map[string]*Stat)}
}

// Record 写入一次测量结果。ok=true 重置连续失败计数并记录延迟；
// ok=false（超时/错误/HTTP>=400）累加连续失败计数。
func (m *Monitor) Record(key, label string, ok bool, latencyMs int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.stats[key]
	if st == nil {
		st = &Stat{Key: key}
		m.stats[key] = st
	}
	if label != "" {
		st.Label = label
	}
	now := time.Now()
	st.LastCheck = now
	if ok {
		st.Status = "ok"
		st.LatencyMs = latencyMs
		st.ConsecFails = 0
	} else {
		if latencyMs > 0 {
			st.LatencyMs = latencyMs
		}
		st.Status = "fail"
		st.ConsecFails++
	}
}

// ConsecFails 返回某镜像当前连续失败次数（未探测过 = 0）。
func (m *Monitor) ConsecFails(key string) int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if st, ok := m.stats[key]; ok {
		return st.ConsecFails
	}
	return 0
}

// Snapshot 返回全部镜像的状态（未探测过的 key 不出现；UI 侧按完整列表补位）。
func (m *Monitor) Snapshot() []Stat {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Stat, 0, len(m.stats))
	for _, st := range m.stats {
		out = append(out, *st)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// LastProbe 返回最近一次后台探测完成时间（零值 = 还没探测过）。
func (m *Monitor) LastProbe() time.Time {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.lastProbe
}

// SetLastProbe 标记一轮探测完成。
func (m *Monitor) SetLastProbe(t time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastProbe = t
}

// SetLastSwitch 记录一次自动切换（由 api 层在真正改配置后调用）。
func (m *Monitor) SetLastSwitch(info SwitchInfo) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastSwitch = &info
}

// LastSwitch 返回最近一次自动切换（可能为 nil）。
func (m *Monitor) LastSwitch() *SwitchInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.lastSwitch == nil {
		return nil
	}
	cp := *m.lastSwitch
	return &cp
}

// Rank 把传入的镜像 key 按「可用性优先」排序：
//  1. 探测成功（连续失败少 → 延迟低）
//  2. 尚未探测（保持传入顺序 —— 声明顺序已是按实测延迟排好的）
//  3. 探测失败（连续失败多的更靠后）
//
// 同一状态内用稳定排序保持传入顺序，保证结果确定。
func (m *Monitor) Rank(keys []string) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	type item struct {
		key   string
		tier  int // 0=ok 1=未探测 2=fail
		fails int
		lat   int
		idx   int
	}
	items := make([]item, 0, len(keys))
	for i, k := range keys {
		it := item{key: k, tier: 1, idx: i}
		if st, ok := m.stats[k]; ok {
			if st.Status == "ok" {
				it.tier = 0
			} else {
				it.tier = 2
			}
			it.fails = st.ConsecFails
			it.lat = st.LatencyMs
		}
		items = append(items, it)
	}
	sort.SliceStable(items, func(a, b int) bool {
		if items[a].tier != items[b].tier {
			return items[a].tier < items[b].tier
		}
		if items[a].fails != items[b].fails {
			return items[a].fails < items[b].fails
		}
		if items[a].lat != items[b].lat {
			return items[a].lat < items[b].lat
		}
		return items[a].idx < items[b].idx
	})
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.key
	}
	return out
}

// BestStable 返回当前探测成功且延迟最低的镜像 key。
func (m *Monitor) BestStable() (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	bestKey, bestLat := "", 0
	found := false
	for _, st := range m.stats {
		if st.Status != "ok" {
			continue
		}
		if !found || st.LatencyMs < bestLat {
			bestKey, bestLat, found = st.Key, st.LatencyMs, true
		}
	}
	return bestKey, found
}
