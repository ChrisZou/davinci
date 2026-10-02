import { useRef, useState } from 'react'
import type { Document, Layer, LayerRow } from '../types'

/**
 * The layer list, front-to-back.
 *
 * Click selects, ⌘-click toggles, ⇧-click selects a range; a double-click on
 * the name renames in place. Dragging a row shows where it will land before it
 * is dropped. Every change is a command, like everywhere else.
 */
export function LayerPanel({
  rows,
  doc,
  selected,
  onSelect,
  onRun,
}: {
  rows: LayerRow[]
  doc: Document
  selected: string[]
  onSelect: (ids: string[]) => void
  onRun: (cmd: unknown) => void
}) {
  const [drag, setDrag] = useState<string | null>(null)
  const [over, setOver] = useState<{ id: string; above: boolean } | null>(null)
  const [renaming, setRenaming] = useState<string | null>(null)
  // The row a ⇧-click range starts from: the last plain or ⌘ click.
  const anchor = useRef<string | null>(null)

  const click = (e: React.MouseEvent, id: string) => {
    if (e.shiftKey && anchor.current && rows.some((r) => r.id === anchor.current)) {
      const a = rows.findIndex((r) => r.id === anchor.current)
      const b = rows.findIndex((r) => r.id === id)
      const [lo, hi] = a < b ? [a, b] : [b, a]
      onSelect(rows.slice(lo, hi + 1).map((r) => r.id))
      return
    }
    anchor.current = id
    if (e.metaKey || e.ctrlKey) {
      onSelect(selected.includes(id) ? selected.filter((x) => x !== id) : [...selected, id])
      return
    }
    onSelect([id])
  }

  const drop = () => {
    if (drag && over && drag !== over.id) {
      // Rows are front-to-back, document indices back-to-front. setZIndex takes
      // the object out and puts it back at the index, so the index depends on
      // whether the dragged row came from below or above the target.
      const n = rows.length
      const from = n - 1 - rows.findIndex((r) => r.id === drag)
      const to = n - 1 - rows.findIndex((r) => r.id === over.id)
      const index = over.above ? (from < to ? to : to + 1) : from < to ? to - 1 : to
      if (index !== from) void onRun({ type: 'setZIndex', id: drag, index })
    }
    setDrag(null)
    setOver(null)
  }

  return (
    <section className="flex min-h-0 flex-1 flex-col" aria-label="图层">
      <h2 className="flex items-baseline justify-between px-5 pb-3 pt-5">
        <span className="section-title">图层</span>
        <span className="text-xs text-muted">{selected.length > 1 ? `已选 ${selected.length} / ` : ''}{rows.length} 个</span>
      </h2>
      {!rows.length && <p className="px-5 text-xs leading-relaxed text-faint">还没有图层。用上方的工具添加，或者把图片拖进画布。</p>}
      <ul
        className="min-h-0 flex-1 overflow-y-auto px-3 pb-3"
        data-testid="layer-list"
        // A click below the rows lets go of the selection, like empty canvas does.
        onClick={(e) => {
          if (e.target === e.currentTarget) onSelect([])
        }}
        onDragLeave={(e) => {
          if (!e.currentTarget.contains(e.relatedTarget as Node)) setOver(null)
        }}
      >
        {rows.map((r) => {
          const layer = doc.layers.find((l) => l.id === r.id)
          const isSel = selected.includes(r.id)
          return (
            <li
              key={r.id}
              draggable={renaming !== r.id}
              data-layer-name={r.name}
              data-layer-type={r.type}
              onDragStart={(e) => {
                setDrag(r.id)
                e.dataTransfer.effectAllowed = 'move'
              }}
              onDragEnd={() => {
                setDrag(null)
                setOver(null)
              }}
              onDragOver={(e) => {
                if (!drag) return
                e.preventDefault()
                const box = e.currentTarget.getBoundingClientRect()
                const above = e.clientY < box.top + box.height / 2
                if (over?.id !== r.id || over.above !== above) setOver({ id: r.id, above })
              }}
              onDrop={(e) => {
                e.preventDefault()
                drop()
              }}
              className={`group relative flex h-12 cursor-default items-center gap-3 rounded-xl px-2 transition-colors ${
                isSel ? 'bg-accent-soft' : 'hover:bg-paper'
              } ${drag === r.id ? 'opacity-40' : ''}`}
              onClick={(e) => click(e, r.id)}
            >
              {over?.id === r.id && drag && drag !== r.id && (
                <span
                  className={`pointer-events-none absolute left-2 right-2 h-0.5 rounded bg-accent ${over.above ? '-top-px' : '-bottom-px'}`}
                  data-testid="drop-indicator"
                />
              )}
              <Thumb layer={layer} type={r.type} />
              <span className={`flex min-w-0 flex-1 flex-col ${r.visible ? '' : 'opacity-45'}`}>
                {renaming === r.id ? (
                  <RenameBox
                    name={r.name}
                    onDone={(name) => {
                      setRenaming(null)
                      if (name && name !== r.name) void onRun({ type: 'renameLayer', id: r.id, name })
                    }}
                  />
                ) : (
                  <span
                    className="truncate text-[13px] font-medium text-ink"
                    title={`${r.preview}\n双击重命名`}
                    onDoubleClick={(e) => {
                      e.stopPropagation()
                      setRenaming(r.id)
                    }}
                  >
                    {r.name}
                  </span>
                )}
                <span className="truncate text-[11px] text-faint">{kindOf(layer, r)}</span>
              </span>
              <button
                className={`flex h-7 w-7 shrink-0 items-center justify-center rounded-lg text-faint hover:bg-card hover:text-ink ${r.visible ? 'opacity-0 group-hover:opacity-100' : ''}`}
                title={r.visible ? '隐藏' : '显示'}
                aria-label={r.visible ? '隐藏' : '显示'}
                onClick={(e) => {
                  e.stopPropagation()
                  void onRun({ type: 'setVisible', id: r.id, visible: !r.visible })
                }}
              >
                <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round">
                  {r.visible ? (
                    <path d="M2 12s4-7 10-7 10 7 10 7-4 7-10 7S2 12 2 12zM12 9a3 3 0 1 0 0 6 3 3 0 0 0 0-6z" />
                  ) : (
                    <path d="M3 3l18 18M10.6 5.1A10 10 0 0 1 12 5c6 0 10 7 10 7a17 17 0 0 1-3.2 3.9M6.6 6.6C3.8 8.4 2 12 2 12s4 7 10 7a9.6 9.6 0 0 0 5.4-1.6" />
                  )}
                </svg>
              </button>
              <button
                className={`-ml-1.5 flex h-7 w-7 shrink-0 items-center justify-center rounded-lg hover:bg-card ${r.locked ? 'text-accent' : 'text-faint opacity-0 hover:text-ink group-hover:opacity-100'}`}
                title={r.locked ? '解锁' : '锁定'}
                aria-label={r.locked ? '解锁' : '锁定'}
                onClick={(e) => {
                  e.stopPropagation()
                  void onRun({ type: 'setLocked', id: r.id, locked: !r.locked })
                }}
              >
                <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.9" strokeLinecap="round" strokeLinejoin="round">
                  <path d={r.locked ? 'M5 11h14v10H5zM8 11V8a4 4 0 0 1 8 0v3' : 'M5 11h14v10H5zM8 11V8a4 4 0 0 1 7.5-2'} />
                </svg>
              </button>
            </li>
          )
        })}
      </ul>
    </section>
  )
}

/** The row's second line: what kind of layer, and anything unusual about it. */
function kindOf(layer: Layer | undefined, r: LayerRow): string {
  const base =
    r.type === 'text' ? '文字' : r.type === 'image' ? '图片' : r.type === 'group' ? `编组 · ${layer?.children?.length ?? 0} 个` : ({ rect: '矩形', ellipse: '椭圆', triangle: '三角形', line: '直线' } as Record<string, string>)[layer?.shape?.kind ?? 'rect'] ?? '形状'
  const flags = [r.locked ? '已锁定' : '', r.visible ? '' : '已隐藏'].filter(Boolean)
  return [base, ...flags].join(' · ')
}

function RenameBox({ name, onDone }: { name: string; onDone: (name: string) => void }) {
  const [draft, setDraft] = useState(name)
  const done = useRef(false)
  const finish = (v: string) => {
    if (done.current) return
    done.current = true
    onDone(v.trim())
  }
  return (
    <input
      autoFocus
      className="w-full min-w-0 rounded-md bg-card px-1.5 py-0.5 text-[13px] text-ink outline-none ring-2 ring-accent/40"
      value={draft}
      onFocus={(e) => e.target.select()}
      onClick={(e) => e.stopPropagation()}
      onChange={(e) => setDraft(e.target.value)}
      onBlur={() => finish(draft)}
      onKeyDown={(e) => {
        if (e.key === 'Enter') finish(draft)
        if (e.key === 'Escape') finish(name)
      }}
    />
  )
}

/**
 * A small picture of the layer: the image itself, the shape in its fill, the
 * text's first letter in its colour. Drawn from the document rather than
 * rendered, so it costs nothing on every edit.
 */
function Thumb({ layer, type }: { layer?: Layer; type: Layer['type'] }) {
  const style = (layer?.style ?? {}) as Record<string, any>
  const box = 'dv-checker flex h-8 w-8 shrink-0 items-center justify-center overflow-hidden rounded-lg border border-line'
  if (type === 'image' && layer?.image?.url) {
    return (
      <span className={box}>
        <img src={layer.image.url} alt="" className="h-full w-full object-cover" draggable={false} />
      </span>
    )
  }
  if (type === 'text') {
    const fill = String(style.fill ?? '#111')
    const bg = style.textBackgroundColor ? String(style.textBackgroundColor) : undefined
    return (
      <span className={box} style={bg ? { background: bg } : undefined}>
        <span className="font-serif text-[14px] font-black leading-none" style={{ color: fill, WebkitTextStroke: '0.3px rgba(0,0,0,.35)' }}>
          {(layer?.text ?? 'T').trim().slice(0, 1) || 'T'}
        </span>
      </span>
    )
  }
  if (type === 'shape' && layer?.shape?.kind === 'line') {
    const c = String(style.stroke ?? '#1c1b18')
    return (
      <span className={box}>
        <svg width="20" height="20" viewBox="0 0 20 20">
          <path d="M3 10h14" stroke={c} strokeWidth="2" strokeDasharray={style.lineStyle === 'dashed' ? '3 2' : undefined} />
          {style.arrow && style.arrow !== 'none' && <path d="M17 10l-4-3v6z" fill={c} />}
          {style.arrow === 'both' && <path d="M3 10l4-3v6z" fill={c} />}
        </svg>
      </span>
    )
  }
  if (type === 'shape') {
    const kind = layer?.shape?.kind ?? 'rect'
    const fill = String(style.fill ?? '#999')
    const shape =
      kind === 'ellipse' ? 'rounded-full' : kind === 'triangle' ? '' : style.cornerRadius ? 'rounded-[3px]' : ''
    return (
      <span className={box}>
        <span
          className={`h-5 w-5 ${shape}`}
          style={kind === 'triangle' ? { background: fill, clipPath: 'polygon(50% 0, 100% 100%, 0 100%)' } : { background: fill }}
        />
      </span>
    )
  }
  return (
    <span className={box}>
      <span className="grid grid-cols-2 gap-0.5">
        <span className="h-2 w-2 rounded-[2px] bg-ink-2" />
        <span className="h-2 w-2 rounded-[2px] bg-ghost" />
        <span className="h-2 w-2 rounded-[2px] bg-ghost" />
        <span className="h-2 w-2 rounded-[2px] bg-ink-2" />
      </span>
    </span>
  )
}
