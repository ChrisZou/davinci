import type { Canvas, CanvasKit, GrDirectContext, Paint, Shader, Surface } from 'canvaskit-wasm'
import type { Board, Document, Layer, LayerRow, ProjectDoc } from '../types'
import { boardDocument } from '../types'
import type { Renderer } from '../render/renderer'
import { parseColour } from '../render/renderer'
import { boundsOf, boxSize, centreOf, cornersOf, type Frame } from '../render/geometry'
import { blobToDataURL, encodeBoard, getRenderer, type ImageFormat } from './ck'
import { fontStack } from './fonts'
import { trackPointer } from './keyboard'

/**
 * The editor canvas: a view of the server's document, drawn with CanvasKit.
 *
 * It never changes the document. A drag, a resize, a rotation or a typing
 * session is shown live as a local override ("ghost") on top of the document
 * and, when it settles, handed to `onCommit` as an ordinary command — the
 * same kind the CLI sends. The ghost stays until the server's answer arrives,
 * so nothing jumps back in between.
 *
 * Zoom and pan never touch the document either: the board is drawn under a
 * view transform (screen = board · zoom + pan), so every command speaks board
 * pixels however far the human has zoomed.
 */

export interface EditorCallbacks {
  onSelection?: (ids: string[]) => void
  onEditing?: (id: string | null) => void
  /** The zoom or pan changed (the zoom readout listens). */
  onView?: () => void
  /** A gesture settled into a command; resolves once the server answered. */
  onCommit?: (cmd: unknown) => Promise<unknown>
  /** A picture was double-clicked (the page opens the crop dialog). */
  onImageOpen?: (layer: Layer) => void
}

export interface ExportOptions {
  /** 0–1, JPEG/WebP only. */
  quality?: number
  /** Leave the background colour and image out (PNG/WebP). */
  transparent?: boolean
  /** Render only these layers, cropped to their combined bounds. */
  ids?: string[]
}

export const ZOOM_MIN = 0.1
export const ZOOM_MAX = 4

/** The stage around the page (the UI's paper), and the veil over anything hanging off it. */
const PASTEBOARD = '#f3efe6'
const PASTEBOARD_DIM = 'rgba(243, 239, 230, 0.78)'
/** Selection frames and handles: the UI's accent. */
const ACCENT = '#c23a22'
const GUIDE_COLOUR = 'rgba(194, 58, 34, 0.9)'
/** Spacing labels: dark, so "how far" never reads as "aligned". */
const GAP_COLOUR = 'rgba(28, 27, 24, 0.9)'
const HANDLE_R = 5.5
const HANDLE_HIT = 9
const ROTATE_OFFSET = 26
/** Snap distance in screen pixels, whatever the zoom. */
const SNAP_PX = 6
/** A press that moves less than this (screen px) is a click, not a drag. */
const DRAG_PX = 3

type Pt = { x: number; y: number }
type Rect = { left: number; top: number; right: number; bottom: number }
type Handle = 'tl' | 'tr' | 'br' | 'bl' | 'mt' | 'mr' | 'mb' | 'ml' | 'rot'

/** Where each handle sits on the box, as fractions of its width and height. */
const HANDLE_AT: Record<Exclude<Handle, 'rot'>, [number, number]> = {
  tl: [0, 0],
  mt: [0.5, 0],
  tr: [1, 0],
  mr: [1, 0.5],
  br: [1, 1],
  mb: [0.5, 1],
  bl: [0, 1],
  ml: [0, 0.5],
}

/** A distance to label: from → to along one axis, drawn at `at` on the other. */
interface Gap {
  axis: 'x' | 'y'
  from: number
  to: number
  at: number
}

type Gesture =
  | { kind: 'move'; from: Pt; items: { id: string; x: number; y: number }[]; box: Rect; moved: boolean; clicked: string | null; additive: boolean }
  | { kind: 'resize'; handle: Handle; layer: Layer; frame: Frame; from: Pt; moved: boolean }
  | { kind: 'rotate'; layer: Layer; frame: Frame; centre: Pt; start: number; moved: boolean }
  | { kind: 'marquee'; from: Pt; to: Pt; base: string[] }
  | { kind: 'pan'; last: Pt }

type Ghost = Partial<Omit<Layer, 'style'>> & { style?: Record<string, unknown> }

const rad = (d: number) => (d * Math.PI) / 180

function rotate(p: Pt, deg: number): Pt {
  const c = Math.cos(rad(deg))
  const s = Math.sin(rad(deg))
  return { x: p.x * c - p.y * s, y: p.x * s + p.y * c }
}

function rectOf(b: { left: number; top: number; width: number; height: number }): Rect {
  return { left: b.left, top: b.top, right: b.left + b.width, bottom: b.top + b.height }
}

function union(rs: Rect[]): Rect {
  return {
    left: Math.min(...rs.map((r) => r.left)),
    top: Math.min(...rs.map((r) => r.top)),
    right: Math.max(...rs.map((r) => r.right)),
    bottom: Math.max(...rs.map((r) => r.bottom)),
  }
}

/** The nearest line to any of `edges` within reach, as {d: line − edge, line}. */
function nearest(lines: number[], edges: number[], reach: number): { d: number; line: number } | null {
  let best: { d: number; line: number } | null = null
  for (const line of lines)
    for (const edge of edges) {
      const d = line - edge
      if (Math.abs(d) <= reach && (!best || Math.abs(d) < Math.abs(best.d))) best = { d, line }
    }
  return best
}

export class Editor {
  readonly renderer: Renderer
  private ck: CanvasKit
  private container: HTMLElement
  private cb: EditorCallbacks
  private wrap: HTMLDivElement
  private surfaceEl: HTMLCanvasElement
  private overlay: HTMLCanvasElement
  private gl: GrDirectContext | null = null
  private surface: Surface | null = null
  private checker: Shader | null = null
  private observer?: ResizeObserver
  private disposed = false
  private frameQueued = false
  private dpr = 1
  private width = 1
  private height = 1

  private project: ProjectDoc | null = null
  /** The board drawn last with everything it needs loaded (shown while the next one loads). */
  private shown: Board | null = null

  // view
  private z = 1
  private tx = 0
  private ty = 0
  /** True while the view is the fit davinci chose; a resize re-fits only then. */
  private autoFit = true
  /** Room kept free under the page when fitting (the board strip floats there). */
  insetBottom = 88

  // interaction
  private selection: string[] = []
  private hover: string | null = null
  private gesture: Gesture | null = null
  private guides: { x?: number; y?: number }[] = []
  private gaps: Gap[] = []
  private panning = false
  private panFrom: Pt = { x: 0, y: 0 }
  /** Gesture results waiting for the server, by layer. */
  private ghosts = new Map<string, Ghost>()
  /** Slider values being dragged, by layer. */
  private previews = new Map<string, Ghost>()
  private editing: { id: string; ta: HTMLTextAreaElement; done: boolean } | null = null

  static async create(container: HTMLElement, cb: EditorCallbacks = {}): Promise<Editor> {
    return new Editor(container, await getRenderer(), cb)
  }

  private constructor(container: HTMLElement, renderer: Renderer, cb: EditorCallbacks) {
    this.container = container
    this.renderer = renderer
    this.ck = renderer.ck
    this.cb = cb

    this.wrap = document.createElement('div')
    this.wrap.className = 'absolute inset-0 overflow-hidden'
    this.surfaceEl = document.createElement('canvas')
    this.surfaceEl.setAttribute('data-testid', 'davinci-canvas')
    this.overlay = document.createElement('canvas')
    this.overlay.setAttribute('data-testid', 'davinci-overlay')
    for (const el of [this.surfaceEl, this.overlay]) {
      el.style.position = 'absolute'
      el.style.inset = '0'
      el.style.width = '100%'
      el.style.height = '100%'
      this.wrap.appendChild(el)
    }
    this.overlay.style.touchAction = 'none'
    container.prepend(this.wrap)

    this.bindPointer()
    this.makeChecker()
    if (typeof ResizeObserver !== 'undefined') {
      this.observer = new ResizeObserver(() => this.resize())
      this.observer.observe(container)
    }
    this.resize()
  }

  destroy() {
    this.disposed = true
    this.observer?.disconnect()
    window.removeEventListener('pointermove', this.onWindowMove)
    this.editing?.ta.remove()
    this.surface?.delete()
    this.checker?.delete()
    this.wrap.remove()
  }

  // --- the document ---------------------------------------------------------

  /** Shows a project (the server's latest). The view is re-fitted when the board changes. */
  setProject(p: ProjectDoc) {
    const before = this.project?.active
    this.project = p
    const b = this.board
    if (!b) return
    if (before !== p.active) {
      this.shown = null
      this.gesture = null
      this.ghosts.clear()
      this.previews.clear()
      this.finishEditing(false)
      this.fitToScreen()
    }
    const alive = this.selection.filter((id) => b.layers.some((l) => l.id === id))
    if (alive.length !== this.selection.length) this.setSelection(alive)
    if (this.editing && !b.layers.some((l) => l.id === this.editing!.id)) this.finishEditing(false)
    this.render()
  }

  /** The active board. */
  get board(): Board | null {
    const p = this.project
    if (!p) return null
    return p.boards.find((b) => b.id === p.active) ?? p.boards[0] ?? null
  }

  /** The active board as a single-page document (what the panels read). */
  get doc(): Document | null {
    const b = this.board
    return b ? boardDocument(b) : null
  }

  /** Drops the gesture results the server has now answered for. */
  settle() {
    this.ghosts.clear()
    this.render()
  }

  /** The board as drawn: the document with gestures and previews laid over it. */
  private viewBoard(): Board | null {
    const b = this.board
    if (!b) return null
    if (!this.ghosts.size && !this.previews.size) return b
    const layers = b.layers.map((l) => {
      const g = this.ghosts.get(l.id)
      const p = this.previews.get(l.id)
      if (!g && !p) return l
      return {
        ...l,
        ...g,
        ...p,
        style: { ...(l.style ?? {}), ...(g?.style ?? {}), ...(p?.style ?? {}) },
      } as Layer
    })
    return { ...b, layers }
  }

  private layer(id: string): Layer | undefined {
    return this.viewBoard()?.layers.find((l) => l.id === id)
  }

  /** Layer rows for the panel, front to back. */
  layerRows(): LayerRow[] {
    const layers = this.board?.layers ?? []
    return layers
      .map((l, i) => ({
        id: l.id,
        name: l.name,
        type: l.type,
        x: l.x,
        y: l.y,
        width: l.width,
        height: l.height,
        rotation: l.rotation,
        opacity: l.opacity,
        visible: l.visible,
        locked: l.locked,
        preview: previewOf(l),
        index: i,
      }))
      .reverse()
  }

  // --- live previews -----------------------------------------------------

  /**
   * Shows a value while a slider is still being dragged. A slider only sends
   * its command when it settles (one undo step, not hundreds); until then the
   * canvas shows the value from here.
   */
  preview(id: string, key: 'opacity' | 'filters', value: number | Record<string, number>) {
    const g: Ghost = { ...(this.previews.get(id) ?? {}) }
    if (key === 'opacity') g.opacity = Math.max(0, Math.min(1, Number(value)))
    else g.style = { ...(g.style ?? {}), filters: value }
    this.previews.set(id, g)
    this.render()
  }

  endPreview() {
    if (!this.previews.size) return
    this.previews.clear()
    this.render()
  }

  // --- selection ---------------------------------------------------------

  selectionIDs(): string[] {
    return [...this.selection]
  }

  /** Layers a "select all" picks: visible and unlocked. */
  selectableIDs(): string[] {
    return (this.board?.layers ?? []).filter((l) => l.visible !== false && !l.locked).map((l) => l.id)
  }

  select(id: string | null) {
    this.selectMany(id ? [id] : [])
  }

  /**
   * Selects any number of layers. Locked layers can be selected this way (the
   * layer panel does it) — that is how the human gets at their properties.
   */
  selectMany(ids: string[]) {
    const have = new Set((this.board?.layers ?? []).map((l) => l.id))
    this.setSelection(ids.filter((id) => have.has(id)))
  }

  private setSelection(ids: string[]) {
    const same = ids.length === this.selection.length && ids.every((id, i) => id === this.selection[i])
    this.selection = ids
    if (!same) this.cb.onSelection?.(this.selectionIDs())
    this.render()
  }

  /** The layer under a pointer event (unlocked, visible), if any. */
  layerAt(e: { clientX: number; clientY: number }): string | null {
    return this.hit(this.toBoard(this.local(e)))
  }

  private hit(p: Pt): string | null {
    const b = this.viewBoard()
    if (!b) return null
    for (let i = b.layers.length - 1; i >= 0; i--) {
      const l = b.layers[i]
      if (l.visible === false || l.locked) continue
      if (this.contains(l, p)) return l.id
    }
    return null
  }

  /** True when a board point is inside a layer's (turned) box. */
  private contains(l: Layer, p: Pt): boolean {
    const f = this.renderer.frame(l)
    const { w, h } = boxSize(f)
    // Thin things (a line, a hairline rect) get a few screen pixels of slack.
    const slack = 4 / this.z
    const q = rotate({ x: p.x - l.x, y: p.y - l.y }, -(l.rotation || 0))
    return q.x >= -slack && q.y >= -slack && q.x <= w + slack && q.y <= h + slack
  }

  private boxOf(l: Layer): Rect {
    return rectOf(boundsOf(l, this.renderer.frame(l)))
  }

  // --- text editing -----------------------------------------------------

  /** Opens a text layer for typing, all of its text selected. */
  editText(id: string) {
    const l = this.board?.layers.find((x) => x.id === id)
    if (!l || l.type !== 'text' || l.locked) return
    this.finishEditing(true)
    this.setSelection([id])
    const ta = document.createElement('textarea')
    ta.value = String(l.text ?? '')
    ta.spellcheck = false
    ta.setAttribute('data-testid', 'text-editor')
    Object.assign(ta.style, {
      position: 'absolute',
      margin: '0',
      border: '0',
      outline: `1.5px solid ${ACCENT}`,
      background: 'transparent',
      resize: 'none',
      overflow: 'hidden',
      whiteSpace: 'pre-wrap',
      overflowWrap: 'anywhere',
      transformOrigin: '0 0',
      zIndex: '5',
    } satisfies Partial<CSSStyleDeclaration>)
    this.wrap.appendChild(ta)
    this.editing = { id, ta, done: false }
    this.placeEditor()
    ta.addEventListener('input', () => this.placeEditor())
    ta.addEventListener('blur', () => this.finishEditing(true))
    ta.addEventListener('keydown', (e) => {
      e.stopPropagation()
      if (e.key === 'Escape' || (e.key === 'Enter' && (e.metaKey || e.ctrlKey))) {
        e.preventDefault()
        ta.blur()
      }
    })
    ta.focus()
    ta.select()
    this.cb.onEditing?.(id)
    this.render()
  }

  /** Lays the textarea over the text it edits, in the text's own font. */
  private placeEditor() {
    const ed = this.editing
    if (!ed) return
    const l = this.board?.layers.find((x) => x.id === ed.id)
    if (!l) return
    const st = (l.style ?? {}) as Record<string, any>
    const f = this.renderer.frame(l)
    const z = this.z
    const fs = Number(st.fontSize) || 40
    const lh = Number(st.lineHeight) || 1.16
    const stretch = Number(st.stretch) > 0 ? Number(st.stretch) : 1
    const inset = (f.strokeWidth / 2) * z
    const s = ed.ta.style
    s.left = `${l.x * z + this.tx}px`
    s.top = `${l.y * z + this.ty}px`
    s.width = `${(l.width / stretch) * z + inset * 2}px`
    s.padding = `${inset}px`
    s.transform = `rotate(${l.rotation || 0}deg) scaleX(${stretch})`
    s.fontFamily = fontStack(String(st.fontFamily ?? ''))
    s.fontSize = `${fs * z}px`
    s.fontWeight = String(st.fontWeight ?? 400)
    s.fontStyle = st.fontStyle === 'italic' ? 'italic' : 'normal'
    s.lineHeight = `${1.13 * fs * lh * z}px`
    s.letterSpacing = `${((Number(st.charSpacing) || 0) / 1000) * fs * z}px`
    s.textAlign = String(st.textAlign ?? 'left')
    s.color = String(st.fill ?? '#111111')
    s.caretColor = ACCENT
    s.height = '0px'
    s.height = `${Math.max(ed.ta.scrollHeight, fs * 1.13 * z)}px`
  }

  /** Closes the text box; `commit` sends what was typed. */
  private finishEditing(commit: boolean) {
    const ed = this.editing
    if (!ed || ed.done) return
    ed.done = true
    this.editing = null
    const text = ed.ta.value
    ed.ta.remove()
    this.cb.onEditing?.(null)
    const l = this.board?.layers.find((x) => x.id === ed.id)
    if (commit && l && text !== (l.text ?? '')) {
      if (!text.trim()) {
        this.commit({ type: 'removeLayer', id: l.id }, [])
      } else {
        this.ghosts.set(l.id, { ...(this.ghosts.get(l.id) ?? {}), text })
        this.commit({ type: 'setText', id: l.id, text }, [l.id])
      }
    }
    this.render()
  }

  /** The id being typed into, if any. */
  get editingID(): string | null {
    return this.editing?.id ?? null
  }

  // --- zoom & pan -------------------------------------------------------

  get zoom(): number {
    return this.z
  }

  setZoom(z: number) {
    this.zoomAt(this.width / 2, this.height / 2, z)
  }

  zoomBy(factor: number) {
    this.setZoom(this.z * factor)
  }

  /** Zooms around a point in stage pixels, which stays put. */
  zoomAt(x: number, y: number, z: number) {
    const next = Math.min(ZOOM_MAX, Math.max(ZOOM_MIN, z))
    this.autoFit = false
    const bx = (x - this.tx) / this.z
    const by = (y - this.ty) / this.z
    this.z = next
    this.tx = x - bx * next
    this.ty = y - by * next
    this.viewChanged()
  }

  fitToScreen() {
    const b = this.board
    if (!b) return
    // Breathing room around the page, with extra at the bottom where the zoom
    // pill and the board strip float.
    const padX = 64
    const padTop = 32
    const padBottom = this.insetBottom
    const z = Math.min(
      ZOOM_MAX,
      Math.max(ZOOM_MIN, Math.min((this.width - padX) / b.canvas.width, (this.height - padTop - padBottom) / b.canvas.height)),
    )
    this.autoFit = true
    this.z = z
    this.tx = (this.width - b.canvas.width * z) / 2
    this.ty = (this.height - b.canvas.height * z) / 2 - (padBottom - padTop) / 2
    this.viewChanged()
  }

  setInsetBottom(px: number) {
    if (px === this.insetBottom) return
    this.insetBottom = px
    if (this.autoFit) this.fitToScreen()
  }

  panBy(dx: number, dy: number) {
    this.autoFit = false
    this.tx += dx
    this.ty += dy
    this.viewChanged()
  }

  beginPan(clientX: number, clientY: number) {
    this.panning = true
    this.autoFit = false
    this.panFrom = { x: clientX, y: clientY }
    this.overlay.style.cursor = 'grabbing'
  }

  movePan(clientX: number, clientY: number) {
    if (!this.panning) return
    this.panBy(clientX - this.panFrom.x, clientY - this.panFrom.y)
    this.panFrom = { x: clientX, y: clientY }
  }

  endPan() {
    this.panning = false
    this.overlay.style.cursor = ''
  }

  get isPanning(): boolean {
    return this.panning
  }

  private viewChanged() {
    this.placeEditor()
    this.render()
    this.cb.onView?.()
  }

  /** Client pixels → board pixels (rounded). */
  screenToCanvas(clientX: number, clientY: number): Pt {
    const p = this.toBoard(this.local({ clientX, clientY }))
    return { x: Math.round(p.x), y: Math.round(p.y) }
  }

  /** Board pixels → client pixels. */
  canvasToScreen(x: number, y: number): Pt {
    const r = this.overlay.getBoundingClientRect()
    return { x: r.left + x * this.z + this.tx, y: r.top + y * this.z + this.ty }
  }

  private local(e: { clientX: number; clientY: number }): Pt {
    const r = this.overlay.getBoundingClientRect()
    return { x: e.clientX - r.left, y: e.clientY - r.top }
  }

  private toBoard(p: Pt): Pt {
    return { x: (p.x - this.tx) / this.z, y: (p.y - this.ty) / this.z }
  }

  private toScreen(p: Pt): Pt {
    return { x: p.x * this.z + this.tx, y: p.y * this.z + this.ty }
  }

  // --- export -----------------------------------------------------------

  /** Renders the board to a data URL at a scale. */
  async toDataURL(format: ImageFormat = 'png', multiplier = 1, opts: ExportOptions = {}): Promise<string> {
    const b = this.board
    if (!b) throw new Error('nothing to export')
    return blobToDataURL(await encodeBoard(b, { format, scale: multiplier, ...opts }))
  }

  // --- drawing -------------------------------------------------------------

  /** Sizes both canvases to the container (in device pixels). */
  resize() {
    if (this.disposed) return
    const w = Math.max(1, Math.round(this.container.clientWidth))
    const h = Math.max(1, Math.round(this.container.clientHeight))
    const dpr = Math.max(1, window.devicePixelRatio || 1)
    if (w === this.width && h === this.height && dpr === this.dpr && this.surface) return
    this.width = w
    this.height = h
    this.dpr = dpr
    for (const el of [this.surfaceEl, this.overlay]) {
      el.width = Math.round(w * dpr)
      el.height = Math.round(h * dpr)
    }
    this.makeSurface()
    // The stage is not its final size when the page opens (the side panels
    // claim their width a moment later), so fit again while the view is ours.
    if (this.autoFit) this.fitToScreen()
    this.render()
  }

  private makeSurface() {
    const ck = this.ck
    this.surface?.delete()
    this.surface = null
    const w = this.surfaceEl.width
    const h = this.surfaceEl.height
    // ?sw=1 draws without WebGL (for telling a GPU problem from a drawing one).
    if (!this.gl && !/[?&]sw=1/.test(location.search)) {
      const handle = ck.GetWebGLContext(this.surfaceEl)
      this.gl = handle ? ck.MakeWebGLContext(handle) : null
    }
    if (this.gl) this.surface = ck.MakeOnScreenGLSurface(this.gl, w, h, ck.ColorSpace.SRGB)
    if (!this.surface) this.surface = ck.MakeSWCanvasSurface(this.surfaceEl)
  }

  /** A screen-space checkerboard: a see-through page reads the same at any zoom. */
  private makeChecker() {
    const ck = this.ck
    const s = ck.MakeSurface(16, 16)
    if (!s) return
    const c = s.getCanvas()
    c.clear(ck.WHITE)
    const p = new ck.Paint()
    p.setColor(ck.Color(228, 228, 233, 1))
    c.drawRect(ck.XYWHRect(0, 0, 8, 8), p)
    c.drawRect(ck.XYWHRect(8, 8, 8, 8), p)
    p.delete()
    const img = s.makeImageSnapshot()
    this.checker = img.makeShaderOptions(ck.TileMode.Repeat, ck.TileMode.Repeat, ck.FilterMode.Nearest, ck.MipmapMode.None)
    s.delete()
  }

  /** Asks for a redraw on the next frame. */
  render() {
    if (this.frameQueued || this.disposed) return
    this.frameQueued = true
    requestAnimationFrame(() => {
      this.frameQueued = false
      if (!this.disposed) this.paint()
    })
  }

  private paint() {
    const view = this.viewBoard()
    if (!view) return
    // A board whose fonts or pictures are still loading keeps the last
    // complete picture on screen, and is drawn once they are in.
    if (this.renderer.isReady(view)) {
      this.shown = view
    } else {
      void this.renderer.prepare(view).then(() => this.render())
      if (!this.shown || this.shown.id !== view.id) this.shown = view
    }
    this.paintBoard(this.shown ?? view)
    this.paintOverlay()
  }

  private paintBoard(board: Board) {
    const surface = this.surface
    if (!surface) return
    const ck = this.ck
    const c = surface.getCanvas()
    const d = this.dpr
    const W = board.canvas.width
    const H = board.canvas.height
    // Plain numbers: CanvasKit's rect helpers may hand back a shared scratch buffer.
    const page: [number, number, number, number] = [this.tx, this.ty, this.tx + W * this.z, this.ty + H * this.z]
    const pageRect = () => ck.LTRBRect(...page)
    c.clear(ck.parseColorString(PASTEBOARD))

    c.save()
    c.scale(d, d)
    const p = new ck.Paint()
    p.setAntiAlias(true)
    // The page lifts off the paper: a soft shadow, then its fill.
    p.setColor(ck.Color(60, 48, 20, 0.18))
    const blur = ck.MaskFilter.MakeBlur(ck.BlurStyle.Normal, 18, false)
    p.setMaskFilter(blur)
    c.drawRect(ck.XYWHRect(this.tx, this.ty + 12, W * this.z, H * this.z), p)
    p.setMaskFilter(null)
    blur.delete()
    if (!parseColour(ck, board.canvas.background) && this.checker) {
      p.setShader(this.checker)
      c.drawRect(pageRect(), p)
      p.setShader(null)
    }
    c.restore()

    // The page shows what the export will: everything clipped to its edge.
    // Only the selection also shows what hangs off it (under the veil below),
    // so a layer being placed can be seen whole without the rest cluttering
    // the paper.
    const hide = this.editing ? new Set([this.editing.id]) : undefined
    const pageBox = ck.XYWHRect(0, 0, W, H)
    let depth = c.save()
    c.concat([d * this.z, 0, d * this.tx, 0, d * this.z, d * this.ty, 0, 0, 1])
    c.clipRect(pageBox, ck.ClipOp.Intersect, true)
    this.renderer.draw(c, board, { hide })
    c.restoreToCount(depth)
    const selected = new Set(this.selection)
    if (selected.size) {
      depth = c.save()
      c.concat([d * this.z, 0, d * this.tx, 0, d * this.z, d * this.ty, 0, 0, 1])
      c.clipRect(ck.XYWHRect(0, 0, W, H), ck.ClipOp.Difference, true)
      this.renderer.draw(c, board, { transparent: true, only: selected, hide })
      c.restoreToCount(depth)
    }

    // What the selection hangs off the page is dimmed: it exists, but will not be in the export.
    c.save()
    c.scale(d, d)
    p.setColor(ck.parseColorString(PASTEBOARD_DIM))
    this.veil(c, p, page)
    p.setStyle(ck.PaintStyle.Stroke)
    p.setStrokeWidth(1 / d)
    p.setColor(ck.Color(60, 48, 20, 0.1))
    c.drawRect(ck.LTRBRect(page[0] - 0.5, page[1] - 0.5, page[2] + 0.5, page[3] + 0.5), p)
    c.restore()
    p.delete()
    surface.flush()
  }

  /** Fills everything around the page. */
  private veil(c: Canvas, p: Paint, page: [number, number, number, number]) {
    const ck = this.ck
    const [l, t, r, b] = page
    const W = this.width
    const H = this.height
    c.drawRect(ck.LTRBRect(0, 0, W, t), p)
    c.drawRect(ck.LTRBRect(0, b, W, H), p)
    c.drawRect(ck.LTRBRect(0, t, l, b), p)
    c.drawRect(ck.LTRBRect(r, t, W, b), p)
  }

  /** Selection frames, handles, guides, spacing labels and the marquee. */
  private paintOverlay() {
    const ctx = this.overlay.getContext('2d')
    if (!ctx) return
    const d = this.dpr
    ctx.setTransform(1, 0, 0, 1, 0, 0)
    ctx.clearRect(0, 0, this.overlay.width, this.overlay.height)
    ctx.setTransform(d, 0, 0, d, 0, 0)
    const board = this.viewBoard()
    if (!board) return
    const byID = new Map(board.layers.map((l) => [l.id, l]))

    const outline = (l: Layer, width: number, colour = ACCENT) => {
      const pts = cornersOf(l, this.renderer.frame(l)).map((p) => this.toScreen(p))
      ctx.beginPath()
      pts.forEach((p, i) => (i ? ctx.lineTo(p.x, p.y) : ctx.moveTo(p.x, p.y)))
      ctx.closePath()
      ctx.lineWidth = width
      ctx.strokeStyle = colour
      ctx.stroke()
    }

    const busy = this.gesture && this.gesture.kind !== 'marquee' && this.gesture.kind !== 'pan'
    if (this.hover && !this.selection.includes(this.hover) && !busy) {
      const l = byID.get(this.hover)
      if (l) outline(l, 1.5)
    }

    const sel = this.selection.map((id) => byID.get(id)).filter((l): l is Layer => !!l)
    if (sel.length === 1 && !this.editing) {
      const l = sel[0]
      outline(l, 1.5)
      if (!l.locked && !(this.gesture?.kind === 'move' && this.gesture.moved)) {
        for (const [, p] of this.handlesOf(l)) this.knob(ctx, p)
      }
    } else if (sel.length > 1) {
      for (const l of sel) outline(l, 1, 'rgba(194, 58, 34, 0.55)')
      const r = union(sel.map((l) => this.boxOf(l)))
      const a = this.toScreen({ x: r.left, y: r.top })
      const b = this.toScreen({ x: r.right, y: r.bottom })
      ctx.lineWidth = 1.5
      ctx.strokeStyle = ACCENT
      ctx.strokeRect(a.x, a.y, b.x - a.x, b.y - a.y)
    }

    if (this.gesture?.kind === 'marquee') {
      const a = this.toScreen(this.gesture.from)
      const b = this.toScreen(this.gesture.to)
      ctx.fillStyle = 'rgba(194, 58, 34, 0.06)'
      ctx.fillRect(a.x, a.y, b.x - a.x, b.y - a.y)
      ctx.lineWidth = 1
      ctx.strokeStyle = ACCENT
      ctx.strokeRect(a.x + 0.5, a.y + 0.5, b.x - a.x, b.y - a.y)
    }

    this.paintGuides(ctx)
  }

  private knob(ctx: CanvasRenderingContext2D, p: Pt) {
    ctx.beginPath()
    ctx.arc(p.x, p.y, HANDLE_R, 0, Math.PI * 2)
    ctx.fillStyle = '#ffffff'
    ctx.fill()
    ctx.lineWidth = 1.5
    ctx.strokeStyle = ACCENT
    ctx.stroke()
  }

  private paintGuides(ctx: CanvasRenderingContext2D) {
    const sx = (x: number) => Math.round(x * this.z + this.tx) + 0.5
    const sy = (y: number) => Math.round(y * this.z + this.ty) + 0.5
    ctx.save()
    ctx.lineWidth = 1
    ctx.strokeStyle = GUIDE_COLOUR
    ctx.setLineDash([4, 3])
    for (const g of this.guides) {
      ctx.beginPath()
      if (g.x !== undefined) {
        ctx.moveTo(sx(g.x), 0)
        ctx.lineTo(sx(g.x), this.height)
      } else if (g.y !== undefined) {
        ctx.moveTo(0, sy(g.y))
        ctx.lineTo(this.width, sy(g.y))
      }
      ctx.stroke()
    }
    ctx.setLineDash([])
    ctx.strokeStyle = GAP_COLOUR
    ctx.font = '11px -apple-system, BlinkMacSystemFont, sans-serif'
    ctx.textAlign = 'center'
    ctx.textBaseline = 'middle'
    for (const g of this.gaps) {
      const px = Math.round(g.to - g.from)
      if (px <= 0) continue
      const a = g.axis === 'x' ? { x: sx(g.from), y: sy(g.at) } : { x: sx(g.at), y: sy(g.from) }
      const b = g.axis === 'x' ? { x: sx(g.to), y: sy(g.at) } : { x: sx(g.at), y: sy(g.to) }
      const t = 4
      ctx.beginPath()
      ctx.moveTo(a.x, a.y)
      ctx.lineTo(b.x, b.y)
      if (g.axis === 'x') {
        ctx.moveTo(a.x, a.y - t)
        ctx.lineTo(a.x, a.y + t)
        ctx.moveTo(b.x, b.y - t)
        ctx.lineTo(b.x, b.y + t)
      } else {
        ctx.moveTo(a.x - t, a.y)
        ctx.lineTo(a.x + t, a.y)
        ctx.moveTo(b.x - t, b.y)
        ctx.lineTo(b.x + t, b.y)
      }
      ctx.stroke()
      const label = String(px)
      const mx = (a.x + b.x) / 2
      const my = (a.y + b.y) / 2
      const w = ctx.measureText(label).width + 8
      ctx.fillStyle = GAP_COLOUR
      ctx.fillRect(mx - w / 2, my - 8, w, 16)
      ctx.fillStyle = '#fff'
      ctx.fillText(label, mx, my + 0.5)
    }
    ctx.restore()
  }

  /** The handles a layer offers, in screen pixels. */
  private handlesOf(l: Layer): [Handle, Pt][] {
    const f = this.renderer.frame(l)
    const { w, h } = boxSize(f)
    const at = (u: number, v: number) => this.toScreen({ ...this.addTurned(l, u * w, v * h) })
    const isLine = l.type === 'shape' && l.shape?.kind === 'line'
    const keys: Exclude<Handle, 'rot'>[] =
      l.type === 'text' ? ['tl', 'tr', 'br', 'bl', 'ml', 'mr'] : isLine ? ['ml', 'mr'] : ['tl', 'mt', 'tr', 'mr', 'br', 'mb', 'bl', 'ml']
    const out: [Handle, Pt][] = keys.map((k) => [k, at(...HANDLE_AT[k])])
    // The rotate knob stands off the top edge, whatever the zoom.
    const top = at(0.5, 0)
    const up = rotate({ x: 0, y: -ROTATE_OFFSET }, l.rotation || 0)
    out.push(['rot', { x: top.x + up.x, y: top.y + up.y }])
    return out
  }

  /** A point on a layer's box, given in the box's own (unturned) axes, on the board. */
  private addTurned(l: Pick<Layer, 'x' | 'y' | 'rotation'>, u: number, v: number): Pt {
    const r = rotate({ x: u, y: v }, l.rotation || 0)
    return { x: l.x + r.x, y: l.y + r.y }
  }

  private handleAt(p: Pt): Handle | null {
    if (this.selection.length !== 1 || this.editing) return null
    const l = this.layer(this.selection[0])
    if (!l || l.locked) return null
    for (const [k, q] of this.handlesOf(l)) if (Math.hypot(q.x - p.x, q.y - p.y) <= HANDLE_HIT) return k
    return null
  }

  private cursorFor(h: Handle, rotation: number): string {
    if (h === 'rot') return 'grab'
    const [u, v] = HANDLE_AT[h]
    const a = (Math.atan2(v - 0.5, u - 0.5) * 180) / Math.PI + rotation
    const n = ((Math.round(a / 45) % 4) + 4) % 4
    return ['ew-resize', 'nwse-resize', 'ns-resize', 'nesw-resize'][n]
  }

  // --- pointer --------------------------------------------------------------

  private bindPointer() {
    const el = this.overlay
    el.addEventListener('pointerdown', (e) => this.onDown(e))
    el.addEventListener('pointermove', (e) => this.onMove(e))
    el.addEventListener('pointerup', (e) => this.onUp(e))
    el.addEventListener('pointercancel', (e) => this.onUp(e))
    el.addEventListener('pointerleave', () => {
      if (this.hover && !this.gesture) {
        this.hover = null
        this.render()
      }
    })
    el.addEventListener('dblclick', (e) => this.onDblClick(e))
    el.addEventListener('wheel', (e) => this.onWheel(e), { passive: false })
    window.addEventListener('pointermove', this.onWindowMove)
  }

  /** Space-panning follows the pointer anywhere in the window. */
  private onWindowMove = (e: PointerEvent) => {
    trackPointer(e)
    this.movePan(e.clientX, e.clientY)
  }

  private onDown(e: PointerEvent) {
    if (e.button === 2) return
    const s = this.local(e)
    if (e.button === 1 || this.panning) {
      e.preventDefault()
      this.gesture = { kind: 'pan', last: { x: e.clientX, y: e.clientY } }
      this.overlay.setPointerCapture(e.pointerId)
      this.overlay.style.cursor = 'grabbing'
      return
    }
    if (e.button !== 0) return
    // Clicking away from a text box closes it (its blur commits).
    if (this.editing) this.editing.ta.blur()
    const p = this.toBoard(s)
    const handle = this.handleAt(s)
    const additive = e.shiftKey || e.metaKey || e.ctrlKey
    this.overlay.setPointerCapture(e.pointerId)

    if (handle) {
      const l = this.layer(this.selection[0])!
      const frame = this.renderer.frame(l)
      if (handle === 'rot') {
        const c = centreOf(l, frame)
        this.gesture = { kind: 'rotate', layer: l, frame, centre: c, start: Math.atan2(p.y - c.y, p.x - c.x), moved: false }
      } else {
        this.gesture = { kind: 'resize', handle, layer: l, frame, from: s, moved: false }
      }
      return
    }

    const id = this.hit(p)
    if (!id) {
      this.gesture = { kind: 'marquee', from: p, to: p, base: additive ? this.selectionIDs() : [] }
      if (!additive) this.setSelection([])
      return
    }
    if (additive) {
      if (this.selection.includes(id)) {
        this.setSelection(this.selection.filter((x) => x !== id))
        this.gesture = null
        return
      }
      this.setSelection([...this.selection, id])
    } else if (!this.selection.includes(id)) {
      this.setSelection([id])
    }
    const board = this.viewBoard()!
    const moving = board.layers.filter((l) => this.selection.includes(l.id) && !l.locked)
    if (!moving.length) {
      this.gesture = null
      return
    }
    this.gesture = {
      kind: 'move',
      from: s,
      items: moving.map((l) => ({ id: l.id, x: l.x, y: l.y })),
      box: union(moving.map((l) => this.boxOf(l))),
      moved: false,
      clicked: id,
      additive,
    }
  }

  private onMove(e: PointerEvent) {
    const g = this.gesture
    const s = this.local(e)
    if (!g) {
      if (this.panning) return
      const handle = this.handleAt(s)
      const hover = handle ? null : this.hit(this.toBoard(s))
      const l = handle ? this.layer(this.selection[0]) : undefined
      this.overlay.style.cursor = handle ? this.cursorFor(handle, l?.rotation ?? 0) : hover ? 'move' : ''
      if (hover !== this.hover) {
        this.hover = hover
        this.render()
      }
      return
    }
    switch (g.kind) {
      case 'pan':
        this.panBy(e.clientX - g.last.x, e.clientY - g.last.y)
        g.last = { x: e.clientX, y: e.clientY }
        return
      case 'marquee':
        this.onMarquee(g, this.toBoard(s))
        return
      case 'move':
        this.onDrag(g, s, e.shiftKey)
        return
      case 'resize':
        this.onResize(g, s, e.shiftKey)
        return
      case 'rotate':
        this.onRotate(g, this.toBoard(s), e.shiftKey)
        return
    }
  }

  private onUp(e: PointerEvent) {
    const g = this.gesture
    this.gesture = null
    if (this.overlay.hasPointerCapture(e.pointerId)) this.overlay.releasePointerCapture(e.pointerId)
    this.guides = []
    this.gaps = []
    if (!g) return
    if (g.kind === 'pan') {
      this.overlay.style.cursor = this.panning ? 'grabbing' : ''
      return
    }
    if (g.kind === 'marquee') {
      this.render()
      return
    }
    if (g.kind === 'move') {
      if (!g.moved) {
        // A plain click inside a multi-selection picks that one layer.
        if (!g.additive && g.clicked && this.selection.length > 1) this.setSelection([g.clicked])
        this.render()
        return
      }
      const moves = g.items
        .map((it) => {
          const gh = this.ghosts.get(it.id)
          return { type: 'moveLayer', id: it.id, x: Math.round(gh?.x ?? it.x), y: Math.round(gh?.y ?? it.y) }
        })
        .filter((m, i) => m.x !== g.items[i].x || m.y !== g.items[i].y)
      for (const m of moves) this.ghosts.set(m.id, { ...this.ghosts.get(m.id), x: m.x, y: m.y })
      if (moves.length) this.commit(moves.length === 1 ? moves[0] : { type: 'batch', commands: moves }, moves.map((m) => m.id))
      else this.settleIDs(g.items.map((i) => i.id))
      return
    }
    if (!g.moved) {
      this.render()
      return
    }
    const gh = this.ghosts.get(g.layer.id)
    if (!gh) return
    if (g.kind === 'rotate') {
      this.commit({ type: 'rotateLayer', id: g.layer.id, rotation: gh.rotation ?? g.layer.rotation }, [g.layer.id])
      return
    }
    const props: Record<string, unknown> = { x: Math.round(gh.x ?? g.layer.x), y: Math.round(gh.y ?? g.layer.y), width: Math.round(gh.width ?? g.layer.width) }
    if (g.layer.type === 'text') {
      if (gh.style?.fontSize !== undefined) props.fontSize = gh.style.fontSize
    } else if (!(g.layer.type === 'shape' && g.layer.shape?.kind === 'line')) {
      props.height = Math.round(gh.height ?? g.layer.height)
    }
    this.commit({ type: 'updateLayer', id: g.layer.id, props }, [g.layer.id])
  }

  /** Sends a gesture's command; its ghosts go once the server has answered. */
  private commit(cmd: unknown, ids: string[]) {
    this.render()
    const done = () => this.settleIDs(ids)
    if (!this.cb.onCommit) return done()
    void this.cb.onCommit(cmd).then(done, done)
  }

  private settleIDs(ids: string[]) {
    for (const id of ids) this.ghosts.delete(id)
    this.render()
  }

  private onDblClick(e: MouseEvent) {
    const id = this.hit(this.toBoard(this.local(e)))
    const l = id ? this.board?.layers.find((x) => x.id === id) : undefined
    if (!l) return
    if (l.type === 'text') this.editText(l.id)
    else if (l.type === 'image') this.cb.onImageOpen?.(l)
  }

  /**
   * ⌘/ctrl + wheel (a trackpad pinch arrives as ctrl + wheel) zooms around the
   * board's centre; a plain wheel or two-finger scroll pans.
   */
  private onWheel(e: WheelEvent) {
    e.preventDefault()
    if (e.metaKey || e.ctrlKey) {
      // Around the board's centre, which stays where it is on screen.
      const b = this.board
      const factor = Math.exp(-Math.max(-50, Math.min(50, e.deltaY)) * 0.01)
      if (b) this.zoomAt(this.tx + (b.canvas.width * this.z) / 2, this.ty + (b.canvas.height * this.z) / 2, this.z * factor)
      return
    }
    const dx = e.shiftKey && !e.deltaX ? e.deltaY : e.deltaX
    const dy = e.shiftKey && !e.deltaX ? 0 : e.deltaY
    this.panBy(-dx, -dy)
  }

  // --- gestures -----------------------------------------------------------

  private onMarquee(g: Extract<Gesture, { kind: 'marquee' }>, p: Pt) {
    g.to = p
    const r: Rect = { left: Math.min(g.from.x, p.x), top: Math.min(g.from.y, p.y), right: Math.max(g.from.x, p.x), bottom: Math.max(g.from.y, p.y) }
    const hits = (this.board?.layers ?? [])
      .filter((l) => l.visible !== false && !l.locked)
      .filter((l) => {
        const b = this.boxOf(l)
        return b.left < r.right && b.right > r.left && b.top < r.bottom && b.bottom > r.top
      })
      .map((l) => l.id)
    this.setSelection([...g.base, ...hits.filter((id) => !g.base.includes(id))])
  }

  /** The lines worth snapping to: the page's edges and centre, and every other layer's. */
  private snapLines(skip: Set<string>) {
    const b = this.board!
    const xs = [0, b.canvas.width / 2, b.canvas.width]
    const ys = [0, b.canvas.height / 2, b.canvas.height]
    const boxes: Rect[] = []
    for (const l of this.viewBoard()!.layers) {
      if (skip.has(l.id) || l.visible === false) continue
      const r = this.boxOf(l)
      boxes.push(r)
      xs.push(r.left, (r.left + r.right) / 2, r.right)
      ys.push(r.top, (r.top + r.bottom) / 2, r.bottom)
    }
    return { xs, ys, boxes }
  }

  private onDrag(g: Extract<Gesture, { kind: 'move' }>, s: Pt, shift: boolean) {
    let dx = (s.x - g.from.x) / this.z
    let dy = (s.y - g.from.y) / this.z
    if (!g.moved) {
      if (Math.hypot(s.x - g.from.x, s.y - g.from.y) < DRAG_PX) return
      g.moved = true
    }
    // Shift holds the drag to whichever axis it has moved along more.
    let lock: 'x' | 'y' | undefined
    if (shift) {
      if (Math.abs(dx) >= Math.abs(dy)) {
        dy = 0
        lock = 'y'
      } else {
        dx = 0
        lock = 'x'
      }
    }
    const { xs, ys, boxes } = this.snapLines(new Set(g.items.map((i) => i.id)))
    const reach = SNAP_PX / this.z
    const b = g.box
    const moved: Rect = { left: b.left + dx, top: b.top + dy, right: b.right + dx, bottom: b.bottom + dy }
    const bx = lock === 'x' ? null : nearest(xs, [moved.left, (moved.left + moved.right) / 2, moved.right], reach)
    const by = lock === 'y' ? null : nearest(ys, [moved.top, (moved.top + moved.bottom) / 2, moved.bottom], reach)
    this.guides = []
    if (bx) {
      dx += bx.d
      this.guides.push({ x: bx.line })
    }
    if (by) {
      dy += by.d
      this.guides.push({ y: by.line })
    }
    for (const it of g.items) this.ghosts.set(it.id, { ...this.ghosts.get(it.id), x: it.x + dx, y: it.y + dy })
    this.gaps = gapsOf({ left: b.left + dx, top: b.top + dy, right: b.right + dx, bottom: b.bottom + dy }, boxes, this.board!.canvas)
    this.render()
  }

  /**
   * A resize handle: the opposite corner (or edge) stays put and the box grows
   * toward the pointer. Corners keep the proportions (shift frees them); text
   * scales its font from a corner and rewraps from a side.
   */
  private onResize(g: Extract<Gesture, { kind: 'resize' }>, s: Pt, shift: boolean) {
    if (!g.moved) {
      if (Math.hypot(s.x - g.from.x, s.y - g.from.y) < DRAG_PX) return
      g.moved = true
    }
    const l = g.layer
    const f = g.frame
    const { w, h } = boxSize(f)
    const [hu, hv] = HANDLE_AT[g.handle as Exclude<Handle, 'rot'>]
    const corner = hu !== 0.5 && hv !== 0.5
    const side = hv === 0.5 ? 'x' : hu === 0.5 ? 'y' : null
    // The fixed point, in the box's own axes; a side handle keeps the top (or
    // left) edge, which is what matters when text rewraps and changes height.
    const au = hu === 0.5 ? 0 : 1 - hu
    const av = hv === 0.5 ? 0 : 1 - hv
    const anchor = this.addTurned(l, au * w, av * h)
    const p = this.toBoard(s)
    const q = rotate({ x: p.x - anchor.x, y: p.y - anchor.y }, -(l.rotation || 0))
    let nw = side === 'y' ? w : hu === 1 ? q.x : -q.x
    let nh = side === 'x' ? h : hv === 1 ? q.y : -q.y
    const uniform = corner && (l.type === 'text' || !shift)
    if (uniform) {
      // Project the pointer onto the diagonal, so the corner follows the hand.
      const dxv = hu === 1 ? w : -w
      const dyv = hv === 1 ? h : -h
      const k = (q.x * dxv + q.y * dyv) / (dxv * dxv + dyv * dyv)
      nw = w * k
      nh = h * k
    }
    // Snap the edge that moves (only when the layer is not turned).
    this.guides = []
    if (Math.round(l.rotation || 0) % 360 === 0) {
      const { xs, ys } = this.snapLines(new Set([l.id]))
      const reach = SNAP_PX / this.z
      const ex = side === 'y' ? null : hu === 1 ? anchor.x + nw : anchor.x - nw
      const ey = side === 'x' ? null : hv === 1 ? anchor.y + nh : anchor.y - nh
      const bx = ex === null ? null : nearest(xs, [ex], reach)
      const by = ey === null ? null : nearest(ys, [ey], reach)
      const pickX = bx && (!by || Math.abs(bx.d) <= Math.abs(by.d))
      if (uniform) {
        const best = pickX ? bx : by
        if (best) {
          const k = pickX ? (nw + (hu === 1 ? best.d : -best.d)) / nw : (nh + (hv === 1 ? best.d : -best.d)) / nh
          if (k > 0) {
            nw *= k
            nh *= k
            this.guides.push(pickX ? { x: best.line } : { y: best.line })
          }
        }
      } else {
        if (bx) {
          nw += hu === 1 ? bx.d : -bx.d
          this.guides.push({ x: bx.line })
        }
        if (by) {
          nh += hv === 1 ? by.d : -by.d
          this.guides.push({ y: by.line })
        }
      }
    }
    const minW = Math.max(8, f.strokeWidth + 1)
    nw = Math.max(minW, nw)
    nh = Math.max(minW, nh)

    const ghost: Ghost = {}
    const sw = f.strokeWidth
    if (l.type === 'text') {
      const st = (l.style ?? {}) as Record<string, any>
      if (corner) {
        const k = nw / w
        ghost.width = Math.max(8, l.width * k)
        ghost.style = { fontSize: Math.max(1, Math.round((Number(st.fontSize) || 40) * k)) }
        nh = h * k
      } else {
        ghost.width = Math.max(8, l.width + (nw - w))
      }
    } else if (l.type === 'shape') {
      ghost.width = Math.max(1, nw - sw)
      if (l.shape?.kind !== 'line') ghost.height = Math.max(1, nh - sw)
    } else {
      ghost.width = nw
      ghost.height = nh
    }
    const tl = this.addTurned({ x: anchor.x, y: anchor.y, rotation: l.rotation }, -au * nw, -av * nh)
    ghost.x = tl.x
    ghost.y = tl.y
    this.ghosts.set(l.id, ghost)
    this.gaps = []
    this.render()
  }

  /** The rotate knob: turns about the centre; shift steps by 15°, and it clicks into the right angles. */
  private onRotate(g: Extract<Gesture, { kind: 'rotate' }>, p: Pt, shift: boolean) {
    const a = Math.atan2(p.y - g.centre.y, p.x - g.centre.x)
    let deg = (g.layer.rotation || 0) + ((a - g.start) * 180) / Math.PI
    deg = ((deg % 360) + 360) % 360
    if (shift) deg = Math.round(deg / 15) * 15
    else {
      const right = Math.round(deg / 90) * 90
      if (Math.abs(deg - right) < 4) deg = right
    }
    deg = Math.round(deg) % 360
    g.moved = true
    const { w, h } = boxSize(g.frame)
    const off = rotate({ x: w / 2, y: h / 2 }, deg)
    this.ghosts.set(g.layer.id, { rotation: deg, x: g.centre.x - off.x, y: g.centre.y - off.y })
    this.render()
  }
}

/**
 * The distances from a box to its nearest neighbour on each side (a layer
 * overlapping it on the other axis), else to the page's edge.
 */
function gapsOf(b: Rect, boxes: Rect[], page: { width: number; height: number }): Gap[] {
  const overlapsY = (o: Rect) => o.top < b.bottom && o.bottom > b.top
  const overlapsX = (o: Rect) => o.left < b.right && o.right > b.left
  const cx = (b.left + b.right) / 2
  const cy = (b.top + b.bottom) / 2
  const out: Gap[] = []
  const leftN = boxes.filter((o) => overlapsY(o) && o.right <= b.left).reduce((m, o) => Math.max(m, o.right), 0)
  const rightN = boxes.filter((o) => overlapsY(o) && o.left >= b.right).reduce((m, o) => Math.min(m, o.left), page.width)
  const topN = boxes.filter((o) => overlapsX(o) && o.bottom <= b.top).reduce((m, o) => Math.max(m, o.bottom), 0)
  const bottomN = boxes.filter((o) => overlapsX(o) && o.top >= b.bottom).reduce((m, o) => Math.min(m, o.top), page.height)
  if (b.left > leftN) out.push({ axis: 'x', from: leftN, to: b.left, at: cy })
  if (rightN > b.right) out.push({ axis: 'x', from: b.right, to: rightN, at: cy })
  if (b.top > topN) out.push({ axis: 'y', from: topN, to: b.top, at: cx })
  if (bottomN > b.bottom) out.push({ axis: 'y', from: b.bottom, to: bottomN, at: cx })
  return out
}

/** Short human summary of a layer for the panel. */
export function previewOf(l: Layer): string {
  if (l.type === 'text') return String(l.text ?? '').replace(/\s+/g, ' ').slice(0, 40)
  if (l.type === 'image') {
    const url = String(l.image?.url ?? '')
    return url.length > 48 ? '…' + url.slice(-47) : url
  }
  if (l.type === 'group') return `${l.children?.length ?? 0} 个图层`
  return `${l.shape?.kind ?? 'rect'} ${(l.style as any)?.fill ?? '-'}`
}
