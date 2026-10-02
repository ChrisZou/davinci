import { useEffect, useRef, useState } from 'react'
import type { Layer } from '../types'
import { Modal } from './Dialog'

/**
 * Crop an image layer by drawing a window over the whole picture.
 *
 * The window is kept in the image's own pixels — the same units `cropImage`
 * takes — so what this dialog commits is exactly the command AI would send.
 */

interface Rect {
  x: number
  y: number
  w: number
  h: number
}

type Handle = 'move' | 'nw' | 'n' | 'ne' | 'e' | 'se' | 's' | 'sw' | 'w'

const MIN = 8
const BOX_W = 640
const BOX_H = 480

export function CropDialog({
  layer,
  canvasRatio,
  onApply,
  onClose,
}: {
  layer: Layer
  /** The document's width/height, offered as a ratio preset. */
  canvasRatio: number
  onApply: (crop: { x: number; y: number; width: number; height: number } | null) => void
  onClose: () => void
}) {
  const url = layer.image?.url ?? ''
  const [nat, setNat] = useState<{ w: number; h: number } | null>(null)
  const [rect, setRect] = useState<Rect | null>(null)
  const [ratio, setRatio] = useState<number | null>(null)
  const drag = useRef<{ handle: Handle; px: number; py: number; start: Rect } | null>(null)
  // Enter confirms wherever focus is; the handler reads the latest window.
  const applyRef = useRef<() => void>(() => {})
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Enter') {
        e.preventDefault()
        applyRef.current()
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [])

  useEffect(() => {
    const img = new Image()
    img.onload = () => {
      const w = img.naturalWidth
      const h = img.naturalHeight
      setNat({ w, h })
      const c = layer.image?.crop
      setRect(c ? { x: c.x, y: c.y, w: c.width, h: c.height } : { x: 0, y: 0, w, h })
    }
    img.src = url
    // Keyed by value: the layer object is rebuilt on every document change, and
    // re-running on identity would throw the human's window away mid-drag.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [url, JSON.stringify(layer.image?.crop ?? null)])

  if (!nat || !rect) {
    return (
      <Modal onClose={onClose} label="裁剪图片">
        <div className="p-6 text-sm text-muted">载入图片…</div>
      </Modal>
    )
  }

  const k = Math.min(BOX_W / nat.w, BOX_H / nat.h)
  const dw = Math.round(nat.w * k)
  const dh = Math.round(nat.h * k)

  const clampRect = (r: Rect): Rect => {
    const w = Math.max(MIN, Math.min(nat.w, r.w))
    const h = Math.max(MIN, Math.min(nat.h, r.h))
    return { x: Math.max(0, Math.min(nat.w - w, r.x)), y: Math.max(0, Math.min(nat.h - h, r.y)), w, h }
  }

  /** The biggest window of a ratio that fits inside `r`, centred on it. */
  const fitRatio = (r: Rect, q: number): Rect => {
    let w = r.w
    let h = w / q
    if (h > r.h) {
      h = r.h
      w = h * q
    }
    return clampRect({ x: r.x + (r.w - w) / 2, y: r.y + (r.h - h) / 2, w, h })
  }

  const chooseRatio = (q: number | null) => {
    setRatio(q)
    // A ratio is applied to the whole picture, not the current window, so
    // switching presets never ratchets the window smaller and smaller.
    if (q) setRect(fitRatio({ x: 0, y: 0, w: nat.w, h: nat.h }, q))
  }

  const onPointerDown = (handle: Handle) => (e: React.PointerEvent) => {
    e.preventDefault()
    e.stopPropagation()
    ;(e.target as HTMLElement).setPointerCapture(e.pointerId)
    drag.current = { handle, px: e.clientX, py: e.clientY, start: rect }
  }

  const onPointerMove = (e: React.PointerEvent) => {
    const d = drag.current
    if (!d) return
    const dx = (e.clientX - d.px) / k
    const dy = (e.clientY - d.py) / k
    const s = d.start
    if (d.handle === 'move') {
      setRect(clampRect({ ...s, x: s.x + dx, y: s.y + dy }))
      return
    }
    let { x, y, w, h } = s
    const hd = d.handle
    if (hd.includes('e')) w = Math.min(nat.w - s.x, Math.max(MIN, s.w + dx))
    if (hd.includes('s')) h = Math.min(nat.h - s.y, Math.max(MIN, s.h + dy))
    if (hd.includes('w')) {
      const nx = Math.max(0, Math.min(s.x + s.w - MIN, s.x + dx))
      w = s.w + (s.x - nx)
      x = nx
    }
    if (hd.includes('n')) {
      const ny = Math.max(0, Math.min(s.y + s.h - MIN, s.y + dy))
      h = s.h + (s.y - ny)
      y = ny
    }
    if (ratio) {
      // Keep the ratio from the width, anchored at the corner opposite the handle,
      // then shrink both sides if that runs off the picture.
      h = w / ratio
      if (hd.includes('n')) y = s.y + s.h - h
      const maxH = hd.includes('n') ? s.y + s.h : nat.h - y
      if (h > maxH) {
        h = maxH
        const nw = h * ratio
        if (hd.includes('w')) x += w - nw
        w = nw
        if (hd.includes('n')) y = s.y + s.h - h
      }
    }
    setRect(clampRect({ x, y, w, h }))
  }

  const onPointerUp = () => {
    drag.current = null
  }

  const full = rect.x === 0 && rect.y === 0 && Math.round(rect.w) === nat.w && Math.round(rect.h) === nat.h
  const apply = () => {
    const r = { x: Math.round(rect.x), y: Math.round(rect.y), width: Math.round(rect.w), height: Math.round(rect.h) }
    onApply(full ? null : r)
  }
  applyRef.current = apply

  const handles: Handle[] = ratio ? ['nw', 'ne', 'se', 'sw'] : ['nw', 'n', 'ne', 'e', 'se', 's', 'sw', 'w']
  const handlePos: Record<Handle, string> = {
    move: '',
    nw: 'left-0 top-0 -translate-x-1/2 -translate-y-1/2 cursor-nwse-resize',
    n: 'left-1/2 top-0 -translate-x-1/2 -translate-y-1/2 cursor-ns-resize',
    ne: 'right-0 top-0 translate-x-1/2 -translate-y-1/2 cursor-nesw-resize',
    e: 'right-0 top-1/2 translate-x-1/2 -translate-y-1/2 cursor-ew-resize',
    se: 'right-0 bottom-0 translate-x-1/2 translate-y-1/2 cursor-nwse-resize',
    s: 'left-1/2 bottom-0 -translate-x-1/2 translate-y-1/2 cursor-ns-resize',
    sw: 'left-0 bottom-0 -translate-x-1/2 translate-y-1/2 cursor-nesw-resize',
    w: 'left-0 top-1/2 -translate-x-1/2 -translate-y-1/2 cursor-ew-resize',
  }

  const RATIOS: { label: string; q: number | null }[] = [
    { label: '自由', q: null },
    { label: '原图', q: nat.w / nat.h },
    { label: '画布', q: canvasRatio },
    { label: '1:1', q: 1 },
    { label: '3:4', q: 3 / 4 },
    { label: '4:3', q: 4 / 3 },
    { label: '16:9', q: 16 / 9 },
    { label: '9:16', q: 9 / 16 },
  ]

  return (
    <Modal onClose={onClose} width="w-auto" label="裁剪图片">
      <div className="p-6">
        <div className="mb-3 flex items-center justify-between gap-4">
          <h2 className="font-serif text-lg font-black text-ink">裁剪 · {layer.name}</h2>
          <span className="text-xs tabular-nums text-muted">
            {Math.round(rect.w)} × {Math.round(rect.h)} px（原图 {nat.w} × {nat.h}）
          </span>
        </div>
        <div className="mb-4 flex flex-wrap gap-1.5">
          {RATIOS.map((r) => (
            <button
              key={r.label}
              className={`h-8 rounded-[10px] px-3 text-xs transition-colors ${
                (ratio === null && r.q === null) || (ratio !== null && r.q !== null && Math.abs(ratio - r.q) < 1e-6)
                  ? 'bg-ink font-bold text-card'
                  : 'bg-paper text-ink hover:bg-paper-deep'
              }`}
              onClick={() => chooseRatio(r.q)}
            >
              {r.label}
            </button>
          ))}
        </div>
        <div
          className="dv-checker relative mx-auto touch-none select-none"
          style={{ width: dw, height: dh }}
          onPointerMove={onPointerMove}
          onPointerUp={onPointerUp}
        >
          <img src={url} alt="" draggable={false} className="block h-full w-full" />
          <div
            className="absolute cursor-move outline outline-2 outline-white"
            data-testid="crop-window"
            style={{
              left: rect.x * k,
              top: rect.y * k,
              width: rect.w * k,
              height: rect.h * k,
              // The dimmed outside is one giant shadow around the window.
              boxShadow: '0 0 0 9999px rgba(28,27,24,0.55)',
            }}
            onPointerDown={onPointerDown('move')}
          >
            {/* Rule-of-thirds lines. */}
            <div className="pointer-events-none absolute inset-0 grid grid-cols-3 grid-rows-3">
              {Array.from({ length: 9 }).map((_, i) => (
                <div key={i} className="border border-white/20" />
              ))}
            </div>
            {handles.map((h) => (
              <div
                key={h}
                className={`absolute h-3.5 w-3.5 rounded-full border-2 border-accent bg-white ${handlePos[h]}`}
                onPointerDown={onPointerDown(h)}
              />
            ))}
          </div>
        </div>
        <div className="mt-5 flex items-center justify-between">
          <button
            className="rounded-lg px-2 py-1.5 text-xs text-muted hover:bg-paper hover:text-ink"
            onClick={() => {
              setRatio(null)
              setRect({ x: 0, y: 0, w: nat.w, h: nat.h })
            }}
          >
            重置为完整图片
          </button>
          <div className="flex gap-2">
            <button className="btn-ghost" onClick={onClose}>
              取消
            </button>
            <button className="btn-primary" onClick={apply} data-testid="crop-apply">
              确定 ↵
            </button>
          </div>
        </div>
      </div>
    </Modal>
  )
}
