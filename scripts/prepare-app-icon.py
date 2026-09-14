#!/usr/bin/env python3
"""Extract the original cream tile without redrawing the logo (requires Pillow).

This contour extraction is calibrated for the supplied 1254 px brand artwork.
Only alpha is changed; the source and every RGB pixel are preserved.
Run manually when preparing the checked-in app-icon.png; normal builds use PNG.
"""

import hashlib
import math
from pathlib import Path

from PIL import Image, ImageDraw


ROOT = Path(__file__).resolve().parents[1]
SOURCE = ROOT / "assets/brand/logo.png"
OUTPUT = ROOT / "assets/brand/app-icon.png"
SOURCE_SHA256 = "00338f9b2458cc39f2e81855d411c808064eb09f68cb1bad9f99f79f6d68ed74"


def main():
    if hashlib.sha256(SOURCE.read_bytes()).hexdigest() != SOURCE_SHA256:
        raise SystemExit("Brand source changed; recalibrate and review the contour before regenerating.")
    source = Image.open(SOURCE).convert("RGB")
    pixels = source.load()
    width, height = source.size
    center_x, center_y = 627.0, 620.0

    def brightness(x, y):
        # Bilinear sampling gives subpixel edge positions without changing source pixels.
        left, top = int(x), int(y)
        dx, dy = x - left, y - top
        return sum(
            sum(pixels[left + ox, top + oy]) / 3 * weight
            for ox, oy, weight in (
                (0, 0, (1 - dx) * (1 - dy)), (1, 0, dx * (1 - dy)),
                (0, 1, (1 - dx) * dy), (1, 1, dx * dy),
            )
        )

    count = 1440
    radii = []
    for index in range(count):
        angle = index * 2 * math.pi / count
        dx, dy = math.cos(angle), math.sin(angle)
        best_score, boundary = -math.inf, None
        # The M and coin lie inside this annulus. The tile edge has a sharp
        # luminance drop into its baked outer shadow, unlike the white canvas.
        for step in range(1040, 1560):
            radius = step / 2
            inner_x, inner_y = center_x + (radius - 2) * dx, center_y + (radius - 2) * dy
            outer_x, outer_y = center_x + (radius + 2) * dx, center_y + (radius + 2) * dy
            if not (1 <= outer_x < width - 2 and 1 <= outer_y < height - 2):
                break
            score = brightness(inner_x, inner_y) - brightness(outer_x, outer_y)
            if score > best_score:
                best_score, boundary = score, radius
        if boundary is None or best_score < 5:
            raise SystemExit(f"Cannot reliably detect the tile edge at angle {index}.")
        radii.append(boundary)

    # Smooth subpixel scan noise and inset one pixel to remove the baked gray
    # fringe. Supersampling produces antialiased alpha around the cream tile.
    scale = 4
    points = []
    for index in range(count):
        radius = sum(radii[(index + offset) % count] for offset in range(-4, 5)) / 9 - 1
        angle = index * 2 * math.pi / count
        points.append(((center_x + radius * math.cos(angle)) * scale,
                       (center_y + radius * math.sin(angle)) * scale))
    alpha = Image.new("L", (width * scale, height * scale), 0)
    ImageDraw.Draw(alpha).polygon(points, fill=255)
    alpha = alpha.resize(source.size, Image.Resampling.LANCZOS)
    result = source.convert("RGBA")
    result.putalpha(alpha)
    result.save(OUTPUT)
    print(f"Saved {OUTPUT.relative_to(ROOT)}: {width}x{height} RGBA; RGB pixels unchanged.")


if __name__ == "__main__":
    main()
