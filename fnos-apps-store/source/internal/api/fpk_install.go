package api

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"fnos-store/internal/core"
)

// fpkManifestInfo FPK 根级 manifest 的最小字段（直接安装只需要这三个）。
type fpkManifestInfo struct {
	appName    string // appname
	version    string // version
	fpkVersion string // fpk_version（含 -rN 修订号，比 version 更精确）
}

// readFpkManifest 读取并解析 FPK 根级 manifest（tar 包内 manifest 文件，key = value 格式）。
func readFpkManifest(fpkPath string) (fpkManifestInfo, error) {
	dir, err := os.MkdirTemp("", "fpk-manifest-*")
	if err != nil {
		return fpkManifestInfo{}, err
	}
	defer os.RemoveAll(dir)
	if out, err := exec.Command("tar", "xf", fpkPath, "-C", dir, "manifest").CombinedOutput(); err != nil {
		return fpkManifestInfo{}, fmt.Errorf("无法读取 FPK 清单: %s", strings.TrimSpace(string(out)))
	}
	data, err := os.ReadFile(filepath.Join(dir, "manifest"))
	if err != nil {
		return fpkManifestInfo{}, fmt.Errorf("FPK 中未找到 manifest 文件")
	}
	return parseFpkManifest(string(data)), nil
}

func parseFpkManifest(data string) fpkManifestInfo {
	var m fpkManifestInfo
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		kv := strings.SplitN(line, "=", 2)
		if len(kv) != 2 {
			continue
		}
		key := strings.TrimSpace(kv[0])
		val := strings.TrimSpace(kv[1])
		switch key {
		case "appname":
			m.appName = val
		case "version":
			m.version = val
		case "fpk_version":
			m.fpkVersion = val
		}
	}
	return m
}

// looksLikeDockerFpk 探测 FPK 是否含 docker/docker-compose.yaml。
// compose 嵌套在 app.tgz 里（FPK 顶层只有 manifest/app.tgz/cmd 等），需要两级展开；
// 只展开单个成员，成本可控。任何一步失败都按"非 docker"处理（安装仍可继续）。
func looksLikeDockerFpk(fpkPath string) bool {
	dir, err := os.MkdirTemp("", "fpk-probe-*")
	if err != nil {
		return false
	}
	defer os.RemoveAll(dir)
	appTgz := filepath.Join(dir, "app.tgz")
	if out, err := exec.Command("tar", "xf", fpkPath, "-C", dir, "app.tgz").CombinedOutput(); err != nil {
		_ = out
		return false // 无 app.tgz → 原生应用
	}
	appDir, err := os.MkdirTemp("", "fpk-probe-app-*")
	if err != nil {
		return false
	}
	defer os.RemoveAll(appDir)
	if out, err := exec.Command("tar", "xzf", appTgz, "-C", appDir, "docker/docker-compose.yaml").CombinedOutput(); err != nil {
		_ = out
		return false
	}
	return true
}

// validateFpkName 校验 FPK 缓存文件名（与删除端点同一白名单规则，防路径穿越）。
func validateFpkName(name string) error {
	if name == "" || strings.ContainsAny(name, "/\\") || name == "." || name == ".." ||
		strings.Contains(name, "..") {
		return fmt.Errorf("非法文件名")
	}
	if !strings.EqualFold(filepath.Ext(name), ".fpk") {
		return fmt.Errorf("不是 FPK 文件")
	}
	return nil
}

// handleInstallFpkDownload —— POST /api/fpk-downloads/{name}/install（SSE）
// 直接安装下载缓存里的本地 FPK：不重新下载，安装完成后文件保留在缓存中。
// 通道与商店安装一致（daemon install 优先，daemon 不可达回退 install-local），
// 安装卷沿用「设置 → 应用安装位置」。
func (s *Server) handleInstallFpkDownload(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := validateFpkName(name); err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	dir := s.pipeline.downloads.DownloadDir()
	full := filepath.Join(dir, name)
	if fi, err := os.Stat(full); err != nil || fi.IsDir() {
		writeAPIError(w, http.StatusNotFound, "文件不存在（可能已被清理）")
		return
	}

	m, err := readFpkManifest(full)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, err.Error())
		return
	}
	if m.appName == "" {
		writeAPIError(w, http.StatusBadRequest, "FPK 清单缺少 appname，无法安装")
		return
	}

	// 目录里有的应用用其元数据（docker 类型、服务端口等）；没有则按 manifest 构造。
	// 大小写不敏感回退：manifest 小写 "gitea" vs 注册表 "Gitea"——漏匹配会绕过
	// 下面的「已安装」检查，静默重装正在运行的应用。
	app, inRegistry := s.getRegistryApp(m.appName)
	if !inRegistry {
		app, inRegistry = s.getRegistryAppFold(m.appName)
	}
	if !inRegistry {
		app = core.AppInfo{AppName: m.appName}
	}
	if m.fpkVersion != "" {
		app.FpkVersion = m.fpkVersion
	}
	if app.LatestVersion == "" {
		app.LatestVersion = m.version
	}
	if !inRegistry && looksLikeDockerFpk(full) {
		app.AppType = "docker"
	}

	// 已安装应用：install-fpk / daemon install 都会拒绝，提前给清楚提示。
	if app.Installed {
		writeAPIError(w, http.StatusBadRequest, "「"+app.AppName+"」已安装，请先卸载或使用商店里的更新功能")
		return
	}

	if !s.queue.TryStart("install", m.appName) {
		writeAPIError(w, http.StatusConflict, "another operation is already running")
		return
	}
	defer s.queue.FinishApp(m.appName)

	stream, err := newSSEStream(w, r, m.appName)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.pipeline.runLocalFpkInstall(r.Context(), stream, full, app, s.refreshRegistry)
	s.refreshInstalledNames()
}
