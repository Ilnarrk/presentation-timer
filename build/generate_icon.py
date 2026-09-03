from __future__ import annotations

from pathlib import Path

from PIL import Image, ImageDraw


def draw_icon(size: int) -> Image.Image:
    img = Image.new("RGBA", (size, size), (0, 0, 0, 0))
    draw = ImageDraw.Draw(img)

    margin = size * 0.08
    box = (margin, margin, size - margin, size - margin)
    radius = size * 0.18

    draw.rounded_rectangle(box, radius=radius, fill=(15, 22, 36, 255))
    draw.rounded_rectangle(box, radius=radius, outline=(79, 124, 255, 255), width=max(2, size // 64))

    center = size / 2
    outer_radius = size * 0.28
    inner_radius = size * 0.22

    draw.ellipse(
        (
            center - outer_radius,
            center - outer_radius,
            center + outer_radius,
            center + outer_radius,
        ),
        outline=(103, 212, 255, 255),
        width=max(3, size // 48),
    )

    for angle in range(0, 360, 30):
        import math

        rad = math.radians(angle - 90)
        x1 = center + math.cos(rad) * inner_radius
        y1 = center + math.sin(rad) * inner_radius
        x2 = center + math.cos(rad) * outer_radius
        y2 = center + math.sin(rad) * outer_radius
        draw.line((x1, y1, x2, y2), fill=(120, 150, 200, 180), width=max(1, size // 128))

    hour_len = outer_radius * 0.45
    minute_len = outer_radius * 0.72
    draw.line(
        (center, center, center, center - hour_len),
        fill=(232, 237, 247, 255),
        width=max(3, size // 56),
    )
    draw.line(
        (center, center, center + minute_len * 0.55, center - minute_len * 0.85),
        fill=(56, 189, 148, 255),
        width=max(3, size // 64),
    )
    draw.ellipse(
        (
            center - size * 0.035,
            center - size * 0.035,
            center + size * 0.035,
            center + size * 0.035,
        ),
        fill=(232, 237, 247, 255),
    )

    return img


def main() -> None:
    root = Path(__file__).resolve().parent
    appicon = root / "appicon.png"
    icon_ico = root / "windows" / "icon.ico"

    base = draw_icon(1024)
    base.save(appicon, format="PNG")

    sizes = [(16, 16), (24, 24), (32, 32), (48, 48), (64, 64), (128, 128), (256, 256)]
    icons = [draw_icon(size) for size, _ in sizes]
    # Pillow ignores ICO sizes larger than the primary image, so save 256x256 first.
    icons[-1].save(
        icon_ico,
        format="ICO",
        sizes=sizes,
        append_images=icons[:-1],
    )

    print(f"Wrote {appicon}")
    print(f"Wrote {icon_ico}")


if __name__ == "__main__":
    main()
