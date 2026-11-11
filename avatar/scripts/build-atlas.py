#!/usr/bin/env python3
"""Packs the parts we use from Kenney's Monster Builder Pack (CC0,
https://kenney.nl/assets/monster-builder-pack) into src/atlas.png and
src/atlas.json.

Usage: python3 scripts/build-atlas.py /path/to/unzipped/pack

Only the white limbs and bodies are kept: the renderer tints them to the
player's colour. Needs Pillow.
"""
import json
import sys
from pathlib import Path

from PIL import Image

PARTS = (
    [f"body_white{c}" for c in "ABCDEF"]
    + [f"arm_white{c}" for c in "ABCDE"]
    + [f"leg_white{c}" for c in "ABCDE"]
    + [f"detail_white_{n}" for n in ("antenna_large", "antenna_small", "ear", "ear_round", "horn_large", "horn_small")]
    + [f"mouth{c}" for c in "ABCDEFGHIJ"]
    + [f"mouth_closed_{n}" for n in ("fangs", "happy", "sad", "teeth")]
    + [f"nose_{n}" for n in ("brown", "green", "red", "yellow")]
    + [f"eyebrow{c}" for c in "ABC"]
    + ["eye_closed_happy", "eye_dead"]
)
WIDTH = 1024
PAD = 2
SAMPLES = 21


def short(name: str) -> str:
    return name.replace("_white", "").replace("detail_", "top_")


def outline(im: Image.Image) -> dict:
    """Where a body's silhouette starts, sampled at 21 steps: the top edge per
    column and the left/right edges per row, in source pixels. The renderer
    uses these to seat horns, arms and legs on any body shape."""
    alpha = im.getchannel("A").load()
    w, h = im.size
    solid = lambda x, y: alpha[x, y] > 128
    top, bottom, left, right = [], [], [], []
    for i in range(SAMPLES):
        x = min(w - 1, round(i * (w - 1) / (SAMPLES - 1)))
        ys = [y for y in range(h) if solid(x, y)]
        top.append(ys[0] if ys else h // 2)
        bottom.append(ys[-1] if ys else h // 2)
        y = min(h - 1, round(i * (h - 1) / (SAMPLES - 1)))
        xs = [x2 for x2 in range(w) if solid(x2, y)]
        left.append(xs[0] if xs else w // 2)
        right.append(xs[-1] if xs else w // 2)
    return {"top": top, "bottom": bottom, "left": left, "right": right}


def main() -> None:
    pack = Path(sys.argv[1]) / "PNG" / "Double"
    out = Path(__file__).resolve().parent.parent / "src"
    images = [(short(n), Image.open(pack / f"{n}.png").convert("RGBA")) for n in PARTS]
    images.sort(key=lambda item: -item[1].height)
    x = y = row = 0
    frames = {}
    for name, im in images:
        if x + im.width + PAD > WIDTH:
            x, y, row = 0, y + row + PAD, 0
        frames[name] = [x, y, im.width, im.height]
        x += im.width + PAD
        row = max(row, im.height)
    atlas = Image.new("RGBA", (WIDTH, y + row), (0, 0, 0, 0))
    for name, im in images:
        atlas.paste(im, tuple(frames[name][:2]))
    atlas.save(out / "atlas.png", optimize=True)
    outlines = {name: outline(im) for name, im in images if name.startswith("body")}
    data = {"frames": dict(sorted(frames.items())), "outlines": dict(sorted(outlines.items()))}
    (out / "atlas.json").write_text(json.dumps(data, separators=(",", ":")) + "\n")
    print(f"{len(frames)} parts, {atlas.width}x{atlas.height}")


if __name__ == "__main__":
    main()
