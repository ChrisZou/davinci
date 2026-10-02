import type { Document, Layer, ProjectDoc } from '../types'
import { toProject } from '../types'
import { Editor } from './Editor'

/**
 * The bridge between an editor page and the davinci server.
 *
 * The server owns the document. Every edit — a panel button, a drag on the
 * canvas, a shortcut — is a command sent over the socket, exactly like the
 * CLI's; the server applies it, saves, and pushes the new document to every
 * page showing the project, this one included. The page only draws:
 *
 *   page    →  {"type":"command","reqId":"r1","command":{…}}
 *   server  →  {"type":"doc","document":…,"revision":n,"origin":…,"command":…}
 *   server  →  {"type":"result","reqId":"r1","ok":true,"data":…,"document":…}
 *
 * Undo is the server's too, so ⌘Z takes back a CLI edit as readily as a drag.
 */

export type SaveState = 'idle' | 'dirty' | 'saving' | 'saved' | 'error'

export interface RunResult {
  ok: boolean
  data?: any
  error?: string
}

export interface SessionCallbacks {
  /** The active board's document changed (the panels read this). */
  onDocument?: (doc: Document) => void
  /** The project changed: boards added, renamed, switched, or edited. */
  onProject?: (project: ProjectDoc) => void
  onSelection?: (ids: string[]) => void
  onEditing?: (id: string | null) => void
  onSaveState?: (state: SaveState) => void
  onLog?: (line: string) => void
  /** The view (zoom/pan) moved. */
  onView?: () => void
  /** Undo / redo became (un)available. */
  onHistory?: () => void
  /** Someone else (the CLI, a skill, another tab) changed the project. */
  onRemoteCommand?: (cmd: unknown) => void
  /** A picture was double-clicked on the canvas. */
  onImageOpen?: (layer: Layer) => void
}

interface Envelope {
  type: string
  reqId?: string
  ok?: boolean
  data?: unknown
  error?: string
  document?: unknown
  revision?: number
  origin?: string
  command?: unknown
  history?: { canUndo: boolean; canRedo: boolean }
  message?: string
}

/** Long enough for the slowest command: removing a big picture's background. */
const RESULT_TIMEOUT_MS = 11 * 60_000

export class Session {
  canUndo = false
  canRedo = false
  project: ProjectDoc | null = null

  private projectID: string
  private cb: SessionCallbacks
  private editorInstance?: Editor
  private socket?: WebSocket
  private revision = 0
  /** This connection's name on the server, to tell our own edits from others'. */
  private origin = ''
  private seq = 0
  private pending = new Map<string, (r: RunResult) => void>()
  private outbox: string[] = []
  private disposed = false
  private firstHello?: () => void

  constructor(projectID: string, cb: SessionCallbacks = {}) {
    this.projectID = projectID
    this.cb = cb
  }

  get editor(): Editor {
    if (!this.editorInstance) throw new Error('session is not open')
    return this.editorInstance
  }

  /** Builds the canvas and waits for the server's copy of the project. */
  async open(container: HTMLElement) {
    this.editorInstance = await Editor.create(container, {
      onSelection: (ids) => this.cb.onSelection?.(ids),
      onEditing: (id) => this.cb.onEditing?.(id),
      onView: () => this.cb.onView?.(),
      onCommit: (cmd) => this.run(cmd),
      onImageOpen: (l) => this.cb.onImageOpen?.(l),
    })
    ;(window as any).davinci = { session: this, editor: this.editorInstance }
    const ready = new Promise<void>((resolve) => (this.firstHello = resolve))
    this.connect()
    await ready
  }

  destroy() {
    this.disposed = true
    this.socket?.close()
    for (const resolve of this.pending.values()) resolve({ ok: false, error: '页面已关闭' })
    this.pending.clear()
    this.editorInstance?.destroy()
  }

  // --- commands -----------------------------------------------------------

  /** Sends a command (or a batch, or JSON text) to the server and waits for its result. */
  run(raw: unknown): Promise<RunResult> {
    let cmd = raw
    if (typeof raw === 'string') {
      try {
        cmd = JSON.parse(raw)
      } catch (e: any) {
        return Promise.resolve({ ok: false, error: `JSON 解析失败：${e?.message ?? e}` })
      }
    }
    if (Array.isArray(cmd)) cmd = { type: 'batch', commands: cmd }
    const reqId = `r${++this.seq}`
    this.cb.onSaveState?.('saving')
    return new Promise<RunResult>((resolve) => {
      const timer = window.setTimeout(() => finish({ ok: false, error: '服务器没有响应' }), RESULT_TIMEOUT_MS)
      const finish = (r: RunResult) => {
        window.clearTimeout(timer)
        if (!this.pending.delete(reqId)) return
        // A slider's preview has done its job once the real value is in.
        this.editorInstance?.endPreview()
        if (!this.pending.size) this.cb.onSaveState?.('saved')
        resolve(r)
      }
      this.pending.set(reqId, finish)
      this.send({ type: 'command', reqId, command: cmd })
    })
  }

  undo(): Promise<RunResult> {
    return this.run({ type: 'undo' })
  }

  redo(): Promise<RunResult> {
    return this.run({ type: 'redo' })
  }

  // --- socket ----------------------------------------------------------

  private connect() {
    if (this.disposed) return
    const proto = location.protocol === 'https:' ? 'wss:' : 'ws:'
    const socket = new WebSocket(`${proto}//${location.host}/ws?project=${encodeURIComponent(this.projectID)}`)
    this.socket = socket
    socket.onopen = () => {
      for (const m of this.outbox.splice(0)) socket.send(m)
    }
    socket.onmessage = (ev) => {
      let env: Envelope
      try {
        env = JSON.parse(String(ev.data))
      } catch {
        return
      }
      this.onMessage(env)
    }
    socket.onclose = () => {
      if (this.disposed) return
      // Whatever was in flight is lost with the connection.
      for (const resolve of [...this.pending.values()]) resolve({ ok: false, error: '和服务器的连接断开了' })
      this.cb.onSaveState?.('error')
      window.setTimeout(() => this.connect(), 1000)
    }
    socket.onerror = () => socket.close()
  }

  private onMessage(env: Envelope) {
    switch (env.type) {
      case 'hello':
        this.origin = env.origin ?? ''
        this.revision = 0
        this.adopt(env)
        this.cb.onSaveState?.('saved')
        this.firstHello?.()
        this.firstHello = undefined
        break
      case 'doc': {
        const fresh = this.adopt(env)
        if (fresh && env.command && env.origin !== this.origin) this.cb.onRemoteCommand?.(env.command)
        break
      }
      case 'result':
        this.adopt(env)
        this.pending.get(env.reqId ?? '')?.(env.ok ? { ok: true, data: env.data } : { ok: false, error: env.error || '命令失败' })
        break
      case 'error':
        this.cb.onLog?.(`server: ${env.message ?? 'error'}`)
        break
    }
  }

  /** Takes a document from the server when it is newer than ours. */
  private adopt(env: Envelope): boolean {
    if (env.history) {
      const u = env.history.canUndo === true
      const r = env.history.canRedo === true
      if (u !== this.canUndo || r !== this.canRedo) {
        this.canUndo = u
        this.canRedo = r
        this.cb.onHistory?.()
      }
    }
    if (!env.document) return false
    const rev = Number(env.revision) || 0
    if (rev && rev < this.revision) return false
    if (rev && rev === this.revision && this.project) return false
    this.revision = rev || this.revision
    this.project = toProject(env.document)
    const ed = this.editor
    ed.setProject(this.project)
    const doc = ed.doc
    if (doc) this.cb.onDocument?.(doc)
    this.cb.onProject?.(this.project)
    return true
  }

  private send(msg: Record<string, unknown>) {
    const text = JSON.stringify(msg)
    if (this.socket?.readyState === WebSocket.OPEN) this.socket.send(text)
    else this.outbox.push(text)
  }
}
