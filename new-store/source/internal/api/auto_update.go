package api

import (
	"log"

	"fnos-store/internal/core"
)

// AutoUpdateIfEnabled 在周期检查刷新注册表后调用：自动更新开启时，后台
// 启动一个自动更新周期。若已有周期在跑则跳过（TryLock 抢占）。
func (s *Server) AutoUpdateIfEnabled() {
	cfg := s.configMgr.Get()
	if !cfg.AutoUpdate {
		return
	}
	if !s.autoUpdateMu.TryLock() {
		return // 上一轮还没跑完，跳过本轮
	}
	go func() {
		defer s.autoUpdateMu.Unlock()
		s.runAutoUpdateCycle()
	}()
}

// runAutoUpdateCycle 顺序自动更新所有「有可用更新」的应用（避免同时更新
// 过多应用压垮 NAS）。逐个触发后台任务并等待完成后再下一个。
//
// 排除：
//   - 商店自身（自更新保持手动——会重启本进程，风险高）
//   - 用户已忽略更新的应用
//   - 官方应用（走官方应用中心更新，非 FPK 通道）
func (s *Server) runAutoUpdateCycle() {
	cfg := s.configMgr.Get()
	if !cfg.AutoUpdate {
		return
	}
	apps := s.listRegistryApps()
	updated := 0
	for _, app := range apps {
		if !app.Installed || app.Status != core.AppStatusUpdateAvailable {
			continue
		}
		if app.AppName == s.storeApp {
			continue
		}
		if cfg.IsAppIgnored(app.AppName) {
			continue
		}
		if s.isPanelApp(app) {
			continue
		}
		// 跳过「目标版本 == 已装版本」的无效更新：部分源的 fpk_version 陈旧，
		// has_update 为真但 pipeline 实际会重装同版本（no-op），每轮重复下载+
		// 重启应用。仅在确有版本变化时才自动更新。
		if app.FpkVersion != "" && app.FpkVersion == app.InstalledFpkVersion {
			continue
		}

		task := s.tasks.GetOrCreate(app.AppName, "update")
		if !s.queue.TryStart("update", app.AppName) {
			continue // 该应用已有操作在跑
		}
		log.Printf("auto-update: %s -> %s", app.AppName, app.FpkVersion)
		go s.runOperation(task, "update", app.AppName, app, nil, nil)
		<-task.done() // 等本应用更新完成再下一个（顺序执行，防过载）
		updated++
	}
	if updated > 0 {
		log.Printf("auto-update: 周期完成，共更新 %d 个应用", updated)
	}
}
