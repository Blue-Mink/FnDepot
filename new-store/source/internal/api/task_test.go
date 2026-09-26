package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"fnos-store/internal/core"
)

// TestHandleGetTaskNoSelfDeadlock 回归：handleGetTask 曾在持有 t.mu 时调用
// t.IsFinished()（内部再 t.mu.Lock → 非重入锁自死锁）。一旦查询的 app 有活跃
// 任务，首个请求就会卡死，且永久持有 t.mu，拖垮整个任务子系统（/task、/tasks
// 全部 hang）。修复：持锁时直接读 t.finished 字段。
func TestHandleGetTaskNoSelfDeadlock(t *testing.T) {
	const appName = "qbittorrent"
	s := &Server{registry: core.NewRegistry(), appsDir: t.TempDir(), queue: NewOperationQueue()}
	s.tasks = NewTaskManager("")
	// 制造一个未终态任务（queued），让 handleGetTask 进入 t.mu.Lock 分支。
	s.tasks.GetOrCreate(appName, "download")

	done := make(chan struct{})
	go func() {
		req := httptest.NewRequest(http.MethodGet, "/api/apps/"+appName+"/task", nil)
		req.SetPathValue("appname", appName)
		rec := httptest.NewRecorder()
		s.handleGetTask(rec, req)
		close(done)
	}()
	select {
	case <-done:
		// 正常返回
	case <-time.After(3 * time.Second):
		t.Fatal("handleGetTask 死锁（持 t.mu 又调 IsFinished 自锁）")
	}

	// 并发多个 /task + /tasks 请求，确认不再互相拖死。
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		for j := 0; j < 3; j++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				req := httptest.NewRequest(http.MethodGet, "/api/apps/"+appName+"/task", nil)
				req.SetPathValue("appname", appName)
				s.handleGetTask(httptest.NewRecorder(), req)
			}()
			wg.Add(1)
			go func() {
				defer wg.Done()
				s.handleListTasks(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/tasks", nil))
			}()
		}
	}
	wg.Wait()
}

// TestTaskManagerLoadRestoresFinished 回归：load() 恢复磁盘上的终态任务时，
// 必须置 finished=true。否则内存 finished=false 会被 /api/tasks 当成「进行中」
// 永久返回（重启后通知栏一直刷旧的 done 任务）。
func TestTaskManagerLoadRestoresFinished(t *testing.T) {
	dir := t.TempDir()
	persist := dir + "/tasks.json"

	// 第一个 manager：制造一个 done 任务并持久化。
	m1 := NewTaskManager(persist)
	task := m1.GetOrCreate("oldapp", "update")
	task.update(progressPayload{Step: "verifying", Progress: 50, AppName: "oldapp"})
	task.markDone()
	// 模拟「旧」任务：把 FinishedAt 拨到 25 秒前（超出 20s 窗口）。
	task.mu.Lock()
	task.FinishedAt = time.Now().Add(-25 * time.Second)
	task.mu.Unlock()
	m1.Persist()

	// 第二个 manager：从磁盘 load（模拟重启）。
	m2 := NewTaskManager(persist)
	restored := m2.Get("oldapp")
	if restored == nil {
		t.Fatal("重启后应恢复 oldapp 任务")
	}
	if !restored.IsFinished() {
		t.Fatal("恢复的 done 任务 IsFinished 应为 true（否则 /api/tasks 会永久返回它）")
	}
	if restored.status() != TaskDone {
		t.Fatalf("恢复的任务 status = %s, want done", restored.status())
	}

	// 模拟 handleListTasks 的过滤：旧 done 任务（>20s）不应被返回。
	var returned []string
	for _, tt := range m2.List() {
		tt.mu.Lock()
		finished := tt.finished
		finishedAt := tt.FinishedAt
		tt.mu.Unlock()
		if !(finished && time.Since(finishedAt) > 20*time.Second) {
			returned = append(returned, tt.AppName)
		}
	}
	for _, n := range returned {
		if n == "oldapp" {
			t.Fatal("旧 done 任务不应被 /api/tasks 返回（20s 窗口）")
		}
	}
}

// TestTaskPauseResumeState 暂停/继续状态机：
// queued --update--> running --pause--> paused（cancel 被调用）
// paused --markRunning--> running --markDone--> done；终态后 pause 失败。
func TestTaskPauseResumeState(t *testing.T) {
	task := NewTask("app", "download")
	ctx, cancel := context.WithCancel(context.Background())
	task.setCancel(cancel)

	// update 触发 queued→running
	task.update(progressPayload{Step: "downloading", Progress: 10, AppName: "app"})
	if task.status() != TaskRunning {
		t.Fatalf("update 后 status = %s, want running", task.status())
	}

	if !task.pause() {
		t.Fatal("running 任务 pause 应成功")
	}
	if task.status() != TaskPaused {
		t.Fatalf("pause 后 status = %s, want paused", task.status())
	}
	select {
	case <-ctx.Done():
		// cancel 已被调用（下载会被中断）
	default:
		t.Fatal("pause 未调用 cancel（下载不会停）")
	}

	// 终态前可恢复
	task.markRunning()
	if task.status() != TaskRunning {
		t.Fatalf("markRunning 后 status = %s, want running", task.status())
	}

	// 重复 pause 一个 running 任务应成功；paused 后再 pause 应失败
	task.pause()
	if task.status() != TaskPaused {
		t.Fatalf("二次 pause 后 status = %s, want paused", task.status())
	}
	if task.pause() {
		t.Fatal("对已 paused 任务再 pause 应返回 false")
	}

	// 终态后不可再 pause
	task.markRunning()
	task.markDone()
	if !task.IsFinished() {
		t.Fatal("markDone 后应为终态")
	}
	if task.pause() {
		t.Fatal("对已 done 任务 pause 应返回 false")
	}
}
