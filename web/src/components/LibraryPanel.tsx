import { useEffect, useRef, useState } from 'react'
import type { LibraryItem } from '../api'
import { ItemTile, useLibrary } from './LibraryParts'

/**
 * The material panel in the editor: pick a shelf, search, and click a picture
 * to add it to the design — or drag it onto the canvas to drop it where it
 * belongs. It remembers the last shelf, since a cover is usually built from
 * one shelf at a time.
 */

const LAST_KEY = 'davinci.libraryCategory'

export function LibraryPanel({
  onInsert,
  onClose,
  mode = 'insert',
}: {
  onInsert: (item: LibraryItem) => void
  onClose: () => void
  /** 'replace': the picked picture takes the place of the selected image. */
  mode?: 'insert' | 'replace'
}) {
  const replacing = mode === 'replace'
  const [category, setCategory] = useState(() => {
    try {
      return localStorage.getItem(LAST_KEY) ?? ''
    } catch {
      return ''
    }
  })
  const [query, setQuery] = useState('')
  const { categories, items, error } = useLibrary(category, query)
  const box = useRef<HTMLDivElement>(null)
  // While a picture is dragged out, the panel steps aside: it fades and lets
  // the drop through, so a picture can land on any part of the canvas.
  const [dragging, setDragging] = useState(false)

  // A shelf remembered from before may have been deleted since.
  useEffect(() => {
    if (categories && category && !categories.some((c) => c.id === category)) setCategory('')
  }, [categories, category])

  useEffect(() => {
    try {
      localStorage.setItem(LAST_KEY, category)
    } catch {
      // Storage may be off; the panel just starts on 全部 next time.
    }
  }, [category])

  // Escape and a click outside close it, like any other popover.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    const onDown = (e: MouseEvent) => {
      const t = e.target as HTMLElement
      if (box.current?.contains(t) || t.closest('[data-library-toggle]')) return
      onClose()
    }
    window.addEventListener('keydown', onKey)
    window.addEventListener('mousedown', onDown)
    return () => {
      window.removeEventListener('keydown', onKey)
      window.removeEventListener('mousedown', onDown)
    }
  }, [onClose])

  return (
    <div
      ref={box}
      role="dialog"
      aria-label={replacing ? '从素材库换图' : '素材'}
      // A drawer over the layer card, so the canvas stays in view to drop onto.
      className={`menu fixed bottom-5 left-5 top-[84px] z-30 flex w-[440px] flex-col !p-0 transition-opacity ${dragging ? 'pointer-events-none opacity-30' : ''}`}
      data-testid="library-panel"
    >
      <div className="flex flex-col gap-3 px-5 pb-3 pt-5">
        <div className="flex items-center justify-between">
          <h2 className="m-0 font-serif text-lg font-black">{replacing ? '换成素材库里的图' : '素材'}</h2>
          <a href="/library" target="_blank" rel="noreferrer" className="text-xs text-muted hover:text-accent">
            管理素材库 ↗
          </a>
        </div>
        <input
          autoFocus
          aria-label="搜索素材"
          placeholder="搜名称、标签、说明，比如：指向"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          className="h-9 rounded-full bg-paper px-4 text-[13px] text-ink outline-none placeholder:text-faint focus:ring-2 focus:ring-accent/30"
        />
        <div className="flex flex-wrap gap-1.5">
          {[{ id: '', name: '全部', count: categories?.reduce((n, c) => n + c.count, 0) ?? 0 }, ...(categories ?? [])].map((c) => (
            <button
              key={c.id || 'all'}
              aria-pressed={category === c.id}
              className={`h-7 rounded-full px-3 text-xs transition-colors ${category === c.id ? 'bg-ink font-bold text-card' : 'bg-paper text-ink-2 hover:bg-paper-deep'}`}
              onClick={() => setCategory(c.id)}
            >
              {c.name} <span className={category === c.id ? 'text-card/60' : 'text-faint'}>{c.count}</span>
            </button>
          ))}
        </div>
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto border-t border-line px-5 py-4">
        {error && <p className="text-sm text-accent">{error}</p>}
        {items === null && <p className="text-sm text-faint">载入中……</p>}
        {items?.length === 0 && (
          <p className="py-10 text-center text-sm leading-relaxed text-muted">
            {query ? '没有匹配的素材。' : '这个分类还是空的。'}
            <br />
            <a href="/library" target="_blank" rel="noreferrer" className="text-accent">
              去素材库添加
            </a>
          </p>
        )}
        <div className="grid grid-cols-3 gap-3">
          {items?.map((it) => (
            <ItemTile
              key={it.id}
              item={it}
              draggable={!replacing}
              title={replacing ? `${it.name}\n点一下替换选中的图片` : `${it.name}\n点一下插入，或拖到画布上`}
              onClick={() => onInsert(it)}
              onDragState={setDragging}
            />
          ))}
        </div>
      </div>
      <p className="m-0 border-t border-line px-5 py-2.5 text-[11px] text-faint">{replacing ? '点一张图替换选中的图片，新图按原比例放进原来的位置。' : '点一下插入到画布，或者直接拖到想放的位置。'}</p>
    </div>
  )
}
