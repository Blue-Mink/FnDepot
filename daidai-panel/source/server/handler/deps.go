package handler

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"daidai-panel/database"
	"daidai-panel/middleware"
	"daidai-panel/model"
	"daidai-panel/pkg/response"
	"daidai-panel/service"

	"github.com/gin-gonic/gin"
)

type depLogBroadcaster struct {
	mu   sync.RWMutex
	subs map[chan string]struct{}
}

var (
	depLogStreams   = make(map[uint]*depLogBroadcaster)
	depLogStreamsMu sync.RWMutex
	depOperations   = make(map[uint]context.CancelFunc)
	depOpsMu        sync.Mutex

	dependencyInstallRunner  = installDependency
	dependencyExportTextFunc = buildDependencyExportText
)

// defaultDependencyOperationTimeout 是 dependency_install_timeout_minutes 读不出来时的兜底值，
// 与该配置项的注册默认值保持一致。真正生效的阈值一律走 resolveDependencyOperationTimeout()。
const defaultDependencyOperationTimeout = 20 * time.Minute

// resolveDependencyOperationTimeout 读取用户配置的依赖操作超时。
// 配置项注册在 model 层并带 5-720 分钟的区间校验，这里只对「数据库里存着历史越界值」
// 这一种情况再兜一次底，避免非法值把超时变成 0（等于立刻杀进程）。
func resolveDependencyOperationTimeout() time.Duration {
	minutes := model.GetRegisteredConfigInt("dependency_install_timeout_minutes")
	if minutes < 5 || minutes > 720 {
		return defaultDependencyOperationTimeout
	}
	return time.Duration(minutes) * time.Minute
}

func getOrCreateBroadcaster(id uint) *depLogBroadcaster {
	depLogStreamsMu.Lock()
	defer depLogStreamsMu.Unlock()
	if b, ok := depLogStreams[id]; ok {
		return b
	}
	b := &depLogBroadcaster{subs: make(map[chan string]struct{})}
	depLogStreams[id] = b
	return b
}

func removeBroadcaster(id uint) {
	depLogStreamsMu.Lock()
	defer depLogStreamsMu.Unlock()
	if b, ok := depLogStreams[id]; ok {
		b.mu.Lock()
		for ch := range b.subs {
			close(ch)
		}
		b.mu.Unlock()
		delete(depLogStreams, id)
	}
}

func registerDepOperation(id uint, cancel context.CancelFunc) {
	depOpsMu.Lock()
	defer depOpsMu.Unlock()
	depOperations[id] = cancel
}

func unregisterDepOperation(id uint) {
	depOpsMu.Lock()
	defer depOpsMu.Unlock()
	delete(depOperations, id)
}

func cancelDepOperation(id uint) bool {
	depOpsMu.Lock()
	cancel, exists := depOperations[id]
	depOpsMu.Unlock()
	if !exists {
		return false
	}

	cancel()
	return true
}

func (b *depLogBroadcaster) subscribe() chan string {
	ch := make(chan string, 64)
	b.mu.Lock()
	b.subs[ch] = struct{}{}
	b.mu.Unlock()
	return ch
}

func (b *depLogBroadcaster) unsubscribe(ch chan string) {
	b.mu.Lock()
	delete(b.subs, ch)
	b.mu.Unlock()
}

func (b *depLogBroadcaster) broadcast(line string) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for ch := range b.subs {
		select {
		case ch <- line:
		default:
		}
	}
}

func (b *depLogBroadcaster) done() {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for ch := range b.subs {
		select {
		case ch <- "\x00DONE":
		default:
		}
	}
}

type DepsHandler struct{}

func NewDepsHandler() *DepsHandler {
	return &DepsHandler{}
}

func normalizeDependencyPythonVersion(depType, raw string) (string, error) {
	if depType != model.DepTypePython {
		return "", nil
	}
	return service.NormalizePythonVersionStrict(raw)
}

func dependencyPythonInstallVersions(depType string) []string {
	if depType != model.DepTypePython {
		return []string{""}
	}
	return service.SupportedPythonVersions()
}

func (h *DepsHandler) List(c *gin.Context) {
	depType := c.DefaultQuery("type", "nodejs")

	validTypes := map[string]bool{
		model.DepTypeNodeJS: true,
		model.DepTypePython: true,
		model.DepTypeLinux:  true,
	}
	if !validTypes[depType] {
		response.BadRequest(c, "无效的依赖类型")
		return
	}

	var deps []model.Dependency
	query := database.DB.Where("type = ?", depType)
	if depType == model.DepTypePython {
		pythonVersion, err := normalizeDependencyPythonVersion(depType, c.Query("python_version"))
		if err != nil {
			response.BadRequest(c, err.Error())
			return
		}
		query = query.Where("COALESCE(NULLIF(python_version, ''), ?) = ?", service.LegacyPythonVersion(), pythonVersion)
	}
	query.Order("created_at DESC").Find(&deps)

	data := make([]map[string]interface{}, len(deps))
	for i, d := range deps {
		data[i] = d.ToDict()
	}

	// failed_by_type 是跨类型的失败数汇总，专门给依赖页三个类型页签上的「失败 N」用。
	//
	// 【为什么要额外下发，而不是让前端数 data】
	// 上面的 data 只含当前请求的那个类型（Python 还只含当前版本），前端数出来的失败数
	// 天然只是「当前标签页」的。而侧栏「依赖管理」角标（server/handler/system_badges.go 的
	// deps_failed）是不分类型、不分版本的全表 status='failed' 计数，两个数字对不上，
	// 用户得挨个切三个标签页才能凑出角标那个数。
	//
	// 【为什么 Python 不跟着 python_version 过滤】
	// 同理：只有让 nodejs + python + linux 三个数之和恰好等于全表失败数，页签上的数字
	// 才能和侧栏角标对上。所以这里刻意不复用上面的版本过滤，Python 跨所有版本一起统计。
	//
	// 查询用一条 GROUP BY type 完成，不为每个类型各查一次。
	failedByType := map[string]int64{
		model.DepTypeNodeJS: 0,
		model.DepTypePython: 0,
		model.DepTypeLinux:  0,
	}
	type depFailedCountRow struct {
		Type  string
		Total int64
	}
	var failedRows []depFailedCountRow
	database.DB.Model(&model.Dependency{}).
		Select("type, COUNT(*) AS total").
		Where("status = ?", model.DepStatusFailed).
		Group("type").
		Scan(&failedRows)
	for _, row := range failedRows {
		// 只回填三个已知类型，历史脏数据里若有别的 type，忽略即可，
		// 免得凭空多出一个前端不认识的键。
		if _, ok := failedByType[row.Type]; ok {
			failedByType[row.Type] = row.Total
		}
	}

	response.Success(c, gin.H{"data": data, "total": len(data), "failed_by_type": failedByType})
}

func (h *DepsHandler) Create(c *gin.Context) {
	var req struct {
		Type          string   `json:"type" binding:"required"`
		Names         []string `json:"names" binding:"required"`
		PythonVersion string   `json:"python_version"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请求参数错误")
		return
	}

	validTypes := map[string]bool{
		model.DepTypeNodeJS: true,
		model.DepTypePython: true,
		model.DepTypeLinux:  true,
	}
	if !validTypes[req.Type] {
		response.BadRequest(c, "无效的依赖类型")
		return
	}
	created := []map[string]interface{}{}
	skipped := 0
	for _, name := range req.Names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if strings.ContainsAny(name, ";|&`$(){}") {
			continue
		}

		for _, pythonVersion := range dependencyPythonInstallVersions(req.Type) {
			// Python 依赖按 PEP 503 归一化键去重：同名（忽略大小写/分隔符差异）已存在且
			// 已安装/安装中/排队中的，跳过、不重复安装。
			if req.Type == model.DepTypePython {
				if _, exists := service.FindExistingPythonDependency(name, pythonVersion,
					model.DepStatusInstalled, model.DepStatusInstalling, model.DepStatusQueued); exists {
					skipped++
					continue
				}
			} else {
				// nodejs / linux 同样做「先查后插」，但只按「类型 + 名称」精确匹配，
				// 不套 Python 那套 PEP 503 归一化 —— 各生态的包名归一规则不同（npm 区分大小写、
				// apt 包名带冒号架构后缀），硬套会把不同的包判成同一个，属于误伤。
				// 这里也刻意不给 dependencies 加 DB 唯一索引，理由同上。
				var existingCount int64
				database.DB.Model(&model.Dependency{}).
					Where("type = ? AND name = ? AND status IN ?", req.Type, name,
						[]string{model.DepStatusInstalled, model.DepStatusInstalling, model.DepStatusQueued}).
					Count(&existingCount)
				if existingCount > 0 {
					skipped++
					continue
				}
			}

			dep := model.Dependency{
				Type:          req.Type,
				Name:          name,
				PythonVersion: pythonVersion,
				Status:        model.DepStatusInstalling,
			}
			if err := database.DB.Create(&dep).Error; err != nil {
				continue
			}
			created = append(created, dep.ToDict())

			go dependencyInstallRunner(dep.ID, req.Type, name)
		}
	}

	message := fmt.Sprintf("已提交 %d 个依赖安装", len(created))
	if req.Type == model.DepTypePython && len(created) > 0 {
		message = fmt.Sprintf("已提交 %d 个 Python 版本依赖安装", len(created))
	}
	if skipped > 0 {
		message = fmt.Sprintf("%s，已存在跳过 %d 个", message, skipped)
	}
	response.Created(c, gin.H{
		"message": message,
		"data":    created,
	})
}

func (h *DepsHandler) Delete(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	var dep model.Dependency
	if err := database.DB.First(&dep, id).Error; err != nil {
		response.NotFound(c, "依赖不存在")
		return
	}

	if dep.Status == model.DepStatusQueued || dep.Status == model.DepStatusInstalling || dep.Status == model.DepStatusRemoving {
		response.BadRequest(c, "依赖正在处理中")
		return
	}

	if c.Query("force") == "true" {
		database.DB.Delete(&dep)
		go forceUninstallDependency(dep.Type, dep.Name, dep.PythonVersion)
		response.Success(c, gin.H{"message": "强制卸载中"})
		return
	}

	database.DB.Model(&dep).Update("status", model.DepStatusRemoving)

	go uninstallDependency(dep.ID, dep.Type, dep.Name, dep.PythonVersion)

	response.Success(c, gin.H{"message": "卸载中"})
}

func (h *DepsHandler) BatchDelete(c *gin.Context) {
	var req struct {
		IDs []uint `json:"ids" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || len(req.IDs) == 0 {
		response.BadRequest(c, "请求参数错误")
		return
	}

	var deps []model.Dependency
	database.DB.Where("id IN ? AND status NOT IN ?", req.IDs, []string{model.DepStatusQueued, model.DepStatusInstalling, model.DepStatusRemoving}).Find(&deps)

	for _, dep := range deps {
		database.DB.Delete(&dep)
		go forceUninstallDependency(dep.Type, dep.Name, dep.PythonVersion)
	}

	response.Success(c, gin.H{"message": fmt.Sprintf("已提交 %d 个依赖卸载", len(deps))})
}

func (h *DepsHandler) GetStatus(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	var dep model.Dependency
	if err := database.DB.First(&dep, id).Error; err != nil {
		response.NotFound(c, "依赖不存在")
		return
	}

	response.Success(c, gin.H{"data": dep.ToDictWithLog()})
}

func (h *DepsHandler) LogStream(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	var dep model.Dependency
	if err := database.DB.First(&dep, id).Error; err != nil {
		c.JSON(404, gin.H{"error": "依赖不存在"})
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	if dep.Log != "" {
		for _, line := range strings.Split(dep.Log, "\n") {
			if line != "" {
				fmt.Fprintf(c.Writer, "data: %s\n\n", line)
			}
		}
		c.Writer.Flush()
	}

	if dep.Status != model.DepStatusInstalling && dep.Status != model.DepStatusRemoving {
		fmt.Fprintf(c.Writer, "event: done\ndata: %s\n\n", dep.Status)
		c.Writer.Flush()
		return
	}

	depLogStreamsMu.RLock()
	b, exists := depLogStreams[uint(id)]
	depLogStreamsMu.RUnlock()

	if !exists {
		fmt.Fprintf(c.Writer, "event: done\ndata: %s\n\n", dep.Status)
		c.Writer.Flush()
		return
	}

	sub := b.subscribe()
	defer b.unsubscribe(sub)

	// pip 现场编译 wheel（例如 opencv）时可以几十分钟一行输出都没有，
	// 原来的「静默 5 分钟就发 done」会让前端以为任务结束了，而进程其实还在跑。
	// 改成周期性心跳注释行（SSE 规范里以 : 开头的行会被客户端忽略），只用来保活连接；
	// 真正的结束仍然只由 \x00DONE 或订阅通道关闭来决定。
	heartbeat := time.NewTicker(30 * time.Second)
	defer heartbeat.Stop()

	// 硬上限只是防止 broadcaster 泄漏导致连接永不释放，正常路径不会走到。
	// 必须比依赖任务本身的超时更长，否则又会退化成「任务还在跑就断流」。
	hardDeadline := time.After(resolveDependencyOperationTimeout() + 5*time.Minute)

	ctx := c.Request.Context()
	for {
		select {
		case line, ok := <-sub:
			if !ok {
				fmt.Fprintf(c.Writer, "event: done\ndata: closed\n\n")
				c.Writer.Flush()
				return
			}
			if line == "\x00DONE" {
				var latest model.Dependency
				database.DB.First(&latest, id)
				fmt.Fprintf(c.Writer, "event: done\ndata: %s\n\n", latest.Status)
				c.Writer.Flush()
				return
			}
			fmt.Fprintf(c.Writer, "data: %s\n\n", line)
			c.Writer.Flush()
		case <-ctx.Done():
			return
		case <-heartbeat.C:
			fmt.Fprintf(c.Writer, ": ping\n\n")
			c.Writer.Flush()
		case <-hardDeadline:
			fmt.Fprintf(c.Writer, "event: done\ndata: timeout\n\n")
			c.Writer.Flush()
			return
		}
	}
}

func (h *DepsHandler) Reinstall(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	var dep model.Dependency
	if err := database.DB.First(&dep, id).Error; err != nil {
		response.NotFound(c, "依赖不存在")
		return
	}

	if dep.Status == model.DepStatusQueued || dep.Status == model.DepStatusInstalling || dep.Status == model.DepStatusRemoving {
		response.BadRequest(c, "依赖正在处理中")
		return
	}

	database.DB.Model(&dep).Updates(map[string]interface{}{
		"status": model.DepStatusInstalling,
		"log":    "",
	})

	go dependencyInstallRunner(dep.ID, dep.Type, dep.Name)

	response.Success(c, gin.H{"message": "重新安装中"})
}

func appendDepsLog(existing, line string) string {
	existing = strings.TrimRight(existing, "\n")
	line = strings.TrimSpace(line)
	if line == "" {
		return existing
	}
	if existing == "" {
		return line
	}
	if strings.Contains(existing, line) {
		return existing
	}
	return existing + "\n" + line
}

func (h *DepsHandler) BatchReinstall(c *gin.Context) {
	var req struct {
		IDs []uint `json:"ids" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || len(req.IDs) == 0 {
		response.BadRequest(c, "请求参数错误")
		return
	}

	var deps []model.Dependency
	database.DB.Where("id IN ? AND status NOT IN ?", req.IDs, []string{model.DepStatusQueued, model.DepStatusInstalling, model.DepStatusRemoving}).Find(&deps)
	if len(deps) == 0 {
		response.BadRequest(c, "选中的依赖当前无法重装")
		return
	}

	depMap := make(map[uint]model.Dependency, len(deps))
	for _, dep := range deps {
		depMap[dep.ID] = dep
	}

	queue := make([]model.Dependency, 0, len(req.IDs))
	for _, id := range req.IDs {
		dep, ok := depMap[id]
		if !ok {
			continue
		}
		queue = append(queue, dep)
	}
	if len(queue) == 0 {
		response.BadRequest(c, "选中的依赖当前无法重装")
		return
	}

	for index, dep := range queue {
		database.DB.Model(&model.Dependency{}).Where("id = ?", dep.ID).Updates(map[string]interface{}{
			"status": model.DepStatusQueued,
			"log":    appendDepsLog(dep.Log, fmt.Sprintf("[批量重装] 已加入顺序队列（%d/%d）", index+1, len(queue))),
		})
	}

	go func(ordered []model.Dependency) {
		for index, dep := range ordered {
			var current model.Dependency
			if err := database.DB.First(&current, dep.ID).Error; err != nil {
				continue
			}

			database.DB.Model(&model.Dependency{}).Where("id = ?", dep.ID).Updates(map[string]interface{}{
				"status": model.DepStatusInstalling,
				"log":    appendDepsLog(current.Log, fmt.Sprintf("[批量重装] 开始执行（%d/%d）", index+1, len(ordered))),
			})

			dependencyInstallRunner(dep.ID, dep.Type, dep.Name)
		}
	}(queue)

	response.Success(c, gin.H{"message": fmt.Sprintf("已提交 %d 个依赖顺序重装", len(queue))})
}

func (h *DepsHandler) Cancel(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	var dep model.Dependency
	if err := database.DB.First(&dep, id).Error; err != nil {
		response.NotFound(c, "依赖不存在")
		return
	}

	if dep.Status != model.DepStatusInstalling && dep.Status != model.DepStatusRemoving {
		response.BadRequest(c, "当前依赖任务未在处理中")
		return
	}

	if !cancelDepOperation(uint(id)) {
		response.BadRequest(c, "当前依赖任务未在运行中")
		return
	}

	response.Success(c, gin.H{"message": "取消请求已提交"})
}

func (h *DepsHandler) PipList(c *gin.Context) {
	pythonVersion, err := service.NormalizePythonVersionStrict(c.Query("python_version"))
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	pipEnv := service.WritableHomeEnv(service.SanitizePipEnv(os.Environ()))
	listCmd, err := service.NewPipCommandForPythonVersion(pythonVersion, []string{"list", "--format=json"})
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	listCmd.Env = pipEnv
	out, err := listCmd.Output()
	if err != nil {
		response.InternalError(c, "pip 不可用")
		return
	}
	c.Data(200, "application/json", out)
}

func (h *DepsHandler) Export(c *gin.Context) {
	depType := c.DefaultQuery("type", model.DepTypeNodeJS)

	validTypes := map[string]bool{
		model.DepTypeNodeJS: true,
		model.DepTypePython: true,
		model.DepTypeLinux:  true,
	}
	if !validTypes[depType] {
		response.BadRequest(c, "无效的依赖类型")
		return
	}

	var deps []model.Dependency
	query := database.DB.Where("type = ? AND status = ?", depType, model.DepStatusInstalled)
	filenameType := depType
	if depType == model.DepTypePython {
		pythonVersion, err := normalizeDependencyPythonVersion(depType, c.Query("python_version"))
		if err != nil {
			response.BadRequest(c, err.Error())
			return
		}
		query = query.Where("COALESCE(NULLIF(python_version, ''), ?) = ?", service.LegacyPythonVersion(), pythonVersion)
		filenameType = depType + "-" + strings.ReplaceAll(pythonVersion, ".", "")
	}
	query.Order("name ASC").Find(&deps)

	text, err := dependencyExportTextFunc(depType, deps)
	if err != nil {
		response.InternalError(c, "导出依赖清单失败: "+err.Error())
		return
	}

	filename := fmt.Sprintf("dependencies-%s-%s.txt", filenameType, time.Now().Format("20060102-150405"))
	c.Header("Content-Type", "text/plain; charset=utf-8")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))
	c.String(200, text)
}

func (h *DepsHandler) PythonRuntimes(c *gin.Context) {
	response.Success(c, gin.H{
		"data":            service.PythonRuntimeInfos(),
		"default_version": service.DefaultPythonVersion(),
	})
}

func (h *DepsHandler) SetDefaultPythonRuntime(c *gin.Context) {
	var req struct {
		Version string `json:"version" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请求参数错误")
		return
	}
	version, err := service.NormalizePythonVersionStrict(req.Version)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	if !service.PythonVersionSupportedByCurrentRuntime(version) {
		response.BadRequest(c, fmt.Sprintf("当前镜像不支持 Python %s，请切换到对应 Python 版本镜像或 all 镜像", version))
		return
	}
	if err := model.SetConfig("python_default_version", version); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.Success(c, gin.H{"message": "默认 Python 版本已更新", "default_version": version})
}

func (h *DepsHandler) NpmList(c *gin.Context) {
	// 与安装路径共用同一份 HOME 判定：HOME 不可写时 npm 连 list 都跑不起来
	// （启动即初始化 $HOME/.npm 的 cache），会变成「装得上却看不到」。
	listCmd := exec.Command("npm", "list", "-g", "--json", "--depth=0")
	listCmd.Env = service.WritableHomeEnv(os.Environ())
	out, err := listCmd.Output()
	if err != nil {
		response.InternalError(c, "npm 不可用")
		return
	}
	c.Data(200, "application/json", out)
}

func (h *DepsHandler) GetMirrors(c *gin.Context) {
	result := gin.H{
		"pip_mirror":             service.CurrentEffectivePipMirror(),
		"npm_mirror":             service.CurrentEffectiveNpmMirror(),
		"linux_mirror":           "",
		"linux_package_manager":  "",
		"linux_distribution":     "",
		"linux_mirror_supported": false,
		"linux_mirror_label":     "Linux",
		"linux_mirror_message":   "",
	}

	linuxMirrorInfo := getLinuxMirrorInfo()
	result["linux_package_manager"] = linuxMirrorInfo.Manager
	result["linux_distribution"] = linuxMirrorInfo.Distribution
	result["linux_mirror"] = linuxMirrorInfo.Mirror
	result["linux_mirror_supported"] = linuxMirrorInfo.Supported
	result["linux_mirror_label"] = linuxMirrorInfo.Label
	result["linux_mirror_message"] = linuxMirrorInfo.Message

	response.Success(c, result)
}

func (h *DepsHandler) SetMirrors(c *gin.Context) {
	var req struct {
		PipMirror   *string `json:"pip_mirror"`
		NpmMirror   *string `json:"npm_mirror"`
		LinuxMirror *string `json:"linux_mirror"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请求参数错误")
		return
	}

	var errors []string

	if req.PipMirror != nil {
		if err := service.SetPipMirror(*req.PipMirror); err != nil {
			errors = append(errors, err.Error())
		}
	}

	if req.NpmMirror != nil {
		if err := service.SetNpmMirror(*req.NpmMirror); err != nil {
			errors = append(errors, err.Error())
		}
	}

	if req.LinuxMirror != nil {
		mirror := strings.TrimSpace(*req.LinuxMirror)
		manager, err := detectLinuxPackageManager()
		if err != nil {
			errors = append(errors, err.Error())
		} else {
			distribution := detectLinuxDistribution()
			if err := setLinuxMirror(manager, distribution, mirror); err != nil {
				errors = append(errors, "设置 Linux 镜像源失败: "+err.Error())
			}
		}
	}

	if len(errors) > 0 {
		response.BadRequest(c, strings.Join(errors, "; "))
		return
	}

	response.Success(c, gin.H{"message": "镜像源设置成功"})
}

func runCmdWithSSE(cmd *exec.Cmd, id uint, successStatus string, deleteOnSuccess bool) {
	broadcaster := getOrCreateBroadcaster(id)
	defer removeBroadcaster(id)

	service.SetPgid(cmd)

	pipe, err := cmd.StdoutPipe()
	if err != nil {
		database.DB.Model(&model.Dependency{}).Where("id = ?", id).Updates(map[string]interface{}{
			"status": model.DepStatusFailed,
			"log":    err.Error(),
		})
		broadcaster.done()
		return
	}
	cmd.Stderr = cmd.Stdout

	// 阈值只在任务启动时读一次并存下来，保证下面日志里写的数字就是本次实际生效的数字；
	// 中途用户改配置不影响已经跑起来的任务。
	operationTimeout := resolveDependencyOperationTimeout()

	ctx, cancel := context.WithTimeout(context.Background(), operationTimeout)
	registerDepOperation(id, cancel)
	defer func() {
		cancel()
		unregisterDepOperation(id)
	}()

	if err := cmd.Start(); err != nil {
		database.DB.Model(&model.Dependency{}).Where("id = ?", id).Updates(map[string]interface{}{
			"status": model.DepStatusFailed,
			"log":    err.Error(),
		})
		broadcaster.done()
		return
	}

	var logBuf strings.Builder
	var logMu sync.Mutex
	var existing model.Dependency
	if err := database.DB.Select("log").First(&existing, id).Error; err == nil && existing.Log != "" {
		logBuf.WriteString(existing.Log)
		if !strings.HasSuffix(existing.Log, "\n") {
			logBuf.WriteString("\n")
		}
	}
	lastPersistAt := time.Now()
	logDirty := false
	appendLine := func(line string, broadcast bool) {
		logMu.Lock()
		defer logMu.Unlock()

		logBuf.WriteString(line)
		logBuf.WriteString("\n")
		logDirty = true
		if broadcast {
			broadcaster.broadcast(line)
		}
	}
	flushLog := func(force bool) {
		logMu.Lock()
		defer logMu.Unlock()

		if !logDirty {
			return
		}
		if !force && time.Since(lastPersistAt) < 250*time.Millisecond {
			return
		}
		database.DB.Model(&model.Dependency{}).Where("id = ?", id).Update("log", logBuf.String())
		lastPersistAt = time.Now()
		logDirty = false
	}

	appendLine(fmt.Sprintf("[依赖任务已启动，超时阈值：%s，可在「系统设置 - 依赖安装超时(分钟)」调整]", operationTimeout.Truncate(time.Second)), true)

	scanDone := make(chan struct{})
	go func() {
		defer close(scanDone)

		scanner := bufio.NewScanner(pipe)
		scanner.Buffer(make([]byte, 64*1024), 256*1024)
		for scanner.Scan() {
			appendLine(scanner.Text(), true)
			flushLog(false)
		}

		if err := scanner.Err(); err != nil {
			appendLine("[读取安装输出失败] "+err.Error(), true)
		}
	}()

	waitCh := make(chan error, 1)
	go func() {
		waitCh <- cmd.Wait()
	}()

	status := successStatus
	waitErr := error(nil)
	select {
	case waitErr = <-waitCh:
	case <-ctx.Done():
		if cmd.Process != nil {
			service.KillProcessGroup(cmd.Process)
		}
		waitErr = <-waitCh
		switch {
		case waitErr == nil:
			// 临界情况：进程恰好在超时/取消的同一瞬间正常退出，杀进程没杀到活的。
			// 命令本身是成功的，不能因为抢跑了 ctx.Done() 就把成功记成失败。
			appendLine("[依赖任务在超时/取消触发的同时已正常结束，按成功处理]", true)
		case ctx.Err() == context.DeadlineExceeded:
			appendLine("[依赖任务已超时，进程已终止]", true)
			status = model.DepStatusFailed
		default:
			appendLine("[依赖任务已取消]", true)
			status = model.DepStatusCancelled
		}
	}

	<-scanDone
	if waitErr != nil && status == successStatus {
		status = model.DepStatusFailed
		if hint := buildDependencyFailureHint(logBuf.String()); hint != "" {
			appendLine(hint, true)
		}
	}

	flushLog(true)

	if deleteOnSuccess && status == successStatus {
		database.DB.Delete(&model.Dependency{}, id)
	} else {
		logMu.Lock()
		finalLog := logBuf.String()
		logMu.Unlock()
		database.DB.Model(&model.Dependency{}).Where("id = ?", id).Updates(map[string]interface{}{
			"status": status,
			"log":    finalLog,
		})
	}

	if status == successStatus {
		go service.SnapshotDepsToHost()
	}

	broadcaster.done()
}

func buildDependencyFailureHint(logText string) string {
	lower := strings.ToLower(logText)
	switch {
	case strings.Contains(lower, "could not get lock") ||
		strings.Contains(lower, "unable to acquire the dpkg frontend lock") ||
		strings.Contains(lower, "unable to lock database") ||
		strings.Contains(lower, "another app is currently holding the yum lock"):
		return "[检测到系统包管理器锁冲突，请稍后重试，或先确认没有其他 apt/yum/dnf/apk 任务正在运行]"
	// DNS 解析失败必须与「网络/镜像源不可达」分开报。
	// 这两类的排查方向完全相反：解析失败时宿主机往往一切正常（宿主走系统 DNS，
	// 容器走自己的 /etc/resolv.conf），此时让用户去「检查宿主机网络连通性」
	// 只会让他反复确认一个本来就没问题的东西，真正坏掉的那条线索反而被抹掉。
	// 模块版 Debian 容器的装依赖失败就长这样，之前一直被归到下面那条里误诊。
	case strings.Contains(lower, "temporary failure resolving") ||
		strings.Contains(lower, "temporary failure in name resolution") ||
		strings.Contains(lower, "could not resolve") ||
		strings.Contains(lower, "name or service not known"):
		return "[检测到容器内 DNS 解析失败：域名解析不出来，这与宿主机能否上网是两回事——" +
			"容器用的是自己的 /etc/resolv.conf。请在容器内执行 getent hosts mirrors.nju.edu.cn 复现，" +
			"并确认 /etc/resolv.conf 里的 nameserver 在当前网络下可用（校园网/企业网强制 DNS、" +
			"公共 Wi-Fi 登录门户、运营商屏蔽对外 53 端口都会导致这种失败）；" +
			"若 root 能解析而 apt 仍失败，则是 apt 降权用户 _apt 被限制联网，" +
			"需在 /etc/apt/apt.conf.d/ 下配置 APT::Sandbox::User \"root\"]"
	case strings.Contains(lower, "connection timed out") ||
		strings.Contains(lower, "connection refused") ||
		strings.Contains(lower, "failed to fetch"):
		return "[检测到镜像源不可达或网络中断（域名能解析但连不上/下载失败），" +
			"请检查 Linux 镜像源配置、代理设置和网络连通性，必要时更换镜像源后重试]"
	// 顺序契约：这条必须夹在「镜像源」与「Alpine glibc 不兼容」之间。
	//   - 排在锁冲突 / DNS / 镜像源之后：那三类是更靠前的次生故障，先解决它们才对；
	//   - 排在 isAlpineGlibcIncompatible 之前：后者的关键词（failed to build installable wheels、
	//     manylinux）太宽，日志里一出现就会盖掉「编译器 / CMake 根本不存在」这个更具体的真因。
	//     两条结论的第一步动作并不相同（换镜像拿预编译包 vs 装工具链现场编），所以两边的文案
	//     都必须把这两条出路并列写出来，而不是各给一半（issue #120）。
	case isMissingBuildToolchain(lower):
		return buildMissingToolchainHint(detectDependencyToolchainEnv())
	case isAlpineGlibcIncompatible(lower):
		return buildAlpineGlibcHint(detectDependencyToolchainEnv())
	default:
		return ""
	}
}

// buildAlpineGlibcHint 是「musl 上没有预编译包」这条归因的结论文案。
//
// 主出路是换 Debian 版镜像：PyPI 上的科学计算包普遍只发 manylinux wheel（只认 glibc）、
// 不发 musllinux wheel，换过去 pip 直接下预编译包，根本不用编译 ——
// issue #120 的 opencv-python 就属于这一类（有 manylinux_2_17 的 x86_64/aarch64 wheel，没有任何 musllinux wheel）。
// 但必须同时写上退路：万一该包连 manylinux wheel 都没有，换完镜像照样会掉进源码编译，
// 那时该做的是装编译工具链，而不是继续换镜像。
//
// 这条退路要指到哪儿，必须跟着面板的运行身份走，理由与 buildMissingToolchainHint 完全一样：
// 降权部署（PUID/PGID）下用户照着「去 Linux 页签装」点下去，会先建出 3 条依赖记录、
// 再被 EnsureLinuxPackageManagerPrivilege 全部拒掉，白白多出 3 条 failed 和侧栏角标。
// 所以这里从 const 改成按环境组装的函数，非 root 时改指提权出路。
func buildAlpineGlibcHint(env dependencyToolchainEnv) string {
	install := "请到「依赖管理 → Linux」页签装好编译器与 cmake" +
		"（Alpine 装 build-base、linux-headers、cmake，Debian 装 build-essential、cmake）后再重装"
	if env.PrivilegeHint != "" {
		install = "面板内的「依赖管理 → Linux」页签在当前的非 root 运行身份下装不了系统包，" +
			"只能按下面的提权出路装 build-base、linux-headers、cmake" +
			"（换到 Debian 版镜像后则是 build-essential、cmake）后再重装"
	}

	hint := "[当前容器使用 Alpine 镜像（musl libc），该依赖在 musl 上没有可用的预编译包" +
		"（PyPI 上常见的 manylinux wheel 只认 glibc）。请切换到 Debian 版镜像（如 linzixuanzz/daidai-panel:debian）后重试，" +
		"多数这类包换过去就能直接装上、不用编译；若换镜像后仍然报编译失败，说明该包连 manylinux wheel 都没有，只能现场编译 —— " +
		install
	if env.PrivilegeHint != "" {
		// 与 buildMissingToolchainHint 同款收尾：把作用域写清楚，
		// 否则 PrivilegeHint 末句「Node.js / Python 依赖不受此限制」贴在一条刚失败的
		// Python 依赖日志末尾，会被读成自相矛盾。
		hint += "。装系统包这一步受面板运行身份限制：" + env.PrivilegeHint
	}
	return hint + "]"
}

func isAlpineGlibcIncompatible(lowerLog string) bool {
	if detectLinuxDistribution() != "alpine" {
		return false
	}
	return matchesAlpineGlibcHints(lowerLog)
}

// matchesAlpineGlibcHints 只做关键词判定、不读环境。
//
// 拆出来有两个原因：一是让 issue #120 那份原样日志能在非 Alpine 的开发机 / CI 上被断言
// 「仍然落在 glibc 这条车道里」；二是钉住 failed to build installable wheels ——
// 它是 #120 那份日志唯一的命中项，从这里删掉等于让用户拿到一条空提示。
func matchesAlpineGlibcHints(lowerLog string) bool {
	glibcHints := []string{
		"no matching distribution",
		"resolutionimpossible",
		"not a supported wheel on this platform",
		"failed to build installable wheels",
		"manylinux",
	}
	for _, hint := range glibcHints {
		if strings.Contains(lowerLog, hint) {
			return true
		}
	}
	return false
}

// isMissingBuildToolchain 判断日志是不是「机器上根本没有 C/C++ 编译器 / CMake」。
//
// 典型场景：pip 装 opencv-python、lxml、numpy 这类没有对应平台 wheel 的包时会回退到
// 源码编译，而面板镜像的精简档刻意不装编译器（Dockerfile 里还有构建期断言守着），
// 完整档虽有 build-base / build-essential 但不含 cmake，于是编译在第一步就断了。
//
// 关键词只收「工具本身不存在」这一类明确信号，不收 "failed building wheel" 之类
// 只说明「编译失败了」的宽泛结论 —— 那种日志的真实原因可能是缺头文件、缺系统库、
// 版本不兼容等等，硬归到这里就是新的误诊。只写 Failed building wheel 的日志由后面的
// matchesAlpineGlibcHints 接住（在 musl 上它多半确实是「没有预编译包」），两条结论的
// 文案都并列给了「换镜像」和「装工具链」，所以落到哪一条都不会把用户引进死胡同。
func isMissingBuildToolchain(lowerLog string) bool {
	hints := []string{
		// CMake 在、编译器不在：opencv-python 这类把 cmake 写进 build-system.requires 的包，
		// pip 构建隔离会先从 PyPI 装一个 cmake wheel（musllinux 的也有），于是现场变成
		// 「cmake 有、gcc/g++ 没有」，此时 CMake 会点名吐出下面这几句，pip 那一层只剩
		// Failed building wheel 这种看不出真因的结论（issue #120 的最可能形态）。
		//
		// 反过来，"An error occurred while configuring with CMake." 这句绝不能收：
		// 它不是 CMake 自己吐的，而是 scikit-build 的 cmaker.configure() 包装 ——
		// cmake 子进程返回非 0 就抛。也就是说它出现时 cmake 明明已经跑起来了，
		// 与「cmake / 编译器不存在」互斥；缺 zlib 之类系统开发包的失败照样会带上它
		// （上面还明明白白印着 The CXX compiler identification is GNU 12.2.0），
		// 收进来就是把工具链齐全的现场误判成缺工具链、把真因盖掉。
		// 真缺工具链时 CMake 必定另外打印下面这几条点名信号，不靠这句也照样命中。
		"no cmake_cxx_compiler could be found",
		"no cmake_c_compiler could be found",
		"cmake_cxx_compiler not set",
		"cmake_c_compiler not set",
		"the cxx compiler identification is unknown",
		"the c compiler identification is unknown",
		// 编译器缺失：sh / bash / exec.Command 三种报法都覆盖
		"gcc: not found",
		"g++: not found",
		"cc: not found",
		"make: not found",
		"gcc: command not found",
		"g++: command not found",
		"make: command not found",
		`exec: "gcc"`,
		`exec: "g++"`,
		`exec: "cc"`,
		"command 'gcc' failed",
		"command 'cc' failed",
		"command 'g++' failed",
		"unable to execute 'cc1plus'",
		"unable to execute 'gcc'",
		// CMake / Ninja 缺失：setuptools、scikit-build、cmake 包装器各有各的措辞
		"cmake: not found",
		"cmake: command not found",
		"cmake must be installed",
		"no cmake found",
		"cmake is required",
		"could not find cmake",
		"problem with the cmake installation",
		"ninja is required",
		"error: [errno 2] no such file or directory: 'cmake'",
		// 缺开发头文件，本质同样是「编译环境没准备好」
		"fatal error: python.h",
		// Windows 二进制部署下的等价故障
		"visual c++ 14.0 or greater is required",
	}
	for _, hint := range hints {
		if strings.Contains(lowerLog, hint) {
			return true
		}
	}
	return false
}

// dependencyToolchainEnv 是「缺编译工具链」这条归因分支需要的全部环境事实。
//
// 之所以抽成参数而不是在文案函数里现读环境：isAlpineGlibcIncompatible 直接读
// /etc/os-release，在 Windows 开发机上恒为 false，那条分支的文案单测根本覆盖不到。
// 这里改成「外层读环境、内层纯函数」，buildMissingToolchainHint 就能被直接调用测试。
type dependencyToolchainEnv struct {
	// Distribution 取 /etc/os-release 的 ID（alpine / debian / ubuntu ...），探测不到为空串
	Distribution string
	// PackageManager 取包管理器名（apk / apt / dnf / yum / microdnf / zypper），探测不到为空串
	PackageManager string
	// PrivilegeHint 非空即表示当前是非 root：内容就是用户真去点「安装」时会撞上的那段拦截说明
	PrivilegeHint string
}

// detectDependencyToolchainEnv 负责读环境，文案拼装交给纯函数 buildMissingToolchainHint。
func detectDependencyToolchainEnv() dependencyToolchainEnv {
	env := dependencyToolchainEnv{Distribution: detectLinuxDistribution()}
	if manager, err := detectLinuxPackageManager(); err == nil {
		env.PackageManager = manager.Name
	}
	// 非 root 时不能只叫用户「去 Linux 页签装」——他一点安装就会被
	// EnsureLinuxPackageManagerPrivilege 拦下，提示就自相矛盾了。
	// 这里直接复用它那段（已经按 Docker / Magisk / 裸机分岔好的）出路说明，避免两处维护同一套文案。
	if err := service.EnsureLinuxPackageManagerPrivilege(); err != nil {
		env.PrivilegeHint = err.Error()
	}
	return env
}

// buildMissingToolchainHint 是这条分支的纯函数内核：只依赖入参，不碰环境、不碰 DB。
func buildMissingToolchainHint(env dependencyToolchainEnv) string {
	// 包管理器比 /etc/os-release 更能代表「实际能用哪条命令装」，所以优先按它分岔；
	// 只有探测不到包管理器时才退回发行版 ID。
	family := strings.ToLower(strings.TrimSpace(env.PackageManager))
	if family == "" {
		switch strings.ToLower(strings.TrimSpace(env.Distribution)) {
		case "alpine":
			family = "apk"
		case "debian", "ubuntu":
			family = "apt"
		}
	}

	// packages 只回答「要装哪些包」，「去哪儿装」由下面按 PrivilegeHint 决定。
	// 两件事必须拆开：非 root 时面板内的「依赖管理 → Linux」页签根本装不了
	// （BuildLinuxPackageCommand 第一行就被 EnsureLinuxPackageManagerPrivilege 拒掉），
	// 再指路过去只会凭空多出几条 failed 依赖记录、把侧栏角标顶上去。
	packages := ""
	// musl 上优先换镜像：多数科学计算包（opencv-python、grpcio…）在 PyPI 上只发
	// manylinux wheel、不发 musllinux wheel，换到 Debian 版镜像 pip 直接下预编译包，
	// 根本不用编译；留在 Alpine 硬编反而是最慢、最容易撞超时的一条路。
	preferDebianImage := false
	switch family {
	case "apk":
		packages = "build-base、linux-headers、cmake"
		preferDebianImage = true
	case "apt":
		packages = "build-essential、cmake"
	case "dnf", "yum", "microdnf", "zypper":
		packages = "gcc、gcc-c++、make、cmake"
	}

	var body string
	if packages == "" {
		// 探测不到包管理器（Windows、裸机等），只能给通用结论，不编造包名。
		body = "当前环境探测不到 Linux 包管理器，需要先自行装好 C/C++ 编译工具链" +
			"（Linux 为 gcc/g++/make，Windows 为 Visual Studio 生成工具）与 CMake，再重装本依赖"
	} else {
		install := "请到「依赖管理 → Linux」页签安装 " + packages
		if env.PrivilegeHint != "" {
			install = "面板内的「依赖管理 → Linux」页签在当前的非 root 运行身份下装不了系统包，" +
				"只能按下面的提权出路装 " + packages
		}
		compile := install + "，装完再重装本依赖；现场编译很慢，" +
			"记得先到「系统设置 - 依赖安装超时(分钟)」把阈值调大，默认 20 分钟往往不够"
		if preferDebianImage {
			// 两条出路是并列关系，不是二选一：先给成本最低的换镜像，再给兜底的现场编译。
			body = "出路一：换到 Debian 版镜像（如 linzixuanzz/daidai-panel:debian）后重装 —— " +
				"多数科学计算包在 musl 上没有预编译包、在 glibc 上却有 manylinux wheel，换过去往往直接装上、不用编译。" +
				"出路二：若该包连 manylinux wheel 都没有，就只能现场编译：" + compile
		} else {
			body = compile
		}
	}

	hint := "[检测到缺少 C/C++ 编译工具链或 CMake：当前平台找不到可直接使用的预编译包，" +
		"回退到源码编译时发现编译器或 CMake 不存在。" + body
	if env.PrivilegeHint != "" {
		// 作用域必须写清楚：这条限制只卡「装 Linux 系统包」这一步。
		// 否则 PrivilegeHint 末句「Node.js / Python 依赖不受此限制」贴在一条刚失败的
		// Python 依赖日志末尾，会被读成自相矛盾。
		hint += "。装系统包这一步受面板运行身份限制：" + env.PrivilegeHint
	}
	return hint + "]"
}

func ensureTmpDir() {
	os.MkdirAll("/tmp", 0o1777)
}

func installDependency(id uint, depType, name string) {
	ensureTmpDir()
	var cmd *exec.Cmd
	pythonVersion := ""
	if depType == model.DepTypePython {
		var dep model.Dependency
		if err := database.DB.Select("python_version").First(&dep, id).Error; err == nil {
			pythonVersion = dep.PythonVersion
		}
		pythonVersion = service.NormalizePythonVersionOrDefault(pythonVersion)
		database.DB.Model(&model.Dependency{}).Where("id = ?", id).Update("python_version", pythonVersion)
	}
	switch depType {
	case model.DepTypeNodeJS:
		nodeUnlock := service.LockNodePackageOperation()
		defer nodeUnlock()

		if notice := service.NodeInstallCompatibilityNotice(name); notice != "" {
			database.DB.Model(&model.Dependency{}).Where("id = ?", id).Update("log", notice+"\n")
		}

		var err error
		cmd, err = service.NewNpmInstallCommand(name)
		if err != nil {
			database.DB.Model(&model.Dependency{}).Where("id = ?", id).Updates(map[string]interface{}{
				"status": model.DepStatusFailed,
				"log":    err.Error(),
			})
			return
		}
	case model.DepTypePython:
		var err error
		cmd, err = service.NewPipInstallCommandForPythonVersion(pythonVersion, name)
		if err != nil {
			database.DB.Model(&model.Dependency{}).Where("id = ?", id).Updates(map[string]interface{}{
				"status": model.DepStatusFailed,
				"log":    err.Error(),
			})
			return
		}
		cmd.Env = append(service.PipInstallEnv(service.AppendProxyEnv(os.Environ()), service.CurrentPipMirror()), "TMPDIR=/tmp")
	case model.DepTypeLinux:
		linuxPackageOperationMu.Lock()
		defer linuxPackageOperationMu.Unlock()

		manager, err := detectLinuxPackageManager()
		if err != nil {
			database.DB.Model(&model.Dependency{}).Where("id = ?", id).Updates(map[string]interface{}{
				"status": model.DepStatusFailed,
				"log":    err.Error(),
			})
			return
		}

		initialLog := fmt.Sprintf("[Linux] 已检测到包管理器：%s", manager.Binary)
		database.DB.Model(&model.Dependency{}).Where("id = ?", id).Update("log", initialLog+"\n")

		cmd, err = buildLinuxPackageCommand(manager, "install", name, false)
		if err != nil {
			database.DB.Model(&model.Dependency{}).Where("id = ?", id).Updates(map[string]interface{}{
				"status": model.DepStatusFailed,
				"log":    initialLog + "\n" + err.Error(),
			})
			return
		}
	default:
		database.DB.Model(&model.Dependency{}).Where("id = ?", id).Updates(map[string]interface{}{
			"status": model.DepStatusFailed,
			"log":    "不支持的类型",
		})
		return
	}

	runCmdWithSSE(cmd, id, model.DepStatusInstalled, false)
}

func uninstallDependency(id uint, depType, name, pythonVersion string) {
	var cmd *exec.Cmd
	switch depType {
	case model.DepTypeNodeJS:
		nodeUnlock := service.LockNodePackageOperation()
		defer nodeUnlock()

		var err error
		cmd, err = service.NewNpmUninstallCommand(name, false)
		if err != nil {
			database.DB.Model(&model.Dependency{}).Where("id = ?", id).Updates(map[string]interface{}{
				"status": model.DepStatusFailed,
				"log":    err.Error(),
			})
			return
		}
	case model.DepTypePython:
		var err error
		cmd, err = service.NewPipUninstallCommandForPythonVersion(pythonVersion, name)
		if err != nil {
			database.DB.Model(&model.Dependency{}).Where("id = ?", id).Updates(map[string]interface{}{
				"status": model.DepStatusFailed,
				"log":    err.Error(),
			})
			return
		}
		cmd.Env = service.SanitizePipEnv(service.AppendProxyEnv(os.Environ()))
	case model.DepTypeLinux:
		linuxPackageOperationMu.Lock()
		defer linuxPackageOperationMu.Unlock()

		manager, err := detectLinuxPackageManager()
		if err != nil {
			database.DB.Delete(&model.Dependency{}, id)
			return
		}

		cmd, err = buildLinuxPackageCommand(manager, "remove", name, false)
		if err != nil {
			// 这里【不能】删记录。容器配了 PUID/PGID 降权之后，这条路会因为
			// 「apk/apt 需要 root」直接返回错误；删掉记录的话前端看到行消失，
			// 等同于「卸载成功」，而包其实还在系统里，那段专门写的中文说明
			// 也一个字都不会显示出来。改成与上面 NodeJS / Python 两个分支一致：
			// 标记 failed 并把原因写进日志。
			database.DB.Model(&model.Dependency{}).Where("id = ?", id).Updates(map[string]interface{}{
				"status": model.DepStatusFailed,
				"log":    err.Error(),
			})
			return
		}
	default:
		database.DB.Delete(&model.Dependency{}, id)
		return
	}

	runCmdWithSSE(cmd, id, model.DepStatusInstalled, true)
}

func forceUninstallDependency(depType, name, pythonVersion string) {
	var cmd *exec.Cmd
	switch depType {
	case model.DepTypeNodeJS:
		nodeUnlock := service.LockNodePackageOperation()
		defer nodeUnlock()

		var err error
		cmd, err = service.NewNpmUninstallCommand(name, true)
		if err != nil {
			return
		}
	case model.DepTypePython:
		var err error
		cmd, err = service.NewPipUninstallCommandForPythonVersion(pythonVersion, name, "--no-deps")
		if err != nil {
			return
		}
		cmd.Env = service.SanitizePipEnv(service.AppendProxyEnv(os.Environ()))
	case model.DepTypeLinux:
		linuxPackageOperationMu.Lock()
		defer linuxPackageOperationMu.Unlock()

		manager, err := detectLinuxPackageManager()
		if err != nil {
			return
		}

		cmd, err = buildLinuxPackageCommand(manager, "remove", name, true)
		if err != nil {
			return
		}
	default:
		return
	}

	// 强制卸载时依赖行已经被删掉了，没有 id 可以注册取消函数，前端也没有入口去点取消。
	// 但它照样持有 apt / npm 的包锁（上面的 linuxPackageOperationMu、LockNodePackageOperation），
	// 卡死就会把后续所有依赖任务一起堵住，所以这里必须有超时兜底。
	service.SetPgid(cmd)

	ctx, cancel := context.WithTimeout(context.Background(), resolveDependencyOperationTimeout())
	defer cancel()

	if err := cmd.Start(); err != nil {
		return
	}

	waitCh := make(chan error, 1)
	go func() {
		waitCh <- cmd.Wait()
	}()

	select {
	case <-waitCh:
	case <-ctx.Done():
		if cmd.Process != nil {
			service.KillProcessGroup(cmd.Process)
		}
		<-waitCh
	}
}

func (h *DepsHandler) RegisterRoutes(r *gin.RouterGroup) {
	deps := r.Group("/deps", middleware.JWTAuth(), middleware.RequireAdmin())
	{
		deps.GET("", h.List)
		deps.POST("", h.Create)
		deps.POST("/batch-reinstall", h.BatchReinstall)
		deps.POST("/batch-delete", h.BatchDelete)
		deps.DELETE("/:id", h.Delete)
		deps.PUT("/:id/cancel", h.Cancel)
		deps.GET("/:id/status", h.GetStatus)
		deps.GET("/:id/log-stream", h.LogStream)
		deps.PUT("/:id/reinstall", h.Reinstall)
		deps.GET("/export", h.Export)

		deps.GET("/python-runtimes", h.PythonRuntimes)
		deps.PUT("/python-runtime-default", h.SetDefaultPythonRuntime)
		deps.GET("/pip", h.PipList)
		deps.GET("/npm", h.NpmList)

		deps.GET("/mirrors", h.GetMirrors)
		deps.PUT("/mirrors", h.SetMirrors)
	}
}
