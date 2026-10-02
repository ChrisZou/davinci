import type { CanvasKit, Font, Typeface } from 'canvaskit-wasm'
import type { Layer } from '../types'

/**
 * Text layout for a text layer — the same rules Fabric.js applied when these
 * documents were written, so an old cover lays out exactly as it did:
 *
 *  - a line is 1.13 × fontSize tall, times lineHeight for every line but
 *    the last;
 *  - a line's baseline sits 1.13 × fontSize × (1 − 0.222) below its top,
 *    whatever the font's own ascent says;
 *  - letter spacing (charSpacing, in thousandths of an em) follows every
 *    character, and the last one's is not counted in the line's width.
 *
 * Wrapping improves on Fabric's (which only broke at spaces, so a long
 * Chinese line never wrapped and widened its box instead): lines also break
 * between CJK characters, with a little slack so a width saved from Fabric's
 * measurements never pushes the last character of a line onto the next.
 */

export const FONT_SIZE_MULT = 1.13
export const FONT_SIZE_FRACTION = 0.222
export const DEFAULT_LINE_HEIGHT = 1.16
export const DEFAULT_FONT_SIZE = 40
/** Chrome's synthetic italic: glyphs sheared by a quarter of their height. */
export const SYNTHETIC_ITALIC_SKEW = -0.25

/** A loaded face, and what it really is (for faking bold and italic). */
export interface Face {
  typeface: Typeface
  weight: number
  italic: boolean
}

/** Looks faces up; misses are null, never thrown. */
export interface FaceLookup {
  face(family: string, weight: number, italic: boolean): Face | null
  /** Families tried, in order, for characters the layer's font lacks. */
  fallbacks: string[]
}

export interface Glyph {
  /** The character(s) this glyph cluster stands for. */
  text: string
  font: Font
  ids: Uint16Array
  /** Advance including letter spacing. */
  advance: number
  /** Advance without letter spacing (what the background box counts). */
  width: number
}

export interface Line {
  glyphs: Glyph[]
  /** Measured width (letter spacing after the last glyph not counted). */
  width: number
  /** Sum of every glyph's advance (the background box's width). */
  boxWidth: number
}

export interface TextLayout {
  lines: Line[]
  /** Box height, as Fabric computed it. */
  height: number
  /** The widest line. */
  width: number
  fontSize: number
  lineHeight: number
  /** Top of line i, and its baseline, from the top of the box. */
  lineTop(i: number): number
  baseline(i: number): number
}

export function numericWeight(w: unknown): number {
  if (w === 'bold') return 700
  if (w === 'normal' || w === undefined || w === null || w === '') return 400
  const n = Number(w)
  return Number.isFinite(n) && n > 0 ? n : 400
}

const CJK = /[⺀-鿿가-힯豈-﫿＀-￯　-〿]/

function graphemes(s: string): string[] {
  const Seg = (Intl as any).Segmenter
  if (Seg) return Array.from(new Seg(undefined, { granularity: 'grapheme' }).segment(s), (x: any) => x.segment as string)
  return Array.from(s)
}

const faceIDs = new WeakMap<object, number>()
let nextFaceID = 1
function faceID(face: Face): number {
  let id = faceIDs.get(face.typeface)
  if (!id) {
    id = nextFaceID++
    faceIDs.set(face.typeface, id)
  }
  return id
}

/** Fonts are cached per (typeface, size, synthetic flags). */
export class FontCache {
  private cache = new Map<string, Font>()
  constructor(private ck: CanvasKit) {}
  get(face: Face, size: number, wantWeight: number, wantItalic: boolean): Font {
    // Chrome fakes what the face lacks: bold from 600 up, a slant for italic.
    const bold = wantWeight >= 600 && face.weight < 600
    const slant = wantItalic && !face.italic
    const k = `${faceID(face)}|${size}|${bold}|${slant}`
    let f = this.cache.get(k)
    if (!f) {
      f = new this.ck.Font(face.typeface, size)
      f.setSubpixel(true)
      f.setLinearMetrics(true)
      if (bold) f.setEmbolden(true)
      if (slant) f.setSkewX(SYNTHETIC_ITALIC_SKEW)
      this.cache.set(k, f)
    }
    return f
  }
  dispose() {
    for (const f of this.cache.values()) f.delete()
    this.cache.clear()
  }
}

/** The families a text layer needs loaded before it can be laid out. */
export function textFaces(l: Layer, fallbacks: string[]): { family: string; weight: number; italic: boolean }[] {
  const st = (l.style ?? {}) as Record<string, any>
  const weight = numericWeight(st.fontWeight)
  const italic = st.fontStyle === 'italic'
  const out = [{ family: String(st.fontFamily || fallbacks[0] || ''), weight, italic }]
  for (const f of fallbacks) out.push({ family: f, weight, italic })
  return out
}

/** Lays out a text layer. `wrapWidth` is the box width in the text's own units. */
export function layoutText(l: Layer, wrapWidth: number, faces: FaceLookup, fonts: FontCache): TextLayout {
  const st = (l.style ?? {}) as Record<string, any>
  const fs = Number(st.fontSize) > 0 ? Number(st.fontSize) : DEFAULT_FONT_SIZE
  const lh = Number(st.lineHeight) > 0 ? Number(st.lineHeight) : DEFAULT_LINE_HEIGHT
  const spacing = (Number(st.charSpacing) || 0) * fs / 1000
  const weight = numericWeight(st.fontWeight)
  const italic = st.fontStyle === 'italic'
  const family = String(st.fontFamily || faces.fallbacks[0] || '')
  const chain: Face[] = []
  for (const fam of [family, ...faces.fallbacks]) {
    const f = faces.face(fam, weight, italic)
    if (f && !chain.includes(f)) chain.push(f)
  }

  const shape = (g: string): Glyph => {
    for (let i = 0; i < chain.length; i++) {
      const font = fonts.get(chain[i], fs, weight, italic)
      const ids = font.getGlyphIDs(g)
      const missing = ids.length === 0 || Array.from(ids).some((id) => id === 0)
      if (missing && i < chain.length - 1 && g.trim()) continue
      const widths = font.getGlyphWidths(ids)
      const w = widths.reduce((a, b) => a + b, 0)
      return { text: g, font, ids, width: w, advance: w + spacing }
    }
    return { text: g, font: undefined as unknown as Font, ids: new Uint16Array(), width: 0, advance: spacing }
  }

  const limit = wrapWidth * 1.02 + 1
  const lines: Line[] = []
  const finish = (glyphs: Glyph[]) => {
    const boxWidth = glyphs.reduce((a, g) => a + g.advance, 0)
    lines.push({ glyphs, width: Math.max(0, boxWidth - (glyphs.length ? spacing : 0)), boxWidth })
  }
  for (const para of String(l.text ?? '').split('\n')) {
    const gs = graphemes(para).map(shape)
    if (!gs.length) {
      finish([])
      continue
    }
    // Break opportunities: after a space, and before or after a CJK character.
    const tokens: Glyph[][] = []
    let cur: Glyph[] = []
    for (let i = 0; i < gs.length; i++) {
      const g = gs[i]
      const cjk = CJK.test(g.text)
      if (cjk && cur.length) {
        tokens.push(cur)
        cur = []
      }
      cur.push(g)
      if (cjk || /\s/.test(g.text)) {
        tokens.push(cur)
        cur = []
      }
    }
    if (cur.length) tokens.push(cur)
    let line: Glyph[] = []
    let width = 0
    for (const t of tokens) {
      const tw = t.reduce((a, g) => a + g.advance, 0)
      const trailing = t.length && /\s/.test(t[t.length - 1].text) ? t[t.length - 1].advance : 0
      if (line.length && width + tw - trailing - spacing > limit) {
        while (line.length && /\s/.test(line[line.length - 1].text)) line.pop()
        finish(line)
        line = []
        width = 0
        if (/^\s+$/.test(t.map((g) => g.text).join(''))) continue
      }
      line.push(...t)
      width += tw
    }
    finish(line)
  }

  const lineBox = fs * FONT_SIZE_MULT
  const height = lines.length ? (lines.length - 1) * lineBox * lh + lineBox : lineBox
  return {
    lines,
    height,
    width: Math.max(0, ...lines.map((ln) => ln.width)),
    fontSize: fs,
    lineHeight: lh,
    lineTop: (i) => i * lineBox * lh,
    baseline: (i) => i * lineBox * lh + lineBox * (1 - FONT_SIZE_FRACTION),
  }
}
