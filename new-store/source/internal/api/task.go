package api

import (
	"context"
	"encoding/json"
	"os"
	"sync"
	"time"
)

// TaskStatus 是后台操作的生命周期状态。
type TaskStatus string

const (
	TaskQueued  TaskStatus = "queued"
	TaskRunning TaskStatus = "running"
	TaskPaused  TaskStatus = "paused" // 下载暂停（非终态，可继续；.part 保留）
	TaskDone    TaskStatus = "done"
	TaskFailed  TaskStatus = "failed"
)

// Task 是一次后台安装/更新/下载操作。
//
// 核心价值：把操作从「请求作用域」解耦到「服务端后台」。客户端（浏览器）
// 关闭后 r.Context() 取消，但任务用自己的 detached context 继续跑；进度写入
// 这里，供 ① SSE 实时视图（subscribe）② GET /task 轮询 ③ 磁盘持久化 读取。
type Task struct {
	AppName    string     `json:"appname"`
	Op         string     `json:"op"`
	Status     TaskStatus `json:"status"`
	Step       string     `json:"step,omitempty"`
	Progress   int        `json:"progress,omitempty"`
	Message    string     `json:"message,omitempty"`
	NewVersion string     `json:"new_version,omitempty"`
	Downloaded int64      `json:"downloaded,omitempty"`
	Total      int64      `json:"total,omitempty"`
	Speed      int64      `json:"speed,omitempty"`
	Error      string     `json:"error,omitempty"`
	StartedAt  time.Time  `json:"started_at"`
	UpdatedAt  time.Time  `json:"updated_at,omitempty"`
	FinishedAt time.Time  `json:"finished_at,omitempty"`

	mu          sync.Mutex
	subscribers map[chan progressPayload]struct{}
	doneCh      chan struct{} // 终态(done/failed)关闭；paused 不关闭（可恢复）
	finished    bool
	cancel      context.CancelFunc // 当前在途下载的取消句柄（暂停用）
}

// NewTask 创建 queued 状态的任务。
func NewTask(appname, op string) *Task {
	now := time.Now()
	return &Task{
		AppName:     appname,
		Op:          op,
		Status:      TaskQueued,
		StartedAt:   now,
		UpdatedAt:   now,
		subscribers: make(map[chan progressPayload]struct{}),
		doneCh:      make(chan struct{}),
	}
}

// snapshot 返回当前进度快照（线程安全）。
func (t *Task) snapshot() progressPayload {
	t.mu.Lock()
	defer t.mu.Unlock()
	return progressPayload{
		Step:       t.Step,
		Progress:   t.Progress,
		Message:    t.Message,
		NewVersion: t.NewVersion,
		AppName:    t.AppName,
		Speed:      t.Speed,
		Downloaded: t.Downloaded,
		Total:      t.Total,
	}
}

// status 返回当前状态（线程安全）。
func (t *Task) status() TaskStatus {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.Status
}

// update 记录一次进度并广播给订阅者（非阻塞，慢订阅者丢弃——最新态可随时查）。
func (t *Task) update(p progressPayload) {
	t.mu.Lock()
	t.Step = p.Step
	t.Progress = p.Progress
	t.Message = p.Message
	if p.NewVersion != "" {
		t.NewVersion = p.NewVersion
	}
	if p.Downloaded > 0 || p.Total > 0 {
		t.Downloaded = p.Downloaded
		t.Total = p.Total
	}
	if p.Speed > 0 {
		t.Speed = p.Speed
	}
	t.UpdatedAt = time.Now()
	if t.Status == TaskQueued {
		t.Status = TaskRunning
	}
	subs := make([]chan progressPayload, 0, len(t.subscribers))
	for ch := range t.subscribers {
		subs = append(subs, ch)
	}
	t.mu.Unlock()

	for _, ch := range subs {
		select {
		case ch <- p:
		default:
		}
	}
}

// fail 标记失败、广播、关闭 doneCh（幂等）。
func (t *Task) fail(msg string) {
	t.mu.Lock()
	if t.finished {
		t.mu.Unlock()
		return
	}
	t.Status = TaskFailed
	t.Error = msg
	t.Message = msg
	t.UpdatedAt = time.Now()
	t.FinishedAt = time.Now()
	t.finished = true
	subs := make([]chan progressPayload, 0, len(t.subscribers))
	for ch := range t.subscribers {
		subs = append(subs, ch)
	}
	t.mu.Unlock()

	for _, ch := range subs {
		select {
		case ch <- progressPayload{Step: "error", Message: msg, AppName: t.AppName}:
		default:
		}
	}
	close(t.doneCh)
}

// markDone 标记成功完成（管道无错返回后调用，幂等）。
func (t *Task) markDone() {
	t.mu.Lock()
	if t.finished {
		t.mu.Unlock()
		return
	}
	t.Status = TaskDone
	t.UpdatedAt = time.Now()
	t.FinishedAt = time.Now()
	t.finished = true
	t.mu.Unlock()
	close(t.doneCh)
}

// done 返回任务结束（done/failed）时关闭的 channel。
func (t *Task) done() <-chan struct{} { return t.doneCh }

// IsFinished 是否到达终态。
func (t *Task) IsFinished() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.finished
}

// broadcast 非阻塞广播进度给所有订阅者。
func (t *Task) broadcast(p progressPayload) {
	t.mu.Lock()
	subs := make([]chan progressPayload, 0, len(t.subscribers))
	for ch := range t.subscribers {
		subs = append(subs, ch)
	}
	t.mu.Unlock()
	for _, ch := range subs {
		select {
		case ch <- p:
		default:
		}
	}
}

// setCancel 记录当前在途下载的取消句柄（暂停时调用）。
func (t *Task) setCancel(c context.CancelFunc) {
	t.mu.Lock()
	t.cancel = c
	t.mu.Unlock()
}

// clearCancel 清除取消句柄（下载结束/暂停后）。
func (t *Task) clearCancel() {
	t.mu.Lock()
	t.cancel = nil
	t.mu.Unlock()
}

// pause 暂停进行中的下载：取消在途请求 + 标记 paused（非终态，可恢复，.part 保留）。
// 仅对 running 态有效；返回是否暂停成功。
func (t *Task) pause() bool {
	t.mu.Lock()
	if t.finished || t.Status != TaskRunning {
		t.mu.Unlock()
		return false
	}
	t.Status = TaskPaused
	t.Message = "下载已暂停"
	t.UpdatedAt = time.Now()
	cancel := t.cancel
	t.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	t.broadcast(progressPayload{Step: "paused", Message: "下载已暂停", AppName: t.AppName})
	return true
}

// markRunning 把 paused 任务恢复为 running（继续下载时调用）。
func (t *Task) markRunning() {
	t.mu.Lock()
	if t.finished || t.Status != TaskPaused {
		t.mu.Unlock()
		return
	}
	t.Status = TaskRunning
	t.Message = "继续下载中..."
	t.UpdatedAt = time.Now()
	t.mu.Unlock()
	t.broadcast(progressPayload{Step: "downloading", Message: "继续下载中...", AppName: t.AppName})
}

// subscribe 注册实时进度订阅；新订阅者会立即收到当前快照（不漏进度）。
func (t *Task) subscribe() (<-chan progressPayload, func()) {
	ch := make(chan progressPayload, 32)
	t.mu.Lock()
	t.subscribers[ch] = struct{}{}
	t.mu.Unlock()
	go func() {
		select {
		case ch <- t.snapshot():
		default:
		}
	}()
	unsub := func() {
		t.mu.Lock()
		delete(t.subscribers, ch)
		t.mu.Unlock()
	}
	return ch, unsub
}

// taskSink 把管道进度写入后台任务（实现 pipelineSink）。
type taskSink struct {
	task *Task
	mu   sync.Mutex
	err  string
}

func (s *taskSink) sendProgress(p progressPayload) error {
	s.task.update(p)
	return nil
}

func (s *taskSink) sendError(msg string) error {
	s.mu.Lock()
	s.err = msg
	s.mu.Unlock()
	s.task.fail(msg)
	return nil
}

func (s *taskSink) hadError() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err != ""
}

// teeSink 同时转发到多个 sink（典型：taskSink + sseStream）。
type teeSink struct{ sinks []pipelineSink }

func (t *teeSink) sendProgress(p progressPayload) error {
	var lastErr error
	for _, s := range t.sinks {
		if err := s.sendProgress(p); err != nil {
			lastErr = err
		}
	}
	return lastErr
}

func (t *teeSink) sendError(msg string) error {
	var lastErr error
	for _, s := range t.sinks {
		if err := s.sendError(msg); err != nil {
			lastErr = err
		}
	}
	return lastErr
}

// ---- TaskManager ----

// TaskManager 持有进行中和近期的后台任务（按 appname 键，单写者由 queue 保证）。
type TaskManager struct {
	mu          sync.Mutex
	tasks       map[string]*Task
	persistPath string
}

func NewTaskManager(persistPath string) *TaskManager {
	m := &TaskManager{tasks: make(map[string]*Task), persistPath: persistPath}
	m.load()
	return m
}

// GetOrCreate 返回 appname 的任务；无进行中任务则新建。
// 若槽位被「暂停的下载」占用且新 op 不同（如安装/更新），则让位新建——
// 暂停的下载可稍后重下，安装/更新优先。
func (m *TaskManager) GetOrCreate(appname, op string) *Task {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.tasks[appname]; ok && !t.IsFinished() {
		// 已持 m.mu；在 t.mu 下读 status/op（锁序 m.mu→t.mu，与 Persist 一致）
		t.mu.Lock()
		needYield := t.Status == TaskPaused && t.Op != op
		t.mu.Unlock()
		if needYield {
			nt := NewTask(appname, op)
			m.tasks[appname] = nt
			return nt
		}
		return t
	}
	t := NewTask(appname, op)
	m.tasks[appname] = t
	return t
}

// Get 返回 appname 的任务（无则 nil）。
func (m *TaskManager) Get(appname string) *Task {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.tasks[appname]
}

// List 返回全部任务快照。
func (m *TaskManager) List() []*Task {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*Task, 0, len(m.tasks))
	for _, t := range m.tasks {
		out = append(out, t)
	}
	return out
}

// Persist 把全部任务状态写到磁盘（进度持久化，跨 daemon 重启保留）。
func (m *TaskManager) Persist() {
	if m.persistPath == "" {
		return
	}
	m.mu.Lock()
	type wire struct {
		AppName    string     `json:"appname"`
		Op         string     `json:"op"`
		Status     TaskStatus `json:"status"`
		Step       string     `json:"step,omitempty"`
		Progress   int        `json:"progress,omitempty"`
		Message    string     `json:"message,omitempty"`
		NewVersion string     `json:"new_version,omitempty"`
		Downloaded int64      `json:"downloaded,omitempty"`
		Total      int64      `json:"total,omitempty"`
		Speed      int64      `json:"speed,omitempty"`
		Error      string     `json:"error,omitempty"`
		StartedAt  time.Time  `json:"started_at"`
		UpdatedAt  time.Time  `json:"updated_at,omitempty"`
		FinishedAt time.Time  `json:"finished_at,omitempty"`
	}
	out := make(map[string]wire, len(m.tasks))
	for name, t := range m.tasks {
		t.mu.Lock()
		out[name] = wire{
			AppName: t.AppName, Op: t.Op, Status: t.Status, Step: t.Step,
			Progress: t.Progress, Message: t.Message, NewVersion: t.NewVersion,
			Downloaded: t.Downloaded, Total: t.Total, Speed: t.Speed,
			Error: t.Error, StartedAt: t.StartedAt, UpdatedAt: t.UpdatedAt,
			FinishedAt: t.FinishedAt,
		}
		t.mu.Unlock()
	}
	m.mu.Unlock()

	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return
	}
	tmp := m.persistPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, m.persistPath)
}

// load 从磁盘恢复任务（进行中任务被重启打断 → 标记 failed）。
func (m *TaskManager) load() {
	if m.persistPath == "" {
		return
	}
	data, err := os.ReadFile(m.persistPath)
	if err != nil {
		return
	}
	type wire struct {
		AppName    string     `json:"appname"`
		Op         string     `json:"op"`
		Status     TaskStatus `json:"status"`
		Step       string     `json:"step,omitempty"`
		Progress   int        `json:"progress,omitempty"`
		Message    string     `json:"message,omitempty"`
		NewVersion string     `json:"new_version,omitempty"`
		Downloaded int64      `json:"downloaded,omitempty"`
		Total      int64      `json:"total,omitempty"`
		Speed      int64      `json:"speed,omitempty"`
		Error      string     `json:"error,omitempty"`
		StartedAt  time.Time  `json:"started_at"`
		UpdatedAt  time.Time  `json:"updated_at"`
		FinishedAt time.Time  `json:"finished_at"`
	}
	var out map[string]wire
	if err := json.Unmarshal(data, &out); err != nil {
		return
	}
	for name, w := range out {
		t := NewTask(name, w.Op)
		t.AppName = w.AppName
		t.Status = w.Status
		t.Step = w.Step
		t.Progress = w.Progress
		t.Message = w.Message
		t.NewVersion = w.NewVersion
		t.Downloaded = w.Downloaded
		t.Total = w.Total
		t.Speed = w.Speed
		t.Error = w.Error
		t.StartedAt = w.StartedAt
		t.UpdatedAt = w.UpdatedAt
		t.FinishedAt = w.FinishedAt
		// 恢复 finished 标志，否则内存里 finished=false 会被 /api/tasks 当成
		// 「进行中」永久返回（通知栏一直刷旧的 done 任务）。
		switch w.Status {
		case TaskRunning, TaskQueued:
			// 重启前进行中的任务其 goroutine 已不存在 → 标记失败（被重启打断）。
			t.Status = TaskFailed
			t.Error = "应用重启，操作被中断"
			t.FinishedAt = time.Now()
			t.finished = true
			close(t.doneCh)
		case TaskDone, TaskFailed:
			// 终态任务：恢复 finished（doneCh 关闭，无活动订阅者，安全）。
			t.finished = true
			close(t.doneCh)
		}
		// paused 任务保留（.part 仍在，可继续下载）：finished 保持 false。
		m.tasks[name] = t
	}
}
