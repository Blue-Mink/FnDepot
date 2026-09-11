package service

import (
	"log"
	"time"

	"daidai-panel/database"
	"daidai-panel/model"
)

var globalScheduler *SchedulerV2
var globalExecutor *TaskExecutor

// maxConcurrentTasksConfigKey 是「定时任务最大并发数」的配置键，
// 初始化与热生效两条路径共用，避免字面量拼写漂移。
const maxConcurrentTasksConfigKey = "max_concurrent_tasks"

// defaultSchedulerWorkerCount 是配置缺失或非法时的兜底并发数。
const defaultSchedulerWorkerCount = 4

// resolveSchedulerWorkerCount 读取配置里的并发数，非法值回落到兜底值。
func resolveSchedulerWorkerCount() int {
	workerCount := model.GetRegisteredConfigInt(maxConcurrentTasksConfigKey)
	if workerCount < 1 {
		return defaultSchedulerWorkerCount
	}
	return workerCount
}

// ApplySchedulerWorkerCount 让「定时任务最大并发数」在保存后立刻生效，不必重启面板。
// 调大立刻补 worker；调小时多余的 worker 只在两次任务之间退出，不会打断正在执行的任务。
func ApplySchedulerWorkerCount() {
	scheduler := globalScheduler
	if scheduler == nil {
		return
	}

	previous, applied := scheduler.SetWorkerCount(resolveSchedulerWorkerCount())
	if previous == applied {
		return
	}
	log.Printf("scheduler v2 concurrency limit updated: %d -> %d worker(s)", previous, applied)
}

func InitSchedulerV2() {
	globalExecutor = NewTaskExecutor()
	if count := RecoverAbandonedActiveTasks("面板上次异常退出，运行中的任务已标记为中断"); count > 0 {
		log.Printf("recovered %d abandoned active task(s)", count)
	}

	workerCount := resolveSchedulerWorkerCount()

	cfg := SchedulerConfig{
		WorkerCount: workerCount,
		// worker 会阻塞到任务执行结束，队列积压概率显著上升；
		// 队列容量与并发数解耦，取固定的较大值，避免正常波动就把请求丢掉。
		QueueSize:    1000,
		RateInterval: 200 * time.Millisecond,
	}

	globalScheduler = NewSchedulerV2(cfg, globalExecutor)
	globalScheduler.Start()

	var tasks []model.Task
	// 与 AddJob / ReloadAllJobs 的口径对齐：上面 RecoverAbandonedActiveTasks 已经把残留的
	// 排队中/运行中拉回启用态，这里再放宽一次是防御性的，避免三处条件各写各的以后漂移。
	database.DB.Where("status <> ?", model.TaskStatusDisabled).Find(&tasks)

	for _, task := range tasks {
		if err := globalScheduler.AddJob(&task); err != nil {
			log.Printf("任务 %d 启动时注册调度失败（它不会自动触发）: %v", task.ID, err)
		}
	}

	startupCount := globalScheduler.EnqueueStartupTasks()
	log.Printf("scheduler v2 initialized with %d tasks", len(tasks))
	if startupCount > 0 {
		log.Printf("scheduler v2 enqueued %d startup task(s)", startupCount)
	}
}

func ShutdownSchedulerV2() {
	// worker 会阻塞到任务结束，必须先中断执行中的进程，再回收 worker，
	// 否则每次关机都要白等满一个等待超时。
	if globalScheduler != nil {
		globalScheduler.SignalStop()
	}

	if globalExecutor != nil {
		killed := globalExecutor.StopAllRunningTasks()
		if killed > 0 {
			log.Printf("interrupted %d running task process(es) during panel shutdown", killed)
		}
	}

	if globalScheduler != nil {
		if ok := globalScheduler.WaitWorkers(5 * time.Second); !ok {
			log.Println("timed out waiting for scheduler workers to finish")
		}
		log.Println("scheduler v2 stopped")
	}

	if globalExecutor != nil {
		if ok := globalExecutor.Wait(5 * time.Second); !ok {
			log.Println("timed out waiting for running task cleanup")
		}
	}

	if count := MarkActiveTasksInterrupted("面板正在关闭或重启，任务已被中断"); count > 0 {
		log.Printf("marked %d active task(s) as interrupted during shutdown", count)
	}

	if globalScheduler != nil {
		globalScheduler = nil
	}
	globalExecutor = nil
}

func GetSchedulerV2() *SchedulerV2 {
	return globalScheduler
}

func GetTaskExecutor() *TaskExecutor {
	return globalExecutor
}
