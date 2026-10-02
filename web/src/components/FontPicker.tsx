import { useEffect, useMemo, useRef, useState } from 'react'
import { createPortal } from 'react-dom'
import { api, type FontInfo } from '../api'
import { fontStack } from '../editor/fonts'

/**
 * The font picker: a search box, the starred fonts on top, then the rest.
 * Fonts nobody puts on a cover (system UI faces, other scripts' faces) are
 * hidden until "显示隐藏的字体"; any font can be starred or hidden by hand.
 * Stars and hides live on the server, so the CLI lists fonts the same way.
 */
export function FontPicker({ value, onChange }: { value: string; onChange: (family: string) => void }) {
  const [fonts, setFonts] = useState<FontInfo[]>([])
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState('')
  const [showHidden, setShowHidden] = useState(false)
  const box = useRef<HTMLDivElement>(null)
  const pop = useRef<HTMLDivElement>(null)
  const [at, setAt] = useState<{ top: number; right: number; maxHeight: number } | null>(null)
  const search = useRef<HTMLInputElement>(null)

  useEffect(() => {
    void api.fonts().then(setFonts).catch(() => setFonts([]))
  }, [])

  useEffect(() => {
    if (!open) return
    // The list floats over the page (the side panel clips anything wider
    // than itself), pinned under the button's right edge.
    const r = box.current?.getBoundingClientRect()
    if (r) setAt({ top: r.bottom + 6, right: window.innerWidth - r.right, maxHeight: Math.max(240, window.innerHeight - r.bottom - 24) })
    const onDown = (e: MouseEvent) => {
      const t = e.target as Node
      if (!box.current?.contains(t) && !pop.current?.contains(t)) setOpen(false)
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

  const q = query.trim().toLowerCase()
  const { favorites, rest, hiddenCount } = useMemo(() => {
    const match = (f: FontInfo) => !q || f.family.toLowerCase().includes(q) || (f.aliases ?? []).some((a) => a.toLowerCase().includes(q))
    return {
      favorites: fonts.filter((f) => f.favorite && match(f)),
      rest: fonts.filter((f) => !f.favorite && (showHidden || !f.hidden) && match(f)),
      hiddenCount: fonts.filter((f) => f.hidden).length,
    }
  }, [fonts, q, showHidden])

  const current = fonts.find((f) => f.family === value)
  const setPref = (family: string, pref: { favorite?: boolean; hidden?: boolean }) => {
    void api.setFontPref(family, pref).then(setFonts)
  }
  const pick = (family: string) => {
    setOpen(false)
    setQuery('')
    onChange(family)
  }

  const row = (f: FontInfo) => (
    <div key={f.family} className={`group flex h-9 items-center gap-1 rounded-lg pr-1 hover:bg-paper ${f.family === value ? 'bg-paper' : ''}`}>
      <button
        className={`flex h-8 w-8 shrink-0 items-center justify-center rounded-md ${f.favorite ? 'text-accent' : 'text-faint opacity-0 hover:text-ink group-hover:opacity-100'}`}
        title={f.favorite ? '取消收藏' : '收藏'}
        aria-label={f.favorite ? `取消收藏 ${f.family}` : `收藏 ${f.family}`}
        onClick={() => setPref(f.family, { favorite: !f.favorite })}
      >
        <Star filled={!!f.favorite} />
      </button>
      <button className="flex min-w-0 flex-1 items-baseline gap-2 text-left" onClick={() => pick(f.family)} title={f.family}>
        {/* A Chinese font shows its Chinese name, in itself: that is what it is known by, and what a cover will use it for. */}
        <span className={`${chineseName(f) ? 'max-w-[65%] shrink-0' : 'min-w-0'} truncate text-[15px] ${f.hidden ? 'text-faint' : 'text-ink'}`} style={{ fontFamily: fontStack(f.family) }}>
          {chineseName(f) || f.family}
        </span>
        {chineseName(f) && <span className="min-w-0 truncate text-[11px] text-faint">{f.family}</span>}
      </button>
      {!f.favorite && (
        <button
          className="h-7 shrink-0 rounded-md px-2 text-[11px] text-faint opacity-0 hover:bg-card hover:text-ink group-hover:opacity-100"
          onClick={() => setPref(f.family, { hidden: !f.hidden })}
        >
          {f.hidden ? '取消隐藏' : '隐藏'}
        </button>
      )}
    </div>
  )

  return (
    <div className="relative" ref={box}>
      <button
        className="flex h-9 w-full items-center justify-between rounded-[10px] bg-paper pl-3 pr-3 text-[13px] text-ink outline-none focus:ring-2 focus:ring-accent/30"
        onClick={() => setOpen((v) => !v)}
        aria-haspopup="listbox"
        aria-expanded={open}
        data-testid="text-font"
      >
        <span className="truncate" style={value ? { fontFamily: fontStack(value) } : undefined}>
          {value ? (current && chineseName(current)) || value : '默认字体'}
        </span>
        <svg className="shrink-0 text-faint" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
          <path d="m6 9 6 6 6-6" />
        </svg>
      </button>
      {open && at && createPortal(
        <div
          ref={pop}
          className="menu fixed z-50 flex w-[320px] flex-col p-1.5"
          style={{ top: at.top, right: at.right, maxHeight: Math.min(at.maxHeight, window.innerHeight * 0.7) }}
          role="listbox"
        >
          <input
            ref={search}
            autoFocus
            className="mb-1.5 h-8 shrink-0 rounded-lg bg-paper px-2.5 text-[13px] text-ink outline-none placeholder:text-faint focus:ring-2 focus:ring-accent/30"
            placeholder="搜索字体"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            onKeyDown={(e) => e.stopPropagation()}
            data-testid="font-search"
          />
          <div className="min-h-0 overflow-y-auto">
            {!q && (
              <button className={`flex h-9 w-full items-center rounded-lg pl-9 text-left text-[13px] hover:bg-paper ${!value ? 'bg-paper' : ''}`} onClick={() => pick('')}>
                默认字体
              </button>
            )}
            {favorites.length > 0 && (
              <>
                <div className="px-2 pb-1 pt-2 text-[11px] text-faint">收藏</div>
                {favorites.map(row)}
                <div className="mx-2 my-1.5 h-px bg-line" />
              </>
            )}
            {rest.map(row)}
            {!favorites.length && !rest.length && <div className="px-3 py-4 text-center text-xs text-faint">没有找到「{query}」</div>}
          </div>
          {hiddenCount > 0 && (
            <button className="mt-1 h-8 shrink-0 rounded-lg text-[12px] text-muted hover:bg-paper hover:text-ink" onClick={() => setShowHidden((v) => !v)}>
              {showHidden ? '收起隐藏的字体' : `显示隐藏的字体（${hiddenCount}）`}
            </button>
          )}
        </div>,
        document.body,
      )}
    </div>
  )
}

/** A family's Chinese name, simplified when it has one ("苹方-简", not "蘋方-簡"). */
function chineseName(f: FontInfo): string {
  const names = (f.aliases ?? []).filter((a) => /[\u4e00-\u9fff]/.test(a))
  return names.find((a) => /[简体]/.test(a) && !/[體簡]/.test(a)) ?? names[0] ?? ''
}

function Star({ filled }: { filled: boolean }) {
  return (
    <svg width="15" height="15" viewBox="0 0 24 24" fill={filled ? 'currentColor' : 'none'} stroke="currentColor" strokeWidth="1.8" strokeLinejoin="round">
      <path d="m12 3.5 2.6 5.3 5.9.9-4.3 4.1 1 5.8L12 16.9l-5.2 2.7 1-5.8-4.3-4.1 5.9-.9z" />
    </svg>
  )
}
