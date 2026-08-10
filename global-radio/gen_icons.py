from PIL import Image, ImageDraw
import os

OUT_DIR = "/vol1/@appshare/com.dustinky.qwenpaw/.qwenpaw/workspaces/cloud-orchestrator/global-radio-fpk"

def create_icon(size, out_path):
    img = Image.new("RGBA", (size, size), (0, 0, 0, 0))
    draw = ImageDraw.Draw(img)

    # Background - deep blue gradient
    for y in range(size):
        r = int(20 + (y / size) * 30)
        g = int(60 + (y / size) * 50)
        b = int(130 + (y / size) * 60)
        for x in range(size):
            cx, cy = size // 2, size // 2
            dist = ((x - cx) ** 2 + (y - cy) ** 2) ** 0.5
            if dist <= size // 2:
                if dist > size // 2 - 1:
                    alpha = int(255 * (1 - (dist - (size // 2 - 1))))
                else:
                    alpha = 255
                draw.point((x, y), fill=(r, g, b, alpha))

    c = size // 2
    r1 = int(size * 0.2)
    # Earth circle
    draw.ellipse([c - r1, c - r1, c + r1, c + r1], outline=(100, 200, 255, 230), width=max(2, size // 32))
    # Meridians
    draw.arc([c - r1, c - r1, c + r1, c + r1], 0, 180, fill=(100, 200, 255, 180), width=max(1, size // 48))
    draw.arc([c - r1, c - r1, c + r1, c + r1], 90, 270, fill=(100, 200, 255, 180), width=max(1, size // 48))
    # Signal waves
    for i in range(1, 4):
        r2 = int(r1 + i * size * 0.08)
        draw.arc([c - r2, c - r2, c + r2, c + r2], -60 + i * 10, 60 + i * 10,
                 fill=(80, 180, 255, max(60, 150 - i * 30)), width=max(1, size // 40))

    img.save(out_path, "PNG")
    print(f"Created {out_path} ({size}x{size})")

create_icon(64, os.path.join(OUT_DIR, "ICON.PNG"))
create_icon(256, os.path.join(OUT_DIR, "ICON_256.PNG"))
create_icon(256, os.path.join(OUT_DIR, "app/ui/images/256.png"))
print("Done")
