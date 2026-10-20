// Package certtrack 记录局域网客户端对本机根证书的信任情况。
//
// 目的：把「这台设备到底装没装证书」变成可观测的事实。
// 数据来源是代理侧真实发生的 TLS 握手结果：
//   - 中间人握手失败（客户端不认识我们的证书）→ MarkUntrusted
//   - 中间人握手成功（客户端信任了我们的证书）→ MarkTrusted
//
// 只记录 IP 与时间，不记录任何请求内容；条目有上限与过期清理，
// 不会无限增长，也不会被当成审计日志使用。
package certtrack

import (
	"net"
	"sort"
	"sync"
	"time"
)

// Entry 是一台客户端的信任状态快照。
type Entry struct {
	IP        string    `json:"ip"`
	Trusted   bool      `json:"trusted"`
	FirstSeen time.Time `json:"first_seen"`
	LastSeen  time.Time `json:"last_seen"`
	Failures  int       `json:"failures"`
	Hosts     []string  `json:"hosts,omitempty"`
}

const (
	// maxEntries 限制登记表规模（家庭/办公室局域网远小于这个量级）。
	maxEntries = 256
	// keepFor 之后没有任何活动的条目会被清理。
	keepFor = 7 * 24 * time.Hour
	// hostLimit 每台设备最多保留几个「被拒域名」用于定位。
	hostLimit = 3
)

type entry struct {
	Entry
	hosts []string
}

var (
	mu      sync.Mutex
	entries = make(map[string]*entry)
)

// MarkUntrusted 记录一次「客户端不信任本机证书」的握手失败。
func MarkUntrusted(ip, host string) {
	ip = normalize(ip)
	if ip == "" {
		return
	}
	now := time.Now()
	mu.Lock()
	defer mu.Unlock()
	e := touch(ip, now)
	e.Trusted = false
	e.Failures++
	if host != "" && !contains(e.hosts, host) {
		if len(e.hosts) >= hostLimit {
			e.hosts = e.hosts[1:]
		}
		e.hosts = append(e.hosts, host)
	}
	e.Hosts = append([]string(nil), e.hosts...)
}

// MarkTrusted 记录一次成功的中间人握手（说明该客户端已信任本机证书）。
func MarkTrusted(ip string) {
	ip = normalize(ip)
	if ip == "" {
		return
	}
	now := time.Now()
	mu.Lock()
	defer mu.Unlock()
	e := touch(ip, now)
	e.Trusted = true
	e.Failures = 0
	e.hosts = nil
	e.Hosts = nil
}

// Lookup 返回单台设备的状态快照。
func Lookup(ip string) (Entry, bool) {
	ip = normalize(ip)
	if ip == "" {
		return Entry{}, false
	}
	mu.Lock()
	defer mu.Unlock()
	e, ok := entries[ip]
	if !ok {
		return Entry{}, false
	}
	return e.Entry, true
}

// List 返回全部设备，最近活动在前。
func List() []Entry {
	mu.Lock()
	defer mu.Unlock()
	pruneLocked(time.Now())
	out := make([]Entry, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Entry)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastSeen.After(out[j].LastSeen) })
	return out
}

// Reset 清空登记（仅供测试与「清除记录」使用）。
func Reset() {
	mu.Lock()
	defer mu.Unlock()
	entries = make(map[string]*entry)
}

func touch(ip string, now time.Time) *entry {
	pruneLocked(now)
	e, ok := entries[ip]
	if !ok {
		// 超出上限时淘汰最久未活动的条目，避免长跑后无限增长。
		if len(entries) >= maxEntries {
			var oldestIP string
			var oldest time.Time
			for k, v := range entries {
				if oldestIP == "" || v.LastSeen.Before(oldest) {
					oldestIP, oldest = k, v.LastSeen
				}
			}
			delete(entries, oldestIP)
		}
		e = &entry{Entry: Entry{IP: ip, FirstSeen: now}}
		entries[ip] = e
	}
	e.LastSeen = now
	return e
}

func pruneLocked(now time.Time) {
	for k, v := range entries {
		if now.Sub(v.LastSeen) > keepFor {
			delete(entries, k)
		}
	}
}

func normalize(ip string) string {
	if ip == "" {
		return ""
	}
	if host, _, err := net.SplitHostPort(ip); err == nil {
		ip = host
	}
	// IPv4 映射到 IPv6 的写法统一回 IPv4。
	if parsed := net.ParseIP(ip); parsed != nil {
		return parsed.String()
	}
	return ""
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
