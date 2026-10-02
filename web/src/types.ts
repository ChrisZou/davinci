/**
 * The davinci document model. This is the authoritative shape shared by the
 * renderer, the command layer and the server: the server stores documents as
 * opaque JSON, the browser is what gives them meaning.
 */

export type LayerType = 'text' | 'image' | 'shape' | 'group'

export interface TextStyle {
  fontFamily?: string
  fontSize?: number
  fontWeight?: string | number
  fontStyle?: 'normal' | 'italic'
  fill?: string
  textAlign?: 'left' | 'center' | 'right'
  lineHeight?: number
  charSpacing?: number
  /** Textbox only: shadow as [color, blur, offsetX, offsetY]. */
  shadow?: string
  /** Textbox only: colour painted behind the glyphs. */
  textBackgroundColor?: string
  /** Textbox only: breathing room around the (multi-line) text block. */
  padding?: number
  /** Outline, as "colour:width". */
  stroke?: string
  /** Paint the stroke under the fill instead of over it. */
  paintFirst?: boolean
  /** Underline / strike are part of the text decorations. */
  underline?: boolean
  linethrough?: boolean
  /**
   * Slant in degrees; positive leans the tops to the right like italic type,
   * but by any amount (Chinese faces have no italic of their own).
   */
  skew?: number
  /** Horizontal scale of the glyphs: 0.8 condenses, 1.2 widens. */
  stretch?: number
  /** Envelope warp (稿定's 变形): arch, flag, bulge, taperRight … see editor/warp.ts. */
  warp?: string
  /** Warp strength, −100…100 (negative flips the shape). */
  warpAmount?: number
  /** Warp relative height, −100…100: −100 keeps the bottom edge straight, 100 the top. */
  warpBias?: number
}

export interface ImageStyle {
  /** Rounded corners, in canvas units. */
  cornerRadius?: number
  flipX?: boolean
  flipY?: boolean
  /** Fabric filter names → values, e.g. { brightness: 0.1, blur: 0.4 }. */
  filters?: Record<string, number>
}

export interface ShapeStyle {
  fill?: string
  stroke?: string
  strokeWidth?: number
  cornerRadius?: number
  /** Line shapes only: solid or dashed. */
  lineStyle?: 'solid' | 'dashed'
  /** Line shapes only: arrowheads — none, at the right end, or at both ends. */
  arrow?: 'none' | 'end' | 'both'
}

export interface Layer {
  id: string
  /** Human-facing name; unique per document (duplicates get a suffix). */
  name: string
  type: LayerType
  x: number
  y: number
  width: number
  height: number
  rotation: number
  opacity: number
  visible: boolean
  locked: boolean
  /** Top-left origin, like Fabric's default. */
  originX?: 'left' | 'center'
  originY?: 'top' | 'center'

  text?: string
  style?: TextStyle & ShapeStyle & ImageStyle
  image?: {
    url: string
    sha?: string
    originalWidth?: number
    originalHeight?: number
    /** The visible part of the image, in its natural pixels. Absent = uncropped. */
    crop?: { x: number; y: number; width: number; height: number }
    /** The picture before its background was removed (removeBackground). */
    original?: string
  }
  /**
   * Group only: the members, back-to-front. Their x/y are relative to the
   * group's own top-left corner and their sizes are the unscaled ones — the
   * group's width/height (vs. the members' bounding box) is what scales them.
   */
  children?: Layer[]
  shape?: {
    kind: 'rect' | 'ellipse' | 'triangle' | 'line'
  }
}

export interface Document {
  version: number
  canvas: {
    width: number
    height: number
    background?: string
    backgroundImage?: string
  }
  layers: Layer[]
}

/** A layer as reported to AI: derived state, never the live Fabric object. */
export interface LayerRow {
  id: string
  name: string
  type: LayerType
  x: number
  y: number
  width: number
  height: number
  rotation: number
  opacity: number
  visible: boolean
  locked: boolean
  /** Short human summary: the text, or the image url tail, or the fill colour. */
  preview: string
  /** Index in the layers array (0 = back). */
  index: number
}

export const DOC_VERSION = 1

export function blankDocument(width: number, height: number): Document {
  return {
    version: DOC_VERSION,
    canvas: { width, height, background: '#ffffff' },
    layers: [],
  }
}

let counter = 0

/** Short, sortable, collision-resistant ids that read well in a terminal. */
export function newID(prefix: string): string {
  counter += 1
  const rand = Math.random().toString(36).slice(2, 6)
  return `${prefix}_${Date.now().toString(36).slice(-4)}${counter.toString(36)}${rand}`
}

export function findLayer(doc: Document, ref: string): Layer | undefined {
  return doc.layers.find((l) => l.id === ref) ?? doc.layers.find((l) => l.name === ref)
}

/** Names must be unique for `id or name` lookups to stay unambiguous. */
export function uniqueName(doc: Document, base: string): string {
  const taken = new Set(doc.layers.map((l) => l.name))
  if (!taken.has(base)) return base
  for (let i = 2; i < 500; i++) {
    const name = `${base} ${i}`
    if (!taken.has(name)) return name
  }
  return `${base} ${doc.layers.length + 1}`
}

export function clamp(n: number, lo: number, hi: number): number {
  return Math.min(hi, Math.max(lo, n))
}

// --- boards ----------------------------------------------------------------

/**
 * One page of a project. A project holds any number of boards (稿定's 多画板):
 * a layered design next to its reference image, a cover in three sizes. A
 * board is exactly what a single-page document used to be — its own canvas
 * and layers — plus an id and a name.
 */
export interface Board {
  id: string
  name: string
  canvas: Document['canvas']
  layers: Layer[]
}

/**
 * What the server stores for a project. `active` is the board a human last
 * looked at; commands that name no board act on it, so the CLI and the page
 * agree on "the current board". The first board is the project's face: its
 * size and thumbnail are what the home page shows.
 */
export interface ProjectDoc {
  version: 2
  active?: string
  boards: Board[]
}

export const PROJECT_VERSION = 2

export function newBoard(width: number, height: number, name: string): Board {
  return { id: newID('board'), name, canvas: { width, height, background: '#ffffff' }, layers: [] }
}

/**
 * Accepts anything the server may hold — a v2 project, or a v1 single-page
 * document from before boards existed — and returns a well-formed project.
 */
export function toProject(raw: unknown): ProjectDoc {
  const r = (raw && typeof raw === 'object' ? raw : {}) as any
  if (Array.isArray(r.boards) && r.boards.length) {
    const boards: Board[] = r.boards.map((b: any, i: number) => ({
      id: typeof b?.id === 'string' && b.id ? b.id : newID('board'),
      name: typeof b?.name === 'string' && b.name ? b.name : `画板 ${i + 1}`,
      canvas: b?.canvas ?? { width: 1242, height: 1656, background: '#ffffff' },
      layers: Array.isArray(b?.layers) ? b.layers : [],
    }))
    const active = boards.some((b) => b.id === r.active) ? r.active : boards[0].id
    return { version: 2, active, boards }
  }
  const board: Board = {
    id: newID('board'),
    name: '画板 1',
    canvas: r.canvas ?? { width: 1242, height: 1656, background: '#ffffff' },
    layers: Array.isArray(r.layers) ? r.layers : [],
  }
  return { version: 2, active: board.id, boards: [board] }
}

/** The single-page document the canvas edits, for one board. */
export function boardDocument(b: Board): Document {
  return { version: DOC_VERSION, canvas: b.canvas, layers: b.layers }
}

/**
 * Finds a board by id, by name, or by its 1-based position ("2" is the second
 * board — what a person reading "画板 2/5" would type).
 */
export function findBoard(p: ProjectDoc, ref: unknown): Board | undefined {
  const key = String(ref ?? '').trim()
  if (!key) return undefined
  const byID = p.boards.find((b) => b.id === key) ?? p.boards.find((b) => b.name === key)
  if (byID) return byID
  if (/^\d+$/.test(key)) return p.boards[Number(key) - 1]
  return undefined
}

export function uniqueBoardName(p: ProjectDoc, base: string): string {
  const taken = new Set(p.boards.map((b) => b.name))
  if (!taken.has(base)) return base
  for (let i = 2; i < 500; i++) if (!taken.has(`${base} ${i}`)) return `${base} ${i}`
  return `${base} ${p.boards.length + 1}`
}

/** The next "画板 N" nobody has taken yet. */
export function nextBoardName(p: ProjectDoc): string {
  const taken = new Set(p.boards.map((b) => b.name))
  for (let i = p.boards.length + 1; i < 1000; i++) if (!taken.has(`画板 ${i}`)) return `画板 ${i}`
  return uniqueBoardName(p, '画板')
}
