import { useEffect, useRef, useState } from 'react'
import type { Board, ProjectDoc } from '../types'
import { renderBoard } from '../editor/ck'

/**
 * The boards of a project along the bottom of the stage (稿定's 多画板 strip).
 *
 * Every board is a thumbnail; the active one is ringed. Folded, the strip
 * becomes a pill — "画板 2/5" with previous / next. Everything it does is a
 * command
 * (selectBoard, addBoard, moveBoard …), so it goes through the same path the
 * CLI does and lands in the same undo history.
 */

const THUMB_H = 76
const OPEN_KEY = 'davinci.filmstrip'

/** Height the strip (or, folded, the pill) takes up from the bottom of the stage, for fit-to-screen. */
export function filmstripInset(open: boolean): number {
  return open ? 20 + THUMB_H + 34 + 24 : 88
}

/** Whether the strip is unfolded, remembered across pages. */
export function useFilmstripOpen(): [boolean, (v: boolean) => void] {
  const [open, setOpen] = useState(() => {
    try {
      return localStorage.getItem(OPEN_KEY) !== '0'
    } catch {
      return true
    }
  })
  const set = (v: boolean) => {
    setOpen(v)
    try {
      localStorage.setItem(OPEN_KEY, v ? '1' : '0')
    } catch {
      // Storage refused (private window): the choice lasts for this page only.
    }
  }
  return [open, set]
}

interface Props {
  project: ProjectDoc
  open: boolean
  onOpen: (v: boolean) => void
  onRun: (cmd: Record<string, unknown>) => void
}

export function Filmstrip({ project, open, onOpen, onRun }: Props) {
  const thumbs = useThumbnails(project)
  const [menu, setMenu] = useState<{ x: number; y: number; board: Board } | null>(null)
  const [renaming, setRenaming] = useState<string | null>(null)
  const [dragging, setDragging] = useState<string | null>(null)
  const [dropAt, setDropAt] = useState<number | null>(null)
  const activeRef = useRef<HTMLButtonElement | null>(null)

  const boards = project.boards
  const index = Math.max(0, boards.findIndex((b) => b.id === project.active))
  const go = (i: number) => {
    const b = boards[i]
    if (b && b.id !== project.active) onRun({ type: 'selectBoard', id: b.id })
  }

  useEffect(() => {
    activeRef.current?.scrollIntoView({ block: 'nearest', inline: 'nearest' })
  }, [project.active, open])

  useEffect(() => {
    if (!menu) return
    const close = () => setMenu(null)
    window.addEventListener('mousedown', close)
    window.addEventListener('blur', close)
    return () => {
      window.removeEventListener('mousedown', close)
      window.removeEventListener('blur', close)
    }
  }, [menu])

  return (
    <div className="pointer-events-none absolute inset-x-5 bottom-5 z-10 flex flex-col items-center gap-2" data-testid="filmstrip">
      {open ? (
        <div className="island pointer-events-auto flex max-w-full items-end gap-2 overflow-x-auto rounded-2xl p-2.5">
          {boards.map((b, i) => {
            const active = b.id === project.active
            const w = Math.max(28, Math.round((THUMB_H * b.canvas.width) / Math.max(1, b.canvas.height)))
            return (
              <div
                key={b.id}
                className="relative flex shrink-0 flex-col items-center gap-1"
                onDragOver={(e) => {
                  if (!dragging) return
                  e.preventDefault()
                  const r = e.currentTarget.getBoundingClientRect()
                  setDropAt(e.clientX < r.left + r.width / 2 ? i : i + 1)
                }}
                onDrop={(e) => {
                  e.preventDefault()
                  if (dragging && dropAt !== null) {
                    const from = boards.findIndex((x) => x.id === dragging)
                    const to = dropAt > from ? dropAt - 1 : dropAt
                    if (to !== from) onRun({ type: 'moveBoard', id: dragging, index: to + 1 })
                  }
                  setDragging(null)
                  setDropAt(null)
                }}
              >
                {dragging && dropAt === i && <span className="absolute -left-[6px] top-0 h-[76px] w-[3px] rounded bg-accent" />}
                {dragging && dropAt === i + 1 && i === boards.length - 1 && (
                  <span className="absolute -right-[6px] top-0 h-[76px] w-[3px] rounded bg-accent" />
                )}
                <button
                  ref={active ? activeRef : undefined}
                  draggable
                  onDragStart={(e) => {
                    setDragging(b.id)
                    e.dataTransfer.effectAllowed = 'move'
                  }}
                  onDragEnd={() => {
                    setDragging(null)
                    setDropAt(null)
                  }}
                  onClick={() => go(i)}
                  onContextMenu={(e) => {
                    e.preventDefault()
                    setMenu({ x: e.clientX, y: e.clientY, board: b })
                  }}
                  title={`${b.name}（${b.canvas.width}×${b.canvas.height}）`}
                  className={`relative overflow-hidden rounded-md bg-paper-deep transition-shadow ${
                    active ? 'ring-2 ring-accent ring-offset-2 ring-offset-card' : 'ring-1 ring-line hover:ring-line-strong'
                  } ${dragging === b.id ? 'opacity-40' : ''}`}
                  style={{ width: w, height: THUMB_H }}
                  aria-label={b.name}
                  aria-current={active ? 'true' : undefined}
                  data-testid="board-thumb"
                >
                  {thumbs[b.id] && <img src={thumbs[b.id]} alt="" draggable={false} className="h-full w-full object-contain" />}
                  <span className="absolute left-1 top-1 rounded bg-ink/70 px-1 text-[10px] font-bold leading-4 text-white tabular-nums">{i + 1}</span>
                </button>
                {renaming === b.id ? (
                  <input
                    autoFocus
                    defaultValue={b.name}
                    className="h-5 w-[88px] rounded border border-line-strong bg-card px-1 text-center text-[11px] outline-none"
                    onFocus={(e) => e.currentTarget.select()}
                    onBlur={(e) => {
                      const v = e.currentTarget.value.trim()
                      setRenaming(null)
                      if (v && v !== b.name) onRun({ type: 'renameBoard', id: b.id, name: v })
                    }}
                    onKeyDown={(e) => {
                      e.stopPropagation()
                      if (e.key === 'Enter') e.currentTarget.blur()
                      if (e.key === 'Escape') setRenaming(null)
                    }}
                  />
                ) : (
                  <span
                    className={`max-w-[96px] truncate text-[11px] leading-5 ${active ? 'font-bold text-ink' : 'text-muted'}`}
                    style={{ maxWidth: Math.max(56, w + 8) }}
                    onDoubleClick={() => setRenaming(b.id)}
                    title="双击重命名"
                  >
                    {b.name}
                  </span>
                )}
              </div>
            )
          })}
          <div className="flex shrink-0 flex-col items-center gap-1">
            <button
              className="flex items-center justify-center rounded-md border border-dashed border-line-strong text-faint hover:border-accent hover:text-accent"
              style={{ width: 52, height: THUMB_H }}
              onClick={() => onRun({ type: 'addBoard' })}
              title="新建画板"
              aria-label="新建画板"
              data-testid="add-board"
            >
              <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round">
                <path d="M12 5v14M5 12h14" />
              </svg>
            </button>
            <span className="text-[11px] leading-5 text-faint">新建</span>
          </div>
          <div className="flex shrink-0 flex-col items-center gap-1">
            <button
              className="icon-btn rounded-md text-faint hover:text-ink"
              style={{ width: 36, height: THUMB_H }}
              onClick={() => onOpen(false)}
              title="收起画板"
              aria-label="收起画板"
              data-testid="collapse-boards"
            >
              <Chevron d="m6 9 6 6 6-6" />
            </button>
            <span className="text-[11px] leading-5 text-faint">收起</span>
          </div>
        </div>
      ) : (
        <div className="island pointer-events-auto flex h-10 shrink-0 items-center gap-0.5 whitespace-nowrap rounded-full px-1">
          <button className="icon-btn h-8 w-8 rounded-full" aria-label="上一个画板" disabled={index <= 0} onClick={() => go(index - 1)}>
            <Chevron d="m15 6-6 6 6 6" />
          </button>
          <button
            className="flex h-8 shrink-0 items-center gap-1.5 whitespace-nowrap rounded-full px-2.5 text-[13px] text-ink hover:bg-paper"
            onClick={() => onOpen(true)}
            aria-expanded={false}
            title="展开画板"
            data-testid="board-pill"
          >
            <span className="font-bold">画板</span>
            <span className="tabular-nums text-muted">
              {index + 1}/{boards.length}
            </span>
            <Chevron d="m6 15 6-6 6 6" size={13} />
          </button>
          <button className="icon-btn h-8 w-8 rounded-full" aria-label="下一个画板" disabled={index >= boards.length - 1} onClick={() => go(index + 1)}>
            <Chevron d="m9 6 6 6-6 6" />
          </button>
        </div>
      )}

      {menu && (
        <div
          className="menu pointer-events-auto fixed z-40 w-44"
          style={{ left: menu.x, top: menu.y, transform: 'translateY(-100%)' }}
          onMouseDown={(e) => e.stopPropagation()}
        >
          {menuItem('重命名', () => setRenaming(menu.board.id))}
          {menuItem('复制画板', () => onRun({ type: 'duplicateBoard', id: menu.board.id }))}
          {menuItem('在后面新建', () => {
            const i = boards.findIndex((b) => b.id === menu.board.id)
            onRun({ type: 'addBoard', index: i + 2 })
          })}
          <div className="my-1 h-px bg-line" />
          {menuItem('前移', () => onRun({ type: 'moveBoard', id: menu.board.id, index: boards.findIndex((b) => b.id === menu.board.id) }), boards[0].id === menu.board.id)}
          {menuItem('后移', () => onRun({ type: 'moveBoard', id: menu.board.id, index: boards.findIndex((b) => b.id === menu.board.id) + 2 }), boards[boards.length - 1].id === menu.board.id)}
          <div className="my-1 h-px bg-line" />
          {menuItem('删除画板', () => onRun({ type: 'removeBoard', id: menu.board.id }), boards.length <= 1, true)}
        </div>
      )}
    </div>
  )

  function menuItem(label: string, act: () => void, disabled = false, danger = false) {
    return (
      <button
        key={label}
        className={`menu-item ${danger ? 'text-accent' : ''} disabled:opacity-40`}
        disabled={disabled}
        onClick={() => {
          setMenu(null)
          act()
        }}
      >
        {label}
      </button>
    )
  }
}

/**
 * Thumbnails for every board, redrawn only for boards whose JSON changed and
 * never more than a few times a second while a drag is streaming edits in.
 */
function useThumbnails(project: ProjectDoc): Record<string, string> {
  const [urls, setUrls] = useState<Record<string, string>>({})
  const keys = useRef(new Map<string, string>())
  const busy = useRef(false)
  const latest = useRef(project)
  latest.current = project

  useEffect(() => {
    const t = window.setTimeout(() => void pump(), 250)
    return () => window.clearTimeout(t)

    async function pump() {
      if (busy.current) return
      busy.current = true
      try {
        for (;;) {
          const p = latest.current
          const stale = p.boards.find((b) => keys.current.get(b.id) !== JSON.stringify(b))
          if (!stale) break
          const key = JSON.stringify(stale)
          let url = ''
          try {
            url = await renderBoard(stale, { width: 200 })
          } catch {
            // Leave the tile blank; the next change retries.
          }
          keys.current.set(stale.id, key)
          if (url) setUrls((prev) => ({ ...prev, [stale.id]: url }))
        }
      } finally {
        busy.current = false
      }
    }
  }, [project])

  return urls
}

function Chevron({ d, size = 15 }: { d: string; size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round">
      <path d={d} />
    </svg>
  )
}
