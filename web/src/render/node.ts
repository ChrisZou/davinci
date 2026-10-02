/**
 * The server-side renderer: the same Renderer the editor draws with, run in
 * Node for the davinci server. It measures text (the server keeps each text
 * layer's height in the document) and renders exports and thumbnails.
 *
 * Protocol: one JSON request per line on stdin, one JSON reply per line on
 * stdout, matched by `id`:
 *
 *   {"id":1,"op":"measure","layers":[Layer…]}
 *     → {"id":1,"ok":true,"result":{"<layer id>":{"height":…,"width":…,"lines":…}}}
 *   {"id":2,"op":"render","board":{canvas,layers},"scale":1,"format":"png","transparent":false,"ids":[…]}
 *     → {"id":2,"ok":true,"result":{"data":"<base64>","width":…,"height":…}}
 *
 * Fonts and images come from the davinci server named by DAVINCI_URL.
 */
import CanvasKitInit from 'canvaskit-wasm/bin/canvaskit.js'
import { createInterface } from 'node:readline'
import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import type { Board, Layer } from '../types'
import { Renderer, type Loader } from './renderer'

const base = (process.env.DAVINCI_URL || 'http://127.0.0.1:7789').replace(/\/$/, '')
const wasm = process.env.CANVASKIT_WASM || join(dirname(process.argv[1] || '.'), 'canvaskit.wasm')

const loader: Loader = {
  async font(family, weight, italic) {
    const q = new URLSearchParams({ family, weight: String(weight), italic: italic ? '1' : '0' })
    const r = await fetch(`${base}/api/fonts/face?${q}`)
    if (!r.ok) return null
    return {
      bytes: await r.arrayBuffer(),
      weight: Number(r.headers.get('x-font-weight')) || weight,
      italic: r.headers.get('x-font-italic') === 'true',
    }
  },
  async image(url) {
    if (url.startsWith('data:')) {
      const i = url.indexOf(',')
      return Uint8Array.from(Buffer.from(url.slice(i + 1), 'base64')).buffer
    }
    const r = await fetch(/^https?:/i.test(url) ? url : `${base}${url.startsWith('/') ? '' : '/'}${url}`)
    return r.ok ? r.arrayBuffer() : null
  },
}

function allTexts(layers: Layer[], out: Layer[] = []): Layer[] {
  for (const l of layers) {
    if (l.type === 'text') out.push(l)
    if (l.type === 'group') allTexts(l.children ?? [], out)
  }
  return out
}

async function main() {
  const ck = await CanvasKitInit({ wasmBinary: readFileSync(wasm) } as any)
  const renderer = new Renderer(ck, loader)
  const send = (msg: unknown) => process.stdout.write(JSON.stringify(msg) + '\n')
  send({ id: 0, ok: true, result: { ready: true } })

  const lines = createInterface({ input: process.stdin })
  // One request at a time: a render holds a large surface, and the server is
  // happy to queue.
  let queue = Promise.resolve()
  lines.on('line', (line) => {
    queue = queue.then(async () => {
      let req: any
      try {
        req = JSON.parse(line)
      } catch {
        return
      }
      try {
        if (req.op === 'measure') {
          const layers: Layer[] = req.layers ?? []
          await renderer.prepare({ canvas: { width: 1, height: 1 }, layers })
          const out: Record<string, unknown> = {}
          for (const l of allTexts(layers)) out[l.id] = renderer.measure(l)
          send({ id: req.id, ok: true, result: out })
        } else if (req.op === 'render') {
          const board = req.board as Board
          await renderer.prepare(board)
          const bytes = renderer.encode(board, {
            scale: req.scale,
            format: req.format,
            quality: req.quality,
            transparent: req.transparent,
            ids: req.ids,
          })
          send({ id: req.id, ok: true, result: { data: Buffer.from(bytes).toString('base64') } })
        } else if (req.op === 'ping') {
          send({ id: req.id, ok: true, result: { rss: process.memoryUsage().rss } })
        } else {
          send({ id: req.id, ok: false, error: `unknown op ${req.op}` })
        }
      } catch (e: any) {
        send({ id: req.id, ok: false, error: String(e?.message ?? e) })
      }
    })
  })
  lines.on('close', () => process.exit(0))
}

main().catch((e) => {
  process.stderr.write(String(e?.stack ?? e) + '\n')
  process.exit(1)
})
