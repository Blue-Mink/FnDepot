package handler

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"daidai-panel/config"
	"daidai-panel/database"
	"daidai-panel/model"
	"daidai-panel/pkg/response"
	"daidai-panel/service"

	"github.com/gin-gonic/gin"
)

func (h *TaskHandler) LatestLog(c *gin.Context) {
	taskID, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	var taskLog model.TaskLog
	if err := database.DB.Where("task_id = ?", taskID).Order("started_at DESC").First(&taskLog).Error; err != nil {
		// 查不到日志行不等于「从来没跑过」：日志清理是物理删除（service/log_cleanup.go 按
		// log_retention_days 直接删 task_logs 行，默认 7 天），删完之后任务上的 last_run_at /
		// last_run_status 还留着，列表「上次结果」列照样写着成功或失败。
		// 这时再统一回「暂无日志」，同一屏里就出现两个互相矛盾的说法，
		// 把「跑过、但日志被清了」误导成「从来没跑过」，所以按跑没跑过分两种文案。
		var task model.Task
		if err := database.DB.First(&task, taskID).Error; err == nil && task.LastRunAt != nil {
			// Local()：SQLite 驱动取回来的时间可能是 UTC，转成本机时区再格式化，
			// 否则文案里的时间会比用户在列表上看到的早几个小时。
			response.NotFound(c, fmt.Sprintf("该任务的日志已按保留策略清理（最近一次运行 %s），保留天数可在设置里调整",
				task.LastRunAt.Local().Format("2006-01-02 15:04:05")))
			return
		}
		// 任务不存在、或确实一次都没跑过。
		// 文案与面板日志抽屉里一直显示的那句保持一致：前端现在会优先把这里的 message 直接显示出来
		// （为的是让上面「日志被清理」那句能透出去），如果这里还回原来的「暂无日志」，
		// 用户在同一个位置看到的说法就会毫无理由地从「该任务还没有日志记录」变短、变生硬。
		response.NotFound(c, "该任务还没有日志记录")
		return
	}

	result := taskLog.ToDict()
	if taskLog.Content != "" {
		decompressed, err := service.DecompressFromBase64(taskLog.Content)
		if err == nil {
			result["content"] = decompressed
		}
	} else if taskLog.LogPath != nil {
		content, err := service.ReadLogFile(*taskLog.LogPath, config.C.Data.LogDir)
		if err == nil {
			result["content"] = content
		}
	}

	response.Success(c, result)
}

func (h *TaskHandler) LiveLogs(c *gin.Context) {
	taskID, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	var task model.Task
	database.DB.First(&task, taskID)

	done := task.Status != model.TaskStatusRunning

	var lines []string
	manager := service.GetTinyLogManager()
	tinyLog := manager.FindByTaskID(uint(taskID))
	if tinyLog != nil {
		data, _ := tinyLog.ReadLastLines(200)
		if len(data) > 0 {
			lines = strings.Split(string(data), "\n")
		}
	}

	if lines == nil {
		lines = []string{}
	}

	response.Success(c, gin.H{
		"logs":   lines,
		"done":   done,
		"status": task.Status,
	})
}

func (h *TaskHandler) LogFiles(c *gin.Context) {
	taskID, _ := strconv.ParseUint(c.Param("id"), 10, 32)
	files := service.ListLogFiles(uint(taskID), config.C.Data.LogDir)
	response.Success(c, files)
}

func (h *TaskHandler) LogFileContent(c *gin.Context) {
	taskID, _ := strconv.ParseUint(c.Param("id"), 10, 32)
	filename := c.Param("filename")
	filenameOrPath := c.DefaultQuery("path", filename)

	logPath, err := service.ResolveTaskLogPath(uint(taskID), filenameOrPath, config.C.Data.LogDir)
	if err != nil {
		response.NotFound(c, "日志文件不存在")
		return
	}
	content, err := service.ReadLogFile(logPath, config.C.Data.LogDir)
	if err != nil {
		response.NotFound(c, "日志文件不存在")
		return
	}

	response.Success(c, gin.H{"filename": filename, "content": content})
}

func (h *TaskHandler) DeleteLogFile(c *gin.Context) {
	taskID, _ := strconv.ParseUint(c.Param("id"), 10, 32)
	filename := c.Param("filename")
	filenameOrPath := c.DefaultQuery("path", filename)

	logPath, err := service.ResolveTaskLogPath(uint(taskID), filenameOrPath, config.C.Data.LogDir)
	if err != nil {
		response.NotFound(c, "日志文件不存在")
		return
	}
	if err := service.DeleteLogFile(logPath, config.C.Data.LogDir); err != nil {
		response.InternalError(c, "删除日志文件失败")
		return
	}
	response.Success(c, gin.H{"message": "日志文件已删除"})
}

func (h *TaskHandler) DownloadLogFile(c *gin.Context) {
	taskID, _ := strconv.ParseUint(c.Param("id"), 10, 32)
	filename := c.Param("filename")
	filenameOrPath := c.DefaultQuery("path", filename)

	logPath, err := service.ResolveTaskLogPath(uint(taskID), filenameOrPath, config.C.Data.LogDir)
	if err != nil {
		response.NotFound(c, "日志文件不存在")
		return
	}
	content, err := service.ReadLogFile(logPath, config.C.Data.LogDir)
	if err != nil {
		response.NotFound(c, "日志文件不存在")
		return
	}

	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
	c.Data(http.StatusOK, "text/plain; charset=utf-8", []byte(content))
}

func (h *TaskHandler) CleanLogs(c *gin.Context) {
	defaultDays := model.GetRegisteredConfigInt("log_retention_days")
	daysStr := c.DefaultQuery("days", strconv.Itoa(defaultDays))
	days, _ := strconv.Atoi(daysStr)
	if days < 1 {
		days = defaultDays
	}

	count := service.CleanOldLogs(config.C.Data.LogDir, days)
	response.Success(c, gin.H{"message": fmt.Sprintf("已清理 %d 个日志文件（保留最近 %d 天）", count, days)})
}
