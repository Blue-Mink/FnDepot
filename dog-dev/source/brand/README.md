# dog-dev 品牌标（final6 定稿）

用户手绘终稿 → 像素级矢量还原定稿（2026-10-03，用户验收通过）。
完整设计规范见 `dogdev-logo-design-spec-final6-v2.docx`（不入库，留存于工作区）。

## 文件

| 文件 | 用途 |
|---|---|
| `dogdev-logo-final6.svg` | 主源文件（渐变版，印刷/网页/任意缩放） |
| `dogdev-logo-final6-solid.svg` | 实色版（#036099，≤64px / 单色场景必用） |
| `dogdev-logo-final6-white-512.png` | 反白版位图（深底/深色主题必用；由实色 SVG 光栅化生成：蓝块→#FFFFFF、负形→透明，512px） |
| `dogdev-classic-final6-2048-20261003.png` | 高清位图（透明底） |
| `dogdev-classic-final6-512-20261003.png` | 平台图标 / 头像（`fpk/ICON.PNG`、`icon-0.png` 来源） |
| `dogdev-classic-final6-256-20261003.png` | 2048 母版 Lanczos 降采样（`ICON_256.PNG`、`icon-256.png` 来源） |
| `dogdev-classic-final6-64-20261003.png` | favicon / 小图标原档（规范：32/64 直接用随附 PNG，不二次缩放） |
| `dogdev-classic-final6-32-20261003.png` | favicon 最小档 |

## 装配

`scripts/icon.go` 在构建期把上述位图按字节拷贝到
`fpk/ICON{,_256}.PNG`、`fpk/app/ui/images/icon-{0,256,64}.png`、`web/static/favicon.png`。
Web 控制台 Logo（≤64px 显示）用 `logo-solid.svg`（浅）/ `logo-white.png`（深，
反白位图——SVG 直接换色会让负形也变白只剩白团，必须光栅化把负形做成透明），
见 `web/ui/public/`。

## 关键规范（final6-v2）

- 主色四段垂直渐变（自下而上）#026AA6→#066EA9→#025C93→#035F98；负形雾白 #FEFFFC
- 渐变版最小 96px；≤64px 与单色印刷必须实色版（#036099）
- 深底用反白版，禁止蓝底叠加
- 禁止：拉伸/旋转/改色/描边/投影/裁切/拆分元素
