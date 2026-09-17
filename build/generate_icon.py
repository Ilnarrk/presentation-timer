"""Build Windows .ico files from build/appicon.png."""

from __future__ import annotations

import shutil
import sys
from pathlib import Path

from PIL import Image

ICO_SIZES = (256, 128, 64, 48, 32, 24, 16)


def png_to_ico(src: Path, dst: Path) -> None:
    img = Image.open(src).convert("RGBA")
    width, height = img.size
    if width < 256 or height < 256:
        print(
            f"Warning: {src.name} is {width}x{height}; use at least 256x256 for sharp icons.",
            file=sys.stderr,
        )

    dst.parent.mkdir(parents=True, exist_ok=True)
    img.save(dst, format="ICO", sizes=[(size, size) for size in ICO_SIZES])

    ico = Image.open(dst)
    embedded = sorted(ico.info.get("sizes", []))
    print(f"Wrote {dst} ({ico.size[0]}x{ico.size[1]}, embedded: {embedded})")


def main() -> None:
    root = Path(__file__).resolve().parent
    appicon = root / "appicon.png"
    icon_ico = root / "windows" / "icon.ico"
    embedded_ico = root.parent / "internal" / "conference" / "icon.ico"

    if not appicon.is_file():
        print(f"Missing source icon: {appicon}", file=sys.stderr)
        sys.exit(1)

    png_to_ico(appicon, icon_ico)
    embedded_ico.parent.mkdir(parents=True, exist_ok=True)
    shutil.copy2(icon_ico, embedded_ico)
    print(f"Copied to {embedded_ico}")


if __name__ == "__main__":
    main()
