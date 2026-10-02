"""Finds the cover inside a feed screenshot: the picture, without the white
card margins and the title / author / likes caption under it."""
import numpy as np
from PIL import Image


def light_fraction(a: np.ndarray, axis: int, paper: np.ndarray) -> np.ndarray:
    """Share of pixels in the card's own colour (white, or near-black in dark
    mode) per row (axis=1) or column (axis=0)."""
    d = np.abs(a.astype(int) - paper.astype(int)).max(axis=2)
    return (d < 20).mean(axis=axis)


def find_cover(img: Image.Image) -> tuple[int, int, int, int]:
    a = np.asarray(img.convert('RGB'))
    h, w, _ = a.shape
    # The card colour: the bottom-left corner, which in a feed screenshot is
    # always the caption area (white, or near-black in dark mode).
    paper = np.median(a[h - 4:h, 0:6].reshape(-1, 3), axis=0)
    rows = light_fraction(a, 1, paper)

    def solid(i: int) -> bool:
        return rows[i] < 0.55

    # Bottom: walk up from the bottom until 12 consecutive "picture" rows.
    bottom = h
    run = 0
    for y in range(h - 1, -1, -1):
        run = run + 1 if solid(y) else 0
        if run >= 12:
            bottom = y + run
            break
    # Top: only pure card colour counts as margin (a light picture must not
    # be mistaken for it).
    top = 0
    while top < h // 8 and rows[top] > 0.97:
        top += 1
    # A caption band exists only if what we cut is mostly light; otherwise the
    # picture itself is light (a white cover) and we keep everything.
    if bottom < h and rows[bottom:].mean() < 0.6:
        bottom = h
    cols = light_fraction(a[top:bottom], 0, paper)
    left, right = 0, w
    while left < w // 4 and cols[left] > 0.9:
        left += 1
    while right > w * 3 // 4 and cols[right - 1] > 0.9:
        right -= 1
    return left, top, right, bottom


if __name__ == '__main__':
    import sys
    for f in sys.argv[1:]:
        im = Image.open(f)
        print(f, im.size, find_cover(im))
