// Package logbus 提供一个内存环形日志总线。
//
// 作用是把服务运行期的事件同时送到三个地方：
//  1. 标准输出，供飞牛应用中心的日志查看；
//  2. 磁盘文件，便于事后排查；
//  3. 内存环，供 Web 控制台实时拉取。
package logbus

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Entry 是一条日志记录。
type Entry struct {
	Seq     int64     `json:"seq"`
	Time    time.Time `json:"time"`
	Level   string    `json:"level"`
	Message string    `json:"message"`
}

// Bus 是日志总线。
type Bus struct {
	mu      sync.RWMutex
	entries []Entry
	seq     int64
	size    int

	// subs 是实时订阅者，每个订阅者持有一个带缓冲的通道。
	subs map[int]chan Entry
	next int

	// 文件输出。fileMu 保护 file 句柄与轮转（写入方可能并发，
	// 而 os.File.Write 的内置锁不覆盖 close/rename/open 序列）。
	fileMu    sync.Mutex
	file      *os.File
	path      string
	fileBytes int64

	// maxBytes > 0 时按大小轮转：当前文件写满后移为 .1（.1→.2 …），
	// 最多保留 keep 份历史，最旧的一份被丢弃。
	maxBytes int64
	keep     int
}

// New 创建日志总线，size 是内存中保留的日志条数上限（不做文件轮转）。
func New(size int, filePath string) *Bus {
	return newWithRotation(size, filePath, 0, 0)
}

// NewWithRotation 创建带按大小轮转的日志总线。
//
// maxBytes 是当前文件触发轮转的大小上限（<=0 表示不轮转）；
// keep 是保留的历史份数（.1 ~ .keep，<1 按 1 处理）。
func NewWithRotation(size int, filePath string, maxBytes int64, keep int) *Bus {
	return newWithRotation(size, filePath, maxBytes, keep)
}

func newWithRotation(size int, filePath string, maxBytes int64, keep int) *Bus {
	if size <= 0 {
		size = 1000
	}
	if keep < 1 {
		keep = 1
	}
	b := &Bus{
		size:     size,
		subs:     make(map[int]chan Entry),
		path:     filePath,
		maxBytes: maxBytes,
		keep:     keep,
	}
	if filePath != "" {
		if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err == nil {
			// 以追加模式打开，多次启动日志连续。
			b.file, _ = os.OpenFile(filePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
			if b.file != nil {
				// 已存在的文件从当前大小记账，避免历史文件立即触发轮转。
				if fi, err := b.file.Stat(); err == nil {
					b.fileBytes = fi.Size()
				}
			}
		}
	}
	return b
}

// rotate 执行一次「当前文件 → .1，.1 → .2 …」的滚动。
// 调用方必须持有 fileMu。
func (b *Bus) rotate() {
	if b.file != nil {
		_ = b.file.Close()
		b.file = nil
	}
	// 从高编号往低编号搬，避免覆盖；超出 keep 的最旧一份直接丢弃。
	for i := b.keep - 1; i >= 1; i-- {
		_ = os.Rename(b.path+"."+itoa(i), b.path+"."+itoa(i+1))
	}
	_ = os.Rename(b.path, b.path+".1")
	f, err := os.OpenFile(b.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		// 重开失败（磁盘只读等）：停止文件落盘，stdout 与内存环不受影响。
		b.maxBytes = 0
		return
	}
	b.file = f
	b.fileBytes = 0
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	digits := make([]byte, 0, 4)
	for i > 0 {
		digits = append([]byte{byte('0' + i%10)}, digits...)
		i /= 10
	}
	return string(digits)
}

// Logf 写入一条日志，兼容 logf(level, format, args...) 的调用形式。
func (b *Bus) Logf(level, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	e := Entry{
		Time:    time.Now(),
		Level:   level,
		Message: msg,
	}

	b.mu.Lock()
	b.seq++
	e.Seq = b.seq
	b.entries = append(b.entries, e)
	if len(b.entries) > b.size {
		// 环形淘汰，保留最近的 size 条。
		b.entries = b.entries[len(b.entries)-b.size:]
	}
	subs := make([]chan Entry, 0, len(b.subs))
	for _, ch := range b.subs {
		subs = append(subs, ch)
	}
	b.mu.Unlock()

	// 输出到标准输出，飞牛的应用日志会采集这里的内容。
	fmt.Printf("[%s] %-5s %s\n", e.Time.Format("15:04:05"), level, msg)

	// 文件输出：写满 maxBytes 后按大小轮转（.1 ~ .keep）。
	b.fileMu.Lock()
	if b.file != nil {
		line := fmt.Sprintf("%s [%s] %s\n", e.Time.Format("2006-01-02 15:04:05"), level, msg)
		if _, err := b.file.WriteString(line); err == nil {
			b.fileBytes += int64(len(line))
			if b.maxBytes > 0 && b.fileBytes >= b.maxBytes {
				b.rotate()
			}
		}
	}
	b.fileMu.Unlock()

	// 分发给实时订阅者，通道满时丢弃该条以免阻塞业务。
	for _, ch := range subs {
		select {
		case ch <- e:
		default:
		}
	}
}

// Recent 返回最近 n 条日志，n<=0 时返回全部内存日志。
func (b *Bus) Recent(n int, level string, afterSeq int64) []Entry {
	b.mu.RLock()
	defer b.mu.RUnlock()

	out := make([]Entry, 0, len(b.entries))
	for _, e := range b.entries {
		if e.Seq <= afterSeq {
			continue
		}
		if level != "" && e.Level != level {
			continue
		}
		out = append(out, e)
	}
	if n > 0 && len(out) > n {
		out = out[len(out)-n:]
	}
	return out
}

// Subscribe 注册一个实时日志订阅者。
//
// 返回接收通道与注销函数，调用方必须在结束时调用注销函数释放资源。
func (b *Bus) Subscribe() (<-chan Entry, func()) {
	ch := make(chan Entry, 128)

	b.mu.Lock()
	id := b.next
	b.next++
	b.subs[id] = ch
	b.mu.Unlock()

	var once sync.Once
	cancel := func() {
		once.Do(func() {
			b.mu.Lock()
			delete(b.subs, id)
			b.mu.Unlock()
			close(ch)
		})
	}
	return ch, cancel
}

// Close 关闭日志文件句柄。
func (b *Bus) Close() error {
	b.fileMu.Lock()
	defer b.fileMu.Unlock()
	if b.file != nil {
		err := b.file.Close()
		b.file = nil
		return err
	}
	return nil
}

// Path 返回日志文件路径。
func (b *Bus) Path() string { return b.path }
