package api

import (
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// fpkDownloadInfo 已下载的 FPK 缓存条目（设置页「FPK 下载目录」列表）。
type fpkDownloadInfo struct {
	Name  string    `json:"name"`
	Size  int64     `json:"size"`
	ModAt time.Time `json:"mod_at"`
	// AppName 从 FPK 清单解析（解析失败为空）；Installed 表示该应用当前
	// 已安装（含通过官方中心/其他方式安装，见 installedAppNames）。
	AppName   string `json:"appname,omitempty"`
	Installed bool   `json:"installed"`
}

// handleListFpkDownloads —— GET /api/fpk-downloads
// 返回当前 FPK 下载目录 + 已下载文件列表（按修改时间倒序）。
func (s *Server) handleListFpkDownloads(w http.ResponseWriter, r *http.Request) {
	dir := s.pipeline.downloads.DownloadDir()
	resp := map[string]any{"dir": dir, "files": []fpkDownloadInfo{}}
	entries, err := os.ReadDir(dir)
	if err == nil {
		installed := s.installedAppNames(false)
		files := make([]fpkDownloadInfo, 0, len(entries))
		for _, e := range entries {
			if e.IsDir() || strings.HasSuffix(e.Name(), ".tmp") {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			entry := fpkDownloadInfo{
				Name:  e.Name(),
				Size:  info.Size(),
				ModAt: info.ModTime(),
			}
			// 读 FPK 清单拿 appname（manifest 在包首，单成员解压很快）；
			// 失败不影响列表展示。
			if m, merr := readFpkManifest(filepath.Join(dir, e.Name())); merr == nil && m.appName != "" {
				entry.AppName = m.appName
				if _, ok := installed[strings.ToLower(m.appName)]; ok {
					entry.Installed = true
				}
			}
			files = append(files, entry)
		}
		sort.Slice(files, func(i, j int) bool { return files[i].ModAt.After(files[j].ModAt) })
		resp["files"] = files
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleDeleteFpkDownload —— DELETE /api/fpk-downloads/{name}
// 删除单个已下载 FPK 缓存（文件名白名单校验，防路径穿越）。
func (s *Server) handleDeleteFpkDownload(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" || strings.ContainsAny(name, "/\\") || name == "." || name == ".." ||
		strings.Contains(name, "..") {
		writeAPIError(w, http.StatusBadRequest, "非法文件名")
		return
	}
	dir := s.pipeline.downloads.DownloadDir()
	full := filepath.Join(dir, name)
	if fi, err := os.Stat(full); err != nil || fi.IsDir() {
		writeAPIError(w, http.StatusNotFound, "文件不存在")
		return
	}
	if err := os.Remove(full); err != nil {
		writeAPIError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
