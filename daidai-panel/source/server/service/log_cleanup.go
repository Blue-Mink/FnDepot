package service

import (
	"log"
	"sync"
	"time"

	"daidai-panel/config"
	"daidai-panel/database"
	"daidai-panel/model"
)

var (
	logCleanupOnce sync.Once
	logCleanupStop chan struct{}
)

// StartLogCleanupWorker 启动周期清理后台 worker：
// 启动后延迟一小段时间先清一次，之后每 6 小时清理一次。
// 清理「数据库 TaskLog 旧记录」与「磁盘旧 .log 文件」（按 log_retention_days 判定，无开关），
// 以及「已过期的 token 黑名单行」。
func StartLogCleanupWorker() {
	logCleanupOnce.Do(func() {
		logCleanupStop = make(chan struct{})
		go logCleanupLoop()
		log.Println("log cleanup worker started (interval: 6h)")
	})
}

func StopLogCleanupWorker() {
	if logCleanupStop != nil {
		close(logCleanupStop)
	}
}

func logCleanupLoop() {
	ticker := time.NewTicker(6 * time.Hour)
	defer ticker.Stop()

	// 启动延迟，避免与启动迁移争抢
	time.Sleep(60 * time.Second)
	runPeriodicCleanup()

	for {
		select {
		case <-ticker.C:
			runPeriodicCleanup()
		case <-logCleanupStop:
			return
		}
	}
}

// runPeriodicCleanup 汇总挂在这个 6 小时 ticker 上的全部清理动作。
// 新增周期性清理请加在这里，不要再另起一个 goroutine + ticker。
func runPeriodicCleanup() {
	cleanupOldLogs()
	cleanupExpiredTokenBlocklist()
}

// cleanupExpiredTokenBlocklist 清掉已经过期、留着也不改变鉴权结果的 token 黑名单行。
func cleanupExpiredTokenBlocklist() {
	removed, err := CleanExpiredTokenBlocklist()
	if err != nil {
		log.Printf("token blocklist cleanup: delete expired rows failed: %v", err)
		return
	}
	if removed > 0 {
		log.Printf("token blocklist cleanup: removed %d expired rows", removed)
	}
}

// cleanupOldLogs 按 log_retention_days 清理过期日志（DB 记录 + 磁盘文件）。
func cleanupOldLogs() {
	days := model.GetRegisteredConfigInt("log_retention_days")
	if days < 1 {
		days = 1
	}
	cutoff := time.Now().AddDate(0, 0, -days)

	var deletedRecords int64
	if database.DB != nil {
		result := database.DB.Where("started_at < ?", cutoff).Delete(&model.TaskLog{})
		if result.Error != nil {
			log.Printf("log cleanup: delete TaskLog records failed: %v", result.Error)
		} else {
			deletedRecords = result.RowsAffected
		}
	}

	deletedFiles := 0
	if config.C != nil {
		deletedFiles = CleanOldLogs(config.C.Data.LogDir, days)
	}

	log.Printf("log cleanup: removed %d TaskLog records and %d log files (retention: %d days)", deletedRecords, deletedFiles, days)
}
