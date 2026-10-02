"""Creates a davinci project from a layerize output directory.

    python3 import_cover.py out/ original.png "项目名" [--render check.png]

Board 1 (设计稿) is rebuilt from out/design.json; board 2 (原图) holds the
original image untouched. Everything goes through the davinci CLI, exactly as
an agent would do it.
"""
import argparse
import json
import os
import shutil
import subprocess

from PIL import Image

DAVINCI = shutil.which('davinci') or os.path.join(os.path.dirname(__file__), '..', '..', 'bin', 'davinci')


def dv(*args, parse=False):
    out = subprocess.run([DAVINCI, *args], capture_output=True, text=True)
    if out.returncode != 0:
        raise SystemExit(f'davinci {" ".join(args)}: {out.stderr.strip() or out.stdout.strip()}')
    return json.loads(out.stdout) if parse else out.stdout


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('out')
    ap.add_argument('original')
    ap.add_argument('name')
    ap.add_argument('--render', help='render board 1 to this PNG afterwards')
    ap.add_argument('--replace', action='store_true', help='delete an existing project of the same name first')
    args = ap.parse_args()

    design = json.load(open(os.path.join(args.out, 'design.json')))
    size = next(c for c in design if c['type'] == 'setCanvasSize')
    if args.replace:
        listing = dv('projects', '--json', parse=True)
        for p in listing.get('projects', []):
            if p['name'] == args.name:
                dv('projects', 'rm', p['id'])
    created = dv('new', args.name, '--w', str(size['width']), '--h', str(size['height']), '--json', parse=True)
    pid = created['project']['id']
    dv('-p', pid, 'board', 'rename', '1', '设计稿')
    body = [c for c in design if c['type'] != 'setCanvasSize']
    tmp = os.path.join(args.out, 'design.exec.json')
    json.dump(body, open(tmp, 'w'), ensure_ascii=False)
    dv('-p', pid, '-b', '1', 'exec', '--file', tmp)

    orig = Image.open(args.original)
    dv('-p', pid, 'board', 'add', '--name', '原图', '--w', str(orig.width), '--h', str(orig.height))
    dv('-p', pid, '-b', '2', 'exec', json.dumps({
        'type': 'addImage', 'path': os.path.abspath(args.original),
        'x': 0, 'y': 0, 'width': orig.width, 'height': orig.height, 'name': '原图',
    }, ensure_ascii=False))
    dv('-p', pid, 'board', 'use', '1')
    if args.render:
        dv('-p', pid, '-b', '1', 'render', '-o', args.render)
    print(pid)


if __name__ == '__main__':
    main()
