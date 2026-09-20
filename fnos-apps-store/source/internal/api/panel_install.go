package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"fnos-store/internal/core"
	"fnos-store/internal/panel"
	"fnos-store/internal/platform"
	"fnos-store/internal/source"
)

// ── 官方应用中心（fnos-official）cloud 安装通道 ─────────────────────────────
//
// 与 FPK 下载通道（runStandard）并列：官方目录应用没有可直链下载的 FPK，
// 走面板自己的 cloud 下载 + install/task（请求契约来自对官方面板 UI 真实
// 流量抓包，见 internal/panel 包注释）。SSE 事件复用 runStandard 的 step
// 词汇（downloading/installing/verifying/done/error），前端无需新协议。

// panelDepChoice 是用户在依赖弹窗里对单个依赖的选择。
type panelDepChoice struct {
	AppName string `json:"appName"`
	Action  string `json:"action"` // "install" 装官方依赖 | "skip" 跳过（用已有同名应用）
}

// panelInstallParams 是官方安装请求的附加参数（?panel=<json>）。
type panelInstallParams struct {
	VolumeID int              `json:"volumeID"` // 0 = 用商店默认卷
	Deps     []panelDepChoice `json:"deps"`
}

func parsePanelParams(r *http.Request) panelInstallParams {
	var p panelInstallParams
	raw := r.URL.Query().Get("panel")
	if raw == "" {
		return p
	}
	_ = json.Unmarshal([]byte(raw), &p)
	return p
}

// isPanelApp 判断注册表条目是否走面板 cloud 通道。
func (s *Server) isPanelApp(app core.AppInfo) bool {
	return app.Source == source.OfficialSourceID && s.panelClient != nil && s.panelClient.Configured()
}

// panelAppDetailResponse 是 GET /api/apps/{appname}/panel-detail 的返回体：
// 官方应用详情页 + 依赖弹窗数据一次给齐。
type panelAppDetailResponse struct {
	App          panel.PanelDetail   `json:"app"`
	Volume       int                 `json:"volume"` // 建议默认安装卷
	SameNameApps map[string][]string `json:"same_name_apps"` // depAppName -> 商店目录里同名应用（"显示名 v版本 (源)"）
}

// handlePanelDetail 返回官方应用详情（描述/开发者/实时依赖状态）+
// 依赖在商店目录里的同名条目（用户可选「用已有的」而不是重复装官方依赖）。
func (s *Server) handlePanelDetail(w http.ResponseWriter, r *http.Request) {
	appname := r.PathValue("appname")
	app, ok := s.getRegistryApp(appname)
	if !ok {
		writeAPIError(w, http.StatusNotFound, "app not found")
		return
	}
	if !s.isPanelApp(app) {
		writeAPIError(w, http.StatusBadRequest, "该应用不走官方应用中心通道")
		return
	}
	detail, err := s.panelClient.AppDetail(r.Context(), appname)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, err.Error())
		return
	}
	volume := s.defaultPanelVolume()

	sameName := make(map[string][]string)
	for _, dep := range detail.InstallDepApps {
		var names []string
		for _, other := range s.listRegistryApps() {
			if other.AppName == dep.AppName {
				label := other.DisplayName + " v" + other.LatestVersion
				if other.Source != "" && other.Source != "fnos-apps" {
					label += "（" + other.Source + "）"
				} else if other.Source == "" {
					label += "（已安装）"
				}
				names = append(names, label)
			}
		}
		if len(names) > 0 {
			sameName[dep.AppName] = names
		}
	}
	writeJSON(w, http.StatusOK, panelAppDetailResponse{App: *detail, Volume: volume, SameNameApps: sameName})
}

// handlePanelTest 实测一次面板登录 + 目录拉取。
// 支持请求体覆盖 {username,password,base_url}（设置页未保存先测试）；
// 无覆盖值时用已保存的面板账号。
func (s *Server) handlePanelTest(w http.ResponseWriter, r *http.Request) {
	client := s.panelClient
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
		BaseURL  string `json:"base_url"`
	}
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&body); err == nil &&
			(strings.TrimSpace(body.Username) != "" || body.Password != "" || strings.TrimSpace(body.BaseURL) != "") {
			client = panel.NewClient(body.BaseURL, body.Username, body.Password)
		}
	}
	if client == nil || !client.Configured() {
		writeAPIError(w, http.StatusBadRequest, "面板账号未配置")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 40*time.Second)
	defer cancel()
	count, err := client.TestLogin(ctx)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "app_count": count})
}

// defaultPanelVolume 返回 cloud 安装建议卷：商店配置的 InstallVolume，0 则 1。
func (s *Server) defaultPanelVolume() int {
	if s.configMgr == nil {
		return 1
	}
	if v := s.configMgr.Get().InstallVolume; v > 0 {
		return v
	}
	return 1
}

// runPanelInstall 执行官方 cloud 安装（含依赖自动安装）。
// wizardParams 是用户在向导弹窗里填写的主应用安装参数（无向导的应用为空）；
// 依赖的向导字段按 install/info 的 initValue 自动填充，必填且无默认值的
// 依赖会明确报错（暂不支持对依赖弹向导）。
func (s *Server) runPanelInstall(ctx context.Context, stream *sseStream, opName string, app core.AppInfo, pparams panelInstallParams, wizardParams []platform.WizardParam) {
	client := s.panelClient
	volume := pparams.VolumeID
	if volume <= 0 {
		volume = s.defaultPanelVolume()
	}

	// 1) 依赖（顺序安装，面板目录里依赖先于主应用可用）
	detail, err := client.AppDetail(ctx, app.AppName)
	if err != nil {
		_ = stream.sendError("获取应用依赖失败: " + err.Error())
		return
	}
	depAction := make(map[string]string, len(pparams.Deps))
	for _, c := range pparams.Deps {
		depAction[c.AppName] = c.Action
	}
	for _, dep := range detail.InstallDepApps {
		if dep.Status != "noinstall" {
			_ = stream.sendProgress(progressPayload{Step: "installing", Message: fmt.Sprintf("依赖 %s 已安装（%s），跳过", dep.Name, dep.Status)})
			continue
		}
		if act, ok := depAction[dep.AppName]; ok && act == "skip" {
			_ = stream.sendProgress(progressPayload{Step: "installing", Message: fmt.Sprintf("按你的选择跳过依赖 %s（使用已有同名应用）", dep.Name)})
			continue
		}
		depParams, err := s.autoFillWizardParams(ctx, client, dep.AppName, dep.SourceID, dep.Version)
		if err != nil {
			_ = stream.sendError(fmt.Sprintf("依赖 %s: %s", dep.Name, err.Error()))
			return
		}
		_ = stream.sendProgress(progressPayload{Step: "installing", Message: fmt.Sprintf("正在安装依赖 %s v%s ...", dep.Name, dep.Version)})
		if err := s.panelInstallOne(ctx, stream, client, dep.AppName, dep.SourceID, dep.Version, volume, true, depParams); err != nil {
			_ = stream.sendError(fmt.Sprintf("依赖 %s 安装失败: %s", dep.Name, err.Error()))
			return
		}
	}

	// 2) 主应用
	if err := s.panelInstallOne(ctx, stream, client, app.AppName, app.PanelSourceID, app.LatestVersion, volume, false, wizardParams); err != nil {
		_ = stream.sendError(err.Error())
		return
	}

	// 3) 验证（daemon 列表里应已出现该应用）
	if err := s.verifyPanelInstalled(ctx, app.AppName); err != nil {
		_ = stream.sendError("安装验证失败: " + err.Error())
		return
	}

	// 本机下载次数 +1（与 FPK 通道一致的口径）
	if s.configMgr != nil {
		local := s.configMgr.Get()
		if local.LocalInstalls == nil {
			local.LocalInstalls = make(map[string]int)
		}
		local.LocalInstalls[app.AppName]++
		_ = s.configMgr.SaveConfig(local)
	}

	_ = s.refreshRegistry(ctx)
	_ = stream.sendProgress(progressPayload{Step: "done", NewVersion: app.LatestVersion, Message: "操作完成"})
}

// panelInstallOne 单个官方应用的 cloud 下载 + 安装 + 轮询。
func (s *Server) panelInstallOne(ctx context.Context, stream *sseStream, client *panel.Client, appName, sourceID, version string, volume int, isDep bool, customParams []platform.WizardParam) error {
	if sourceID == "" {
		return fmt.Errorf("缺少面板 sourceID（目录数据可能过期，请点「检查更新」后重试）")
	}

	_ = stream.sendProgress(progressPayload{Step: "downloading", Message: "正在从官方应用中心发起下载..."})
	dlID, err := client.DownloadTask(ctx, appName, sourceID, version, volume)
	if err != nil {
		return fmt.Errorf("启动下载失败: %w", err)
	}
	// 下载轮询（官方源包大小不一，10 分钟预算；进度按 download/status 的 progress 字段）
	deadline := time.Now().Add(10 * time.Minute)
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("下载超时")
		}
		st, err := client.DownloadStatus(ctx, dlID)
		if err != nil {
			return err
		}
		switch st.Status {
		case panel.TaskSuccess:
			break
		case panel.TaskFailed:
			return fmt.Errorf("下载失败: %s", st.Message)
		default:
			pct := int(st.Progress * 100)
			if pct < 0 {
				pct = 0
			}
			if pct > 99 {
				pct = 99
			}
			_ = stream.sendProgress(progressPayload{Step: "downloading", Progress: pct, Message: "正在从官方应用中心下载..."})
		}
		if st.Status == panel.TaskSuccess {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}

	_ = stream.sendProgress(progressPayload{Step: "installing", Message: "正在安装..."})
	panelParams := make([]panel.WizardParam, 0, len(customParams))
	for _, p := range customParams {
		panelParams = append(panelParams, panel.WizardParam{Key: p.Key, Value: p.Value})
	}
	taskID, err := client.InstallTask(ctx, appName, version, volume, panelParams)
	if err != nil {
		return fmt.Errorf("提交安装失败: %w", err)
	}
	deadline = time.Now().Add(15 * time.Minute)
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("安装超时")
		}
		st, err := client.InstallStatus(ctx, taskID)
		if err != nil {
			return err
		}
		switch st.Status {
		case panel.TaskSuccess:
			if isDep {
				_ = stream.sendProgress(progressPayload{Step: "installing", Message: fmt.Sprintf("依赖 %s 安装完成", appName)})
			}
			return nil
		case panel.TaskFailed:
			msg := st.OutputText
			if msg == "" {
				msg = "未知错误"
			}
			return fmt.Errorf("安装失败: %s", msg)
		default:
			pct := int(st.Progress * 100)
			if pct < 0 {
				pct = 0
			}
			if pct > 99 {
				pct = 99
			}
			_ = stream.sendProgress(progressPayload{Step: "installing", Progress: pct, Message: "正在安装..."})
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

// panelDownloadPackage 静默执行一次 cloud 下载（无进度上报），供安装向导
// 预取与依赖准备使用。下载结果留在面板的 appcenter-downloads 缓存里，
// 随后真正的安装会复用，不产生重复传输。
func (s *Server) panelDownloadPackage(ctx context.Context, client *panel.Client, appName, sourceID, version string) error {
	if sourceID == "" {
		return errors.New("缺少面板 sourceID（目录数据可能过期，请点「检查更新」后重试）")
	}
	dlID, err := client.DownloadTask(ctx, appName, sourceID, version, s.defaultPanelVolume())
	if err != nil {
		return err
	}
	deadline := time.Now().Add(10 * time.Minute)
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if time.Now().After(deadline) {
			return errors.New("下载超时")
		}
		st, err := client.DownloadStatus(ctx, dlID)
		if err != nil {
			return err
		}
		switch st.Status {
		case panel.TaskSuccess:
			return nil
		case panel.TaskFailed:
			return fmt.Errorf("下载失败: %s", st.Message)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

// preparePanelInstallInfo 下载包后取回向导定义（面板只对已下载的包返回
// install/info，未下载报 10100）。这是面板通道应用安装向导弹窗的「预取」
// 步骤，与原生应用中心「先下载再弹向导」的流程一致。
func (s *Server) preparePanelInstallInfo(ctx context.Context, app core.AppInfo) (*panel.InstallInfo, error) {
	client := s.panelClient
	if client == nil || !client.Configured() {
		return nil, errors.New("官方应用中心未启用：请先在设置里配置面板账号")
	}
	if app.PanelSourceID == "" {
		return nil, errors.New("缺少面板 sourceID（目录数据可能过期，请点「检查更新」后重试）")
	}
	if err := s.panelDownloadPackage(ctx, client, app.AppName, app.PanelSourceID, app.LatestVersion); err != nil {
		return nil, err
	}
	return client.InstallInfo(ctx, app.AppName, app.LatestVersion)
}

// autoFillWizardParams 为依赖应用自动填充向导参数：字段取向导声明的
// initValue。必填且无默认值的字段会明确报错（暂不对依赖弹向导）。
func (s *Server) autoFillWizardParams(ctx context.Context, client *panel.Client, appName, sourceID, version string) ([]platform.WizardParam, error) {
	if err := s.panelDownloadPackage(ctx, client, appName, sourceID, version); err != nil {
		return nil, fmt.Errorf("下载依赖包失败: %w", err)
	}
	info, err := client.InstallInfo(ctx, appName, version)
	if err != nil {
		if errors.Is(err, panel.ErrEndpointNotFound) {
			// Older panel without install/info: proceed without wizard
			// answers (required-field apps then fail with 19000 on submit,
			// exactly as before this fix — no worse).
			return nil, nil
		}
		return nil, fmt.Errorf("获取安装信息失败: %w", err)
	}
	if !info.WizardInfo.HasWizard || len(info.WizardInfo.WizardContent) == 0 {
		return nil, nil
	}
	var steps []struct {
		Items []struct {
			Type      string `json:"type"`
			Field     string `json:"field"`
			InitValue string `json:"initValue"`
			Rules     []struct {
				Required bool `json:"required"`
			} `json:"rules"`
		} `json:"items"`
	}
	if err := json.Unmarshal(info.WizardInfo.WizardContent, &steps); err != nil {
		return nil, fmt.Errorf("解析向导定义失败: %w", err)
	}
	var params []platform.WizardParam
	var missing []string
	for _, st := range steps {
		for _, it := range st.Items {
			if it.Type == "tips" || it.Field == "" {
				continue
			}
			required := false
			for _, r := range it.Rules {
				if r.Required {
					required = true
					break
				}
			}
			if it.InitValue == "" {
				if required {
					missing = append(missing, it.Field)
				}
				continue
			}
			params = append(params, platform.WizardParam{Key: it.Field, Value: it.InitValue})
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("以下字段必填且无默认值: %s，请在官方应用中心安装该依赖", strings.Join(missing, "、"))
	}
	return params, nil
}

// verifyPanelInstalled 等 daemon 列表里出现该应用（安装回调有秒级延迟）。
func (s *Server) verifyPanelInstalled(ctx context.Context, appName string) error {
	deadline := time.Now().Add(30 * time.Second)
	var lastErr error
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		found := false
		listErr := s.queue.WithCLI(func() error {
			list, err := s.ac.List()
			if err != nil {
				return err
			}
			for _, a := range list {
				if a.AppName == appName {
					found = true
					break
				}
			}
			return nil
		})
		if listErr == nil && found {
			return nil
		}
		lastErr = listErr
		if time.Now().After(deadline) {
			if lastErr != nil {
				return lastErr
			}
			return errors.New("应用未出现在已安装列表")
		}
		time.Sleep(2 * time.Second)
	}
}
