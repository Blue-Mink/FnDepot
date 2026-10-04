#!/usr/bin/env python3
"""生成 Dog-dev 图标 dogdev.png（256x256）。

按用户提供的定稿设计：黑底 squircle（沿用原 GitHub++ 图标的圆角蒙版，
保持同族外观）+ 居中的白色狗骨图形（顶部微亮渐变，无文字）。
"""
from PIL import Image, ImageDraw

SRC = "github++.png"
OUT = "dogdev.png"
BLACK = (1, 1, 1, 255)

im = Image.open(SRC).convert("RGBA")
w, h = im.size
mask = im.getchannel("A")

# 1) 黑底 squircle（蒙版内填充）
out = Image.new("RGBA", (w, h), (0, 0, 0, 0))
black = Image.new("RGBA", (w, h), BLACK)
out.paste(black, (0, 0), mask)

# 2) 白色狗骨（形状层，纯白）
shape = Image.new("L", (w, h), 0)
d = ImageDraw.Draw(shape)
cx, cy = 128, 106
shaft_w, shaft_h = 96, 36
lobe_r = 24
lobe_dx, lobe_dy = 44, 14
# 骨杆
d.rounded_rectangle([cx - shaft_w / 2, cy - shaft_h / 2,
                     cx + shaft_w / 2, cy + shaft_h / 2],
                    radius=shaft_h / 2, fill=255)
# 两端各两瓣
for sx in (-1, 1):
    bx = cx + sx * lobe_dx
    for sy in (-1, 1):
        by = cy + sy * lobe_dy
        d.ellipse([bx - lobe_r, by - lobe_r, bx + lobe_r, by + lobe_r], fill=255)

# 3) 顶部微亮渐变（255 → 238，极淡的立体感）
grad = Image.new("L", (w, h), 0)
gd = ImageDraw.Draw(grad)
for y in range(h):
    v = 255 - int(17 * y / h)  # 255 → ~238
    gd.line([(0, y), (w, y)], fill=v)
bone = Image.new("RGBA", (w, h), (0, 0, 0, 0))
grad_full = grad.point(lambda v: v)  # 渐变值即 RGB
bone_rgba = Image.merge("RGBA", (grad, grad, grad, Image.new("L", (w, h), 255)))
bone.putalpha(shape)
bone = Image.composite(bone_rgba, Image.new("RGBA", (w, h), (0, 0, 0, 0)), shape)

out = Image.alpha_composite(out, bone)
out.save(OUT)
print("written", OUT, out.size)
