import type { Editor } from './Editor'
import type { Layer } from '../types'

/**
 * Keyboard shortcuts and the clipboard.
 *
 * Every shortcut below does nothing but build a command and hand it to the same
 * `run` the panels use — so a shortcut can never do something AI could not.
 *
 * Copy and paste go through the system clipboard (the `copy`/`paste` events),
 * not a variable in this page: what was copied last wins, whether it was layers
 * in davinci, a screenshot or a line of text in another app. Copied layers are
 * whole layer JSON, so a paste still works after the original is deleted, in
 * another project, or after a reload — and the same JSON can be handed to
 * `davinci exec` as an `insertLayers` command.
 */

export interface RunResult {
  ok: boolean
  data?: any
  error?: string
}

export type Tool = 'text' | 'rect' | 'ellipse' | 'triangle'

/** The four kinds of line the 直线 menu offers. */
export type LineKind = 'solid' | 'dashed' | 'arrow' | 'double'

export interface ShortcutHost {
  /** Runs a command through the session (to the server, which applies it). */
  run(cmd: unknown): Promise<RunResult | undefined>
  /** True while a textbox is being typed into: shortcuts must stand down. */
  isEditing(): boolean
  /** Selects these layers (after a paste, a duplicate, a group). */
  select(ids: string[]): void
  /** Adds a new layer from the toolbar's set. */
  addTool(tool: Tool): void
  /** Uploads image files and adds them as layers. */
  addImageFiles(files: File[]): void
  /** Toggles the shortcut cheat sheet. */
  toggleHelp?(): void
}

export interface Shortcuts {
  dispose(): void
}

/** The marker that tells our own layer JSON apart from any other text. */
const CLIP_KIND = 'davinci/layers'

interface ClipPayload {
  kind: typeof CLIP_KIND
  layers: Layer[]
}

/** Parses clipboard text as copied davinci layers, or returns null. */
export function parseClip(text: string): Layer[] | null {
  const t = text.trim()
  if (!t.startsWith('{')) return null
  try {
    const v = JSON.parse(t) as ClipPayload
    return v?.kind === CLIP_KIND && Array.isArray(v.layers) && v.layers.length ? v.layers : null
  } catch {
    return null
  }
}

/** Copied layers as clipboard text. */
export function clipText(layers: Layer[]): string {
  return JSON.stringify({ kind: CLIP_KIND, layers } satisfies ClipPayload)
}

/** Selected layers as document layers, back-to-front, with absolute positions. */
export function selectedLayers(ed: Editor): Layer[] {
  const ids = new Set(ed.selectionIDs())
  return (ed.doc?.layers ?? []).filter((l) => ids.has(l.id))
}

/** The ids a batch of add-ish commands produced, in order. */
export function producedIDs(data: unknown): string[] {
  const out: string[] = []
  const visit = (d: any) => {
    if (!d) return
    if (Array.isArray(d)) return d.forEach(visit)
    if (Array.isArray(d.ids)) out.push(...d.ids)
    else if (typeof d.id === 'string') out.push(d.id)
  }
  visit(data)
  return out
}

/** One command for a list of per-layer commands: a batch is one undo step. */
function each(ids: string[], make: (id: string) => Record<string, unknown>): unknown {
  return ids.length === 1 ? make(ids[0]) : { type: 'batch', commands: ids.map(make) }
}

export const removeCommand = (ids: string[]) => each(ids, (id) => ({ type: 'removeLayer', id }))
export const duplicateCommand = (ids: string[]) => each(ids, (id) => ({ type: 'duplicateLayer', id }))
export const ungroupCommand = (ids: string[]) => each(ids, (id) => ({ type: 'ungroupLayers', id }))

export type ZMove = 'bringForward' | 'sendBackward' | 'bringToFront' | 'sendToBack'

/**
 * Restacks every selected layer. The order the steps run in is what keeps a
 * multi-selection's own stacking intact: a one-step raise goes front-most
 * first (so no member leapfrogs another), "to front" goes back-most first (so
 * the front-most ends up on top), and the lowering pair mirrors that.
 */
export function zOrderCommand(ed: Editor, ids: string[], type: ZMove): unknown {
  const order = (ed.doc?.layers ?? []).map((l) => l.id).filter((id) => ids.includes(id))
  const steps = type === 'bringForward' || type === 'sendToBack' ? [...order].reverse() : order
  return each(steps, (id) => ({ type, id }))
}

/** Runs a command and selects the layers it produced. */
export async function runAndSelect(host: ShortcutHost, cmd: unknown) {
  const res = await host.run(cmd)
  if (res?.ok) {
    const ids = producedIDs(res.data)
    if (ids.length) host.select(ids)
  }
  return res
}

/**
 * Pastes clipboard content, whichever way it arrived (the paste event, or the
 * async Clipboard API behind the context menu). Returns false when there was
 * nothing davinci could use, so the caller can leave the event alone.
 *
 * An image wins: a screenshot or a picture copied from the browser is the most
 * common thing to paste into a cover. Then our own layers, then plain text,
 * which becomes a text layer.
 */
export function applyPaste(ed: Editor, host: ShortcutHost, content: { files: File[]; text: string }): boolean {
  const images = content.files.filter((f) => f.type.startsWith('image/'))
  if (images.length) {
    host.addImageFiles(images)
    return true
  }
  const text = content.text ?? ''
  const layers = text ? parseClip(text) : null
  if (layers) {
    // Pasting over the originals nudges the copy so it is visibly a copy; in
    // another project (or once the originals are gone) it lands in place.
    const present = layers.every((l) => (ed.doc?.layers ?? []).some((d) => d.id === l.id && d.x === l.x && d.y === l.y))
    void runAndSelect(host, { type: 'insertLayers', layers, offset: present ? 24 : 0 })
    return true
  }
  const plain = text.trim()
  if (plain) {
    void runAndSelect(host, { type: 'addText', text: plain.slice(0, 2000), name: '文字', style: { textAlign: 'center' } })
    return true
  }
  return false
}

export function installShortcuts(ed: Editor, host: ShortcutHost): Shortcuts {
  const isTyping = (target: EventTarget | null) => {
    const el = target as HTMLElement | null
    return !!el && (/^(INPUT|TEXTAREA|SELECT)$/.test(el.tagName) || el.isContentEditable)
  }
  // Clipboard events land on whatever has focus; a panel input keeps its own
  // native copy/paste.
  const standDown = (target: EventTarget | null) =>
    host.isEditing() || isTyping(target) || isTyping(document.activeElement)

  const removeSelected = (ids: string[]) => {
    if (ids.length) void host.run(removeCommand(ids))
  }

  const onKeyDown = (e: KeyboardEvent) => {
    if (standDown(e.target)) return
    const meta = e.metaKey || e.ctrlKey
    const sel = ed.selectionIDs()
    const key = e.key
    const lower = key.toLowerCase()

    // --- zoom & pan ---
    if (key === ' ') {
      // Space pans, but only when it is not the modifier of some other chord.
      e.preventDefault()
      if (!e.repeat) ed.beginPan(lastPoint.x, lastPoint.y)
      return
    }
    if (meta && key === '0') {
      e.preventDefault()
      ed.fitToScreen()
      return
    }
    if (meta && key === '1') {
      e.preventDefault()
      ed.setZoom(1)
      return
    }
    if (meta && (key === '=' || key === '+')) {
      e.preventDefault()
      ed.zoomBy(1.2)
      return
    }
    if (meta && key === '-') {
      e.preventDefault()
      ed.zoomBy(1 / 1.2)
      return
    }

    // --- history ---
    if (meta && lower === 'z') {
      e.preventDefault()
      void host.run({ type: e.shiftKey ? 'redo' : 'undo' })
      return
    }
    if (meta && lower === 'y') {
      e.preventDefault()
      void host.run({ type: 'redo' })
      return
    }

    // --- selection ---
    if (meta && lower === 'a') {
      e.preventDefault()
      host.select(ed.selectableIDs())
      return
    }
    if (meta && lower === 'd' && sel.length) {
      e.preventDefault()
      void runAndSelect(host, duplicateCommand(sel))
      return
    }

    // --- group ---
    if (meta && lower === 'g') {
      e.preventDefault()
      if (e.shiftKey) {
        const groups = selectedLayers(ed).filter((l) => l.type === 'group')
        if (groups.length) void runAndSelect(host, ungroupCommand(groups.map((g) => g.id)))
      } else if (sel.length > 1) {
        void runAndSelect(host, { type: 'groupLayers', ids: sel })
      }
      return
    }

    // --- delete ---
    if ((key === 'Backspace' || key === 'Delete') && sel.length) {
      e.preventDefault()
      removeSelected(sel)
      return
    }

    // --- z-order (⌘] / ⌘[ and ⇧⌘] / ⇧⌘[) ---
    if (meta && (key === ']' || key === '[' || key === '}' || key === '{') && sel.length) {
      e.preventDefault()
      const front = key === ']' || key === '}'
      const type: ZMove = e.shiftKey ? (front ? 'bringToFront' : 'sendToBack') : front ? 'bringForward' : 'sendBackward'
      void host.run(zOrderCommand(ed, sel, type))
      return
    }

    // --- nudge ---
    if (sel.length && (key === 'ArrowLeft' || key === 'ArrowRight' || key === 'ArrowUp' || key === 'ArrowDown')) {
      e.preventDefault()
      const step = e.shiftKey ? 10 : 1
      const dx = key === 'ArrowLeft' ? -step : key === 'ArrowRight' ? step : 0
      const dy = key === 'ArrowUp' ? -step : key === 'ArrowDown' ? step : 0
      const moves = selectedLayers(ed)
        .filter((l) => !l.locked)
        .map((l) => ({ type: 'moveLayer', id: l.id, x: l.x + dx, y: l.y + dy }))
      if (moves.length) void host.run(moves.length === 1 ? moves[0] : { type: 'batch', commands: moves })
      return
    }

    // --- tools (single keys, no modifier) ---
    if (!meta && !e.altKey) {
      const tool: Tool | undefined = ({ t: 'text', r: 'rect', o: 'ellipse' } as Record<string, Tool>)[lower]
      if (tool) {
        e.preventDefault()
        host.addTool(tool)
        return
      }
      if (key === '?') {
        e.preventDefault()
        host.toggleHelp?.()
        return
      }
    }

    // --- escape ---
    if (key === 'Escape') {
      host.select([])
    }
  }

  const onKeyUp = (e: KeyboardEvent) => {
    if (e.key === ' ') ed.endPan()
  }

  const onCopy = (e: ClipboardEvent) => {
    if (standDown(e.target)) return
    const layers = selectedLayers(ed)
    if (!layers.length || !e.clipboardData) return
    e.preventDefault()
    e.clipboardData.setData('text/plain', clipText(layers))
  }

  const onCut = (e: ClipboardEvent) => {
    if (standDown(e.target)) return
    const layers = selectedLayers(ed)
    if (!layers.length || !e.clipboardData) return
    e.preventDefault()
    e.clipboardData.setData('text/plain', clipText(layers))
    removeSelected(layers.map((l) => l.id))
  }

  const onPaste = (e: ClipboardEvent) => {
    if (standDown(e.target)) return
    const data = e.clipboardData
    if (!data) return
    const files = Array.from(data.files ?? [])
    const text = data.getData('text/plain')
    if (applyPaste(ed, host, { files, text })) e.preventDefault()
  }

  window.addEventListener('keydown', onKeyDown)
  window.addEventListener('keyup', onKeyUp)
  document.addEventListener('copy', onCopy)
  document.addEventListener('cut', onCut)
  document.addEventListener('paste', onPaste)

  return {
    dispose() {
      window.removeEventListener('keydown', onKeyDown)
      window.removeEventListener('keyup', onKeyUp)
      document.removeEventListener('copy', onCopy)
      document.removeEventListener('cut', onCut)
      document.removeEventListener('paste', onPaste)
    },
  }
}

/** Where the pointer last was, so space-panning starts under the cursor. */
let lastPoint = { x: 0, y: 0 }

export function trackPointer(e: PointerEvent) {
  lastPoint = { x: e.clientX, y: e.clientY }
}
