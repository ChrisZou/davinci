"""Measures where davinci (Fabric in Chrome) draws each candidate font
relative to PIL, and writes calib.json. Run after changing fonts.py:

    python3 calibrate.py        # needs the davinci CLI and a running server
"""
import json
import os
import subprocess
import tempfile

import numpy as np
from PIL import Image

from fonts import FONTS
from glyphs import draw, ink_box

HERE = os.path.dirname(os.path.abspath(__file__))
DAVINCI = os.environ.get('DAVINCI', 'davinci')
ROW, FS = 300, 200


def dv(*args):
    return subprocess.run([DAVINCI, *args], capture_output=True, text=True, check=True).stdout


def main():
    name = 'layerize 字体校准'
    cmds = [{'type': 'setBackground', 'color': '#ffffff'}]
    for i, (key, family, weight, *_rest) in enumerate(FONTS):
        for ch, x in (('国', 100), ('H', 900)):
            cmds.append({'type': 'addText', 'text': ch, 'x': x, 'y': ROW * i + 20, 'width': 400,
                         'name': f'{key}-{ch}', 'style': {'fontFamily': family, 'fontWeight': weight, 'fontSize': FS, 'fill': '#000000'}})
    with tempfile.TemporaryDirectory() as tmp:
        f = os.path.join(tmp, 'c.json')
        json.dump(cmds, open(f, 'w'), ensure_ascii=False)
        dv('new', name, '--w', '2400', '--h', str(ROW * len(FONTS)))
        try:
            dv('-p', name, 'exec', '--file', f)
            png = os.path.join(tmp, 'c.png')
            dv('-p', name, 'render', '-o', png)
            a = np.asarray(Image.open(png).convert('L')) < 128
        finally:
            dv('projects', 'rm', name)
    out = {}
    for i, (key, *_rest) in enumerate(FONTS):
        r = {}
        for ch, x0 in (('国', 100), ('H', 900)):
            ys, xs = np.nonzero(a[ROW * i:ROW * i + ROW, x0 - 60:x0 + 700])
            m, (ox, oy) = draw(key, ch, FS)
            b = ink_box(m)
            r[ch] = {'dx': round((xs.min() - 60 - (b[0] - ox)) / FS, 4), 'dy': round((ys.min() - 20 - (b[1] - oy)) / FS, 4),
                     'hr': round((ys.max() - ys.min() + 1) / (b[3] - b[1]), 3), 'wr': round((xs.max() - xs.min() + 1) / (b[2] - b[0]), 3)}
        out[key] = r
        print(key, r['H'])
    json.dump(out, open(os.path.join(HERE, 'calib.json'), 'w'), indent=1, ensure_ascii=False)


if __name__ == '__main__':
    main()
