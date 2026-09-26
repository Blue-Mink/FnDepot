package source

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
)

// TestProbeBlueMink 用真实的 Blue-Mink/FnDepot 索引跑一遍转换，打印每条的去留。
// 本地开发机探针：索引文件不在时跳过（不破坏 go test ./... 的通用可跑性）。
func TestProbeBlueMink(t *testing.T) {
	raw, err := os.ReadFile("/vol1/@appshare/com.dustinky.qwenpaw/.qwenpaw/workspaces/cloud-orchestrator/FnDepot_repo/fnpack.json")
	if err != nil {
		t.Skipf("本地探针文件不存在，跳过: %v", err)
	}
	var entries map[string]fndepotAppEntry
	if err := json.Unmarshal(raw, &entries); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	base := "https://raw.githubusercontent.com/Blue-Mink/FnDepot/HEAD/"
	names := make([]string, 0, len(entries))
	for n := range entries {
		names = append(names, n)
	}
	for _, name := range names {
		e := entries[name]
		app, ok := translateFndepotApp(name, e, base, "BlueMink", "")
		if !ok {
			fmt.Printf("DROPPED: %-20s platform=%s version=%s download_url=%q\n",
				name, string(e.Platform), e.Version, e.DownloadURL)
			continue
		}
		fmt.Printf("OK: %-20s v%-12s icon=%s\n     previews=%v\n",
			app.AppName, app.Version, app.IconURL, app.PreviewURLs)
	}
}
