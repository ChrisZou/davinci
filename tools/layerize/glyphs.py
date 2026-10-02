"""Draws text with PIL exactly as davinci will lay it out, so a guess can be
compared with the cover pixel for pixel."""
import json
import os
from functools import lru_cache

import numpy as np
from PIL import Image, ImageDraw, ImageFont

from fonts import FONTS

HERE = os.path.dirname(os.path.abspath(__file__))
BY_KEY = {f[0]: f for f in FONTS}


@lru_cache(maxsize=256)
def font(key: str, size: int) -> ImageFont.FreeTypeFont:
    f = BY_KEY[key]
    return ImageFont.truetype(f[3], max(4, size), index=f[4])


# Chrome's synthetic italic (fontStyle: italic on a face that has none):
# glyphs sheared by a quarter of their height above the baseline.
ITALIC_SKEW = 0.25


def draw(key: str, text: str, size: float, spacing: float = 0.0, italic: bool = False) -> tuple[np.ndarray, tuple[int, int]]:
    """Renders `text` one glyph at a time (so letter spacing can be applied the
    way Fabric applies charSpacing: an extra advance after each glyph).
    Returns the ink mask and the pixel position of the text origin in it."""
    f = font(key, int(round(size)))
    pad = int(size)
    width = int(sum(f.getlength(c) for c in text) + max(0, spacing) * len(text) + 2 * pad) + 4
    img = Image.new('L', (max(8, width), int(size * 2.2) + 2 * pad), 0)
    d = ImageDraw.Draw(img)
    x = float(pad)
    for c in text:
        d.text((x, pad), c, font=f, fill=255)
        x += f.getlength(c) + spacing
    a = np.asarray(img)
    if italic:
        import cv2
        yb = pad + f.getmetrics()[0]
        m = np.float32([[1, -ITALIC_SKEW, ITALIC_SKEW * yb], [0, 1, 0]])
        a = cv2.warpAffine(a, m, (a.shape[1] + int(size), a.shape[0]), flags=cv2.INTER_LINEAR)
    return a > 127, (pad, pad)


def ink_box(mask: np.ndarray):
    ys, xs = np.nonzero(mask)
    if len(xs) == 0:
        return None
    return xs.min(), ys.min(), xs.max() + 1, ys.max() + 1


def calibration():
    with open(os.path.join(HERE, 'calib.json')) as fh:
        return json.load(fh)
