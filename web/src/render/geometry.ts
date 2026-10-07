import type { Layer } from '../types'

/**
 * Where a layer sits on its board — the one geometry every part of davinci
 * agrees on: the renderer draws with it, the editor hit-tests and draws
 * selection frames with it, and the server ports it for alignment.
 *
 * It follows the layout Fabric.js used when these documents were made, so
 * documents from then render exactly where they were:
 *
 *  - (x, y) is the top-left corner of the layer's box, and the box turns
 *    about that corner by `rotation` degrees clockwise;
 *  - the box counts half the stroke on each side (a stroke of 1 when none is
 *    set, except images and groups), and for slanted or sheared text it is
 *    the box around that shape;
 *  - the content (text lines, image pixels, a shape) is drawn centred in the
 *    box, in its own units, then stretched, slanted and flipped.
 */

/** A 2D affine transform as [a, b, c, d, e, f]: x' = a·x + c·y + e, y' = b·x + d·y + f. */
export type Mat = [number, number, number, number, number, number]

export const IDENTITY: Mat = [1, 0, 0, 1, 0, 0]

export function multiply(m: Mat, n: Mat): Mat {
  return [
    m[0] * n[0] + m[2] * n[1],
    m[1] * n[0] + m[3] * n[1],
    m[0] * n[2] + m[2] * n[3],
    m[1] * n[2] + m[3] * n[3],
    m[0] * n[4] + m[2] * n[5] + m[4],
    m[1] * n[4] + m[3] * n[5] + m[5],
  ]
}

export function apply(m: Mat, x: number, y: number): { x: number; y: number } {
  return { x: m[0] * x + m[2] * y + m[4], y: m[1] * x + m[3] * y + m[5] }
}

export function invert(m: Mat): Mat {
  const det = m[0] * m[3] - m[1] * m[2] || 1e-12
  return [
    m[3] / det,
    -m[1] / det,
    -m[2] / det,
    m[0] / det,
    (m[2] * m[5] - m[3] * m[4]) / det,
    (m[1] * m[4] - m[0] * m[5]) / det,
  ]
}

const rad = (deg: number) => (deg * Math.PI) / 180

function rotation(deg: number): Mat {
  const c = Math.cos(rad(deg))
  const s = Math.sin(rad(deg))
  return [c, s, -s, c, 0, 0]
}

/**
 * How a layer's content is sized and deformed in its own units: the content
 * box (cw × ch) and the scale / slant / flip applied to it.
 */
export interface Frame {
  /** Content size in the content's own units (text wrap width, image pixels, …). */
  cw: number
  ch: number
  scaleX: number
  scaleY: number
  /** Fabric's skewX in degrees (negative leans the tops to the right). */
  skewX: number
  /** Fabric's skewY in degrees (negative lifts the right end). */
  skewY: number
  flipX: boolean
  flipY: boolean
  /** Counted into the box: half on each side. */
  strokeWidth: number
}

/** The scale · slant · flip part of the transform. */
function dimensions(f: Frame): Mat {
  let m: Mat = [f.flipX ? -f.scaleX : f.scaleX, 0, 0, f.flipY ? -f.scaleY : f.scaleY, 0, 0]
  if (f.skewX) m = multiply(m, [1, 0, Math.tan(rad(f.skewX)), 1, 0, 0])
  if (f.skewY) m = multiply(m, [1, Math.tan(rad(f.skewY)), 0, 1, 0, 0])
  return m
}

/** The size of the layer's box on the board, before rotation. */
export function boxSize(f: Frame): { w: number; h: number } {
  const dx = f.cw + f.strokeWidth
  const dy = f.ch + f.strokeWidth
  if (!f.skewX && !f.skewY) return { w: dx * f.scaleX, h: dy * f.scaleY }
  const m = dimensions(f)
  const pts = [
    apply(m, -dx / 2, -dy / 2),
    apply(m, dx / 2, -dy / 2),
    apply(m, -dx / 2, dy / 2),
    apply(m, dx / 2, dy / 2),
  ]
  const xs = pts.map((p) => p.x)
  const ys = pts.map((p) => p.y)
  return { w: Math.max(...xs) - Math.min(...xs), h: Math.max(...ys) - Math.min(...ys) }
}

/** The centre of the layer's box on the board. */
export function centreOf(l: Pick<Layer, 'x' | 'y' | 'rotation'>, f: Frame): { x: number; y: number } {
  const { w, h } = boxSize(f)
  const r = rotation(l.rotation || 0)
  const off = apply(r, w / 2, h / 2)
  return { x: l.x + off.x, y: l.y + off.y }
}

/**
 * Content units → board: draw the content in [-cw/2, cw/2] × [-ch/2, ch/2]
 * under this matrix.
 */
export function contentMatrix(l: Pick<Layer, 'x' | 'y' | 'rotation'>, f: Frame): Mat {
  const c = centreOf(l, f)
  return multiply(multiply([1, 0, 0, 1, c.x, c.y], rotation(l.rotation || 0)), dimensions(f))
}

/** The four corners of the layer's box on the board: tl, tr, br, bl. */
export function cornersOf(l: Pick<Layer, 'x' | 'y' | 'rotation'>, f: Frame): { x: number; y: number }[] {
  const c = centreOf(l, f)
  const { w, h } = boxSize(f)
  const m = multiply([1, 0, 0, 1, c.x, c.y], rotation(l.rotation || 0))
  return [apply(m, -w / 2, -h / 2), apply(m, w / 2, -h / 2), apply(m, w / 2, h / 2), apply(m, -w / 2, h / 2)]
}

/** The axis-aligned box around a layer on the board. */
export function boundsOf(l: Pick<Layer, 'x' | 'y' | 'rotation'>, f: Frame): { left: number; top: number; width: number; height: number } {
  const pts = cornersOf(l, f)
  const xs = pts.map((p) => p.x)
  const ys = pts.map((p) => p.y)
  const left = Math.min(...xs)
  const top = Math.min(...ys)
  return { left, top, width: Math.max(...xs) - left, height: Math.max(...ys) - top }
}

/**
 * A layer's frame from the document alone. Text needs its measured height
 * (the document keeps it in layer.height); an image its natural or cropped
 * pixel size; a group the box around its members.
 */
export function frameOf(l: Layer): Frame {
  const st = (l.style ?? {}) as Record<string, any>
  if (l.type === 'text') {
    const stretch = Number(st.stretch) > 0 ? Number(st.stretch) : 1
    const sw = st.stroke ? strokeWidthOf(st.stroke, st.strokeWidth) : 1
    return {
      cw: Math.max(1, l.width / stretch),
      ch: Math.max(1, l.height),
      scaleX: stretch,
      scaleY: 1,
      skewX: -(Number(st.skew) || 0),
      skewY: -(Number(st.skewY) || 0),
      flipX: false,
      flipY: false,
      strokeWidth: sw,
    }
  }
  if (l.type === 'image') {
    const crop = l.image?.crop
    const nw = crop?.width || l.image?.originalWidth || l.width || 1
    const nh = crop?.height || l.image?.originalHeight || l.height || 1
    return {
      cw: nw,
      ch: nh,
      scaleX: (l.width || nw) / nw,
      scaleY: (l.height || nh) / nh,
      skewX: 0,
      skewY: 0,
      flipX: st.flipX === true,
      flipY: st.flipY === true,
      strokeWidth: 0,
    }
  }
  if (l.type === 'group') {
    const b = membersBox(l.children ?? [])
    return {
      cw: b.width || 1,
      ch: b.height || 1,
      scaleX: (l.width || b.width || 1) / (b.width || 1),
      scaleY: (l.height || b.height || 1) / (b.height || 1),
      skewX: 0,
      skewY: 0,
      flipX: false,
      flipY: false,
      strokeWidth: 0,
    }
  }
  // Shapes (a line included): sized by width/height directly.
  const sw = st.strokeWidth !== undefined ? Number(st.strokeWidth) || 0 : 1
  return { cw: Math.max(0.5, l.width), ch: Math.max(0.5, l.height), scaleX: 1, scaleY: 1, skewX: 0, skewY: 0, flipX: false, flipY: false, strokeWidth: sw }
}

/** The box around a group's members, in the group's own (unscaled) units. */
export function membersBox(children: Layer[]): { left: number; top: number; width: number; height: number } {
  if (!children.length) return { left: 0, top: 0, width: 0, height: 0 }
  const boxes = children.map((c) => boundsOf(c, frameOf(c)))
  const left = Math.min(...boxes.map((b) => b.left))
  const top = Math.min(...boxes.map((b) => b.top))
  const right = Math.max(...boxes.map((b) => b.left + b.width))
  const bottom = Math.max(...boxes.map((b) => b.top + b.height))
  return { left, top, width: right - left, height: bottom - top }
}

/** "colour:width" or a colour plus a separate width. */
export function strokeWidthOf(stroke: unknown, strokeWidth: unknown): number {
  const w = Number(strokeWidth)
  if (Number.isFinite(w) && w > 0) return w
  const s = String(stroke ?? '')
  const i = s.lastIndexOf(':')
  if (i > 0) {
    const n = Number(s.slice(i + 1))
    if (Number.isFinite(n)) return n
  }
  return 1
}
