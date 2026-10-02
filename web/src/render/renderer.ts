import type { Canvas, CanvasKit, Image, ImageFilter, Paint } from 'canvaskit-wasm'
import type { Board, Layer } from '../types'
import { contentMatrix, frameOf, membersBox, type Frame, type Mat } from './geometry'
import { FontCache, layoutText, numericWeight, textFaces, type Face, type FaceLookup, type TextLayout } from './text'

/**
 * The davinci renderer: turns a board of the document into pixels with
 * CanvasKit (Skia). The editor draws its canvas with it, and the server draws
 * exports and measures text with the very same code in Node, so what is on
 * screen is what gets exported.
 *
 * Drawing is synchronous; `prepare()` first loads whatever fonts and images a
 * board needs. That split is what lets the editor redraw on every pointer
 * move without waiting on anything.
 */

export interface Loader {
  /** The best face for a family; null when there is none. */
  font(family: string, weight: number, italic: boolean): Promise<{ bytes: ArrayBuffer; weight: number; italic: boolean } | null>
  image(url: string): Promise<ArrayBuffer | null>
}

export interface DrawOptions {
  /** Leave the board's background colour and image out. */
  transparent?: boolean
  /** Draw only these layers (ids). */
  only?: Set<string>
  /** Draw nothing for these layers (the editor hides a text box it is editing). */
  hide?: Set<string>
}

/** Families tried for characters a layer's own font lacks. */
export const DEFAULT_FALLBACKS = ['PingFang SC', 'Hiragino Sans GB', 'Noto Sans CJK SC', 'Noto Sans SC', 'Arial Unicode MS']

const PREVIEW_STRIPS = 240

export class Renderer implements FaceLookup {
  readonly fonts: FontCache
  fallbacks: string[]
  private faces = new Map<string, Face | null>()
  private facePending = new Map<string, Promise<void>>()
  private images = new Map<string, Image | null>()
  private imagePending = new Map<string, Promise<void>>()
  private layouts = new Map<string, { key: string; layout: TextLayout }>()
  private fallbackDone = new Set<string>()
  private flats = new Map<string, { key: string; flat: FlatText }>()

  constructor(
    readonly ck: CanvasKit,
    private loader: Loader,
    fallbacks: string[] = DEFAULT_FALLBACKS,
  ) {
    this.fonts = new FontCache(ck)
    this.fallbacks = fallbacks
  }

  // --- resources ------------------------------------------------------------

  face(family: string, weight: number, italic: boolean): Face | null {
    return this.faces.get(faceKey(family, weight, italic)) ?? null
  }

  image(url: string): Image | null {
    return this.images.get(url) ?? null
  }

  /** Loads every font and image the layers (and the board background) need. */
  async prepare(board: Pick<Board, 'canvas' | 'layers'>): Promise<void> {
    const jobs: Promise<void>[] = []
    const texts: Layer[] = []
    const want = (layers: Layer[]) => {
      for (const l of layers) {
        if (l.type === 'text') {
          // The layer's own font first; fallbacks only for what it lacks.
          const [own] = textFaces(l, this.fallbacks)
          jobs.push(this.loadFace(own.family, own.weight, own.italic))
          texts.push(l)
        } else if (l.type === 'image' && l.image?.url) {
          jobs.push(this.loadImage(l.image.url))
        } else if (l.type === 'group') {
          want(l.children ?? [])
        }
      }
    }
    want(board.layers)
    if (board.canvas.backgroundImage) jobs.push(this.loadImage(board.canvas.backgroundImage))
    await Promise.all(jobs)
    // A CJK face is 10 MB or more: load a fallback only when some character
    // actually needs one, and stop at the first that covers everything.
    for (const l of texts) {
      const [own, ...rest] = textFaces(l, this.fallbacks)
      let missing = this.missingChars(String(l.text ?? ''), this.face(own.family, own.weight, own.italic))
      for (const f of rest) {
        if (!missing) break
        await this.loadFace(f.family, f.weight, f.italic)
        const face = this.face(f.family, f.weight, f.italic)
        if (face) missing = this.missingChars(missing, face)
      }
      this.fallbackDone.add(fallbackKey(l))
    }
  }

  /** The characters of `text` a face has no glyph for ('' when it covers all). */
  private missingChars(text: string, face: Face | null): string {
    const chars = Array.from(new Set(Array.from(text.replace(/\s/g, ''))))
    if (!face) return chars.join('')
    const font = this.fonts.get(face, 16, face.weight, face.italic)
    const ids = font.getGlyphIDs(chars.join(''))
    return chars.filter((_, i) => ids[i] === 0).join('')
  }

  /** True when prepare() would load nothing new. */
  isReady(board: Pick<Board, 'canvas' | 'layers'>): boolean {
    const ok = (layers: Layer[]): boolean =>
      layers.every((l) => {
        if (l.type === 'text') {
          const [own] = textFaces(l, this.fallbacks)
          return this.faces.has(faceKey(own.family, own.weight, own.italic)) && this.fallbackDone.has(fallbackKey(l))
        }
        if (l.type === 'image' && l.image?.url) return this.images.has(l.image.url)
        if (l.type === 'group') return ok(l.children ?? [])
        return true
      })
    return ok(board.layers) && (!board.canvas.backgroundImage || this.images.has(board.canvas.backgroundImage))
  }

  private loadFace(family: string, weight: number, italic: boolean): Promise<void> {
    const key = faceKey(family, weight, italic)
    if (this.faces.has(key)) return Promise.resolve()
    let p = this.facePending.get(key)
    if (!p) {
      p = (async () => {
        let face: Face | null = null
        if (family) {
          try {
            const got = await this.loader.font(family, weight, italic)
            if (got) {
              const tf = this.ck.Typeface.MakeTypefaceFromData(got.bytes)
              if (tf) face = { typeface: tf, weight: got.weight, italic: got.italic }
            }
          } catch {
            face = null
          }
        }
        this.faces.set(key, face)
        this.facePending.delete(key)
        this.layouts.clear()
      })()
      this.facePending.set(key, p)
    }
    return p
  }

  private loadImage(url: string): Promise<void> {
    if (this.images.has(url)) return Promise.resolve()
    let p = this.imagePending.get(url)
    if (!p) {
      p = (async () => {
        let img: Image | null = null
        try {
          const bytes = await this.loader.image(url)
          if (bytes) img = this.ck.MakeImageFromEncoded(new Uint8Array(bytes))
        } catch {
          img = null
        }
        this.images.set(url, img)
        this.imagePending.delete(url)
      })()
      this.imagePending.set(url, p)
    }
    return p
  }

  // --- text ------------------------------------------------------------------

  /** Lays out a text layer (cached until its text, style or width change). */
  layout(l: Layer): TextLayout {
    const st = (l.style ?? {}) as Record<string, any>
    const stretch = Number(st.stretch) > 0 ? Number(st.stretch) : 1
    const wrap = Math.max(1, l.width / stretch)
    const key = JSON.stringify([l.text, st.fontFamily, st.fontSize, st.fontWeight, st.fontStyle, st.lineHeight, st.charSpacing, wrap])
    const hit = this.layouts.get(l.id)
    if (hit && hit.key === key) return hit.layout
    const layout = layoutText(l, wrap, this, this.fonts)
    this.layouts.set(l.id, { key, layout })
    return layout
  }

  /** The box height a text layer has once laid out (fonts must be prepared). */
  measure(l: Layer): { height: number; width: number; lines: number } {
    const t = this.layout(l)
    return { height: t.height, width: t.width, lines: t.lines.length }
  }

  /** A layer's frame, with a text layer's height taken from its layout. */
  frame(l: Layer): Frame {
    const f = frameOf(l)
    if (l.type === 'text') f.ch = this.layout(l).height
    return f
  }

  // --- drawing ----------------------------------------------------------------

  /** Draws a board at 1 unit = 1 board pixel into the canvas's current transform. */
  draw(canvas: Canvas, board: Pick<Board, 'canvas' | 'layers'>, opts: DrawOptions = {}) {
    const { width, height } = board.canvas
    if (!opts.transparent) this.drawBackground(canvas, board.canvas, width, height)
    for (const l of board.layers) {
      if (opts.only && !opts.only.has(l.id)) continue
      if (opts.hide?.has(l.id)) continue
      this.drawLayer(canvas, l)
    }
  }

  drawBackground(canvas: Canvas, c: Board['canvas'], width: number, height: number) {
    const ck = this.ck
    const bg = parseColour(ck, c.background)
    if (bg) {
      const p = new ck.Paint()
      p.setColor(bg)
      canvas.drawRect(ck.XYWHRect(0, 0, width, height), p)
      p.delete()
    }
    const img = c.backgroundImage ? this.image(c.backgroundImage) : null
    if (img) {
      // Scaled to cover, anchored at the top-left corner.
      const s = Math.max(width / img.width(), height / img.height())
      canvas.save()
      canvas.clipRect(ck.XYWHRect(0, 0, width, height), ck.ClipOp.Intersect, true)
      const p = new ck.Paint()
      canvas.drawImageRectOptions(img, ck.XYWHRect(0, 0, img.width(), img.height()), ck.XYWHRect(0, 0, img.width() * s, img.height() * s), ck.FilterMode.Linear, ck.MipmapMode.Linear, p)
      p.delete()
      canvas.restore()
    }
  }

  drawLayer(canvas: Canvas, l: Layer) {
    if (l.visible === false) return
    const ck = this.ck
    const f = this.frame(l)
    const opacity = Math.max(0, Math.min(1, l.opacity ?? 1))
    if (opacity <= 0) return
    const depth = canvas.save()
    if (opacity < 1) {
      const p = new ck.Paint()
      p.setAlphaf(opacity)
      canvas.saveLayer(p)
      p.delete()
    }
    // A picture's shadow follows whatever it draws (a cut-out casts its own
    // outline), in board pixels: laid on before the layer's own transform, so
    // it does not turn or stretch with the picture.
    if (l.type === 'image') {
      const st = (l.style ?? {}) as Record<string, unknown>
      const sh = parseShadow(ck, st.shadow)
      if (sh) {
        const p = new ck.Paint()
        const filter = ck.ImageFilter.MakeDropShadow(sh.dx, sh.dy, sh.sigma, sh.sigma, sh.colour, null)
        p.setImageFilter(filter)
        canvas.saveLayer(p)
        p.delete()
        filter.delete()
      }
      // An outline along whatever the picture draws — a cut-out portrait gets
      // a sticker's border, a photo a frame: its opaque area grown by the
      // stroke width, painted in the stroke colour, under the picture. Inside
      // the shadow's layer, so the shadow falls from the outline too.
      const outline = imageOutline(ck, st)
      if (outline) {
        const p = new ck.Paint()
        p.setImageFilter(outline)
        canvas.saveLayer(p)
        p.delete()
        outline.delete()
      }
    }
    canvas.concat(toSkMatrix(contentMatrix(l, f)))
    if (l.type === 'text') this.drawText(canvas, l, f)
    else if (l.type === 'image') this.drawImage(canvas, l, f)
    else if (l.type === 'group') this.drawGroup(canvas, l, f)
    else this.drawShape(canvas, l, f)
    canvas.restoreToCount(depth)
  }

  private drawGroup(canvas: Canvas, l: Layer, f: Frame) {
    const b = membersBox(l.children ?? [])
    canvas.save()
    canvas.translate(-f.cw / 2 - b.left, -f.ch / 2 - b.top)
    for (const c of l.children ?? []) this.drawLayer(canvas, c)
    canvas.restore()
  }

  private drawImage(canvas: Canvas, l: Layer, f: Frame) {
    const ck = this.ck
    const st = (l.style ?? {}) as Record<string, any>
    const dest = ck.XYWHRect(-f.cw / 2, -f.ch / 2, f.cw, f.ch)
    const img = l.image?.url ? this.image(l.image.url) : null
    if (!img) {
      // A lost picture: an unmistakable stand-in, like the one Fabric drew.
      const p = new ck.Paint()
      p.setColor(ck.parseColorString('#e8e8e8'))
      canvas.drawRect(dest, p)
      p.setStyle(ck.PaintStyle.Stroke)
      p.setColor(ck.parseColorString('#e5322d'))
      p.setStrokeWidth(3 / Math.max(f.scaleX, 1e-6))
      p.setPathEffect(ck.PathEffect.MakeDash([14 / f.scaleX, 9 / f.scaleX]))
      canvas.drawRect(dest, p)
      p.delete()
      return
    }
    const crop = l.image?.crop
    const src = crop ? ck.XYWHRect(crop.x, crop.y, crop.width, crop.height) : ck.XYWHRect(0, 0, img.width(), img.height())
    const radius = Number(st.cornerRadius) || 0
    canvas.save()
    if (radius > 0) {
      // Corners are measured on the board, not in the picture's pixels.
      const rx = radius / Math.max(Math.abs(f.scaleX), 1e-6)
      const ry = radius / Math.max(Math.abs(f.scaleY), 1e-6)
      canvas.clipRRect(ck.RRectXY(dest, rx, ry), ck.ClipOp.Intersect, true)
    }
    const p = new ck.Paint()
    const filters = (st.filters ?? {}) as Record<string, number>
    const cf = colourFilter(ck, filters)
    if (cf) p.setColorFilter(cf)
    const blur = Number(filters.blur) || 0
    if (blur > 0) {
      // Fabric's blur scaled with the picture: a fraction of its larger side.
      const sigma = blur * Math.max(f.cw, f.ch) * 0.03
      p.setImageFilter(ck.ImageFilter.MakeBlur(sigma, sigma, ck.TileMode.Clamp, null))
    }
    canvas.drawImageRectOptions(img, src, dest, ck.FilterMode.Linear, ck.MipmapMode.Linear, p)
    p.delete()
    canvas.restore()
  }

  private drawShape(canvas: Canvas, l: Layer, f: Frame) {
    const ck = this.ck
    const st = (l.style ?? {}) as Record<string, any>
    const kind = l.shape?.kind ?? 'rect'
    const w = f.cw
    const h = f.ch
    if (kind === 'line') return this.drawLine(canvas, l, w)
    const b = new ck.PathBuilder()
    if (kind === 'ellipse') b.addOval(ck.XYWHRect(-w / 2, -h / 2, w, h))
    else if (kind === 'triangle') {
      b.moveTo(-w / 2, h / 2)
      b.lineTo(0, -h / 2)
      b.lineTo(w / 2, h / 2)
      b.close()
    } else {
      const r = Number(st.cornerRadius) || 0
      if (r > 0) b.addRRect(ck.RRectXY(ck.XYWHRect(-w / 2, -h / 2, w, h), r, r))
      else b.addRect(ck.XYWHRect(-w / 2, -h / 2, w, h))
    }
    const path = b.detach()
    b.delete()
    const fill = parseColour(ck, st.fill)
    const p = new ck.Paint()
    p.setAntiAlias(true)
    if (fill) {
      p.setColor(fill)
      canvas.drawPath(path, p)
    }
    const stroke = parseColour(ck, st.stroke)
    const sw = Number(st.strokeWidth) || 0
    if (stroke && sw > 0) {
      p.setStyle(ck.PaintStyle.Stroke)
      p.setStrokeWidth(sw)
      p.setColor(stroke)
      canvas.drawPath(path, p)
    }
    p.delete()
    path.delete()
  }

  /** A straight line along the box's middle, maybe dashed, maybe with arrowheads. */
  private drawLine(canvas: Canvas, l: Layer, length: number) {
    const ck = this.ck
    const st = (l.style ?? {}) as Record<string, any>
    const w = Math.max(0.5, Number(st.strokeWidth) || 1)
    const colour = parseColour(ck, st.stroke) ?? ck.parseColorString('#1c1b18')
    const head = Math.max(w * 3.2, 12)
    const half = head * 0.62
    const dashed = st.lineStyle === 'dashed'
    const arrow = st.arrow === 'end' || st.arrow === 'both' ? st.arrow : 'none'
    let x0 = -length / 2
    let x1 = length / 2
    if (arrow === 'both') x0 += head * 0.85
    if (arrow !== 'none') x1 -= head * 0.85
    const p = new ck.Paint()
    p.setAntiAlias(true)
    p.setColor(colour)
    p.setStyle(ck.PaintStyle.Stroke)
    p.setStrokeWidth(w)
    p.setStrokeCap(dashed ? ck.StrokeCap.Butt : ck.StrokeCap.Round)
    if (dashed) p.setPathEffect(ck.PathEffect.MakeDash([w * 3, w * 2]))
    canvas.drawLine(x0, 0, Math.max(x0, x1), 0, p)
    p.setPathEffect(null)
    p.setStyle(ck.PaintStyle.Fill)
    const tip = (x: number, dir: number) => {
      const b = new ck.PathBuilder()
      b.moveTo(x, 0)
      b.lineTo(x - dir * head, -half)
      b.lineTo(x - dir * head, half)
      b.close()
      const path = b.detach()
      b.delete()
      canvas.drawPath(path, p)
      path.delete()
    }
    if (arrow !== 'none') tip(length / 2, 1)
    if (arrow === 'both') tip(-length / 2, -1)
    p.delete()
  }

  private drawText(canvas: Canvas, l: Layer, f: Frame) {
    const st = (l.style ?? {}) as Record<string, any>
    const layout = this.layout(l)
    const warp = st.warp && Number(st.warpAmount || 0) !== 0 ? String(st.warp) : ''
    if (!warp) {
      canvas.save()
      canvas.translate(-f.cw / 2, -f.ch / 2)
      this.paintText(canvas, l, layout, f.cw)
      canvas.restore()
      return
    }
    // A warp is not an affine transform. The text is drawn flat once, into
    // an image at the resolution it will show at, and that image is laid
    // down in thin vertical strips, each squashed for where it sits.
    const m = canvas.getTotalMatrix()
    const scale = Math.max(0.05, Math.min(8, Math.hypot(m[0], m[3]) || 1))
    const flat = this.flatText(l, layout, f, scale)
    if (flat) drawWarped(this.ck, canvas, flat, st, layout, f)
  }

  /** The text drawn flat into an image (cached per look and resolution). */
  private flatText(l: Layer, t: TextLayout, f: Frame, scale: number): FlatText | null {
    const ck = this.ck
    const st = (l.style ?? {}) as Record<string, any>
    const q = Math.round(scale * 8) / 8
    const key = JSON.stringify([l.text, st, l.width, f.ch, q])
    const hit = this.flats.get(l.id)
    if (hit && hit.key === key) return hit.flat
    const [, sw] = splitStroke(typeof st.stroke === 'string' ? st.stroke : '', st.strokeWidth)
    const pad = t.fontSize * 0.6 + sw * 2 + 8
    const region = { left: -f.cw / 2 - pad, top: -f.ch / 2 - pad, width: f.cw + pad * 2, height: f.ch + pad * 2 }
    const k = Math.min(q, 4096 / Math.max(region.width, region.height))
    const surface = ck.MakeSurface(Math.max(1, Math.ceil(region.width * k)), Math.max(1, Math.ceil(region.height * k)))
    if (!surface) return null
    const c = surface.getCanvas()
    c.clear(ck.TRANSPARENT)
    c.scale(k, k)
    c.translate(-region.left - f.cw / 2, -region.top - f.ch / 2)
    this.paintText(c, l, t, f.cw)
    const image = surface.makeImageSnapshot()
    surface.delete()
    hit?.flat.image.delete()
    const flat = { image, region, k }
    this.flats.set(l.id, { key, flat })
    return flat
  }

  /** Draws the text with its box's top-left corner at the origin. */
  private paintText(canvas: Canvas, l: Layer, t: TextLayout, boxWidth: number) {
    const ck = this.ck
    const st = (l.style ?? {}) as Record<string, any>
    const align = st.textAlign === 'center' || st.textAlign === 'right' ? st.textAlign : 'left'
    const leftOf = (w: number) => (align === 'center' ? (boxWidth - w) / 2 : align === 'right' ? boxWidth - w : 0)
    const fill = parseColour(ck, st.fill ?? '#000000')

    // Line backgrounds first, under everything.
    const bg = parseColour(ck, st.textBackgroundColor)
    if (bg) {
      const p = new ck.Paint()
      p.setColor(bg)
      t.lines.forEach((ln, i) => {
        if (!ln.glyphs.length) return
        canvas.drawRect(ck.XYWHRect(leftOf(ln.width), t.lineTop(i), ln.boxWidth, t.fontSize * 1.13), p)
      })
      p.delete()
    }

    const strokeStr = typeof st.stroke === 'string' ? st.stroke : ''
    const [strokeCol, strokeW] = splitStroke(strokeStr, st.strokeWidth)
    const stroke = strokeCol ? parseColour(ck, strokeCol) : null
    const shadow = parseShadow(ck, st.shadow)

    const run = (paint: Paint) => {
      t.lines.forEach((ln, i) => {
        let x = leftOf(ln.width)
        const y = t.baseline(i)
        let k = 0
        while (k < ln.glyphs.length) {
          const font = ln.glyphs[k].font
          const ids: number[] = []
          const pos: number[] = []
          while (k < ln.glyphs.length && ln.glyphs[k].font === font) {
            const g = ln.glyphs[k]
            let gx = x
            const widths = font && g.ids.length ? font.getGlyphWidths(g.ids) : []
            g.ids.forEach((id, j) => {
              ids.push(id)
              pos.push(gx, 0)
              gx += widths[j] ?? 0
            })
            x += g.advance
            k++
          }
          if (font && ids.length) canvas.drawGlyphs(ids, pos, 0, y, font, paint)
        }
      })
    }

    const fillPaint = new ck.Paint()
    fillPaint.setAntiAlias(true)
    if (fill) fillPaint.setColor(fill)
    if (shadow) fillPaint.setImageFilter(ck.ImageFilter.MakeDropShadow(shadow.dx, shadow.dy, shadow.sigma, shadow.sigma, shadow.colour, null))
    const strokePaint = stroke && strokeW > 0 ? new ck.Paint() : null
    if (strokePaint && stroke) {
      strokePaint.setAntiAlias(true)
      strokePaint.setStyle(ck.PaintStyle.Stroke)
      strokePaint.setStrokeWidth(strokeW)
      strokePaint.setStrokeJoin(ck.StrokeJoin.Round)
      strokePaint.setColor(stroke)
    }
    if (strokePaint && st.paintFirst) run(strokePaint)
    if (fill) run(fillPaint)
    if (strokePaint && !st.paintFirst) run(strokePaint)

    // Underline and strike-through, where Fabric put them.
    if (fill && (st.underline || st.linethrough)) {
      const p = new ck.Paint()
      p.setColor(fill)
      const thick = t.fontSize / 15
      t.lines.forEach((ln, i) => {
        if (!ln.glyphs.length) return
        const x = leftOf(ln.width)
        const base = t.baseline(i)
        if (st.underline) canvas.drawRect(ck.XYWHRect(x, base + 0.1 * t.fontSize, ln.boxWidth, thick), p)
        if (st.linethrough) canvas.drawRect(ck.XYWHRect(x, base - 0.28167 * t.fontSize - thick / 2, ln.boxWidth, thick), p)
      })
      p.delete()
    }
    fillPaint.delete()
    strokePaint?.delete()
  }

  // --- whole images ------------------------------------------------------------

  /**
   * Renders a board to an encoded image. `ids` crops the output to the box
   * around those layers and draws only them.
   */
  encode(
    board: Pick<Board, 'canvas' | 'layers'>,
    opts: { scale?: number; format?: 'png' | 'jpeg' | 'webp'; quality?: number; transparent?: boolean; ids?: string[] } = {},
  ): Uint8Array {
    const ck = this.ck
    const scale = Math.max(0.01, opts.scale ?? 1)
    let area = { left: 0, top: 0, width: board.canvas.width, height: board.canvas.height }
    let only: Set<string> | undefined
    if (opts.ids?.length) {
      only = new Set(opts.ids)
      const picked = board.layers.filter((l) => only!.has(l.id))
      if (picked.length) {
        const boxes = picked.map((l) => this.bounds(l))
        const left = Math.floor(Math.min(...boxes.map((b) => b.left)))
        const top = Math.floor(Math.min(...boxes.map((b) => b.top)))
        const right = Math.ceil(Math.max(...boxes.map((b) => b.left + b.width)))
        const bottom = Math.ceil(Math.max(...boxes.map((b) => b.top + b.height)))
        area = { left, top, width: Math.max(1, right - left), height: Math.max(1, bottom - top) }
      }
    }
    const w = Math.max(1, Math.round(area.width * scale))
    const h = Math.max(1, Math.round(area.height * scale))
    const surface = ck.MakeSurface(w, h)
    if (!surface) throw new Error(`could not make a ${w}×${h} surface`)
    const canvas = surface.getCanvas()
    const format = opts.format ?? 'png'
    canvas.clear(format === 'jpeg' && (opts.transparent || only) ? ck.WHITE : ck.TRANSPARENT)
    canvas.scale(scale, scale)
    canvas.translate(-area.left, -area.top)
    this.draw(canvas, board, { transparent: opts.transparent || !!only, only })
    const img = surface.makeImageSnapshot()
    const fmt = format === 'jpeg' ? ck.ImageFormat.JPEG : format === 'webp' ? ck.ImageFormat.WEBP : ck.ImageFormat.PNG
    const bytes = img.encodeToBytes(fmt, Math.round((opts.quality ?? 0.92) * 100))
    img.delete()
    surface.delete()
    if (!bytes) throw new Error('could not encode the image')
    return bytes
  }

  /** A layer's axis-aligned box on its board. */
  bounds(l: Layer): { left: number; top: number; width: number; height: number } {
    const m = contentMatrix(l, this.frame(l))
    const f = this.frame(l)
    const pts = [
      [-f.cw / 2 - f.strokeWidth / 2, -f.ch / 2 - f.strokeWidth / 2],
      [f.cw / 2 + f.strokeWidth / 2, -f.ch / 2 - f.strokeWidth / 2],
      [f.cw / 2 + f.strokeWidth / 2, f.ch / 2 + f.strokeWidth / 2],
      [-f.cw / 2 - f.strokeWidth / 2, f.ch / 2 + f.strokeWidth / 2],
    ].map(([x, y]) => ({ x: m[0] * x + m[2] * y + m[4], y: m[1] * x + m[3] * y + m[5] }))
    const xs = pts.map((p) => p.x)
    const ys = pts.map((p) => p.y)
    return { left: Math.min(...xs), top: Math.min(...ys), width: Math.max(...xs) - Math.min(...xs), height: Math.max(...ys) - Math.min(...ys) }
  }

  dispose() {
    this.fonts.dispose()
    for (const f of this.faces.values()) f?.typeface.delete()
    for (const i of this.images.values()) i?.delete()
    for (const f of this.flats.values()) f.flat.image.delete()
    this.flats.clear()
    this.faces.clear()
    this.images.clear()
  }
}

// --- helpers --------------------------------------------------------------------

/** Warped text, drawn flat: the image and the content region it covers. */
interface FlatText {
  image: Image
  region: { left: number; top: number; width: number; height: number }
  /** Image pixels per content unit. */
  k: number
}

/** A text's fallbacks depend on its characters and its face. */
function fallbackKey(l: Layer): string {
  const st = (l.style ?? {}) as Record<string, any>
  return JSON.stringify([l.text, st.fontFamily, st.fontWeight, st.fontStyle])
}

function faceKey(family: string, weight: number, italic: boolean): string {
  return `${family}|${numericWeight(weight)}|${italic ? 1 : 0}`
}

export function toSkMatrix(m: Mat): number[] {
  return [m[0], m[2], m[4], m[1], m[3], m[5], 0, 0, 1]
}

/** A CSS colour as a CanvasKit colour; null for none / transparent. */
export function parseColour(ck: CanvasKit, v: unknown): Float32Array | null {
  const s = String(v ?? '').trim()
  if (!s || s === 'transparent' || s === 'none') return null
  try {
    return ck.parseColorString(s)
  } catch {
    return null
  }
}

/** "colour:width" (an rgba() colour has no colons of its own) plus an optional separate width. */
function splitStroke(stroke: string, width: unknown): [string, number] {
  const w = Number(width)
  const i = stroke.lastIndexOf(':')
  let colour = stroke
  let n = NaN
  if (i > 0 && i > stroke.lastIndexOf(')')) {
    colour = stroke.slice(0, i)
    n = Number(stroke.slice(i + 1))
  }
  const sw = Number.isFinite(w) && w > 0 ? w : Number.isFinite(n) ? n : colour ? 1 : 0
  return [colour, sw]
}

/** A picture's outline filter (grown alpha in the stroke colour, then the picture), or null. */
function imageOutline(ck: CanvasKit, st: Record<string, unknown>): ImageFilter | null {
  const colour = parseColour(ck, splitColour(st.stroke))
  const w = Number(st.strokeWidth ?? 12)
  if (!colour || !(w > 0)) return null
  // A cut-out's edge is a haze of half-transparent pixels; grown as they are,
  // they make a blurry halo. So: harden the alpha, grow it, soften it a hair
  // (rounding the dilation's square corners), and harden it again, which
  // leaves a crisp but anti-aliased edge.
  const hard = (k: number, at: number) => ck.ColorFilter.MakeMatrix([1, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, k, -k * at])
  const h1 = hard(8, 0.4)
  const solid = ck.ImageFilter.MakeColorFilter(h1, null)
  const grown = ck.ImageFilter.MakeDilate(w, w, solid)
  const sigma = Math.max(0.6, w * 0.15)
  const soft = ck.ImageFilter.MakeBlur(sigma, sigma, ck.TileMode.Decal, grown)
  const h2 = hard(4, 0.35)
  const edged = ck.ImageFilter.MakeColorFilter(h2, soft)
  const tint = ck.ColorFilter.MakeBlend(colour, ck.BlendMode.SrcIn)
  const coloured = ck.ImageFilter.MakeColorFilter(tint, edged)
  const out = ck.ImageFilter.MakeBlend(ck.BlendMode.SrcOver, coloured, null)
  for (const x of [h1, solid, grown, soft, h2, edged, tint, coloured]) x.delete()
  return out
}

/** The colour part of a stroke that may be written "colour:width". */
function splitColour(v: unknown): string {
  const s = String(v ?? '').trim()
  const i = s.lastIndexOf(':')
  return i > s.lastIndexOf(')') && i > 0 ? s.slice(0, i) : s
}

/** "colour:blur:offsetX:offsetY" (the colour may itself contain colons in rgba()). */
function parseShadow(ck: CanvasKit, v: unknown): { colour: Float32Array; sigma: number; dx: number; dy: number } | null {
  const s = String(v ?? '').trim()
  if (!s) return null
  const parts = s.split(':')
  if (parts.length < 4) return null
  const dy = Number(parts.pop())
  const dx = Number(parts.pop())
  const blur = Number(parts.pop())
  const colour = parseColour(ck, parts.join(':'))
  if (!colour) return null
  // Canvas's shadowBlur is twice the Gaussian sigma.
  return { colour, sigma: Math.max(0, blur) / 2, dx: dx || 0, dy: dy || 0 }
}

/** Fabric's image filters as one colour matrix (blur is separate). */
function colourFilter(ck: CanvasKit, f: Record<string, number>) {
  let m = identity4x5()
  const b = Number(f.brightness) || 0
  if (b) m = mul(m, [1, 0, 0, 0, b, 0, 1, 0, 0, b, 0, 0, 1, 0, b, 0, 0, 0, 1, 0])
  const c = Number(f.contrast) || 0
  if (c) {
    const k = (259 * (c * 255 + 255)) / (255 * (259 - c * 255))
    const o = (128 * (1 - k)) / 255
    m = mul(m, [k, 0, 0, 0, o, 0, k, 0, 0, o, 0, 0, k, 0, o, 0, 0, 0, 1, 0])
  }
  const s = Number(f.saturation) || 0
  if (s) {
    const x = 1 + s
    const lr = 0.2126 * (1 - x)
    const lg = 0.7152 * (1 - x)
    const lb = 0.0722 * (1 - x)
    m = mul(m, [lr + x, lg, lb, 0, 0, lr, lg + x, lb, 0, 0, lr, lg, lb + x, 0, 0, 0, 0, 0, 1, 0])
  }
  if (Number(f.grayscale)) m = mul(m, [0.299, 0.587, 0.114, 0, 0, 0.299, 0.587, 0.114, 0, 0, 0.299, 0.587, 0.114, 0, 0, 0, 0, 0, 1, 0])
  if (Number(f.sepia)) m = mul(m, [0.393, 0.769, 0.189, 0, 0, 0.349, 0.686, 0.168, 0, 0, 0.272, 0.534, 0.131, 0, 0, 0, 0, 0, 1, 0])
  if (m.every((v, i) => v === identity4x5()[i])) return null
  return ck.ColorFilter.MakeMatrix(m)
}

function identity4x5(): number[] {
  return [1, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 1, 0, 0, 0, 0, 0, 1, 0]
}

/** Applies n then m (both 4×5 colour matrices, offsets in 0..1). */
function mul(m: number[], n: number[]): number[] {
  const out = new Array(20).fill(0)
  for (let r = 0; r < 4; r++) {
    for (let c = 0; c < 5; c++) {
      let v = c === 4 ? m[r * 5 + 4] : 0
      for (let k = 0; k < 4; k++) v += m[r * 5 + k] * n[k * 5 + c]
      out[r * 5 + c] = v
    }
  }
  return out
}

/**
 * The 梯形 warp: the right end shrinks toward the chosen edge. The flat text
 * is laid down in vertical strips, each squashed for its position: y ↦
 * y0 + top·H + (y − y0)·k, where k shrinks toward the right end.
 */
function drawWarped(ck: CanvasKit, canvas: Canvas, flat: FlatText, st: Record<string, any>, t: TextLayout, f: Frame) {
  const s = Math.max(-1, Math.min(1, Number(st.warpAmount) / 100))
  const bias = Math.max(-1, Math.min(1, Number(st.warpBias || 0) / 100))
  const W = f.cw
  const H = f.ch
  const x0 = -W / 2
  const y0 = -H / 2
  // The shape runs across the text itself, not the whole box.
  const lineW = Math.min(W, Math.max(1, t.width))
  const align = st.textAlign === 'center' ? 0.5 : st.textAlign === 'right' ? 1 : 0
  const left = x0 + (W - lineW) * align
  const { region, k: px } = flat
  // About one image pixel per strip keeps the steps invisible.
  const strips = Math.max(PREVIEW_STRIPS, Math.min(1600, Math.ceil(region.width * px)))
  const sw = region.width / strips
  const paint = new ck.Paint()
  paint.setAntiAlias(false)
  for (let i = 0; i < strips; i++) {
    const sx = region.left + i * sw
    const u = Math.min(1, Math.max(0, (sx + sw / 2 - left) / lineW))
    const k = Math.max(0.02, 1 - 0.8 * s * u)
    const top = y0 + ((1 - k) * (1 - bias)) / 2 * H
    const dy = top + (region.top - y0) * k
    const src = ck.XYWHRect(i * sw * px, 0, sw * px, region.height * px)
    const dst = ck.XYWHRect(sx, dy, sw, region.height * k)
    canvas.drawImageRectOptions(flat.image, src, dst, ck.FilterMode.Linear, ck.MipmapMode.None, paint)
  }
  paint.delete()
}
