import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { api } from '../api'
import type { Document, Layer, LayerRow, ProjectDoc } from '../types'
import { navigate } from '../App'
import { Session, type SaveState } from '../editor/bridge'
import { loadFonts } from '../editor/fonts'
import {
  applyPaste,
  clipText,
  duplicateCommand,
  installShortcuts,
  removeCommand,
  runAndSelect,
  selectedLayers,
  ungroupCommand,
  zOrderCommand,
  type RunResult,
  type ShortcutHost,
  type LineKind,
  type Tool,
  type ZMove,
} from '../editor/keyboard'
import { Properties } from '../components/Properties'
import { LayerPanel } from '../components/LayerPanel'
import { ExportMenu } from '../components/ExportMenu'
import { CropDialog } from '../components/CropDialog'
import { Modal, modalOpen } from '../components/Dialog'
import { LibraryPanel } from '../components/LibraryPanel'
import { Mark } from '../components/Brand'
import { Filmstrip, filmstripInset, useFilmstripOpen } from '../components/Filmstrip'
import { LIBRARY_DRAG_TYPE } from '../components/LibraryParts'
import type { LibraryItem } from '../api'

/**
 * The editor page.
 *
 * It is deliberately thin: the server owns the document, and every edit —
 * a panel button, a drag on the canvas, a shortcut — is a command handed to
 * `session.run()`, which sends it to the server exactly as the CLI would. The
 * panels only *describe* commands; the canvas only draws what comes back.
 *
 * Layout: three floating islands along the top (project, the add tools, undo
 * and export), the layer card on the left, the properties card on the right,
 * and the canvas on the paper between them.
 */

/** The one-line notice at the bottom of the stage. */
interface Notice {
  kind: 'remote' | 'error' | 'info'
  text: string
  /** Offer to take the change back (a command-line edit that just landed). */
  undo?: boolean
}

interface MenuState {
  x: number
  y: number
  /** The layer right-clicked, or null for empty canvas. */
  target: string | null
}

const DEV_KEY = 'davinci.devDrawer'

export function Editor({ projectID }: { projectID: string }) {
  const stageRef = useRef<HTMLDivElement>(null)
  const sessionRef = useRef<Session | null>(null)

  const [doc, setDoc] = useState<Document | null>(null)
  const [project, setProject] = useState<ProjectDoc | null>(null)
  const [stripOpen, setStripOpen] = useFilmstripOpen()
  const [name, setName] = useState('')
  const [rows, setRows] = useState<LayerRow[]>([])
  const [selected, setSelected] = useState<string[]>([])
  const [editing, setEditing] = useState<string | null>(null)
  const [saveState, setSaveState] = useState<SaveState>('idle')
  const [ready, setReady] = useState(false)
  const [log, setLog] = useState<string[]>([])
  const [error, setError] = useState('')
  const [cmdText, setCmdText] = useState('')
  const [cmdOut, setCmdOut] = useState('')
  const [zoom, setZoom] = useState(100)
  const [, setHistoryTick] = useState(0)
  const [notice, setNotice] = useState<Notice | null>(null)
  const [cropFor, setCropFor] = useState<Layer | null>(null)
  const [help, setHelp] = useState(false)
  const [menu, setMenu] = useState<MenuState | null>(null)
  const [dropping, setDropping] = useState(false)
  const [libraryOpen, setLibraryOpen] = useState(false)
  /** The image layer the library is open to replace, when it is. */
  const [replaceFor, setReplaceFor] = useState<string | null>(null)
  const [devOpen, setDevOpen] = useState(() => {
    try {
      return localStorage.getItem(DEV_KEY) === '1'
    } catch {
      return false
    }
  })
  // Read by the shortcut handler, which is installed once and must not go stale.
  const editingRef = useRef<string | null>(null)
  editingRef.current = editing
  const menuRef = useRef<MenuState | null>(null)
  menuRef.current = menu
  const noticeTimer = useRef<number | undefined>(undefined)

  const notify = useCallback((n: Notice | null, ms = 5000) => {
    window.clearTimeout(noticeTimer.current)
    setNotice(n)
    if (n) noticeTimer.current = window.setTimeout(() => setNotice(null), ms)
  }, [])

  const addLog = useCallback(
    (line: string) => {
      setLog((prev) => [...prev.slice(-40), line])
      // Failures (and confirmations like "copied") matter to the human even with
      // the developer drawer closed.
      if (line.startsWith('✗')) notify({ kind: 'error', text: line.replace(/^✗\s*/, '') })
      else if (line.startsWith('✓')) notify({ kind: 'info', text: line.replace(/^✓\s*/, '') }, 2500)
    },
    [notify],
  )

  useEffect(() => {
    let alive = true
    void (async () => {
      try {
        // Fonts must be registered before the canvas measures any text.
        await loadFonts().catch(() => {})
        const p = await api.project(projectID)
        if (!alive) return
        setName(p.name)
        document.title = `${p.name} · davinci`

        const session = new Session(projectID, {
          onDocument: (d) => {
            setDoc(d)
            setRows(session.editor.layerRows())
          },
          onProject: setProject,
          onSelection: setSelected,
          onEditing: setEditing,
          onSaveState: setSaveState,
          onLog: addLog,
          onView: () => setZoom(Math.round(session.editor.zoom * 100)),
          onHistory: () => setHistoryTick((t) => t + 1),
          // An edit from the command line (a skill, the CLI) lands live on this
          // page; say what it touched and offer to take it back — the one place
          // the page mentions that anyone else is working on the design.
          onRemoteCommand: (cmd) => {
            const type = (cmd as any)?.type
            if (type === 'undo' || type === 'redo') return
            notify({ kind: 'remote', text: describeRemote(cmd, session.editor.doc), undo: true }, 6000)
          },
          // Double-clicking a picture opens the crop dialog.
          onImageOpen: (l) => {
            if (!l.locked) setCropFor(l)
          },
        })
        sessionRef.current = session
        const stage = stageRef.current
        if (!stage) throw new Error('canvas container never rendered')
        await session.open(stage)
        setRows(session.editor.layerRows())
        setZoom(Math.round(session.editor.zoom * 100))
        if (alive) setReady(true)
      } catch (e: any) {
        if (alive) setError(e?.message ?? String(e))
      }
    })()
    return () => {
      alive = false
      sessionRef.current?.destroy()
      sessionRef.current = null
    }
  }, [projectID, addLog, notify])

  // The board strip takes room under the page; fit-to-screen keeps clear of it.
  useEffect(() => {
    if (!ready) return
    sessionRef.current?.editor.setInsetBottom(filmstripInset(stripOpen))
  }, [ready, stripOpen])

  useEffect(() => {
    try {
      localStorage.setItem(DEV_KEY, devOpen ? '1' : '0')
    } catch {
      // A private window may refuse storage; the drawer just forgets.
    }
  }, [devOpen])

  // --- commands ---------------------------------------------------------

  const run = async (cmd: unknown, quiet = false): Promise<RunResult | undefined> => {
    const s = sessionRef.current
    if (!s) return
    const res = await s.run(cmd)
    setCmdOut(JSON.stringify(res.ok ? res.data ?? { ok: true } : { error: res.error }, null, 2))
    if (!res.ok && !quiet) addLog(`✗ ${res.error}`)
    return res
  }

  const select = (ids: string[]) => {
    const ed = sessionRef.current?.editor
    if (!ed) return
    ed.selectMany(ids)
    setSelected(ed.selectionIDs())
  }

  /**
   * Uploads a file for the panels that need a binary in (a background image, a
   * replacement photo). Returns null rather than throwing so a panel can just
   * drop the command it was about to run.
   */
  const upload = async (file: File): Promise<string | null> => {
    try {
      return await api.uploadAsset(file)
    } catch (e: any) {
      addLog(`✗ 上传失败：${e?.message ?? e}`)
      return null
    }
  }

  /**
   * Adds dropped, pasted or picked image files as layers — one undo step for
   * the lot. Each is fitted inside the canvas (a 4000px photo on a 1242px cover
   * is never what anyone wants) and centred on `at`, a screen point, or on the
   * canvas centre.
   */
  const addImageFiles = async (files: File[], at?: { x: number; y: number }) => {
    const ed = sessionRef.current?.editor
    const d = ed?.doc
    if (!ed || !d || !files.length) return
    const centre = at ? ed.screenToCanvas(at.x, at.y) : { x: d.canvas.width / 2, y: d.canvas.height / 2 }
    notify({ kind: 'info', text: `正在上传 ${files.length} 张图片…` }, 20000)
    try {
      const cmds = await Promise.all(
        files.map(async (f, i) => {
          const [url, size] = await Promise.all([api.uploadAsset(f), naturalSize(f)])
          const fit = Math.min(1, (d.canvas.width * 0.8) / size.w, (d.canvas.height * 0.8) / size.h)
          const width = Math.max(1, Math.round(size.w * fit))
          const height = Math.max(1, Math.round(size.h * fit))
          return {
            type: 'addImage',
            url,
            x: Math.round(centre.x - width / 2 + i * 24),
            y: Math.round(centre.y - height / 2 + i * 24),
            width,
            height,
            name: imageName(f.name),
          }
        }),
      )
      await runAndSelect(host, cmds.length === 1 ? cmds[0] : { type: 'batch', commands: cmds })
      notify(null)
    } catch (e: any) {
      addLog(`✗ 上传失败：${e?.message ?? e}`)
    }
  }

  /**
   * Adds a picture from the material library. It is fitted inside the canvas;
   * dropped at a point it centres there, otherwise a portrait stands on the
   * bottom edge (how a cover uses one) and anything else sits in the middle.
   */
  const insertLibraryItem = async (item: LibraryItem, at?: { x: number; y: number }) => {
    const ed = sessionRef.current?.editor
    const d = ed?.doc
    if (!ed || !d) return
    const cmd: Record<string, unknown> = { type: 'addImage', url: item.url, name: item.name }
    if (item.width > 0 && item.height > 0) {
      const k = Math.min(1, (d.canvas.width * 0.8) / item.width, (d.canvas.height * 0.8) / item.height)
      const width = Math.round(item.width * k)
      const height = Math.round(item.height * k)
      const centre = at ? ed.screenToCanvas(at.x, at.y) : null
      Object.assign(cmd, {
        width,
        height,
        x: Math.round(centre ? centre.x - width / 2 : (d.canvas.width - width) / 2),
        y: Math.round(centre ? centre.y - height / 2 : item.category === '人像' ? d.canvas.height - height : (d.canvas.height - height) / 2),
      })
    }
    await runAndSelect(host, cmd)
  }

  /** Adds a line across the middle of the canvas from the 直线 menu. */
  const addLine = async (kind: LineKind) => {
    const d = sessionRef.current?.editor.doc
    if (!d) return
    const w = Math.round(d.canvas.width * 0.5)
    const n = d.layers.length % 6
    await runAndSelect(host, {
      type: 'addShape',
      kind: 'line',
      name: LINES.find((l) => l.kind === kind)?.label,
      x: Math.round((d.canvas.width - w) / 2) + n * 24,
      y: Math.round(d.canvas.height / 2) + n * 24,
      width: w,
      stroke: '#1c1b18',
      strokeWidth: Math.max(4, Math.round(Math.min(d.canvas.width, d.canvas.height) / 200)),
      lineStyle: kind === 'dashed' ? 'dashed' : 'solid',
      arrow: kind === 'arrow' ? 'end' : kind === 'double' ? 'both' : 'none',
    })
  }

  /** Adds a new layer from the tool dock. A new text box opens for typing. */
  const addTool = async (tool: Tool) => {
    const d = sessionRef.current?.editor.doc
    if (!d) return
    if (tool === 'text') {
      const res = await run({ type: 'addText', text: '输入文字', name: '文字', style: { textAlign: 'center' } })
      const id = res?.ok ? (res.data as any)?.id : undefined
      if (id) sessionRef.current?.editor.editText(id)
      return
    }
    const side = Math.round(Math.min(d.canvas.width, d.canvas.height) * 0.4)
    const w = tool === 'rect' ? Math.round(side * 1.4) : side
    const h = side
    const n = d.layers.length % 6
    const fill = { rect: '#c23a22', ellipse: '#2f4a43', triangle: '#f2c230' }[tool]
    await runAndSelect(host, {
      type: 'addShape',
      kind: tool,
      x: Math.round((d.canvas.width - w) / 2) + n * 24,
      y: Math.round((d.canvas.height - h) / 2) + n * 24,
      width: w,
      height: h,
      fill,
    })
  }

  // The same host serves the shortcuts, the context menu and the panels.
  const host: ShortcutHost = {
    run: (cmd) => run(cmd),
    isEditing: () => editingRef.current !== null || modalOpen() || menuRef.current !== null,
    select,
    addTool: (t) => void addTool(t),
    addImageFiles: (files) => void addImageFiles(files),
    toggleHelp: () => setHelp((v) => !v),
  }
  const hostRef = useRef(host)
  hostRef.current = host

  // Shortcuts only build commands and hand them to run(), so they can never
  // outpace what AI can do.
  useEffect(() => {
    if (!ready) return
    const s = sessionRef.current
    if (!s) return
    const ed = s.editor
    // The host is read through a ref at call time; re-installing on every render
    // would tear the listeners down mid-gesture.
    const keys = installShortcuts(ed, {
      run: (cmd) => hostRef.current.run(cmd),
      isEditing: () => hostRef.current.isEditing(),
      select: (ids) => hostRef.current.select(ids),
      addTool: (t) => hostRef.current.addTool(t),
      addImageFiles: (f) => hostRef.current.addImageFiles(f),
      toggleHelp: () => hostRef.current.toggleHelp?.(),
    })
    return () => keys.dispose()
  }, [ready])

  const runJSON = async () => {
    const s = sessionRef.current
    if (!s) return
    let parsed: unknown
    try {
      parsed = JSON.parse(cmdText)
    } catch (e: any) {
      setCmdOut(`JSON 解析失败：${e?.message ?? e}`)
      return
    }
    const cmds = Array.isArray(parsed) ? parsed : [parsed]
    const results = []
    for (const c of cmds) results.push(await s.run(c))
    if (results.some((r) => !r.ok)) addLog(`✗ ${results.find((r) => !r.ok)?.error}`)
    setCmdOut(
      JSON.stringify(
        results.length === 1 ? results[0].ok ? results[0].data ?? { ok: true } : { error: results[0].error } : results.map((r) => (r.ok ? r.data ?? { ok: true } : { error: r.error })),
        null,
        2,
      ),
    )
  }

  const selection = useMemo(() => (doc ? doc.layers.filter((l) => selected.includes(l.id)) : []), [doc, selected])
  const layer = selection.length === 1 ? selection[0] : undefined

  if (error) {
    return (
      <div className="flex h-full items-center justify-center bg-paper p-6">
        <div className="card flex max-w-md flex-col gap-4 p-8">
          <h1 className="font-serif text-xl font-black">打不开这个项目</h1>
          <p className="text-sm leading-relaxed text-muted">{error}</p>
          <button className="btn-ghost self-start" onClick={() => navigate('/')}>
            返回首页
          </button>
        </div>
      </div>
    )
  }

  // The stage element has to exist on the very first render: the session mounts
  // the canvas into it before any document state exists, and unmounting it later
  // would throw that canvas away. So the chrome comes and goes *around* it —
  // never the other way round.
  const showPanels = doc !== null
  const session = sessionRef.current

  return (
    <div className="flex h-full flex-col bg-paper">
      <header className="relative h-[84px] shrink-0">
        {doc && session && (
          <>
            <div className="island absolute left-5 top-4 flex h-[52px] items-center gap-2.5 pl-2 pr-4">
              <a
                href="/"
                className="icon-btn"
                title="返回首页"
                aria-label="返回首页"
                onClick={(e) => {
                  e.preventDefault()
                  navigate('/')
                }}
              >
                <Mark size={24} />
              </a>
              <ProjectName projectID={projectID} name={name} onLog={addLog} onRenamed={(n) => {
                setName(n)
                document.title = `${n} · davinci`
              }} />
              <SaveDot state={saveState} />
            </div>

            <ToolDock
              onTool={(t) => void addTool(t)}
              onLine={(k) => void addLine(k)}
              onFiles={(files) => void addImageFiles(files)}
              libraryOpen={libraryOpen}
              onToggleLibrary={() => setLibraryOpen((v) => !v)}
            />

            <div className="island absolute right-5 top-4 flex h-[52px] items-center gap-0.5 pl-1.5 pr-2">
              <button className="icon-btn" disabled={!session.canUndo} title="撤销 ⌘Z" aria-label="撤销" onClick={() => void run({ type: 'undo' })}>
                <Icon d="M9 14 4 9l5-5M4 9h11a5 5 0 0 1 0 10h-3" />
              </button>
              <button className="icon-btn" disabled={!session.canRedo} title="重做 ⇧⌘Z" aria-label="重做" onClick={() => void run({ type: 'redo' })}>
                <Icon d="m15 14 5-5-5-5M20 9H9a5 5 0 0 0 0 10h3" />
              </button>
              <ExportMenu doc={doc} name={name} editor={session.editor} selected={selected} onLog={addLog} />
            </div>
          </>
        )}
      </header>

      <div className="flex min-h-0 flex-1 px-5 pb-5">
        {showPanels && (
          <aside className="card flex w-[252px] shrink-0 flex-col overflow-hidden">
            <LayerPanel rows={rows} doc={doc} selected={selected} onSelect={select} onRun={run} />
            <DevDrawer open={devOpen} onToggle={() => setDevOpen((v) => !v)} errors={log.filter((l) => l.startsWith('✗')).length}>
              <Console cmdText={cmdText} setCmdText={setCmdText} out={cmdOut} onRun={runJSON} />
              <Log lines={log} />
            </DevDrawer>
          </aside>
        )}

        <div
          className="relative min-w-0 flex-1"
          ref={stageRef}
          data-testid="stage"
          onContextMenu={(e) => {
            if (!showPanels || !session) return
            e.preventDefault()
            const id = session.editor.layerAt(e.nativeEvent)
            if (id && !session.editor.selectionIDs().includes(id)) select([id])
            if (!id) select([])
            setMenu({ x: e.clientX, y: e.clientY, target: id })
          }}
          onDragOver={(e) => {
            const types = Array.from(e.dataTransfer.types)
            if (!showPanels || !(types.includes('Files') || types.includes(LIBRARY_DRAG_TYPE))) return
            e.preventDefault()
            e.dataTransfer.dropEffect = 'copy'
            if (!dropping) setDropping(true)
          }}
          onDragLeave={(e) => {
            if (!e.currentTarget.contains(e.relatedTarget as Node)) setDropping(false)
          }}
          onDrop={(e) => {
            if (!showPanels) return
            e.preventDefault()
            setDropping(false)
            // A picture dragged out of the material panel.
            const fromLibrary = e.dataTransfer.getData(LIBRARY_DRAG_TYPE)
            if (fromLibrary) {
              try {
                void insertLibraryItem(JSON.parse(fromLibrary) as LibraryItem, { x: e.clientX, y: e.clientY })
              } catch {
                addLog('✗ 没能读出拖进来的素材')
              }
              return
            }
            const files = Array.from(e.dataTransfer.files).filter((f) => f.type.startsWith('image/'))
            if (files.length) void addImageFiles(files, { x: e.clientX, y: e.clientY })
            else addLog('✗ 只能拖入图片文件')
          }}
        >
          {showPanels && session && <ZoomPill zoom={zoom} editor={session.editor} onHelp={() => setHelp(true)} />}
          {showPanels && project && <Filmstrip project={project} open={stripOpen} onOpen={setStripOpen} onRun={(cmd) => void run(cmd)} />}
          {dropping && (
            <div className="pointer-events-none absolute inset-4 z-10 flex items-center justify-center rounded-3xl border-2 border-dashed border-accent/60 bg-accent-soft/60 font-serif text-lg font-black text-accent">
              松开鼠标，把图片放到这里
            </div>
          )}
          {notice && (
            <div
              role="status"
              className="island absolute left-1/2 top-4 z-20 flex h-10 max-w-[80%] -translate-x-1/2 items-center gap-2.5 rounded-full pl-4 pr-1.5"
              data-testid="toast"
            >
              <span className={`h-1.5 w-1.5 shrink-0 rounded-full ${notice.kind === 'info' ? 'bg-ok' : 'bg-accent'}`} />
              <span className={`truncate text-[13px] ${notice.kind === 'error' ? 'text-accent' : 'text-ink-2'}`}>{notice.text}</span>
              {notice.undo ? (
                <button
                  className="h-7 shrink-0 rounded-full bg-paper px-3 text-xs font-bold text-accent hover:bg-paper-deep"
                  onClick={() => {
                    notify(null)
                    void run({ type: 'undo' })
                  }}
                >
                  撤回
                </button>
              ) : (
                <button className="icon-btn h-7 w-7 shrink-0 text-faint" aria-label="关闭提示" onClick={() => notify(null)}>
                  <Icon d="M6 6l12 12M18 6 6 18" size={13} />
                </button>
              )}
            </div>
          )}
        </div>

        {showPanels && (
          <aside className="card flex w-[280px] shrink-0 flex-col overflow-hidden">
            <Properties
              doc={doc}
              layer={layer}
              selection={selection}
              onRun={run}
              onRunSelect={(cmd) => void runAndSelect(host, cmd)}
              onUpload={upload}
              onPickFromLibrary={(l) => {
                setLibraryOpen(false)
                setReplaceFor(l.id)
              }}
              onCrop={setCropFor}
              onPreview={(id, key, value) => session?.editor.preview(id, key, value)}
            />
          </aside>
        )}
      </div>

      {replaceFor && showPanels ? (
        <LibraryPanel
          mode="replace"
          onInsert={(it) => {
            const id = replaceFor
            setReplaceFor(null)
            void run({ type: 'replaceImage', id, url: it.url })
          }}
          onClose={() => setReplaceFor(null)}
        />
      ) : (
        libraryOpen && showPanels && <LibraryPanel onInsert={(it) => void insertLibraryItem(it)} onClose={() => setLibraryOpen(false)} />
      )}
      {menu && session && (
        <ContextMenu menu={menu} editor={session.editor} doc={doc} host={host} onCrop={setCropFor} onLog={addLog} onClose={() => setMenu(null)} />
      )}
      {cropFor && doc && (
        <CropDialog
          layer={cropFor}
          canvasRatio={doc.canvas.width / doc.canvas.height}
          onClose={() => setCropFor(null)}
          onApply={(crop) => {
            const id = cropFor.id
            setCropFor(null)
            void run(crop ? { type: 'cropImage', id, ...crop } : { type: 'cropImage', id, reset: true })
          }}
        />
      )}
      {help && <HelpDialog onClose={() => setHelp(false)} />}
    </div>
  )
}

/** What a command from the command line did, in a few words. */
function describeRemote(raw: unknown, doc: Document | null): string {
  let cmd: any = raw
  if (typeof raw === 'string') {
    try {
      cmd = JSON.parse(raw)
    } catch {
      return '命令行刚改了画布'
    }
  }
  const nameOf = (ref: unknown) => doc?.layers.find((l) => l.id === ref || l.name === ref)?.name ?? String(ref)
  if (cmd?.type === 'batch') return `命令行刚做了 ${Array.isArray(cmd.commands) ? cmd.commands.length : ''} 步修改`
  const added: Record<string, string> = { addText: '文字', addImage: '图片', addShape: '形状', insertLayers: '图层' }
  if (added[cmd?.type]) return `命令行刚添加了${added[cmd.type]}`
  if (cmd?.type === 'removeLayer') return `命令行刚删除了「${cmd.id}」`
  if (cmd?.id) return `命令行刚改了「${nameOf(cmd.id)}」`
  if (Array.isArray(cmd?.ids)) return `命令行刚改了 ${cmd.ids.length} 个图层`
  return '命令行刚改了画布'
}

/** A file's pixel size, read before upload so the layer can be fitted at once. */
async function naturalSize(file: File): Promise<{ w: number; h: number }> {
  try {
    const bmp = await createImageBitmap(file)
    const out = { w: bmp.width, h: bmp.height }
    bmp.close()
    return out
  } catch {
    const url = URL.createObjectURL(file)
    try {
      return await new Promise((resolve, reject) => {
        const img = new Image()
        img.onload = () => resolve({ w: img.naturalWidth, h: img.naturalHeight })
        img.onerror = reject
        img.src = url
      })
    } finally {
      URL.revokeObjectURL(url)
    }
  }
}

/** A layer name from a file name; a pasted screenshot arrives as "image.png". */
function imageName(file: string): string {
  const base = file.replace(/\.[a-z0-9]+$/i, '').trim()
  return !base || base === 'image' ? '图片' : base
}

function Icon({ d, size = 17 }: { d: string; size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round">
      <path d={d} />
    </svg>
  )
}

// --- top islands ----------------------------------------------------------

/** The project name, in serif; click to rename it in place. */
function ProjectName({
  projectID,
  name,
  onRenamed,
  onLog,
}: {
  projectID: string
  name: string
  onRenamed: (name: string) => void
  onLog: (line: string) => void
}) {
  const [draft, setDraft] = useState<string | null>(null)
  const commit = async () => {
    const next = (draft ?? '').trim()
    setDraft(null)
    if (!next || next === name) return
    try {
      await api.renameProject(projectID, next)
      onRenamed(next)
    } catch (e: any) {
      onLog(`✗ 重命名失败：${e?.message ?? e}`)
    }
  }
  if (draft !== null) {
    return (
      <input
        autoFocus
        className="w-56 rounded-lg bg-paper px-2 py-1 font-serif text-base font-semibold text-ink outline-none ring-2 ring-accent/30"
        value={draft}
        onFocus={(e) => e.target.select()}
        onChange={(e) => setDraft(e.target.value)}
        onBlur={() => void commit()}
        onKeyDown={(e) => {
          if (e.key === 'Enter') (e.target as HTMLInputElement).blur()
          if (e.key === 'Escape') setDraft(null)
        }}
        aria-label="项目名"
        data-testid="project-name-input"
      />
    )
  }
  return (
    <button
      className="max-w-72 truncate rounded-lg px-1.5 py-1 font-serif text-base font-semibold text-ink hover:bg-paper"
      title="点击重命名"
      onClick={() => setDraft(name)}
      data-testid="project-name"
    >
      {name}
    </button>
  )
}

/** Saved / saving / unsaved, as a dot with the words in its tooltip. */
function SaveDot({ state }: { state: SaveState }) {
  const [colour, label] =
    state === 'saving'
      ? ['bg-ghost animate-pulse', '保存中…']
      : state === 'dirty'
        ? ['bg-ghost', '有未保存的修改']
        : state === 'error'
          ? ['bg-accent', '保存失败']
          : ['bg-ok', '已保存']
  return <span className={`h-1.5 w-1.5 shrink-0 rounded-full ${colour}`} title={label} aria-label={label} role="img" />
}

const TOOLS: { tool: Tool | 'image'; label: string; key?: string; icon: string }[] = [
  { tool: 'text', label: '文字', key: 'T', icon: 'M5 6V4h14v2M12 4v16M9 20h6' },
  { tool: 'image', label: '图片', icon: 'M3 4h18v16H3zM9 8a2 2 0 1 0 0 4 2 2 0 0 0 0-4zM21 17l-5-5-9 8' },
  { tool: 'rect', label: '矩形', key: 'R', icon: 'M4 6h16v12H4z' },
  { tool: 'ellipse', label: '椭圆', key: 'O', icon: 'M12 4a8 8 0 1 0 0 16 8 8 0 0 0 0-16z' },
]

const LINES: { kind: LineKind; label: string; icon: string; dash?: boolean }[] = [
  { kind: 'solid', label: '实线', icon: 'M3 12h18' },
  { kind: 'dashed', label: '虚线', icon: 'M3 12h18', dash: true },
  { kind: 'arrow', label: '单向箭头', icon: 'M3 12h16M15 8l4 4-4 4' },
  { kind: 'double', label: '双向箭头', icon: 'M5 12h14M9 8l-4 4 4 4M15 8l4 4-4 4' },
]

/** The add tools, centred along the top. Each click adds one layer; 图片 also opens the material library. */
function ToolDock({
  onTool,
  onLine,
  onFiles,
  libraryOpen,
  onToggleLibrary,
  children,
}: {
  onTool: (t: Tool) => void
  onLine: (k: LineKind) => void
  onFiles: (files: File[]) => void
  libraryOpen: boolean
  onToggleLibrary: () => void
  children?: ReactNode
}) {
  const fileRef = useRef<HTMLInputElement>(null)
  return (
    <nav aria-label="添加元素" className="island absolute left-1/2 top-4 z-20 flex h-[52px] -translate-x-1/2 items-center gap-0.5 px-1.5">
      {TOOLS.map((t) =>
        t.tool === 'image' ? (
          <DockMenu
            key={t.tool}
            label={t.label}
            icon={<Icon d={t.icon} />}
            title="添加图片（也可以拖进画布或 ⌘V 粘贴）"
            testId="image-tool"
            items={[
              { label: '从素材库选择', libraryToggle: true, onClick: () => !libraryOpen && onToggleLibrary() },
              { label: '从本地上传', onClick: () => fileRef.current?.click() },
            ]}
          />
        ) : (
          <button
            key={t.tool}
            className="flex h-10 items-center gap-1.5 rounded-[11px] px-3 text-[13px] text-ink transition-colors hover:bg-paper"
            title={`添加${t.label}${t.key ? `（${t.key}）` : ''}`}
            onClick={() => onTool(t.tool as Tool)}
          >
            <Icon d={t.icon} />
            {t.label}
          </button>
        ),
      )}
      <DockMenu
        label="直线"
        icon={<Icon d="M4 20 20 4" />}
        title="添加直线：实线、虚线、箭头"
        testId="line-tool"
        items={LINES.map((l) => ({
          label: l.label,
          icon: (
            <svg width="28" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
              <path d={l.icon} strokeDasharray={l.dash ? '4 3' : undefined} />
            </svg>
          ),
          onClick: () => onLine(l.kind),
        }))}
      />
      <input
        ref={fileRef}
        type="file"
        accept="image/*"
        multiple
        className="hidden"
        onChange={(e) => {
          const files = Array.from(e.target.files ?? [])
          e.target.value = ''
          if (files.length) onFiles(files)
        }}
      />
      {children}
    </nav>
  )
}

/** A tool-dock button that opens a small menu of ways to add something. */
function DockMenu({
  label,
  icon,
  title,
  testId,
  items,
}: {
  label: string
  icon: ReactNode
  title: string
  testId?: string
  items: { label: string; icon?: ReactNode; libraryToggle?: boolean; onClick: () => void }[]
}) {
  const [open, setOpen] = useState(false)
  const box = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (!open) return
    const onDown = (e: MouseEvent) => {
      if (!box.current?.contains(e.target as Node)) setOpen(false)
    }
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setOpen(false)
    }
    window.addEventListener('mousedown', onDown)
    window.addEventListener('keydown', onKey)
    return () => {
      window.removeEventListener('mousedown', onDown)
      window.removeEventListener('keydown', onKey)
    }
  }, [open])
  return (
    <div className="relative" ref={box}>
      <button
        className={`flex h-10 items-center gap-1.5 rounded-[11px] px-3 text-[13px] text-ink transition-colors ${open ? 'bg-paper' : 'hover:bg-paper'}`}
        title={title}
        aria-haspopup="menu"
        aria-expanded={open}
        data-testid={testId}
        onClick={() => setOpen((v) => !v)}
      >
        {icon}
        {label}
      </button>
      {open && (
        <div className="menu absolute left-0 top-full z-30 mt-2 w-44" role="menu">
          {items.map((it) => (
            <button
              key={it.label}
              className="menu-item"
              role="menuitem"
              {...(it.libraryToggle ? { 'data-library-toggle': true } : {})}
              onClick={() => {
                setOpen(false)
                it.onClick()
              }}
            >
              <span className="flex items-center gap-2.5">
                {it.icon}
                {it.label}
              </span>
            </button>
          ))}
        </div>
      )}
    </div>
  )
}

// --- stage overlays -------------------------------------------------------

const ZOOM_STEPS = [0.25, 0.5, 1, 2]

/** Zoom out / the zoom (a menu) / zoom in, and the shortcut sheet. */
function ZoomPill({ zoom, editor, onHelp }: { zoom: number; editor: import('../editor/Editor').Editor; onHelp: () => void }) {
  const [open, setOpen] = useState(false)
  const box = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (!open) return
    const onDown = (e: MouseEvent) => {
      if (box.current && !box.current.contains(e.target as Node)) setOpen(false)
    }
    window.addEventListener('mousedown', onDown)
    return () => window.removeEventListener('mousedown', onDown)
  }, [open])

  const item = (label: string, keys: string, act: () => void) => (
    <button
      key={label}
      className="menu-item"
      onClick={() => {
        act()
        setOpen(false)
      }}
    >
      <span>{label}</span>
      <span className="text-xs text-faint">{keys}</span>
    </button>
  )

  return (
    <div className="absolute bottom-5 right-5 z-10 flex items-center gap-2" ref={box}>
      <button className="island flex h-10 w-10 items-center justify-center rounded-full text-sm text-muted hover:text-ink" onClick={onHelp} title="快捷键（?）" aria-label="快捷键">
        ?
      </button>
      <div className="island relative flex h-10 items-center gap-0.5 rounded-full px-1">
        <button className="icon-btn h-8 w-8 rounded-full" aria-label="缩小" onClick={() => editor.zoomBy(1 / 1.2)}>
          <Icon d="M5 12h14" size={15} />
        </button>
        <button
          className="h-8 w-12 rounded-full text-[13px] tabular-nums text-ink hover:bg-paper"
          onClick={() => setOpen((v) => !v)}
          aria-expanded={open}
          title="缩放"
          data-testid="zoom-button"
        >
          {zoom}%
        </button>
        <button className="icon-btn h-8 w-8 rounded-full" aria-label="放大" onClick={() => editor.zoomBy(1.2)}>
          <Icon d="M12 5v14M5 12h14" size={15} />
        </button>
        {open && (
          <div className="menu absolute bottom-full right-0 mb-2 w-44">
            {item('适应画布', '⌘0', () => editor.fitToScreen())}
            <div className="my-1 h-px bg-line" />
            {ZOOM_STEPS.map((z) => item(`${z * 100}%`, z === 1 ? '⌘1' : '', () => editor.setZoom(z)))}
          </div>
        )}
      </div>
    </div>
  )
}

// --- context menu ---------------------------------------------------------

/** Reads whatever is on the system clipboard, for the menu's "粘贴". */
async function readClipboard(): Promise<{ files: File[]; text: string }> {
  const files: File[] = []
  let text = ''
  try {
    for (const item of await navigator.clipboard.read()) {
      const img = item.types.find((t) => t.startsWith('image/'))
      if (img) files.push(new File([await item.getType(img)], 'image.png', { type: img }))
      else if (item.types.includes('text/plain')) text = await (await item.getType('text/plain')).text()
    }
  } catch {
    text = await navigator.clipboard.readText()
  }
  return { files, text }
}

function ContextMenu({
  menu,
  editor,
  doc,
  host,
  onCrop,
  onLog,
  onClose,
}: {
  menu: MenuState
  editor: import('../editor/Editor').Editor
  doc: Document | null
  host: ShortcutHost
  onCrop: (layer: Layer) => void
  onLog: (line: string) => void
  onClose: () => void
}) {
  const box = useRef<HTMLDivElement>(null)
  const [pos, setPos] = useState({ x: menu.x, y: menu.y })

  useEffect(() => {
    const onDown = (e: MouseEvent) => {
      if (box.current && !box.current.contains(e.target as Node)) onClose()
    }
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('mousedown', onDown)
    window.addEventListener('keydown', onKey)
    window.addEventListener('blur', onClose)
    return () => {
      window.removeEventListener('mousedown', onDown)
      window.removeEventListener('keydown', onKey)
      window.removeEventListener('blur', onClose)
    }
  }, [onClose])

  // Keep the menu on screen near the window's right and bottom edges.
  useEffect(() => {
    const el = box.current
    if (!el) return
    const r = el.getBoundingClientRect()
    setPos({
      x: Math.max(4, Math.min(menu.x, window.innerWidth - r.width - 4)),
      y: Math.max(4, Math.min(menu.y, window.innerHeight - r.height - 4)),
    })
  }, [menu.x, menu.y])

  const layers = selectedLayers(editor)
  const ids = layers.map((l) => l.id)
  const groups = layers.filter((l) => l.type === 'group')
  const single = layers.length === 1 ? layers[0] : undefined
  const allLocked = layers.length > 0 && layers.every((l) => l.locked)
  const allHidden = layers.length > 0 && layers.every((l) => !l.visible)

  const act = (f: () => void | Promise<unknown>) => () => {
    onClose()
    void f()
  }
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(clipText(layers))
    } catch (e: any) {
      onLog(`✗ 复制失败：${e?.message ?? e}`)
    }
  }
  const paste = async () => {
    try {
      if (!applyPaste(editor, host, await readClipboard())) onLog('✗ 剪贴板里没有能粘贴的内容')
    } catch (e: any) {
      onLog(`✗ 读取剪贴板失败：${e?.message ?? e}（可以直接按 ⌘V）`)
    }
  }
  const z = (type: ZMove) => act(() => host.run(zOrderCommand(editor, ids, type)))

  type Entry = { label: string; keys?: string; run: () => void; danger?: boolean; disabled?: boolean } | 'sep'
  const entries: Entry[] = layers.length
    ? [
        { label: '复制', keys: '⌘C', run: act(copy) },
        {
          label: '剪切',
          keys: '⌘X',
          run: act(async () => {
            await copy()
            await host.run(removeCommand(ids))
          }),
        },
        { label: '粘贴', keys: '⌘V', run: act(paste) },
        { label: '创建副本', keys: '⌘D', run: act(() => runAndSelect(host, duplicateCommand(ids))) },
        'sep',
        { label: '置于顶层', keys: '⇧⌘]', run: z('bringToFront') },
        { label: '上移一层', keys: '⌘]', run: z('bringForward') },
        { label: '下移一层', keys: '⌘[', run: z('sendBackward') },
        { label: '置于底层', keys: '⇧⌘[', run: z('sendToBack') },
        'sep',
        ...(layers.length > 1 ? [{ label: '编组', keys: '⌘G', run: act(() => runAndSelect(host, { type: 'groupLayers', ids })) }] : []),
        ...(groups.length ? [{ label: '解散编组', keys: '⇧⌘G', run: act(() => runAndSelect(host, ungroupCommand(groups.map((g) => g.id)))) }] : []),
        { label: '水平居中到画布', run: act(() => host.run({ type: 'alignLayers', ids, h: 'center', to: 'canvas' })) },
        { label: '垂直居中到画布', run: act(() => host.run({ type: 'alignLayers', ids, v: 'middle', to: 'canvas' })) },
        ...(single?.type === 'image' ? [{ label: '裁剪', run: act(() => onCrop(single)) }] : []),
        'sep',
        {
          label: allLocked ? '解锁' : '锁定',
          run: act(() => host.run({ type: 'batch', commands: ids.map((id) => ({ type: 'setLocked', id, locked: !allLocked })) })),
        },
        {
          label: allHidden ? '显示' : '隐藏',
          run: act(() => host.run({ type: 'batch', commands: ids.map((id) => ({ type: 'setVisible', id, visible: allHidden })) })),
        },
        { label: '删除', keys: '⌫', danger: true, run: act(() => host.run(removeCommand(ids))) },
      ]
    : [
        { label: '粘贴', keys: '⌘V', run: act(paste) },
        {
          label: '全选',
          keys: '⌘A',
          disabled: !doc?.layers.length,
          run: act(() => host.select(editor.selectableIDs())),
        },
        { label: '适应画布', keys: '⌘0', run: act(() => editor.fitToScreen()) },
      ]

  return (
    <div
      ref={box}
      className="menu fixed z-40 w-52"
      style={{ left: pos.x, top: pos.y }}
      data-testid="context-menu"
      role="menu"
      onContextMenu={(e) => e.preventDefault()}
    >
      {entries.map((e, i) =>
        e === 'sep' ? (
          <div key={i} className="mx-2 my-1 h-px bg-line" />
        ) : (
          <button key={e.label} role="menuitem" disabled={e.disabled} className={`menu-item ${e.danger ? 'text-accent' : ''}`} onClick={e.run}>
            <span>{e.label}</span>
            {e.keys && <span className="text-xs text-faint">{e.keys}</span>}
          </button>
        ),
      )}
    </div>
  )
}

// --- help -------------------------------------------------------------------

const SHORTCUTS: [string, [string, string][]][] = [
  [
    '添加',
    [
      ['T', '文字'],
      ['R', '矩形'],
      ['O', '椭圆'],
      ['拖入 / ⌘V', '图片（截图也行）'],
    ],
  ],
  [
    '编辑',
    [
      ['⌘Z / ⇧⌘Z', '撤销 / 重做'],
      ['⌘C / ⌘X / ⌘V', '复制 / 剪切 / 粘贴（可跨项目）'],
      ['⌘D', '创建副本'],
      ['⌫', '删除'],
      ['⌘G / ⇧⌘G', '编组 / 解散编组'],
      ['⌘] / ⌘[', '上移 / 下移一层'],
      ['⇧⌘] / ⇧⌘[', '置顶 / 置底'],
      ['方向键 / ⇧方向键', '微移 1px / 10px'],
      ['双击图片', '裁剪'],
      ['双击文字', '编辑文字'],
    ],
  ],
  [
    '选择',
    [
      ['⌘A', '全选'],
      ['⇧点击 / ⌘点击', '多选（画布和图层列表都行）'],
      ['Esc', '取消选择'],
      ['拖动时按住 ⇧', '只沿水平或垂直方向移动'],
    ],
  ],
  [
    '视图',
    [
      ['空格 + 拖动', '平移'],
      ['双指滑动 / 滚轮', '平移'],
      ['⌘ + 滚轮 / 双指捏合', '缩放'],
      ['⌘0 / ⌘1', '适应画布 / 100%'],
      ['⌘+ / ⌘−', '放大 / 缩小'],
    ],
  ],
]

function HelpDialog({ onClose }: { onClose: () => void }) {
  return (
    <Modal onClose={onClose} width="w-[600px]" label="快捷键">
      <div className="max-h-[80vh] overflow-y-auto p-7">
        <div className="mb-5 flex items-center justify-between">
          <h2 className="font-serif text-xl font-black text-ink">快捷键</h2>
          <button className="icon-btn text-faint" aria-label="关闭" onClick={onClose}>
            <Icon d="M6 6l12 12M18 6 6 18" size={15} />
          </button>
        </div>
        <div className="grid grid-cols-2 gap-x-8 gap-y-6">
          {SHORTCUTS.map(([title, list]) => (
            <div key={title}>
              <h3 className="section-title mb-2">{title}</h3>
              <dl className="flex flex-col gap-1.5 text-[13px]">
                {list.map(([k, v]) => (
                  <div key={k} className="flex gap-3">
                    <dt className="w-32 shrink-0 text-ink">{k}</dt>
                    <dd className="text-muted">{v}</dd>
                  </div>
                ))}
              </dl>
            </div>
          ))}
        </div>
      </div>
    </Modal>
  )
}

// --- developer drawer -----------------------------------------------------

/**
 * The raw command box and the log, folded away by default at the foot of the
 * layer card: they are how the "AI Native" claim is tested by hand, not
 * something a cover needs. Errors still surface in the stage's notice.
 */
function DevDrawer({ open, onToggle, errors, children }: { open: boolean; onToggle: () => void; errors: number; children: ReactNode }) {
  return (
    <section className={`flex flex-col border-t border-line ${open ? 'max-h-[50%] min-h-0' : ''}`}>
      <button className="flex h-10 shrink-0 items-center justify-between px-5 text-xs text-faint hover:text-ink" onClick={onToggle} aria-expanded={open} data-testid="dev-toggle">
        <span>命令与日志</span>
        <span className="flex items-center gap-2">
          {errors > 0 && <span className="rounded-full bg-accent-soft px-1.5 text-accent">{errors}</span>}
          <Icon d={open ? 'm6 9 6 6 6-6' : 'm9 6 6 6-6 6'} size={13} />
        </span>
      </button>
      {open && <div className="min-h-0 overflow-y-auto">{children}</div>}
    </section>
  )
}

/**
 * A raw command box. It is the same box the CLI uses — anything that works here
 * works from `davinci exec`, which is what makes the "AI Native" claim testable
 * by hand.
 */
function Console({ cmdText, setCmdText, out, onRun }: { cmdText: string; setCmdText: (s: string) => void; out: string; onRun: () => void }) {
  return (
    <section className="flex flex-col gap-2 px-4 pb-3">
      <textarea
        className="w-full rounded-[10px] bg-paper px-3 py-2 font-mono text-[11px] text-ink outline-none placeholder:text-faint focus:ring-2 focus:ring-accent/30"
        rows={3}
        placeholder='{"type":"addText","text":"标题"}'
        aria-label="命令 JSON"
        value={cmdText}
        onChange={(e) => setCmdText(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
            e.preventDefault()
            onRun()
          }
        }}
      />
      <button className="chip bg-ink text-card hover:!bg-ink-2" onClick={onRun}>
        执行 ⌘↵
      </button>
      {out && (
        <pre className="max-h-32 overflow-auto rounded-[10px] bg-paper p-2.5 font-mono text-[10px] text-ink-2" data-testid="cmd-out">
          {out}
        </pre>
      )}
    </section>
  )
}

function Log({ lines }: { lines: string[] }) {
  if (!lines.length) return null
  return (
    <section className="flex flex-col gap-0.5 px-5 pb-3">
      {lines.map((l, i) => (
        <div key={i} className={`font-mono text-[10px] ${l.startsWith('✗') ? 'text-accent' : 'text-faint'}`}>
          {l}
        </div>
      ))}
    </section>
  )
}
