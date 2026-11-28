//go:build ignore

// icon 是图标装配工具：把 brand/ 下的 final6 定稿品牌标（用户手绘→矢量化定稿，
// 见 brand/dogdev-logo-final6.svg 与品牌规范文档）按字节拷贝到打包所需位置。
//
// 设计决策（全槽位高清，对齐 magpie 口径）：
//   - 桌面/应用中心所有图标槽位（ICON.PNG / ICON_256.PNG / icon-0/64/128/256/512）
//     统一用 512 母版原图：任何通道、任何 size 都拿同一张大图，靠前端缩放 → 全场景清晰；
//     （此前只有 icon-0 与根 ICON.PNG 是 512，详情弹窗走 ICON.PNG/小槽位那一路会糊）
//   - 仅覆盖 FPK/桌面图标槽位；Web 控制台 favicon（浏览器标签）仍用 kit 64 原档，
//     Web 控制台 logo（≤64px 显示）按规范用实色/反白 SVG，不在此处处理；
//   - 构建期只做字节拷贝 → 构建产物字节确定，可复现。
//
// 用法：go run scripts/icon.go
package main

import (
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	// brand/ 内文件统一按 dogdev-classic-final6-<size>-20261003.png 命名。
	jobs := []struct {
		src string
		out string
	}{
		// FPK 根目录图标（应用中心卡片详情弹窗走 ICON.PNG）。
		{"brand/dogdev-classic-final6-512-20261003.png", "fpk/ICON.PNG"},
		{"brand/dogdev-classic-final6-512-20261003.png", "fpk/ICON_256.PNG"},
		// 桌面/列表入口槽位：app/ui/config 里写 "icon": "images/icon-{0}.png"，
		// {0} 会被系统替换成 0（兜底）/ 64 / 128 / 256 / 512，文件名用连字符。
		// 全槽位统一 512 母版（飞牛服务端只认 64/128/256 且不缩放，大图恒清晰）。
		{"brand/dogdev-classic-final6-512-20261003.png", filepath.Join("fpk", "app", "ui", "images", "icon-0.png")},
		{"brand/dogdev-classic-final6-512-20261003.png", filepath.Join("fpk", "app", "ui", "images", "icon-64.png")},
		{"brand/dogdev-classic-final6-512-20261003.png", filepath.Join("fpk", "app", "ui", "images", "icon-128.png")},
		{"brand/dogdev-classic-final6-512-20261003.png", filepath.Join("fpk", "app", "ui", "images", "icon-256.png")},
		{"brand/dogdev-classic-final6-512-20261003.png", filepath.Join("fpk", "app", "ui", "images", "icon-512.png")},
		// Web 控制台 favicon：规范"favicon 32/64 用 2x 资产"，直接用 kit 64 原档。
		// web/ui/public/ 是 vite 的静态源，这里同步写 web/static/ 保证 go:embed
		// 与前端构建产物一致（icon.go 在 vite build 之后运行）。
		{"brand/dogdev-classic-final6-64-20261003.png", "web/ui/public/favicon.png"},
		{"brand/dogdev-classic-final6-64-20261003.png", "web/static/favicon.png"},
	}
	for _, j := range jobs {
		data, err := os.ReadFile(j.src)
		if err != nil {
			fmt.Println("读取品牌素材失败:", err)
			os.Exit(1)
		}
		if err := os.MkdirAll(filepath.Dir(j.out), 0o755); err != nil {
			fmt.Println("创建目录失败:", err)
			os.Exit(1)
		}
		if err := os.WriteFile(j.out, data, 0o644); err != nil {
			fmt.Println("写入 "+j.out+" 失败:", err)
			os.Exit(1)
		}
		fmt.Printf("已装配 %s (%d bytes)\n", j.out, len(data))
	}
}
