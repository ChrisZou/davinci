import { useCallback, useEffect, useRef, useState } from 'react'
import { api, type LibraryCategory, type LibraryItem } from '../api'

/**
 * Pieces shared by the library page and the editor's material panel: the data
 * (categories, and the items of the chosen one matching the search) and the
 * tile a picture is shown in.
 */

/** Drag payload type: a library item dragged onto the canvas. */
export const LIBRARY_DRAG_TYPE = 'application/x-davinci-library'

export function useLibrary(category: string, query: string) {
  const [categories, setCategories] = useState<LibraryCategory[] | null>(null)
  const [items, setItems] = useState<LibraryItem[] | null>(null)
  const [error, setError] = useState('')
  const seq = useRef(0)

  const loadCategories = useCallback(async () => {
    try {
      setCategories(await api.libraryCategories())
    } catch (e: any) {
      setError(e?.message ?? String(e))
    }
  }, [])

  const loadItems = useCallback(async () => {
    const mine = ++seq.current
    try {
      const out = await api.libraryItems({ category: category || undefined, q: query.trim() || undefined })
      // A slower answer to an older search must not overwrite a newer one.
      if (mine === seq.current) setItems(out.items)
    } catch (e: any) {
      if (mine === seq.current) setError(e?.message ?? String(e))
    }
  }, [category, query])

  useEffect(() => {
    void loadCategories()
  }, [loadCategories])

  // Typing in the search box waits for a pause before asking the server.
  useEffect(() => {
    const t = window.setTimeout(() => void loadItems(), query ? 200 : 0)
    return () => window.clearTimeout(t)
  }, [loadItems, query])

  const reload = useCallback(async () => {
    await Promise.all([loadCategories(), loadItems()])
  }, [loadCategories, loadItems])

  return { categories, items, error, setError, reload }
}

/**
 * A picture on a checkerboard (so a cut-out reads as a cut-out), letterboxed
 * into a square. Draggable onto the canvas when `draggable` is set.
 */
export function ItemTile({
  item,
  selected,
  onClick,
  draggable,
  caption = true,
  title,
  onDragState,
}: {
  item: LibraryItem
  selected?: boolean
  onClick?: () => void
  draggable?: boolean
  caption?: boolean
  title?: string
  /** Told when a drag of this tile starts and ends. */
  onDragState?: (dragging: boolean) => void
}) {
  return (
    <button
      type="button"
      title={title ?? item.name}
      draggable={draggable}
      onDragStart={(e) => {
        if (!draggable) return
        e.dataTransfer.effectAllowed = 'copy'
        e.dataTransfer.setData(LIBRARY_DRAG_TYPE, JSON.stringify(item))
        // Firefox refuses to start a drag without some plain data.
        e.dataTransfer.setData('text/plain', item.name)
        onDragState?.(true)
      }}
      onDragEnd={() => onDragState?.(false)}
      onClick={onClick}
      className="group flex min-w-0 flex-col gap-1.5 text-left"
      data-testid="library-item"
    >
      <span
        className={`dv-checker flex aspect-square w-full items-center justify-center overflow-hidden rounded-xl p-2 transition-shadow ${
          selected ? 'ring-2 ring-accent ring-offset-2 ring-offset-card' : 'group-hover:shadow-[var(--shadow-float)]'
        }`}
      >
        <img src={item.thumb} alt="" loading="lazy" draggable={false} className="max-h-full max-w-full object-contain" />
      </span>
      {caption && <span className="truncate px-0.5 text-xs text-ink-2">{item.name}</span>}
    </button>
  )
}

/** A short, readable name for an imported portrait's annotation. */
export function firstLine(s: string, max = 60): string {
  const line = s.split('\n').find((l) => l.trim()) ?? ''
  return line.length > max ? `${line.slice(0, max)}…` : line
}
