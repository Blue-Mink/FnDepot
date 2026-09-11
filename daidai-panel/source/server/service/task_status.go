package service

import "daidai-panel/model"

func ResolveTaskInactiveStatus(task *model.Task) float64 {
	if task == nil {
		return model.TaskStatusEnabled
	}

	if task.Status == model.TaskStatusDisabled {
		return model.TaskStatusDisabled
	}
	if task.Status == model.TaskStatusEnabled {
		return model.TaskStatusEnabled
	}

	if task.Status == model.TaskStatusRunning {
		// 「运行中被禁用」的意图以显式标记为准，一直有效到用户重新启用（见 hasPendingDisable 的注释）。
		if hasPendingDisable(task) {
			return model.TaskStatusDisabled
		}
		// 兜底：标记只活在内存里，面板中途重启就没了；这时仍按「调度器里已经没有这条任务」反推。
		// 注意它不是可靠判据——任务被重新注册过就会失效，所以只能放在标记之后当兜底。
		scheduler := GetSchedulerV2()
		if scheduler != nil && !scheduler.HasJob(task.ID) {
			return model.TaskStatusDisabled
		}
	}

	return model.TaskStatusEnabled
}
