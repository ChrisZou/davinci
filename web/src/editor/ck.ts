import CanvasKitInit, { type CanvasKit } from 'canvaskit-wasm'
import wasmURL from 'canvaskit-wasm/bin/canvaskit.wasm?url'
import type { Board } from '../types'
import { Renderer, type Loader } from '../render/renderer'

/**
 * CanvasKit in the page: one wasm instance and one Renderer for the editor
 * canvas, the board strip's thumbnails and exports. The server runs the very
 * same Renderer, so all of these match what it measures and exports.
 */

const loader: Loader = {
  async font(family, weight, italic) {
    const q = new URLSearchParams({ family, weight: String(weight), italic: italic ? '1' : '0' })
    const r = await fetch(`/api/fonts/face?${q}`)
    if (!r.ok) return null
    return {
      bytes: await r.arrayBuffer(),
      weight: Number(r.headers.get('x-font-weight')) || weight,
      italic: r.headers.get('x-font-italic') === 'true',
    }
  },
  async image(url) {
    const r = await fetch(url)
    return r.ok ? r.arrayBuffer() : null
  },
}

let booting: Promise<Renderer> | null = null

/** The page's renderer, created on first use. */
export function getRenderer(): Promise<Renderer> {
  if (!booting) {
    booting = (CanvasKitInit({ locateFile: () => wasmURL }) as Promise<CanvasKit>).then((ck) => new Renderer(ck, loader))
    booting.catch(() => {
      booting = null
    })
  }
  return booting
}

export type ImageFormat = 'png' | 'jpeg' | 'webp'

export interface EncodeOptions {
  scale?: number
  format?: ImageFormat
  /** 0–1, JPEG/WebP only. */
  quality?: number
  transparent?: boolean
  /** Only these layers, cropped to their combined box. */
  ids?: string[]
}

/**
 * Renders a board to an image blob. CanvasKit's build only encodes PNG, so a
 * JPEG or WebP goes through the browser's own encoder.
 */
export async function encodeBoard(board: Pick<Board, 'canvas' | 'layers'>, opts: EncodeOptions = {}): Promise<Blob> {
  const r = await getRenderer()
  await r.prepare(board)
  const format = opts.format ?? 'png'
  const png = r.encode(board, { scale: opts.scale, format: 'png', transparent: opts.transparent, ids: opts.ids })
  const blob = new Blob([png as BlobPart], { type: 'image/png' })
  if (format === 'png') return blob
  const bmp = await createImageBitmap(blob)
  const c = document.createElement('canvas')
  c.width = bmp.width
  c.height = bmp.height
  const ctx = c.getContext('2d')!
  if (format === 'jpeg') {
    // JPEG has no alpha: whatever is see-through lands on white.
    ctx.fillStyle = '#ffffff'
    ctx.fillRect(0, 0, c.width, c.height)
  }
  ctx.drawImage(bmp, 0, 0)
  bmp.close()
  return new Promise((resolve, reject) =>
    c.toBlob((b) => (b ? resolve(b) : reject(new Error('could not encode the image'))), `image/${format}`, opts.quality ?? 0.92),
  )
}

export function blobToDataURL(b: Blob): Promise<string> {
  return new Promise((resolve, reject) => {
    const fr = new FileReader()
    fr.onload = () => resolve(String(fr.result))
    fr.onerror = () => reject(fr.error)
    fr.readAsDataURL(b)
  })
}

/** A small picture of a board (the board strip), as a data URL. */
export async function renderBoard(board: Pick<Board, 'canvas' | 'layers'>, opts: { width: number }): Promise<string> {
  const scale = Math.max(0.01, Math.min(1, opts.width / board.canvas.width))
  return blobToDataURL(await encodeBoard(board, { scale }))
}
