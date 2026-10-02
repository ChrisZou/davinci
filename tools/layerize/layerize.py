"""Turns a flat cover image into davinci layers.

    python3 layerize.py cover.png out/ [--crop x0,y0,x1,y1] [--fix fixes.json]

Writes into out/:
  cover.png        the cover itself (cropped out of a feed screenshot)
  bg.png           the background, with text / people / elements painted out
  person-N.png     cut-out people, element-N.png cut-out icons and objects
  design.json      davinci commands that rebuild the cover from those layers
  report.json      what was found (text runs, fonts, scores), for review

How each part is recovered:
  text     Apple Vision OCR (lines + per-character boxes); per character the
           ink is separated from what surrounds it by colour, giving the fill
           colour and, when there is one, the outline colour and width. The
           font is chosen by drawing the recognised text in every candidate
           face at the measured size and keeping the one whose glyphs overlap
           the cover's best. Size and letter spacing come from the ink box;
           the position uses a per-font calibration against davinci itself.
  people   faces from Vision decide whether to look; rembg's portrait model
           cuts them out of the cover with the text already removed.
  elements rembg's general model on what is left (icons, devices, stickers).
  bg       LaMa inpainting under everything that became a layer.
"""
import argparse
import json
import math
import os
import re
import subprocess
import sys

import cv2
import numpy as np
from PIL import Image

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)

from crop import find_cover  # noqa: E402
from fonts import FONTS  # noqa: E402
from glyphs import calibration, draw, ink_box  # noqa: E402

VISION = os.environ.get('LAYERIZE_VISION', os.path.join(HERE, 'bin', 'vision'))
CJK = re.compile(r'[　-鿿＀-￯]')


# --- helpers ------------------------------------------------------------------

def hexcolor(rgb) -> str:
    r, g, b = (int(max(0, min(255, round(v)))) for v in rgb)
    return f'#{r:02x}{g:02x}{b:02x}'


def lab(rgb: np.ndarray) -> np.ndarray:
    a = rgb.reshape(-1, 1, 3).astype(np.uint8)
    return cv2.cvtColor(a, cv2.COLOR_RGB2LAB).reshape(-1, 3).astype(np.float32)


def kmeans(x: np.ndarray, k: int):
    k = max(1, min(k, len(x)))
    cv2.setRNGSeed(7)  # deterministic: the same cover always gives the same layers
    crit = (cv2.TERM_CRITERIA_EPS + cv2.TERM_CRITERIA_MAX_ITER, 30, 0.5)
    _, labels, centers = cv2.kmeans(x.astype(np.float32), k, None, crit, 3, cv2.KMEANS_PP_CENTERS)
    return labels.ravel(), centers


def run_vision(path: str) -> dict:
    out = subprocess.run([VISION, path], capture_output=True, check=True)
    return json.loads(out.stdout)


# --- text ---------------------------------------------------------------------

class TextRun:
    def __init__(self, **kw):
        self.__dict__.update(kw)


def deskew(img: np.ndarray, quad, center):
    """Rotates the image so a text line's baseline is horizontal."""
    (x0, y0), (x1, y1) = quad[0], quad[1]
    angle = math.degrees(math.atan2(y1 - y0, x1 - x0))
    if abs(angle) < 1.2:
        return img, 0.0, np.eye(3)
    m = cv2.getRotationMatrix2D(center, angle, 1.0)
    rot = cv2.warpAffine(img, m, (img.shape[1], img.shape[0]), flags=cv2.INTER_CUBIC, borderMode=cv2.BORDER_REPLICATE)
    return rot, angle, np.vstack([m, [0, 0, 1]])


def tf(M, pts):
    p = np.hstack([np.asarray(pts, float), np.ones((len(pts), 1))])
    return (M @ p.T).T[:, :2]


def find_label(patch: np.ndarray, L: np.ndarray, inside: np.ndarray, rim: np.ndarray):
    """Text set on a label (a coloured tag or bar that ends before the page
    does). The label is everything whose colour does not occur on the patch's
    rim; if it is one solid blob spanning the line, the text is what lies
    inside it in another colour. Returns that text mask, or None."""
    h, w, _ = patch.shape
    if rim is None or len(rim) < 30:
        return None
    _, rc = kmeans(lab(rim), 4)
    far = np.min(np.linalg.norm(L.reshape(-1, 1, 3) - rc.reshape(1, -1, 3), axis=2), axis=1).reshape(h, w) > 30
    if (far & inside).sum() < 0.5 * inside.sum():
        return None
    n_cc, cc, st, _ = cv2.connectedComponentsWithStats(far.astype(np.uint8), connectivity=8)
    if n_cc < 2:
        return None
    big = 1 + int(np.argmax(st[1:, cv2.CC_STAT_AREA]))
    blob = cc == big
    ix = np.nonzero(inside.any(axis=0))[0]
    span = st[big, cv2.CC_STAT_WIDTH] / max(1, ix.max() - ix.min() + 1)
    hull = cv2.convexHull(np.column_stack(np.nonzero(blob)[::-1]).astype(np.int32))
    solidity = blob.sum() / max(1.0, cv2.contourArea(hull))
    if span < 0.85 or solidity < 0.55:
        return None
    # A label is bigger than the words on it; a heavy glyph is not.
    iy = np.nonzero(inside.any(axis=1))[0]
    if st[big, cv2.CC_STAT_HEIGHT] < 1.1 * (iy.max() - iy.min() + 1) and st[big, cv2.CC_STAT_WIDTH] < 1.1 * (ix.max() - ix.min() + 1):
        return None
    region = np.zeros((h, w), np.uint8)
    cv2.fillPoly(region, [hull], 1)
    region = cv2.erode(region, np.ones((5, 5), np.uint8)).astype(bool)
    text = region & ~blob
    if text.sum() < 0.05 * inside.sum():
        # Light words on the label: they are in the blob too; they are the
        # minority colour inside it.
        lbl, cen = kmeans(L[blob], 2)
        minority = int(np.argmin(np.bincount(lbl, minlength=2)))
        text = np.zeros((h, w), bool)
        text[blob] = lbl == minority
        text &= region
    text = cv2.morphologyEx(text.astype(np.uint8), cv2.MORPH_OPEN, np.ones((2, 2), np.uint8)).astype(bool)
    return text if text.sum() >= 12 else None


def separate_ink(patch: np.ndarray, inside: np.ndarray, background: np.ndarray, rim=None):
    """Splits a text patch into background / ink, and the ink into colour
    groups. `inside` marks this line's own character boxes, `background`
    pixels near the line that belong to no text at all. A colour is ink when
    it is common inside the boxes and rare in the background around them.
    Returns the ink label image (-1 = not ink), the groups, and the ink mask."""
    h, w, _ = patch.shape
    L = lab(patch).reshape(h, w, 3)
    labels = np.full((h, w), -1, int)
    ink = np.zeros((h, w), bool)
    if inside.sum() < 12:
        return labels, [], ink
    lbl_in, centers = kmeans(L[inside], 6)
    near = np.argmin(np.linalg.norm(L.reshape(-1, 1, 3) - centers.reshape(1, -1, 3), axis=2), axis=1).reshape(h, w)
    dist = np.min(np.linalg.norm(L.reshape(-1, 1, 3) - centers.reshape(1, -1, 3), axis=2), axis=1).reshape(h, w)
    n_in = np.bincount(near[inside], minlength=len(centers)).astype(float)
    bg = background & (dist < 30)
    n_bg = np.bincount(near[background], minlength=len(centers)).astype(float)
    share_in = n_in / max(1, inside.sum())
    share_bg = n_bg / max(1, background.sum())
    is_ink = [(share_in[c] > 0.04 and share_in[c] > 2.5 * share_bg[c]) for c in range(len(centers))]
    if os.environ.get('LAYERIZE_DEBUG'):
        print('ink?', [(hexcolor(cv2.cvtColor(np.uint8([[centers[c]]]), cv2.COLOR_LAB2RGB)[0, 0]), round(share_in[c], 3), round(share_bg[c], 3), is_ink[c]) for c in range(len(centers))])
    label = find_label(patch, L, inside, rim)
    if label is not None:
        m = label
        rgb = np.median(patch[m], axis=0)
        dt = cv2.distanceTransform(m.astype(np.uint8), cv2.DIST_L2, 3)
        labels[m] = 100
        return labels, [{'id': 100, 'n': int(m.sum()), 'rgb': rgb, 'dt': float(dt[m].mean()), 'edge': 0.0, 'mask': m}], m
    if not any(is_ink):
        # Everything inside also appears around: take the colour most over-represented.
        c = int(np.argmax(share_in / (share_bg + 1e-3)))
        is_ink[c] = True
    zone = cv2.dilate(inside.astype(np.uint8), np.ones((5, 5), np.uint8)).astype(bool) & ~(background & ~inside)
    for c in range(len(centers)):
        if is_ink[c]:
            ink |= (near == c) & zone & (dist < 40)
    ink = cv2.morphologyEx(ink.astype(np.uint8), cv2.MORPH_OPEN, np.ones((2, 2), np.uint8)).astype(bool)
    # Colour groups among the ink, merging near-identical shades.
    groups = {c: c for c in range(len(centers)) if is_ink[c]}
    keys = list(groups)
    for i in keys:
        for j in keys:
            if j < i and groups[j] == j and np.linalg.norm(centers[i] - centers[j]) < 22:
                groups[i] = groups[j]
    for c, g in groups.items():
        labels[ink & (near == c)] = g
    dt = cv2.distanceTransform(ink.astype(np.uint8), cv2.DIST_L2, 3)
    edge = ink & ~cv2.erode(ink.astype(np.uint8), np.ones((3, 3), np.uint8)).astype(bool)
    bg_lab = np.median(L[background], axis=0) if background.any() else np.median(L[~ink], axis=0)
    out = []
    for g in sorted(set(groups.values())):
        m = labels == g
        n = int(m.sum())
        if n < 6:
            continue
        rgb = np.median(patch[m], axis=0)
        contrast = float(np.linalg.norm(np.median(L[m], axis=0) - bg_lab))
        out.append({'id': int(g), 'n': n, 'rgb': rgb, 'dt': float(dt[m].mean()), 'edge': float((m & edge).sum() / n),
                    'mask': m, 'contrast': contrast})
    return labels, out, ink


def classify(groups):
    """Picks fill group(s) and the outline group, if any, among ink groups."""
    if not groups:
        return [], None
    if len(groups) == 1:
        return groups, None
    # A drop shadow or 3D extrusion is ink too, but it barely stands out from
    # the page; the glyph's own fill is what contrasts. Groups far weaker than
    # the strongest are not fill (they still get painted out).
    # A 3D extrusion / drop shadow: darker than another group and sitting
    # lower than it (the light falls from above). Never the fill.
    def cy(g):
        return np.nonzero(g['mask'])[0].mean()

    def lum(g):
        r, gg, b = g['rgb']
        return 0.299 * r + 0.587 * gg + 0.114 * b

    ys = np.nonzero(np.any([g['mask'] for g in groups], axis=0))[0]
    span = max(1, ys.max() - ys.min())
    def cols(g):
        return g['mask'].any(axis=0)

    def under(g, f):
        # A shadow sits under the letters it belongs to: same columns, lower.
        cg, cf = cols(g), cols(f)
        shared = (cg & cf).sum() / max(1, cg.sum())
        return lum(f) - lum(g) > 60 and cy(g) - cy(f) > 0.05 * span and shared > 0.6 and f['n'] >= 0.3 * g['n']

    shadows = [g for g in groups if any(under(g, f) for f in groups if f is not g)]
    if shadows and len(shadows) < len(groups):
        groups = [g for g in groups if g not in shadows]
        if len(groups) == 1:
            return groups, None
    top = max(g.get('contrast', 0) for g in groups)
    deepest = max(g['dt'] for g in groups)
    # ...unless it is the deepest group: the glyph body, however muted (red
    # letters in a white outline on a dark photo).
    strong = [g for g in groups if g.get('contrast', top) >= 0.5 * top or g['dt'] >= deepest]
    if len(strong) < len(groups):
        weak = [g for g in groups if g not in strong]
        outline = None
        # A weak group wrapped all around the fill is an outline, not a shadow.
        for g in weak:
            if g['edge'] > 0.45 and all(f['dt'] > g['dt'] * 1.35 for f in strong):
                outline = g
        return strong, outline
    total = sum(g['n'] for g in groups)
    # An outline hugs the background: most of its pixels sit near the edge of
    # the ink, and it surrounds the fill (the fill sits deeper inside).
    by_depth = sorted(groups, key=lambda g: g['dt'])
    outer = by_depth[0]
    deeper = [g for g in groups if g is not outer]
    if outer['edge'] > 0.45 and outer['n'] > 0.12 * total and all(g['dt'] > outer['dt'] * 1.35 for g in deeper):
        return deeper, outer
    return groups, None


def split_line(text: str, quad) -> list:
    """Character boxes for a line whose text was typed in rather than read:
    the line's box divided evenly, one slice per character."""
    q = np.asarray(quad, float)
    n = max(1, len(text))
    out = []
    for i, c in enumerate(text):
        a, b = i / n, (i + 1) / n
        tl = q[0] + (q[1] - q[0]) * a
        tr = q[0] + (q[1] - q[0]) * b
        bl = q[3] + (q[2] - q[3]) * a
        br = q[3] + (q[2] - q[3]) * b
        out.append({'c': c, 'quad': [tl.tolist(), tr.tolist(), br.tolist(), bl.tolist()]})
    return out


def corrected(line: dict, text: str) -> dict:
    """The same line with its text corrected. Same length: Vision's own
    character boxes keep their places; otherwise the box is re-divided."""
    old = [c for c in line['chars']]
    if len(old) == len(text):
        chars = [{'c': t, 'quad': c['quad']} for t, c in zip(text, old)]
    else:
        chars = split_line(text, line['quad'])
    return {**line, 'text': text, 'chars': chars}


def added(text: str, box, angle: float = 0.0) -> dict:
    """A line OCR missed, given by hand as [x, y, w, h] in cover pixels,
    optionally turned by `angle` degrees (clockwise) about its centre."""
    x, y, w, h = box
    quad = np.array([[x, y], [x + w, y], [x + w, y + h], [x, y + h]], float)
    if angle:
        c = quad.mean(axis=0)
        t = math.radians(angle)
        R = np.array([[math.cos(t), -math.sin(t)], [math.sin(t), math.cos(t)]])
        quad = (quad - c) @ R.T + c
    quad = quad.tolist()
    return {'text': text, 'conf': 1.0, 'manual': True, 'box': [x, y, w, h], 'quad': quad, 'chars': split_line(text, quad)}


def fix_spaces(chars):
    """Vision returns spaces with an empty box at the origin. Give a space the
    gap between its neighbours (so run boundaries stay sane and "Claude
    Design" keeps its space); drop one with no neighbours."""
    out = []
    for i, c in enumerate(chars):
        if cv2.contourArea(np.float32(c['quad'])) > 1:
            out.append(c)
            continue
        if c['c'].strip():
            continue
        prev = next((p for p in reversed(chars[:i]) if cv2.contourArea(np.float32(p['quad'])) > 1), None)
        nxt = next((p for p in chars[i + 1:] if cv2.contourArea(np.float32(p['quad'])) > 1), None)
        if prev is None or nxt is None:
            continue
        out.append({'c': ' ', 'quad': [prev['quad'][1], nxt['quad'][0], nxt['quad'][3], prev['quad'][2]]})
    return out


def analyse_line(cover: np.ndarray, line: dict, fixes: dict, text_boxes: np.ndarray, all_lines=()):
    H, W, _ = cover.shape
    quad = line['quad']
    cx = sum(p[0] for p in quad) / 4
    cy = sum(p[1] for p in quad) / 4
    img, angle, M = deskew(cover, quad, (cx, cy))
    q = tf(M, quad)
    x0, y0 = q[:, 0].min(), q[:, 1].min()
    x1, y1 = q[:, 0].max(), q[:, 1].max()
    lh = y1 - y0
    # Every other line's horizontal extent and vertical centre, in this frame.
    my_cy = (y0 + y1) / 2
    others_c = []
    for o in all_lines:
        if o is line:
            continue
        oq = tf(M, o['quad'])
        others_c.append((oq[:, 0].min(), oq[:, 0].max(), oq[:, 1].mean()))
    pad = max(4, int(lh * 0.35))
    X0, Y0 = int(max(0, x0 - pad)), int(max(0, y0 - pad))
    X1, Y1 = int(min(W, x1 + pad)), int(min(H, y1 + pad))
    patch = img[Y0:Y1, X0:X1]
    # This line's character boxes (grown a little: Vision's are tight), and
    # the untexted pixels around it.
    own = np.zeros(patch.shape[:2], np.uint8)
    grow = max(1, int(lh * 0.08))
    for ch in fix_spaces(line['chars']):
        if not ch['c'].strip():
            continue
        cq = tf(M, ch['quad']) - [X0, Y0]
        cv2.fillPoly(own, [np.round(cq).astype(np.int32)], 1)
    core = own.astype(bool)
    # Other lines' boxes: ink centred there, outside our own characters, is theirs.
    h_p, w_p = own.shape
    other_core = np.zeros((h_p, w_p), np.uint8)
    for o in all_lines:
        if o is line:
            continue
        cv2.fillPoly(other_core, [np.round(tf(M, o['quad']) - [X0, Y0]).astype(np.int32)], 1)
    other_core = other_core.astype(bool)
    own = cv2.dilate(own, np.ones((2 * grow + 1, 2 * grow + 1), np.uint8)).astype(bool)
    others = text_boxes
    if angle:
        others = cv2.warpAffine(text_boxes, M[:2], (W, H), flags=cv2.INTER_NEAREST)
    around = ~(others[Y0:Y1, X0:X1] > 0) & ~own
    if around.sum() < 40:
        around = np.zeros_like(own)
        around[[0, -1], :] = True
        around[:, [0, -1]] = True
    # The page around the line, well clear of it (a label box can reach past
    # the patch): a ring one line-height out, minus any other text.
    RX0, RY0 = int(max(0, x0 - lh)), int(max(0, y0 - lh))
    RX1, RY1 = int(min(W, x1 + lh)), int(min(H, y1 + lh))
    ring = np.ones((RY1 - RY0, RX1 - RX0), bool)
    ring[int(max(0, y0 - 0.7 * lh) - RY0):int(y1 + 0.7 * lh - RY0), int(max(0, x0 - 0.7 * lh) - RX0):int(x1 + 0.7 * lh - RX0)] = False
    ring &= ~(others[RY0:RY1, RX0:RX1] > 0)
    rim = img[RY0:RY1, RX0:RX1][ring]
    st_ = line.get('style') or {}
    given = (st_.get('match') or ([st_['fill']] if st_.get('fill') else None)) if line.get('manual') else None
    if given:
        # The reviewer named the colour(s): the glyphs are the pixels of those
        # colours inside the box, no guessing (a gradient takes several).
        Lp = lab(patch).reshape(patch.shape[0], patch.shape[1], 3)
        near = np.zeros(patch.shape[:2], bool)
        for hx in given:
            c = np.array([int(hx[i:i + 2], 16) for i in (1, 3, 5)], float)
            near |= np.linalg.norm(Lp - lab(c.reshape(1, 3)).reshape(1, 1, 3), axis=2) < 32
        rgb = np.array([int(given[0][i:i + 2], 16) for i in (1, 3, 5)], float)
        ink = near & own
        ink = cv2.morphologyEx(ink.astype(np.uint8), cv2.MORPH_OPEN, np.ones((2, 2), np.uint8)).astype(bool)
        labels = np.where(ink, 100, -1)
        dt = cv2.distanceTransform(ink.astype(np.uint8), cv2.DIST_L2, 3)
        groups = [{'id': 100, 'n': int(ink.sum()), 'rgb': rgb, 'dt': float(dt[ink].mean()) if ink.any() else 0.0,
                   'edge': 0.0, 'mask': ink, 'contrast': 100.0}] if ink.sum() >= 12 else []
        # Colours to paint out with the text but not part of it (a 3D edge).
        for hx in st_.get('erase', []):
            c = np.array([int(hx[i:i + 2], 16) for i in (1, 3, 5)], float)
            ink = ink | ((np.linalg.norm(Lp - lab(c.reshape(1, 3)).reshape(1, 1, 3), axis=2) < 32) & own)
    else:
        labels, groups, ink = separate_ink(patch, own, around, rim)
    fills, outline = classify(groups)
    if not fills:
        return [], None

    ink_dt = cv2.distanceTransform(ink.astype(np.uint8), cv2.DIST_L2, 3)
    # Per character: which fill group owns it.
    chars = fix_spaces(line['chars'])
    if not chars:
        return [], None
    fill_ids = {g['id']: g for g in fills}
    owner = []
    cuts = []
    for ch in chars:
        cq = tf(M, ch['quad'])
        a, b = int(cq[:, 0].min()) - X0, int(math.ceil(cq[:, 0].max())) - X0
        cuts.append((a, b))
        # Judge by the middle of the box: Vision's boxes are loose and the
        # neighbours' strokes reach into their edges.
        m0 = max(0, a + (b - a) // 5)
        m1 = max(m0 + 1, b - (b - a) // 5)
        region = labels[:, m0:m1]
        depth = ink_dt[:, m0:m1]
        # Weighted by depth inside the ink: the glyph body outweighs an
        # outline or a shadow, which are thin bands along its edge.
        counts = {gid: float(depth[region == gid].sum()) for gid in fill_ids}
        owner.append(max(counts, key=counts.get) if counts and max(counts.values()) > 0 else None)
    # Glue characters without ink of their own (spaces, punctuation) to a neighbour.
    for i in range(len(owner)):
        if owner[i] is None:
            owner[i] = owner[i - 1] if i and owner[i - 1] is not None else next((o for o in owner[i:] if o is not None), None)

    # A Latin word is one run even when its colour drifts (a gradient):
    # letters inside a word take the owner of the word's first letter.
    for i in range(1, len(chars)):
        if re.fullmatch(r'[A-Za-z0-9]', chars[i]['c']) and re.fullmatch(r'[A-Za-z0-9]', chars[i - 1]['c']):
            owner[i] = owner[i - 1]

    runs = []
    i = 0
    while i < len(chars):
        j = i
        while j + 1 < len(chars) and owner[j + 1] == owner[i]:
            j += 1
        s = ''.join(c['c'] for c in chars[i:j + 1])
        if s.strip():
            slack = int(lh * 0.3)
            left = cuts[i][0] - slack if i == 0 else (cuts[i - 1][1] + cuts[i][0]) // 2
            right = cuts[j][1] + slack if j == len(chars) - 1 else (cuts[j][1] + cuts[j + 1][0]) // 2
            runs.append((s, owner[i], max(0, left), min(patch.shape[1], right)))
        i = j + 1

    out = []
    for s, gid, a, b in runs:
        g = fill_ids.get(gid)
        if g is None:
            continue
        m = np.zeros_like(ink)
        m[:, a:b] = g['mask'][:, a:b]
        # Every fill colour inside the run's span is glyph: a gradient or a
        # two-tone letter is split across groups (shadows were excluded above).
        g_lab = lab(np.asarray(g['rgb'], float).reshape(1, 3))[0]
        for other in fills:
            if other is not g and np.linalg.norm(lab(np.asarray(other['rgb'], float).reshape(1, 3))[0] - g_lab) < 60:
                m[:, a:b] |= other['mask'][:, a:b]
        # Keep only ink that belongs to this line. Vision's line boxes are
        # generous and overlap, so a neighbour's glyphs reach into the patch:
        # a blob stays only if this line's centre is the nearest one to it.
        n_cc, cc, stats, cents = cv2.connectedComponentsWithStats(m.astype(np.uint8), connectivity=8)
        keep = np.zeros_like(m)
        for c in range(1, n_cc):
            if line.get('manual'):
                keep |= cc == c  # a reviewer drew this box: everything in it is ours
                continue
            px, py = cents[c][0] + X0, cents[c][1] + Y0
            ix, iy = int(cents[c][0]), int(cents[c][1])
            if 0 <= iy < h_p and 0 <= ix < w_p and other_core[iy, ix] and not core[iy, ix]:
                continue
            mine = abs(py - my_cy)
            if all(abs(py - oy) >= mine for ox0, ox1, oy in others_c if ox0 - 4 <= px <= ox1 + 4):
                keep |= cc == c
        m = keep
        box = ink_box(m)
        if box is None or (box[3] - box[1]) < max(6, 0.35 * lh):
            continue
        rgb = np.median(patch[g['mask'][:, a:b].nonzero()[0], np.arange(a, b)[g['mask'][:, a:b].nonzero()[1]]], axis=0) if g['mask'][:, a:b].any() else g['rgb']
        out.append(TextRun(text=s.strip(), mask=m[box[1]:box[3], box[0]:box[2]],
                           box=(box[0] + X0, box[1] + Y0, box[2] + X0, box[3] + Y0),
                           rgb=rgb, angle=angle, M=M, center=(cx, cy)))
    run_ink = np.zeros_like(ink)
    for r in out:
        bx0, by0 = r.box[0] - X0, r.box[1] - Y0
        run_ink[by0:by0 + r.mask.shape[0], bx0:bx0 + r.mask.shape[1]] |= r.mask
    ink = ink | run_ink
    outline_info = None
    if outline is not None:
        outline_info = {'rgb': outline['rgb'], 'width': 2.0 * outline['dt']}
    # Everything that is ink goes into the removal mask, in cover coordinates.
    full = np.zeros((H, W), np.uint8)
    # Glyph edges are anti-aliased and often carry a soft shadow: take the ink
    # generously so no ghost of it survives the inpainting.
    g = max(2, int(round(lh * 0.1)))
    removal = cv2.dilate(ink.astype(np.uint8), np.ones((2 * g + 1, 2 * g + 1), np.uint8))
    full[Y0:Y1, X0:X1] = removal * 255
    if angle:
        full = cv2.warpAffine(full, np.linalg.inv(M)[:2], (W, H), flags=cv2.INTER_NEAREST)
    return out, (outline_info, full)


def fit_font(run: TextRun, calib: dict, only=None, italic_ok=True, spacing_em=None):
    """Finds the candidate face (and size / spacing / synthetic italic) that
    best redraws the run.

    The size comes from the ink height, unless that would need the letters
    squeezed together hard to fit the ink width — then the height is inflated
    (a 3D extrusion or shadow under the glyphs, a descending tail) and the
    width is the better witness. The drawing is then laid over the cover's
    glyph mask, top-left aligned, unstretched, and scored by overlap."""
    obs = run.mask
    oh, ow = obs.shape
    text = run.text
    n = len(text)
    has_cjk = bool(CJK.search(text))
    best = None
    for key, family, weight, *_rest, scripts in FONTS:
        if only and key not in only:
            continue
        if has_cjk and 'cjk' not in scripts and not only:
            continue
        slanted_face = key in ('youshe', 'smiley')
        for italic in ((False,) if slanted_face or not italic_ok else (False, True)):
            try:
                m100, _ = draw(key, text, 100, italic=italic)
            except Exception:
                continue
            b = ink_box(m100)
            if b is None:
                continue
            h100, w100 = b[3] - b[1], b[2] - b[0]
            size_h = 100 * oh / max(1, h100)
            size_w = 100 * (ow + 0.03 * (n - 1) * size_h) / max(1, w100) if n > 1 else 100 * ow / max(1, w100)
            for size in {round(size_h, 2), round(min(size_h, size_w), 2)}:
                if size < 6:
                    continue
                spacing = (ow - w100 * size / 100) / (n - 1) if n > 1 else 0.0
                spacing = max(-0.3 * size, min(0.6 * size, spacing))
                if spacing_em is not None:
                    spacing = spacing_em * size
                m, origin = draw(key, text, size, spacing, italic=italic)
                bb = ink_box(m)
                glyph = m[bb[1]:bb[3], bb[0]:bb[2]]
                gh, gw = glyph.shape
                A = np.zeros((max(gh, oh), max(gw, ow)), bool)
                B = A.copy()
                A[:gh, :gw] = glyph
                B[:oh, :ow] = obs
                # Rows below the drawing's own bottom are extrusion/shadow: ignore.
                A, B = A[:gh], B[:gh]
                score = (A & B).sum() / max(1, (A | B).sum())
                # Covers are set tight, but not this tight: beyond −0.1 em the
                # fit is squeezing an inflated height back into the width.
                em = spacing / size
                score -= 0.1 * abs(em) + 0.8 * max(0.0, -em - 0.1) + (0.01 if italic else 0)
                cand = {'key': key, 'family': family, 'weight': weight, 'size': size, 'spacing': spacing, 'italic': italic,
                        'score': float(score), 'ink_dx': bb[0] - origin[0], 'ink_dy': bb[1] - origin[1],
                        'dy': calib[key]['H' if not has_cjk else '国']['dy'], 'ink_w': gw}
                if best is None or cand['score'] > best['score']:
                    best = cand
    return best


# --- main -----------------------------------------------------------------------

def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('image')
    ap.add_argument('out')
    ap.add_argument('--crop', help='x0,y0,x1,y1 of the cover inside the screenshot')
    ap.add_argument('--fix', help='JSON with corrections: {"text": {"OCR text": "real text"}, "drop": ["text"]}')
    ap.add_argument('--width', type=int, default=0, help='canvas width (default 1242 portrait / 1920 landscape)')
    args = ap.parse_args()
    os.makedirs(args.out, exist_ok=True)
    fixes = json.load(open(args.fix)) if args.fix else {}

    src = Image.open(args.image).convert('RGB')
    box = tuple(int(v) for v in args.crop.split(',')) if args.crop else find_cover(src)
    # Feed cards have rounded corners; step in a little so no card colour is left.
    inset = max(2, round(min(box[2] - box[0], box[3] - box[1]) * 0.006)) if box != (0, 0, src.width, src.height) else 0
    box = (box[0] + inset, box[1] + inset, box[2] - inset, box[3] - inset)
    cover_img = src.crop(box)
    pre = 1.0  # analysis scale vs the source (manual boxes are in source pixels)
    # Big sources (a 2600px export) cost the models gigabytes and gain nothing:
    # the analysis runs at feed-cover resolution; the canvas size is set apart.
    if max(cover_img.size) > 1200:
        f = 1200 / max(cover_img.size)
        pre = f
        cover_img = cover_img.resize((round(cover_img.width * f), round(cover_img.height * f)), Image.LANCZOS)
    cover_path = os.path.join(args.out, 'cover.png')
    cover_img.save(cover_path)
    cover = np.asarray(cover_img)
    H, W, _ = cover.shape
    cw = args.width or (1242 if H >= W else 1920)
    K = cw / W
    ch = round(H * K)

    vis = run_vision(cover_path)
    calib = calibration()
    report = {'crop': box, 'size': [W, H], 'canvas': [cw, ch], 'runs': [], 'faces': vis['faces']}

    # --- text
    # Corrections from review: misread lines, lines that are part of the
    # picture (a shirt logo, a screen), and big lettering OCR did not see.
    fixed_lines = fixes.get('lines', {})
    styles = fixes.get('style', {})
    drop_runs = set(fixes.get('drop_runs', []))
    drop_lines = set(fixes.get('drop', []))
    lines = []
    import difflib

    def like(t, keys):
        """The review key this OCR text is (OCR varies a little run to run)."""
        if t in keys:
            return t
        best = max(keys, key=lambda k: difflib.SequenceMatcher(None, t, k).ratio(), default=None)
        if best is not None and len(t) >= 3 and difflib.SequenceMatcher(None, t, best).ratio() >= 0.6:
            return best
        return None

    for line in vis['lines']:
        t = line['text'].strip()
        if like(t, drop_lines):
            continue
        k = like(t, list(fixed_lines))
        if k:
            line = corrected(line, fixed_lines[k])
        lines.append(line)
    for a in fixes.get('add', []):
        l = added(a['text'], a['box'], a.get('angle', 0.0))  # in cover.png pixels (after any downscale)
        l['style'] = {k: a[k] for k in ('font', 'fill', 'stroke', 'opacity', 'match', 'italic', 'spacing', 'erase', 'erase_box', 'nudge') if k in a}
        l['behind'] = bool(a.get('behind'))
        lines.append(l)
    vis['lines'] = lines
    text_mask = np.zeros((H, W), np.uint8)
    layers = []
    # Every recognised line's box: never sampled as "background" for another line.
    text_boxes = np.zeros((H, W), np.uint8)
    for line in vis['lines']:
        grow = line['box'][3] * 0.12
        q = np.array(line['quad'], float)
        c = q.mean(axis=0)
        q = c + (q - c) * (1 + 2 * grow / max(1.0, line['box'][3]))
        cv2.fillPoly(text_boxes, [np.round(q).astype(np.int32)], 1)
    replace = fixes.get('text', {})
    drop = set(fixes.get('drop', []))
    for line in sorted(vis['lines'], key=lambda l: (l['box'][1], l['box'][0])):
        t = line['text'].strip()
        if not t or (t in drop and not line.get('manual')):
            continue
        if re.fullmatch(r'\d{1,2}:\d{2}', t):  # a video's duration badge: feed UI, not design
            continue
        # Design text on a cover is set big; small print is usually part of a
        # photo (a screen, a document) and stays in the picture.
        if line['box'][3] < H * 0.022 and not line.get('manual'):
            continue
        # Only symbols: an icon Vision took for punctuation.
        if not re.search(r'[\w\u3400-\u9fff]', t):
            continue
        runs, extra = analyse_line(cover, line, fixes, text_boxes, vis['lines'])
        if extra is None:
            continue
        outline, ink_full = extra
        text_mask |= ink_full
        if (line.get('style') or {}).get('erase_box'):
            # The reviewer asked for the whole box to be repainted (the text
            # has glows or outlines colour matching cannot catch).
            cv2.fillPoly(text_mask, [np.round(np.array(line['quad'])).astype(np.int32)], 255)
        for r in runs:
            real = replace.get(r.text, r.text)
            if real != r.text:
                r.text = real
            if r.text in drop_runs and not line.get('manual'):
                continue
            # Review overrides for this run: a face, a colour, an outline.
            ov = {**styles.get(r.text, {}), **line.get('style', {})}
            want = ov.get('font')
            fit = fit_font(r, calib, only=[want] if isinstance(want, str) else want, italic_ok=ov.get('italic', True), spacing_em=ov.get('spacing'))
            if fit is None:
                continue
            size = fit['size']
            # Layer origin in the deskewed frame, then back to the cover.
            ox = r.box[0] - fit['ink_dx']
            oy = r.box[1] - fit['ink_dy'] - fit['dy'] * size
            if r.angle:
                inv = np.linalg.inv(r.M)
                ox, oy = tf(inv, [(ox, oy)])[0]
            nudge = ov.get('nudge')
            if nudge:
                ox, oy = ox + nudge[0], oy + nudge[1]
            style = {
                'fontFamily': fit['family'],
                'fontWeight': fit['weight'],
                'fontSize': round(size * K, 1),
                'fill': hexcolor(r.rgb),
                'charSpacing': round(fit['spacing'] / size * 1000),
                'lineHeight': 1.0,
            }
            if fit['italic']:
                style['fontStyle'] = 'italic'
            if outline and outline['width'] >= 0.8:
                # The visible band outside the glyph is half of Fabric's stroke.
                style['stroke'] = f"{hexcolor(outline['rgb'])}:{round(outline['width'] * 2 * K, 1)}"
                style['paintFirst'] = True
            if 'fill' in ov:
                style['fill'] = ov['fill']
            if 'stroke' in ov:
                if ov['stroke']:
                    style['stroke'] = ov['stroke']
                    style['paintFirst'] = True
                else:
                    style.pop('stroke', None)
                    style.pop('paintFirst', None)
            if 'opacity' in ov:
                style['_opacity'] = ov['opacity']
            layers.append({
                '_behind': bool(line.get('behind')),
                'type': 'addText', 'text': r.text, 'name': r.text[:12],
                'x': round(ox * K, 1), 'y': round(oy * K, 1),
                'width': round((fit['ink_w'] + size * 2) * K),
                'style': style,
                **({'_rotation': round(r.angle, 2)} if r.angle else {}),
            })
            report['runs'].append({'text': r.text, 'font': fit['key'], 'score': round(fit['score'], 3),
                                   'size': round(size * K, 1), 'fill': style['fill'], 'stroke': style.get('stroke'), 'italic': fit['italic'],
                                   'angle': round(r.angle, 2)})

    # Text out first: people and elements are cut from a cover without it.
    from simple_lama_inpainting import SimpleLama
    lama = SimpleLama()
    grow = cv2.dilate(text_mask, np.ones((5, 5), np.uint8), iterations=1)
    clean = np.asarray(lama(cover_img, Image.fromarray(grow)))[:H, :W] if grow.any() else cover.copy()

    # --- people
    from rembg import new_session, remove
    people = np.zeros((H, W), np.uint8)
    pieces = []
    stage = clean
    if vis['faces']:
        alpha = np.asarray(remove(Image.fromarray(clean), session=new_session('birefnet-portrait'), only_mask=True))
        if (alpha > 128).mean() > 0.02:
            people = alpha
            pieces.append(('人物', alpha))
            g = max(7, int(W * 0.025)) | 1
            hole = cv2.dilate((alpha > 20).astype(np.uint8) * 255, np.ones((g, g), np.uint8))
            stage = np.asarray(lama(Image.fromarray(clean), Image.fromarray(hole)))[:H, :W]

    # --- elements: with the people gone, whatever stands out next (a device,
    # a mascot, app icons) is an element. Two rounds, so something that was
    # standing behind the first catch gets its turn.
    general = new_session('birefnet-general')
    k = 0
    person_zone = cv2.dilate((people > 60).astype(np.uint8), np.ones((9, 9), np.uint8)) > 0
    for _round in range(2):
        sal = np.asarray(remove(Image.fromarray(stage), session=general, only_mask=True))
        n, lab_img, stats, _ = cv2.connectedComponentsWithStats((sal > 128).astype(np.uint8), 8)
        found = np.zeros((H, W), np.uint8)
        for i in range(1, n):
            area = stats[i, cv2.CC_STAT_AREA]
            if area < W * H * 0.002 or area > W * H * 0.35:
                continue
            m = lab_img == i
            # Something LaMa made up where the person was is not an element.
            if (m & person_zone).sum() > 0.4 * area:
                continue
            k += 1
            a = np.where(cv2.dilate(m.astype(np.uint8), np.ones((5, 5), np.uint8)) > 0, sal, 0).astype(np.uint8)
            pieces.append((f'元素 {k}', a))
            found |= (a > 40).astype(np.uint8)
        if not found.any():
            break
        hole = cv2.dilate(found * 255, np.ones((7, 7), np.uint8))
        stage = np.asarray(lama(Image.fromarray(stage), Image.fromarray(hole)))[:H, :W]
    bg = stage
    Image.fromarray(bg).resize((cw, ch), Image.LANCZOS).save(os.path.join(args.out, 'bg.png'))

    cmds = [
        {'type': 'setCanvasSize', 'width': cw, 'height': ch},
        {'type': 'addImage', 'path': os.path.abspath(os.path.join(args.out, 'bg.png')), 'x': 0, 'y': 0, 'width': cw, 'height': ch, 'name': '背景'},
    ]
    # Names must be unique: the rotation commands find their layer by it.
    seen = {}
    for l in layers:
        base = l['name']
        seen[base] = seen.get(base, 0) + 1
        if seen[base] > 1:
            l['name'] = f'{base} {seen[base]}'

    def emit(l):
        l.pop('_behind', None)
        rot = l.pop('_rotation', None)
        op = l['style'].pop('_opacity', None) if 'style' in l else None
        cmds.append(l)
        if op is not None:
            cmds.append({'type': 'setOpacity', 'id': l['name'], 'opacity': op})
        if rot:
            # rotateLayer turns about the centre; set the angle, then put the
            # rotated box's corner back where the cover has it.
            cmds.append({'type': 'updateLayer', 'id': l['name'], 'props': {'rotation': rot}})
            cmds.append({'type': 'moveLayer', 'id': l['name'], 'x': l['x'], 'y': l['y']})

    # Lettering the people stand in front of goes under them.
    for l in [l for l in layers if l.get('_behind')]:
        emit(l)
    layers = [l for l in layers if '_behind' in l]
    for idx, (name, a) in enumerate(pieces):
        ys, xs = np.nonzero(a > 10)
        x0, y0, x1, y1 = xs.min(), ys.min(), xs.max() + 1, ys.max() + 1
        rgba = np.dstack([clean[y0:y1, x0:x1], a[y0:y1, x0:x1]])
        f = os.path.join(args.out, f'piece-{idx}.png')
        big = Image.fromarray(rgba, 'RGBA').resize((max(1, round((x1 - x0) * K)), max(1, round((y1 - y0) * K))), Image.LANCZOS)
        big.save(f)
        cmds.append({'type': 'addImage', 'path': os.path.abspath(f), 'x': round(x0 * K), 'y': round(y0 * K),
                     'width': big.width, 'height': big.height, 'name': name})
    for l in layers:
        emit(l)
    json.dump(cmds, open(os.path.join(args.out, 'design.json'), 'w'), ensure_ascii=False, indent=1)
    json.dump(report, open(os.path.join(args.out, 'report.json'), 'w'), ensure_ascii=False, indent=1, default=float)
    print(json.dumps({'canvas': [cw, ch], 'runs': len(report['runs']), 'pieces': [p[0] for p in pieces]}, ensure_ascii=False))


if __name__ == '__main__':
    main()
