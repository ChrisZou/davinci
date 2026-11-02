import { useEffect, useRef, useState, type ReactNode } from 'react'
import { useFavoriteColors } from './favoriteColorState'
import { normalizeColor } from './colorValue'

/**
 * Field primitives for the panels.
 *
 * The rule they all follow: a control never writes to the document per
 * keystroke. They hold what the human typed, and hand a value over only when it
 * settles (blur, Enter, release) or after a short debounce while dragging.
 * Otherwise dragging a slider across a 400px range would bury the undo stack
 * under 400 steps, and typing "1920" would resize the canvas three times.
 */

const DEBOUNCE_MS = 350

/** Where the change actually goes. */
type Commit<T> = (v: T) => void

/**
 * A number in a paper box: the label on the left, the value on the right.
 * Arrow keys nudge it (the browser's number input does that already).
 */
export function NumField({
  label,
  value,
  onCommit,
  onPreview,
  step = 1,
  min,
  max,
  suffix,
  scale = 1,
  testId,
  ariaLabel,
}: {
  label: string
  value: number
  onCommit: Commit<number>
  /** Preview while typing; null cancels an incomplete or abandoned input. */
  onPreview?: (value: number | null) => void
  step?: number
  min?: number
  max?: number
  suffix?: string
  /** Displays value×scale and commits the typed number ÷ scale (percentages). */
  scale?: number
  testId?: string
  ariaLabel?: string
}) {
  const shown = Math.round(value * scale * 100) / 100
  const [draft, setDraft] = useState(String(shown))
  const [focused, setFocused] = useState(false)
  const preview = useRef(onPreview)
  preview.current = onPreview
  useEffect(() => () => preview.current?.(null), [])
  useEffect(() => {
    if (!focused) setDraft(String(shown))
  }, [shown, focused])

  const parse = (raw: string) => {
    const n = Number(raw)
    if (!Number.isFinite(n) || raw.trim() === '') return null
    let next = n / scale
    if (min !== undefined) next = Math.max(min, next)
    if (max !== undefined) next = Math.min(max, next)
    return next
  }

  const commit = (raw: string) => {
    const next = parse(raw)
    if (next === null) {
      setDraft(String(shown))
      onPreview?.(null)
      return
    }
    onCommit(next)
  }

  return (
    <label className="flex h-9 min-w-0 flex-1 cursor-text items-center justify-between gap-2 rounded-[10px] bg-paper px-3 focus-within:ring-2 focus-within:ring-accent/30">
      {label && <span className="shrink-0 text-xs text-muted">{label}</span>}
      <span className="flex min-w-0 items-center gap-0.5">
        <input
          type="number"
          step={step}
          data-testid={testId}
          aria-label={ariaLabel}
          className="w-full min-w-0 bg-transparent text-right text-[13px] tabular-nums text-ink outline-none"
          value={draft}
          onFocus={(e) => {
            setFocused(true)
            e.target.select()
          }}
          onChange={(e) => {
            setDraft(e.target.value)
            onPreview?.(parse(e.target.value))
          }}
          onBlur={() => {
            setFocused(false)
            commit(draft)
          }}
          onKeyDown={(e) => {
            if (e.key === 'Enter') {
              e.preventDefault()
              ;(e.target as HTMLInputElement).blur()
            }
          }}
        />
        {suffix && <span className="text-xs text-faint">{suffix}</span>}
      </span>
    </label>
  )
}

/** A colour picker with a shared, persistent collection of individual colours. */
export function ColorField({
  label,
  value,
  onCommit,
  testId,
  allowNone,
  showFavorites = true,
}: {
  label: string
  value: string
  onCommit: Commit<string>
  testId?: string
  /**
   * The property can be absent (no stroke, no text background). An absent
   * colour shows as "无" instead of a black it does not have, and gets a clear
   * button that commits "".
   */
  allowNone?: boolean
  /** Compact controls (such as gradient endpoints) can omit the swatch row. */
  showFavorites?: boolean
}) {
  const favorites = useFavoriteColors()
  const [saveError, setSaveError] = useState('')
  const none = !value || value === 'transparent' || value === 'none'
  // The native picker needs six-digit hex; the text field preserves alpha.
  const hex = normalizeColor(value)?.slice(0, 7) ?? (none ? '#ffffff' : '#000000')
  const [draft, setDraft] = useState(none ? '' : value)
  const timer = useRef<number | undefined>(undefined)
  useEffect(() => {
    setDraft(none ? '' : value)
  }, [value, none])
  useEffect(() => () => window.clearTimeout(timer.current), [])

  const set = (next: string) => {
    setDraft(next)
    window.clearTimeout(timer.current)
    timer.current = window.setTimeout(() => onCommit(next), DEBOUNCE_MS)
  }
  const now = (next: string) => {
    window.clearTimeout(timer.current)
    setDraft(next)
    onCommit(next)
  }
  const color = normalizeColor(draft)
  const swatches = favorites.colors ?? []
  const starred = color !== null && swatches.includes(color)
  const favoriteLabel = starred ? '取消收藏' : '收藏颜色'

  return (
    <div className="flex min-w-0 flex-1 flex-col gap-2">
      <span className="text-xs text-muted">{label}</span>
      <div className="flex items-center gap-2">
        <label className="relative h-8 w-8 shrink-0 cursor-pointer" title="选择颜色">
          <input
            type="color"
            data-testid={testId}
            aria-label={label}
            className="absolute inset-0 h-full w-full cursor-pointer opacity-0"
            value={hex}
            onChange={(e) => set(e.target.value)}
          />
          <span
            className={`pointer-events-none absolute inset-0 rounded-full border border-line-strong ${none ? 'dv-none' : ''}`}
            style={none ? undefined : { background: value }}
          />
        </label>
        <div className="relative flex min-w-0 flex-1 items-center rounded-[10px] bg-paper focus-within:ring-2 focus-within:ring-accent/30">
          <input
            className="h-8 w-full min-w-0 rounded-[10px] bg-transparent pl-3 pr-9 text-[13px] text-ink outline-none placeholder:text-faint"
            value={draft}
            aria-label={`${label}（色值）`}
            placeholder={allowNone ? '无' : ''}
            onChange={(e) => set(e.target.value)}
            onBlur={() => {
              window.clearTimeout(timer.current)
              if (draft !== (none ? '' : value)) onCommit(draft)
            }}
          />
          <button
            type="button"
            title={favoriteLabel}
            aria-label={`${favoriteLabel} ${label}${color ? ` ${color}` : ''}`}
            aria-pressed={starred}
            data-testid="favorite-color"
            disabled={!color || favorites.colors === null || favorites.saving}
            className={`absolute right-1 flex h-6 w-6 items-center justify-center rounded-md transition-colors hover:bg-card disabled:cursor-default disabled:opacity-30 ${starred ? 'text-accent' : 'text-faint hover:text-ink'}`}
            onClick={() => {
              if (!color) return
              setSaveError('')
              void favorites.setFavorite(color, !starred).catch(() => setSaveError('收藏失败，请重试'))
            }}
          >
            <svg width="15" height="15" viewBox="0 0 24 24" fill={starred ? 'currentColor' : 'none'} stroke="currentColor" strokeWidth="1.8" strokeLinejoin="round" aria-hidden="true">
              <path d="m12 3.5 2.6 5.3 5.9.9-4.3 4.1 1 5.8L12 16.9l-5.2 2.7 1-5.8-4.3-4.1 5.9-.9z" />
            </svg>
          </button>
        </div>
        {allowNone && !none && (
          <button type="button" title="去掉颜色" aria-label="去掉颜色" className="icon-btn h-8 w-8 shrink-0 text-faint" onClick={() => now('')}>
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round">
              <path d="M6 6l12 12M18 6 6 18" />
            </svg>
          </button>
        )}
      </div>
      {favorites.error && <button type="button" className="text-left text-xs text-accent" onClick={() => { void favorites.reload() }}>{favorites.error}</button>}
      {saveError && <span role="alert" className="text-xs text-accent">{saveError}</span>}
      {showFavorites && swatches.length > 0 && (
        <div className="flex max-h-32 flex-wrap gap-1.5 overflow-y-auto p-0.5" role="group" aria-label="收藏的颜色">
          {swatches.map((c) => (
            <button
              key={c}
              type="button"
              title={c}
              aria-label={`使用收藏颜色 ${c}`}
              aria-pressed={c === color}
              className={`h-6 w-6 shrink-0 rounded-full border border-line-strong transition-transform hover:scale-110 ${c === color ? 'ring-1 ring-accent ring-offset-1' : ''}`}
              style={{ background: c }}
              onClick={() => now(c)}
            />
          ))}
        </div>
      )}
    </div>
  )
}

export function Select({
  label,
  value,
  options,
  onChange,
  testId,
}: {
  label: string
  value: string
  options: { value: string; label: string }[]
  onChange: (v: string) => void
  testId?: string
}) {
  return (
    <label className="flex min-w-0 flex-1 flex-col gap-2">
      {label && <span className="text-xs text-muted">{label}</span>}
      <span className="relative">
        <select
          data-testid={testId}
          className="h-9 w-full cursor-pointer appearance-none rounded-[10px] bg-paper pl-3 pr-8 text-[13px] text-ink outline-none focus:ring-2 focus:ring-accent/30"
          value={value}
          onChange={(e) => onChange(e.target.value)}
        >
          {options.map((o) => (
            <option key={o.value} value={o.value}>
              {o.label}
            </option>
          ))}
        </select>
        <svg className="pointer-events-none absolute right-3 top-1/2 -translate-y-1/2 text-faint" width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
          <path d="m6 9 6 6 6-6" />
        </svg>
      </span>
    </label>
  )
}

/** A two-to-four way switch; commits immediately since it is a discrete choice. */
export function Segmented<T extends string>({
  label,
  value,
  options,
  onChange,
}: {
  label: string
  value: T
  options: { value: T; label: ReactNode; title?: string }[]
  onChange: (v: T) => void
}) {
  return (
    <div className="flex min-w-0 flex-1 flex-col gap-2">
      {label && <span className="text-xs text-muted">{label}</span>}
      <div className="flex gap-0.5 rounded-[10px] bg-paper p-0.5">
        {options.map((o) => (
          <button
            key={o.value}
            type="button"
            title={o.title}
            aria-pressed={value === o.value}
            className={`flex h-8 min-w-0 flex-1 items-center justify-center whitespace-nowrap rounded-lg px-1.5 text-xs transition-colors ${
              value === o.value ? 'bg-card font-bold text-ink shadow-[0_1px_2px_rgba(60,48,20,.12)]' : 'text-muted hover:text-ink'
            }`}
            onClick={() => onChange(o.value)}
          >
            {o.label}
          </button>
        ))}
      </div>
    </div>
  )
}

/** A drag control for a continuous value (filters, opacity, line height). */
export function Slider({
  label,
  value,
  onChange,
  min,
  max,
  step,
  display,
  testId,
  onPreview,
}: {
  label: string
  value: number
  onChange: Commit<number>
  /** Called on every movement, for showing the value before it is committed. */
  onPreview?: (v: number) => void
  min: number
  max: number
  step: number
  display?: (v: number) => string
  testId?: string
}) {
  const [drag, setDrag] = useState<number | null>(null)
  const timer = useRef<number | undefined>(undefined)
  useEffect(() => () => window.clearTimeout(timer.current), [])
  const shown = drag ?? value

  const set = (next: number) => {
    setDrag(next)
    onPreview?.(next)
    window.clearTimeout(timer.current)
    timer.current = window.setTimeout(() => {
      setDrag(null)
      onChange(next)
    }, DEBOUNCE_MS)
  }

  const pct = ((shown - min) / (max - min || 1)) * 100

  return (
    <label className="flex flex-col gap-2">
      <span className="flex justify-between text-xs">
        <span className="text-muted">{label}</span>
        <span className="tabular-nums text-ink">{display ? display(shown) : shown}</span>
      </span>
      <input
        type="range"
        min={min}
        max={max}
        step={step}
        value={shown}
        data-testid={testId}
        className="h-1.5 w-full cursor-pointer appearance-none rounded-full accent-accent"
        style={{ background: `linear-gradient(to right, var(--color-accent) ${pct}%, var(--color-line) ${pct}%)` }}
        onChange={(e) => set(Number(e.target.value))}
        onPointerUp={() => {
          window.clearTimeout(timer.current)
          setDrag(null)
          onChange(shown)
        }}
        onBlur={() => {
          window.clearTimeout(timer.current)
          // A value still pending must land, not vanish: its preview is already
          // on the canvas.
          if (drag !== null) onChange(drag)
          setDrag(null)
        }}
      />
    </label>
  )
}

export function Toggle({
  label,
  on,
  onToggle,
}: {
  label: string
  on: boolean
  onToggle: (v: boolean) => void
}) {
  return (
    <button
      type="button"
      aria-pressed={on}
      className={`h-8 rounded-[10px] px-3 text-xs transition-colors ${
        on ? 'bg-ink font-bold text-card' : 'bg-paper text-ink hover:bg-paper-deep'
      }`}
      onClick={() => onToggle(!on)}
    >
      {label}
    </button>
  )
}

/** An on/off switch, for a setting that unfolds more settings when on. */
export function Switch({ label, on, onToggle }: { label: string; on: boolean; onToggle: (v: boolean) => void }) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={on}
      aria-label={label}
      className={`relative h-5 w-9 shrink-0 rounded-full transition-colors ${on ? 'bg-accent' : 'bg-line-strong'}`}
      onClick={() => onToggle(!on)}
    >
      <span className={`absolute top-0.5 h-4 w-4 rounded-full bg-white shadow-sm transition-[left] ${on ? 'left-[18px]' : 'left-0.5'}`} />
    </button>
  )
}

export function Row({ children }: { children: ReactNode }) {
  return <div className="flex flex-wrap items-end gap-2">{children}</div>
}

export function Section({
  title,
  children,
  right,
}: {
  title: string
  children: ReactNode
  right?: ReactNode
}) {
  return (
    <section className="flex flex-col gap-3 border-b border-line px-5 py-5 last:border-b-0">
      <div className="flex min-h-6 items-center justify-between gap-2">
        <h2 className="section-title">{title}</h2>
        {right}
      </div>
      {children}
    </section>
  )
}
