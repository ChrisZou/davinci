import { useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import type { Document, Layer } from '../types'
import { FontPicker } from './FontPicker'
import { presets } from '../editor/presets'
import { textPresets, type TextPreset } from '../editor/textPresets'
import { parseStroke, parseShadow } from '../editor/style'
import { DEFAULT_SHEAR, SHEAR, WARPS, warpShape } from '../editor/warp'
import { keys } from '../editor/keys'
import { ColorField, NumField, Section, Segmented, Select, Slider, Switch, Toggle } from './fields'

/**
 * The right-hand panel: whatever the selection is, or the canvas when nothing
 * is selected.
 *
 * Every control emits a command — the same JSON `davinci exec` takes — and the
 * server applies it, which is why the panel can never do something AI could
 * not, and why the two share one undo stack.
 */

export interface PanelProps {
  doc: Document
  layer?: Layer
  /** Every selected layer, back-to-front; more than one shows the multi panel. */
  selection: Layer[]
  /** Runs a command; the promise settles once the server has answered. */
  onRun: (cmd: unknown) => void | Promise<unknown>
  /** Runs a command and selects whatever layers it produced. */
  onRunSelect: (cmd: unknown) => void
  /** Uploads a local file and returns its /assets/ URL, or null on failure. */
  onUpload: (file: File) => Promise<string | null>
  /** Opens the material library to pick a replacement for an image layer. */
  onPickFromLibrary?: (layer: Layer) => void
  /** Opens the crop dialog for an image layer. */
  onCrop: (layer: Layer) => void
  /** Shows a slider's value on the canvas before it is committed. */
  onPreview: (id: string, key: 'opacity' | 'filters', value: number | Record<string, number>) => void
}

/**
 * Only what the selection can use: a selected layer shows its own settings,
 * several show what applies to several, and the canvas settings are what the
 * panel shows when nothing is selected (click empty canvas or press Esc).
 */
export function Properties(props: PanelProps) {
  const { layer, selection } = props
  return (
    <div className="min-h-0 flex-1 overflow-y-auto" data-testid="properties">
      {selection.length > 1 ? (
        <MultiSection {...props} />
      ) : layer ? (
        <LayerSection {...props} layer={layer} />
      ) : (
        <CanvasSection {...props} />
      )}
    </div>
  )
}

/**
 * The colours this design already uses, most-used first. A cover lives on a
 * handful of colours, so offering them as swatches is usually the whole choice.
 */
function docColours(doc: Document): string[] {
  const count = new Map<string, number>()
  const add = (c: unknown) => {
    if (typeof c !== 'string') return
    const s = c.trim().toLowerCase()
    const hex = /^#([0-9a-f]{3}|[0-9a-f]{6})$/.exec(s)
    if (!hex) return
    const full = hex[1].length === 3 ? `#${hex[1].split('').map((x) => x + x).join('')}` : s
    count.set(full, (count.get(full) ?? 0) + 1)
  }
  add(doc.canvas.background)
  const walk = (layers: Layer[]) => {
    for (const l of layers) {
      const st = (l.style ?? {}) as Record<string, unknown>
      add(st.fill)
      add(st.textBackgroundColor)
      add(parseStroke(st.stroke as string | undefined).stroke)
      if (l.children) walk(l.children)
    }
  }
  walk(doc.layers)
  return [...count.entries()].sort((a, b) => b[1] - a[1]).map(([c]) => c)
}

// --- shared bits ----------------------------------------------------------

const TYPE_LABEL: Record<Layer['type'], string> = { text: '文字图层', image: '图片图层', shape: '形状图层', group: '编组' }

function Icon({ d, size = 16 }: { d: string; size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round">
      <path d={d} />
    </svg>
  )
}

/** Align-to-edge glyphs: a bar for the edge or centre line, two blocks against it. */
const ALIGN_ICONS: Record<string, string> = {
  left: 'M4 3v18M8 7h10M8 13h6',
  center: 'M12 3v18M6 7h12M8 13h8',
  right: 'M20 3v18M6 7h10M10 13h6',
  top: 'M3 4h18M7 8v10M13 8v6',
  middle: 'M3 12h18M7 6v12M13 8v8',
  bottom: 'M3 20h18M7 6v10M13 10v6',
}

const ALIGNS = [
  { h: 'left', v: undefined, icon: 'left', title: '左对齐' },
  { h: 'center', v: undefined, icon: 'center', title: '水平居中' },
  { h: 'right', v: undefined, icon: 'right', title: '右对齐' },
  { h: undefined, v: 'top', icon: 'top', title: '顶对齐' },
  { h: undefined, v: 'middle', icon: 'middle', title: '垂直居中' },
  { h: undefined, v: 'bottom', icon: 'bottom', title: '底对齐' },
] as const

function AlignRow({ onAlign }: { onAlign: (h?: string, v?: string) => void }) {
  return (
    <div className="grid grid-cols-6 gap-1 rounded-[10px] bg-paper p-0.5">
      {ALIGNS.map((a) => (
        <button
          key={a.title}
          type="button"
          title={a.title}
          aria-label={a.title}
          className="flex h-8 items-center justify-center rounded-lg text-ink-2 transition-colors hover:bg-card hover:text-ink"
          onClick={() => onAlign(a.h, a.v)}
        >
          <Icon d={ALIGN_ICONS[a.icon]} />
        </button>
      ))}
    </div>
  )
}

/**
 * 变形, the way 稿定 does it: a card showing the current shape; clicking it
 * opens a panel of shapes — 梯形, and 斜切 that makes a line climb — with the
 * chosen one's sliders under them.
 */
function WarpPicker({ style, patch }: { style: Record<string, any>; patch: (props: Record<string, unknown>) => void }) {
  const [open, setOpen] = useState(false)
  const [pos, setPos] = useState({ top: 0, left: 0 })
  const card = useRef<HTMLButtonElement>(null)
  const panel = useRef<HTMLDivElement>(null)
  // Chosen in the open panel: a shear dragged through 0° (which drops
  // skewY) keeps its tile and slider.
  const [shearing, setShearing] = useState(false)
  const warp = warpShape(style.warp)
  const shape = warp ?? (Number(style.skewY) || shearing ? SHEAR : undefined)

  useEffect(() => {
    if (!open) setShearing(false)
  }, [open])

  useEffect(() => {
    if (!open) return
    const onDown = (e: MouseEvent) => {
      const t = e.target as Node
      if (panel.current?.contains(t) || card.current?.contains(t)) return
      setOpen(false)
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

  const toggle = () => {
    const r = card.current?.getBoundingClientRect()
    if (r) {
      // Beside the property panel, level with the card, kept on screen.
      const h = 380
      setPos({ left: Math.max(8, r.left - 336), top: Math.max(8, Math.min(r.top - 20, window.innerHeight - h - 8)) })
    }
    setOpen((v) => !v)
  }

  // Picking one shape drops the other: a warp and a shear do not stack.
  const pick = (key: string) => {
    setShearing(key === SHEAR.key)
    if (!key) patch({ warp: 'none', skewY: 0 })
    else if (key === SHEAR.key) patch({ warp: 'none', skewY: Number(style.skewY) || DEFAULT_SHEAR })
    else patch({ warp: key, warpAmount: Number(style.warpAmount) || 30, skewY: 0 })
  }

  const tile = (key: string, label: string, icon: string | null) => {
    const on = (shape?.key ?? '') === key
    return (
      <button
        key={key || 'none'}
        title={label}
        aria-pressed={on}
        onClick={() => pick(key)}
        className={`flex h-[72px] items-center justify-center rounded-xl bg-paper transition-shadow hover:bg-paper-deep ${on ? 'ring-2 ring-accent' : ''}`}
      >
        {icon ? <WarpIcon d={icon} size={44} /> : <span className="text-xs text-muted">无</span>}
      </button>
    )
  }

  return (
    <>
      <button
        ref={card}
        onClick={toggle}
        aria-expanded={open}
        data-testid="warp-card"
        className="flex h-14 w-full items-center gap-3 rounded-xl bg-paper px-2.5 text-left hover:bg-paper-deep"
      >
        <span className="flex h-10 w-12 items-center justify-center rounded-lg bg-card">
          {shape ? <WarpIcon d={shape.icon} size={30} /> : <span className="text-[11px] text-faint">无</span>}
        </span>
        <span className="flex-1 text-[13px] font-bold text-ink">变形</span>
        {shape && <span className="text-xs text-muted">{shape === SHEAR ? `斜切 ${Number(style.skewY)}°` : shape.label}</span>}
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" className="text-muted">
          <path d={open ? 'm6 15 6-6 6 6' : 'm6 9 6 6 6-6'} />
        </svg>
      </button>
      {open && (
        <div ref={panel} className="menu fixed z-40 w-[320px] !p-4" style={pos} role="dialog" aria-label="变形" data-testid="warp-panel">
          <div className="mb-3 flex items-center justify-between">
            <h3 className="m-0 text-[15px] font-bold">变形</h3>
            <button className="icon-btn h-7 w-7" aria-label="关闭" onClick={() => setOpen(false)}>
              <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8">
                <path d="M6 6l12 12M18 6 6 18" />
              </svg>
            </button>
          </div>
          <div className="grid grid-cols-3 gap-2.5">
            {tile('', '无变形', null)}
            {WARPS.map((w) => tile(w.key, w.label, w.icon))}
            {tile(SHEAR.key, SHEAR.label, SHEAR.icon)}
          </div>
          {warp && (
            <div className="mt-4 flex flex-col gap-3">
              <Slider
                label={`${warp.label}强度`}
                value={Number(style.warpAmount ?? 0)}
                min={-100}
                max={100}
                step={1}
                testId="warp-amount"
                onChange={(warpAmount) => patch({ warpAmount })}
              />
              <Slider
                label="相对高度"
                value={Number(style.warpBias ?? 0)}
                min={-100}
                max={100}
                step={1}
                testId="warp-bias"
                onChange={(warpBias) => patch({ warpBias })}
              />
            </div>
          )}
          {shape === SHEAR && (
            <div className="mt-4">
              <Slider
                label="斜切角度"
                value={Number(style.skewY ?? 0)}
                min={-30}
                max={30}
                step={1}
                display={(v) => `${v}°`}
                testId="skew-y"
                onChange={(skewY) => patch({ skewY })}
              />
            </div>
          )}
        </div>
      )}
    </>
  )
}

function WarpIcon({ d, size }: { d: string; size: number }) {
  return (
    <svg width={size} height={(size * 24) / 40} viewBox="0 0 40 24" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinejoin="round">
      <path d={d} />
    </svg>
  )
}

/**
 * "去除背景": rembg with a BiRefNet model, on the server. It takes a few
 * seconds (longer the first time, while the model loads), so the button says
 * so while it runs. Once cut, the picture can go back to its original.
 */
function CutoutButton({ layer, onRun }: { layer: Layer; onRun: PanelProps['onRun'] }) {
  const [busy, setBusy] = useState<string | null>(null)
  const [open, setOpen] = useState(false)
  const box = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (!open) return
    const onDown = (e: MouseEvent) => {
      if (!box.current?.contains(e.target as Node)) setOpen(false)
    }
    window.addEventListener('mousedown', onDown)
    return () => window.removeEventListener('mousedown', onDown)
  }, [open])
  const cut = layer.image?.original !== undefined && layer.image.original !== ''
  const go = async (label: string, cmd: Record<string, unknown>) => {
    setOpen(false)
    setBusy(label)
    try {
      await onRun({ type: 'removeBackground', id: layer.id, ...cmd })
    } finally {
      setBusy(null)
    }
  }
  if (busy) {
    return (
      <button className="chip w-full animate-pulse" disabled data-testid="remove-bg">
        {busy}
      </button>
    )
  }
  return (
    <div className="relative" ref={box}>
      <button className="chip w-full" aria-haspopup="menu" aria-expanded={open} onClick={() => setOpen((v) => !v)} data-testid="remove-bg">
        {cut ? '已去背景' : '去除背景'}
      </button>
      {open && (
        <div className="menu absolute right-0 top-full z-20 mt-1.5 w-44" role="menu">
          <button className="menu-item" role="menuitem" onClick={() => void go('抠图中…', { model: 'general' })}>
            <span>通用抠图</span>
            <span className="text-xs text-faint">物品、插画</span>
          </button>
          <button className="menu-item" role="menuitem" onClick={() => void go('抠图中…', { model: 'portrait' })}>
            <span>人像抠图</span>
            <span className="text-xs text-faint">头发更细</span>
          </button>
          {cut && (
            <button className="menu-item" role="menuitem" onClick={() => void go('恢复中…', { restore: true })}>
              恢复原图
            </button>
          )}
        </div>
      )}
    </div>
  )
}

/**
 * A drop shadow: a switch, then its colour, opacity, blur and offset. Stored
 * as "colour:blur:x:y" in board pixels ('' removes it); the panel shows the
 * colour's alpha as its own opacity field, which reads better than rgba().
 */
function ShadowFields({ value, fallback, onChange }: { value?: string; fallback: string; onChange: (shadow: string) => void }) {
  const shadow = parseShadow(value)
  const { hex, alpha } = splitAlpha(shadow?.color ?? '#000000')
  const set = (next: { hex?: string; alpha?: number; blur?: number; offsetX?: number; offsetY?: number }) => {
    const s = { ...shadow!, ...next }
    const color = joinAlpha(next.hex ?? hex, next.alpha ?? alpha)
    onChange(`${color}:${s.blur}:${s.offsetX}:${s.offsetY}`)
  }
  return (
    <div className="flex flex-col gap-3">
      <div className="flex items-center justify-between">
        <span className="text-xs text-muted">投影</span>
        <Switch label="投影" on={!!shadow} onToggle={(on) => onChange(on ? fallback : '')} />
      </div>
      {shadow && (
        <>
          <ColorField label="颜色" value={hex} onCommit={(c) => c && set({ hex: c })} />
          <div className="grid grid-cols-2 gap-2">
            <NumField label="不透明度" value={Math.round(alpha * 100)} min={0} max={100} suffix="%" onCommit={(v) => set({ alpha: Math.max(0, Math.min(100, v)) / 100 })} />
            <NumField label="模糊" value={shadow.blur} min={0} onCommit={(blur) => set({ blur })} />
            <NumField label="X" value={shadow.offsetX} onCommit={(offsetX) => set({ offsetX })} />
            <NumField label="Y" value={shadow.offsetY} onCommit={(offsetY) => set({ offsetY })} />
          </div>
        </>
      )}
    </div>
  )
}

/** A CSS colour as a solid hex and an alpha (0–1). */
function splitAlpha(c: string): { hex: string; alpha: number } {
  const s = c.trim()
  const m = s.match(/^rgba?\(\s*([\d.]+)[\s,]+([\d.]+)[\s,]+([\d.]+)(?:[\s,/]+([\d.]+%?))?\s*\)$/i)
  const h2 = (n: number) => Math.max(0, Math.min(255, Math.round(n))).toString(16).padStart(2, '0')
  if (m) {
    const a = m[4] === undefined ? 1 : m[4].endsWith('%') ? parseFloat(m[4]) / 100 : parseFloat(m[4])
    return { hex: `#${h2(+m[1])}${h2(+m[2])}${h2(+m[3])}`, alpha: Number.isFinite(a) ? a : 1 }
  }
  const x = s.match(/^#([0-9a-f]{3,8})$/i)?.[1]
  if (x && (x.length === 3 || x.length === 4)) {
    const e = x.split('').map((d) => d + d).join('')
    return { hex: `#${e.slice(0, 6)}`, alpha: e.length === 8 ? parseInt(e.slice(6), 16) / 255 : 1 }
  }
  if (x && (x.length === 6 || x.length === 8)) return { hex: `#${x.slice(0, 6)}`, alpha: x.length === 8 ? parseInt(x.slice(6), 16) / 255 : 1 }
  return { hex: s, alpha: 1 }
}

/** A hex colour with an alpha: the hex itself when opaque, else rgba(). */
function joinAlpha(hex: string, alpha: number): string {
  const a = Math.round(Math.max(0, Math.min(1, alpha)) * 100) / 100
  const x = hex.replace('#', '')
  if (a >= 1 || !/^[0-9a-f]{6}$/i.test(x)) return hex
  return `rgba(${parseInt(x.slice(0, 2), 16)},${parseInt(x.slice(2, 4), 16)},${parseInt(x.slice(4, 6), 16)},${a})`
}

/** "换一张图": a picture from the material library, or a file from this computer. */
function ReplaceMenu({ onLibrary, onLocal }: { onLibrary?: () => void; onLocal: () => void }) {
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
  const pick = (act: () => void) => {
    setOpen(false)
    act()
  }
  return (
    <div className="relative" ref={box}>
      <button className="chip w-full" aria-haspopup="menu" aria-expanded={open} onClick={() => setOpen((v) => !v)} data-testid="replace-image">
        换一张图
      </button>
      {open && (
        <div className="menu absolute right-0 top-full z-20 mt-1.5 w-40" role="menu">
          {onLibrary && (
            <button className="menu-item" role="menuitem" onClick={() => pick(onLibrary)}>
              从素材库选择
            </button>
          )}
          <button className="menu-item" role="menuitem" onClick={() => pick(onLocal)}>
            从本地上传
          </button>
        </div>
      )}
    </div>
  )
}

function Grid2({ children }: { children: ReactNode }) {
  return <div className="grid grid-cols-2 gap-2">{children}</div>
}

/** The panel's title block: what kind of thing is selected, and its name. */
function Header({ kicker, children, right }: { kicker: string; children: ReactNode; right?: ReactNode }) {
  return (
    <div className="flex items-start gap-2 border-b border-line px-5 pb-4 pt-5">
      <div className="flex min-w-0 flex-1 flex-col gap-1">
        <span className="text-xs text-muted">{kicker}</span>
        {children}
      </div>
      {right}
    </div>
  )
}

/**
 * The layer's name, as the panel's title. It holds a draft and renames on
 * Enter or blur: renaming per keystroke made every letter an undo step,
 * refused to let the box go empty mid-edit, and could collide with another
 * name halfway through a word.
 */
function NameField({ layer, onRun }: { layer: Layer; onRun: (cmd: unknown) => void }) {
  const [draft, setDraft] = useState(layer.name)
  const [focused, setFocused] = useState(false)
  useEffect(() => {
    if (!focused) setDraft(layer.name)
  }, [layer.id, layer.name, focused])
  const commit = () => {
    const name = draft.trim()
    if (name && name !== layer.name) onRun({ type: 'renameLayer', id: layer.id, name })
    else setDraft(layer.name)
  }
  return (
    <input
      className="-mx-1.5 w-full min-w-0 rounded-lg bg-transparent px-1.5 py-0.5 font-serif text-[22px] font-black text-ink outline-none hover:bg-paper focus:bg-paper"
      data-testid="layer-name"
      aria-label="图层名"
      title="点击改名"
      value={draft}
      onFocus={() => setFocused(true)}
      onChange={(e) => setDraft(e.target.value)}
      onBlur={() => {
        setFocused(false)
        commit()
      }}
      onKeyDown={(e) => {
        if (e.key === 'Enter') (e.target as HTMLInputElement).blur()
        if (e.key === 'Escape') {
          setDraft(layer.name)
          setFocused(false)
          ;(e.target as HTMLInputElement).blur()
        }
      }}
    />
  )
}

// --- nothing selected: the canvas ------------------------------------------

function CanvasSection({ doc, onRun, onUpload }: PanelProps) {
  const bgFile = useRef<HTMLInputElement>(null)
  const palette = useMemo(() => docColours(doc), [doc])

  return (
    <>
      <Header kicker="没有选中图层">
        <span className="font-serif text-[22px] font-black">画布</span>
      </Header>
      <Section title="尺寸">
        <Grid2>
          <NumField
            label="宽"
            value={doc.canvas.width}
            min={1}
            max={20000}
            testId="canvas-width"
            onCommit={(width) => onRun({ type: 'setCanvasSize', width, height: doc.canvas.height })}
          />
          <NumField
            label="高"
            value={doc.canvas.height}
            min={1}
            max={20000}
            testId="canvas-height"
            onCommit={(height) => onRun({ type: 'setCanvasSize', width: doc.canvas.width, height })}
          />
        </Grid2>
        <Select
          label=""
          value=""
          options={[
            { value: '', label: '换成常用尺寸…' },
            ...presets.map((p) => ({ value: p.key, label: `${p.name} · ${p.width}×${p.height}` })),
          ]}
          onChange={(key) => key && onRun({ type: 'setCanvasSize', preset: key })}
        />
      </Section>
      <Section title="背景">
        <ColorField
          label="颜色"
          allowNone
          palette={palette}
          value={doc.canvas.backgroundImage ? '' : doc.canvas.background ?? ''}
          testId="canvas-bg"
          onCommit={(color) => onRun({ type: 'setBackground', color: color || 'transparent' })}
        />
        <div className="flex gap-2">
          <button className="chip flex-1" onClick={() => bgFile.current?.click()}>
            {doc.canvas.backgroundImage ? '更换背景图' : '用图片做背景'}
          </button>
          {doc.canvas.backgroundImage && (
            <button className="chip" onClick={() => onRun({ type: 'setBackground', color: '#ffffff' })} title="移除背景图，回到纯色">
              去掉背景图
            </button>
          )}
        </div>
        <input
          ref={bgFile}
          type="file"
          accept="image/*"
          className="hidden"
          onChange={async (e) => {
            const f = e.target.files?.[0]
            e.target.value = ''
            const url = f ? await onUpload(f) : null
            if (url) onRun({ type: 'setBackground', image: url })
          }}
        />
      </Section>
      <p className="px-5 py-5 text-xs leading-relaxed text-faint">
        {keys('点选图层即可编辑，按住 ⇧ 或 ⌘ 可多选。图片可以直接拖进画布或 ⌘V 粘贴，按 ? 查看全部快捷键。')}
      </p>
    </>
  )
}

// --- several layers ------------------------------------------------------

function MultiSection({ selection, onRun, onRunSelect }: PanelProps) {
  const [toCanvas, setToCanvas] = useState(false)
  const ids = selection.map((l) => l.id)
  const groups = selection.filter((l) => l.type === 'group')
  return (
    <>
      <Header kicker="多选">
        <span className="font-serif text-[22px] font-black">{selection.length} 个图层</span>
        <span className="line-clamp-2 text-xs leading-relaxed text-muted">{[...selection].reverse().map((l) => l.name).join('、')}</span>
      </Header>
      <Section
        title="对齐"
        right={
          <div className="w-28">
            <Segmented
              label=""
              value={toCanvas ? 'canvas' : 'selection'}
              options={[
                { value: 'selection', label: '彼此', title: '对齐到选中图层的整体外框' },
                { value: 'canvas', label: '画布', title: '一起对齐到画布' },
              ]}
              onChange={(v) => setToCanvas(v === 'canvas')}
            />
          </div>
        }
      >
        <AlignRow onAlign={(h, v) => onRun({ type: 'alignLayers', ids, h, v, to: toCanvas ? 'canvas' : 'selection' })} />
        <Grid2>
          <button
            className="chip"
            disabled={selection.length < 3}
            title={selection.length < 3 ? '至少选 3 个图层' : '首尾不动，中间等距'}
            onClick={() => onRun({ type: 'distributeLayers', ids, axis: 'h' })}
          >
            水平等距
          </button>
          <button
            className="chip"
            disabled={selection.length < 3}
            title={selection.length < 3 ? '至少选 3 个图层' : '首尾不动，中间等距'}
            onClick={() => onRun({ type: 'distributeLayers', ids, axis: 'v' })}
          >
            垂直等距
          </button>
        </Grid2>
      </Section>
      <Section title="操作">
        <Grid2>
          <button className="chip" onClick={() => onRunSelect({ type: 'groupLayers', ids })}>
            编组 <span className="text-faint">{keys('⌘G')}</span>
          </button>
          {groups.length > 0 ? (
            <button className="chip" onClick={() => onRunSelect({ type: 'batch', commands: groups.map((g) => ({ type: 'ungroupLayers', id: g.id })) })}>
              解散编组
            </button>
          ) : (
            <button className="chip" onClick={() => onRunSelect({ type: 'batch', commands: ids.map((id) => ({ type: 'duplicateLayer', id })) })}>
              复制 <span className="text-faint">{keys('⌘D')}</span>
            </button>
          )}
        </Grid2>
        <button
          className="chip text-accent"
          onClick={() => onRun({ type: 'batch', commands: ids.map((id) => ({ type: 'removeLayer', id })) })}
        >
          删除这 {selection.length} 个图层
        </button>
      </Section>
    </>
  )
}

// --- one layer -----------------------------------------------------------

function LayerSection(props: PanelProps & { layer: Layer }) {
  const { doc, layer, onRun } = props
  const patch = (next: Record<string, unknown>) => onRun({ type: 'updateLayer', id: layer.id, props: next })
  const top = doc.layers[doc.layers.length - 1]?.id === layer.id
  const bottom = doc.layers[0]?.id === layer.id
  const palette = useMemo(() => docColours(doc), [doc])

  return (
    <>
      <Header
        kicker={TYPE_LABEL[layer.type] + (layer.type === 'group' ? ` · ${layer.children?.length ?? 0} 个图层` : '')}
        right={
          <div className="flex gap-0.5 pt-4">
            <button
              className="icon-btn h-8 w-8"
              title={layer.visible ? '隐藏' : '显示'}
              aria-label={layer.visible ? '隐藏' : '显示'}
              aria-pressed={!layer.visible}
              onClick={() => onRun({ type: 'setVisible', id: layer.id, visible: !layer.visible })}
            >
              {layer.visible ? <Icon d="M2 12s4-7 10-7 10 7 10 7-4 7-10 7S2 12 2 12zM12 9a3 3 0 1 0 0 6 3 3 0 0 0 0-6z" /> : <Icon d="M3 3l18 18M10.6 5.1A10 10 0 0 1 12 5c6 0 10 7 10 7a17 17 0 0 1-3.2 3.9M6.6 6.6C3.8 8.4 2 12 2 12s4 7 10 7a9.6 9.6 0 0 0 5.4-1.6" />}
            </button>
            <button
              className={`icon-btn h-8 w-8 ${layer.locked ? 'text-accent' : ''}`}
              title={layer.locked ? '解锁' : '锁定'}
              aria-label={layer.locked ? '解锁' : '锁定'}
              aria-pressed={layer.locked}
              onClick={() => onRun({ type: 'setLocked', id: layer.id, locked: !layer.locked })}
            >
              {layer.locked ? <Icon d="M5 11h14v10H5zM8 11V8a4 4 0 0 1 8 0v3" /> : <Icon d="M5 11h14v10H5zM8 11V8a4 4 0 0 1 7.5-2" />}
            </button>
          </div>
        }
      >
        <NameField layer={layer} onRun={onRun} />
      </Header>

      {/* Lowest on the left, highest on the right, like the stack itself. */}
      <Section title="层级">
        <div className="grid grid-cols-4 gap-1.5">
          <button className="chip px-0" disabled={bottom} title={keys('⇧⌘[')} onClick={() => onRun({ type: 'sendToBack', id: layer.id })}>
            置底
          </button>
          <button className="chip px-0" disabled={bottom} title={keys('⌘[')} onClick={() => onRun({ type: 'sendBackward', id: layer.id })}>
            下移
          </button>
          <button className="chip px-0" disabled={top} title={keys('⌘]')} onClick={() => onRun({ type: 'bringForward', id: layer.id })}>
            上移
          </button>
          <button className="chip px-0" disabled={top} title={keys('⇧⌘]')} onClick={() => onRun({ type: 'bringToFront', id: layer.id })}>
            置顶
          </button>
        </div>
      </Section>

      {layer.type === 'text' && <TextSections layer={layer} patch={patch} onRun={onRun} palette={palette} />}
      {layer.type === 'image' && <ImageSections {...props} layer={layer} />}
      {layer.type === 'shape' && layer.shape?.kind === 'line' && <LineSection layer={layer} onRun={onRun} palette={palette} />}
      {layer.type === 'shape' && layer.shape?.kind !== 'line' && <ShapeSection layer={layer} onRun={onRun} palette={palette} />}
      {layer.type === 'group' && (
        <Section title="编组">
          <p className="text-xs leading-relaxed text-muted">这几个图层作为一个整体移动、缩放、旋转。要单独改其中某个，先解散。</p>
          <button className="chip" onClick={() => props.onRunSelect({ type: 'ungroupLayers', id: layer.id })}>
            解散编组 <span className="text-faint">{keys('⇧⌘G')}</span>
          </button>
        </Section>
      )}

      <Section title="位置">
        <Grid2>
          <NumField label="X" value={layer.x} testId="layer-x" onCommit={(x) => onRun({ type: 'moveLayer', id: layer.id, x, y: layer.y })} />
          <NumField label="Y" value={layer.y} testId="layer-y" onCommit={(y) => onRun({ type: 'moveLayer', id: layer.id, x: layer.x, y })} />
          <NumField label="宽" value={layer.width} min={1} testId="layer-width" onCommit={(width) => onRun({ type: 'resizeLayer', id: layer.id, width })} />
          {layer.type === 'text' ? (
            <NumField label="旋转" value={layer.rotation} suffix="°" onCommit={(rotation) => onRun({ type: 'rotateLayer', id: layer.id, rotation })} />
          ) : (
            <NumField label="高" value={layer.height} min={1} onCommit={(height) => onRun({ type: 'resizeLayer', id: layer.id, width: layer.width, height })} />
          )}
          {layer.type !== 'text' && (
            <NumField label="旋转" value={layer.rotation} suffix="°" onCommit={(rotation) => onRun({ type: 'rotateLayer', id: layer.id, rotation })} />
          )}
        </Grid2>
        <Slider
          label="不透明度"
          value={layer.opacity}
          min={0}
          max={1}
          step={0.01}
          display={(v) => `${Math.round(v * 100)}%`}
          onPreview={(opacity) => props.onPreview(layer.id, 'opacity', opacity)}
          onChange={(opacity) => onRun({ type: 'setOpacity', id: layer.id, opacity })}
        />
        <div className="flex flex-col gap-2">
          <span className="text-xs text-muted">对齐到画布</span>
          <AlignRow onAlign={(h, v) => onRun({ type: 'alignLayer', id: layer.id, h, v })} />
        </div>
        {layer.type !== 'text' && (
          <Grid2>
            <button className="chip" title="等比放大到盖满画布，多余部分裁到画布外" onClick={() => onRun({ type: 'fitToCanvas', id: layer.id, mode: 'cover' })}>
              铺满画布
            </button>
            <button className="chip" title="等比缩放到完整放进画布" onClick={() => onRun({ type: 'fitToCanvas', id: layer.id, mode: 'contain' })}>
              完整放入
            </button>
          </Grid2>
        )}
      </Section>

      <div className="flex gap-2 px-5 py-5">
        <button className="chip flex-1" onClick={() => props.onRunSelect({ type: 'duplicateLayer', id: layer.id })}>
          复制 <span className="text-faint">{keys('⌘D')}</span>
        </button>
        <button className="chip flex-1 text-accent" onClick={() => onRun({ type: 'removeLayer', id: layer.id })}>
          删除 <span className="text-accent/60">⌫</span>
        </button>
      </div>
    </>
  )
}

// --- text ----------------------------------------------------------------

/** Whether a CSS colour is light enough that it would vanish on paper. */
function isLight(c: string): boolean {
  const s = c.trim().toLowerCase()
  let rgb: number[] | null = null
  const hex = /^#([0-9a-f]{3}|[0-9a-f]{6})$/.exec(s)
  if (hex) {
    const h = hex[1].length === 3 ? hex[1].split('').map((x) => x + x).join('') : hex[1]
    rgb = [0, 2, 4].map((i) => parseInt(h.slice(i, i + 2), 16))
  } else {
    const m = /^rgba?\(([^)]+)\)/.exec(s)
    if (m) rgb = m[1].split(',').slice(0, 3).map((x) => Number(x.trim()))
  }
  if (!rgb) return false
  return (0.299 * rgb[0] + 0.587 * rgb[1] + 0.114 * rgb[2]) / 255 > 0.7
}

/** A preset shown as what it does: its name, set in its own style. */
function PresetTile({ preset, onClick }: { preset: TextPreset; onClick: () => void }) {
  const st = preset.style as Record<string, any>
  const fill = String(st.fill ?? '#1c1b18')
  const light = isLight(fill)
  const bgBox = st.textBackgroundColor ? String(st.textBackgroundColor) : undefined
  const stroke = parseStroke(st.stroke)
  const shadow = parseShadow(st.shadow)
  return (
    <button
      type="button"
      title={`套用「${preset.name}」`}
      className="flex h-16 items-center justify-center overflow-hidden rounded-xl px-2 transition-transform hover:scale-[1.03]"
      // White text needs something dark behind it to be seen at all.
      style={{ background: light && !bgBox ? '#2f4a43' : '#f3efe6' }}
      onClick={onClick}
    >
      <span
        className="truncate text-[15px] leading-tight"
        style={{
          color: fill,
          fontWeight: Number(st.fontWeight ?? 700),
          background: bgBox,
          padding: bgBox ? '2px 6px' : undefined,
          WebkitTextStroke: stroke.stroke ? `2px ${stroke.stroke}` : undefined,
          paintOrder: st.paintFirst ? 'stroke fill' : undefined,
          textShadow: shadow ? `${shadow.offsetX / 3}px ${shadow.offsetY / 3}px ${shadow.blur / 3}px ${shadow.color}` : undefined,
          letterSpacing: st.charSpacing ? `${Number(st.charSpacing) / 100}em` : undefined,
        }}
      >
        {preset.name}
      </span>
    </button>
  )
}

function TextSections({
  layer,
  patch,
  onRun,
  palette,
}: {
  layer: Layer
  patch: (props: Record<string, unknown>) => void
  onRun: (cmd: unknown) => void
  palette: string[]
}) {
  const style = (layer.style ?? {}) as Record<string, any>
  const [text, setText] = useState(String(layer.text ?? ''))
  useEffect(() => setText(String(layer.text ?? '')), [layer.id, layer.text])

  const stroke = parseStroke(style.stroke)

  return (
    <>
      <Section title="文字">
        <textarea
          className="min-h-[60px] w-full resize-y rounded-[10px] bg-paper px-3 py-2 text-[13px] leading-relaxed text-ink outline-none focus:ring-2 focus:ring-accent/30"
          rows={2}
          data-testid="text-content"
          aria-label="文字内容"
          value={text}
          onChange={(e) => setText(e.target.value)}
          onBlur={() => {
            if (text !== layer.text) onRun({ type: 'setText', id: layer.id, text })
          }}
        />
        <FontPicker value={String(style.fontFamily ?? '')} onChange={(fontFamily) => patch({ fontFamily })} />
        <Grid2>
          <NumField label="字号" value={Number(style.fontSize ?? 48)} min={1} testId="text-size" onCommit={(fontSize) => patch({ fontSize })} />
          <NumField label="行高" value={Number(style.lineHeight ?? 1.2)} step={0.05} min={0.5} max={4} onCommit={(lineHeight) => patch({ lineHeight })} />
          <NumField label="字距" value={Number(style.charSpacing ?? 0)} onCommit={(charSpacing) => patch({ charSpacing })} />
          <NumField label="内边距" value={Number(style.padding ?? 0)} min={0} onCommit={(padding) => patch({ padding })} />
        </Grid2>
        <Segmented
          label=""
          value={String(style.fontWeight ?? '400')}
          options={[
            { value: '300', label: '细' },
            { value: '400', label: '常规' },
            { value: '700', label: '粗' },
            { value: '900', label: '黑' },
          ]}
          onChange={(fontWeight) => patch({ fontWeight })}
        />
        <div className="flex flex-wrap gap-2">
          <Toggle label="斜体" on={style.fontStyle === 'italic'} onToggle={(on) => patch({ fontStyle: on ? 'italic' : 'normal' })} />
          <Toggle label="下划线" on={style.underline === true} onToggle={(underline) => patch({ underline })} />
          <Toggle label="删除线" on={style.linethrough === true} onToggle={(linethrough) => patch({ linethrough })} />
        </div>
        <Segmented
          label=""
          value={(style.textAlign ?? 'left') as 'left' | 'center' | 'right'}
          options={[
            { value: 'left', label: <Icon d="M4 6h16M4 12h10M4 18h14" />, title: '左对齐' },
            { value: 'center', label: <Icon d="M4 6h16M7 12h10M5 18h14" />, title: '居中' },
            { value: 'right', label: <Icon d="M4 6h16M10 12h10M6 18h14" />, title: '右对齐' },
          ]}
          onChange={(textAlign) => patch({ textAlign })}
        />
      </Section>

      <Section title="变形">
        <WarpPicker style={style} patch={patch} />
      </Section>

      <Section title="颜色">
        <ColorField label="文字" value={String(style.fill ?? '#000000')} palette={palette} testId="text-color" onCommit={(fill) => patch({ fill })} />
        <ColorField
          label="底色"
          allowNone
          palette={palette}
          value={String(style.textBackgroundColor ?? '')}
          onCommit={(textBackgroundColor) => patch({ textBackgroundColor })}
        />
      </Section>

      <Section title="样式">
        <div className="grid grid-cols-2 gap-2">
          {textPresets.map((p) => (
            <PresetTile key={p.key} preset={p} onClick={() => onRun({ type: 'applyTextPreset', id: layer.id, preset: p.key })} />
          ))}
        </div>
      </Section>

      <Section title="描边与投影">
        <ColorField
          label="描边"
          allowNone
          palette={palette}
          value={stroke.stroke ?? ''}
          // A colour picked for an absent stroke needs a width to be seen.
          onCommit={(c) => patch({ stroke: c ? `${c}:${stroke.strokeWidth || 4}` : '' })}
        />
        {stroke.stroke && (
          <div className="flex items-center gap-2">
            <NumField label="粗细" value={Number(stroke.strokeWidth ?? 0)} min={0} onCommit={(w) => patch(w <= 0 ? { stroke: '' } : { strokeWidth: w })} />
            <Toggle label="描边在字下" on={style.paintFirst === true} onToggle={(paintFirst) => patch({ paintFirst })} />
          </div>
        )}
        <ShadowFields value={style.shadow} fallback="rgba(0,0,0,0.5):12:0:6" onChange={(shadow) => patch({ shadow })} />
      </Section>
    </>
  )
}

// --- image ---------------------------------------------------------------

const FILTERS: { key: string; label: string; min: number; max: number; discrete?: boolean }[] = [
  { key: 'brightness', label: '亮度', min: -1, max: 1 },
  { key: 'contrast', label: '对比度', min: -1, max: 1 },
  { key: 'saturation', label: '饱和度', min: -1, max: 1 },
  { key: 'blur', label: '模糊', min: 0, max: 1 },
]

function ImageSections({ layer, onRun, onUpload, onCrop, onPreview, onPickFromLibrary }: PanelProps & { layer: Layer }) {
  const style = (layer.style ?? {}) as Record<string, any>
  const filters = (style.filters ?? {}) as Record<string, number>
  const fileRef = useRef<HTMLInputElement>(null)
  const setFilters = (next: Record<string, number>) => onRun({ type: 'setImageProps', id: layer.id, filters: next })
  const anyFilter = Object.values(filters).some((v) => Number(v) !== 0)

  return (
    <>
      <Section title="图片">
        <div className="grid grid-cols-3 gap-2">
          <button className="chip" onClick={() => onCrop(layer)} title="也可以在画布上双击图片">
            裁剪
          </button>
          <ReplaceMenu
            onLibrary={onPickFromLibrary ? () => onPickFromLibrary(layer) : undefined}
            onLocal={() => fileRef.current?.click()}
          />
          <CutoutButton layer={layer} onRun={onRun} />
        </div>
        <div className="grid grid-cols-2 gap-2 *:w-full">
          <Toggle label="水平翻转" on={style.flipX === true} onToggle={(flipX) => onRun({ type: 'setImageProps', id: layer.id, flipX })} />
          <Toggle label="垂直翻转" on={style.flipY === true} onToggle={(flipY) => onRun({ type: 'setImageProps', id: layer.id, flipY })} />
        </div>
        {layer.image?.crop && (
          <button className="chip" onClick={() => onRun({ type: 'cropImage', id: layer.id, reset: true })}>
            恢复完整图片
          </button>
        )}
        <input
          ref={fileRef}
          type="file"
          accept="image/*"
          className="hidden"
          onChange={async (e) => {
            const f = e.target.files?.[0]
            e.target.value = ''
            const url = f ? await onUpload(f) : null
            if (url) onRun({ type: 'replaceImage', id: layer.id, url })
          }}
        />
      </Section>

      <Section title="外观">
        <div className="grid grid-cols-2 gap-2">
          <NumField
            label="圆角"
            value={Number(style.cornerRadius ?? 0)}
            min={0}
            max={Math.round(Math.min(layer.width, layer.height) / 2)}
            onCommit={(cornerRadius) => onRun({ type: 'setImageProps', id: layer.id, cornerRadius })}
          />
        </div>
        <div className="flex flex-col gap-3">
          <div className="flex items-center justify-between">
            <span className="text-xs text-muted">描边</span>
            <Switch
              label="描边"
              on={!!style.stroke}
              onToggle={(on) => onRun({ type: 'setImageProps', id: layer.id, ...(on ? { stroke: '#ffffff', strokeWidth: 16 } : { stroke: 'none' }) })}
            />
          </div>
          {style.stroke && (
            <>
              <ColorField label="颜色" value={String(style.stroke)} onCommit={(c) => c && onRun({ type: 'setImageProps', id: layer.id, stroke: c })} />
              <div className="grid grid-cols-2 gap-2">
                <NumField
                  label="粗细"
                  value={Number(style.strokeWidth ?? 12)}
                  min={0}
                  onCommit={(w) => onRun({ type: 'setImageProps', id: layer.id, ...(w > 0 ? { strokeWidth: w } : { stroke: 'none' }) })}
                />
              </div>
            </>
          )}
        </div>
        <ShadowFields
          value={style.shadow}
          fallback="rgba(0,0,0,0.35):40:0:16"
          onChange={(shadow) => onRun({ type: 'setImageProps', id: layer.id, shadow: shadow || 'none' })}
        />
      </Section>

      <Section
        title="调色"
        right={
          anyFilter ? (
            <button className="text-xs text-accent hover:underline" onClick={() => setFilters({ brightness: 0, contrast: 0, saturation: 0, blur: 0, grayscale: 0, sepia: 0 })}>
              全部还原
            </button>
          ) : undefined
        }
      >
        {FILTERS.map((f) => (
          <Slider
            key={f.key}
            label={f.label}
            value={Number(filters[f.key] ?? 0)}
            min={f.min}
            max={f.max}
            step={0.02}
            display={(v) => (v > 0 ? '+' : '') + Math.round(v * 100)}
            testId={`filter-${f.key}`}
            onPreview={(v) => onPreview(layer.id, 'filters', { ...filters, [f.key]: v })}
            onChange={(v) => setFilters({ ...filters, [f.key]: v })}
          />
        ))}
        <div className="flex gap-2">
          <Toggle label="黑白" on={Number(filters.grayscale ?? 0) > 0} onToggle={(on) => setFilters({ ...filters, grayscale: on ? 1 : 0 })} />
          <Toggle label="复古" on={Number(filters.sepia ?? 0) > 0} onToggle={(on) => setFilters({ ...filters, sepia: on ? 1 : 0 })} />
        </div>
      </Section>
    </>
  )
}

// --- shape ---------------------------------------------------------------

/** A line: its colour, thickness, dash and arrowheads. */
function LineSection({ layer, onRun, palette }: { layer: Layer; onRun: (cmd: unknown) => void; palette: string[] }) {
  const style = (layer.style ?? {}) as Record<string, any>
  const set = (props: Record<string, unknown>) => onRun({ type: 'setShapeProps', id: layer.id, ...props })
  return (
    <Section title="线条">
      <ColorField label="颜色" palette={palette} value={String(style.stroke ?? '#1c1b18')} testId="line-color" onCommit={(stroke) => set({ stroke })} />
      <Grid2>
        <NumField label="粗细" value={Number(style.strokeWidth ?? 4)} min={1} max={200} onCommit={(strokeWidth) => set({ strokeWidth })} />
      </Grid2>
      <Segmented
        label="线型"
        value={(style.lineStyle ?? 'solid') as 'solid' | 'dashed'}
        options={[
          { value: 'solid', label: '实线' },
          { value: 'dashed', label: '虚线' },
        ]}
        onChange={(lineStyle) => set({ lineStyle })}
      />
      <Segmented
        label="箭头"
        value={(style.arrow ?? 'none') as 'none' | 'end' | 'both'}
        options={[
          { value: 'none', label: '无' },
          { value: 'end', label: '单向' },
          { value: 'both', label: '双向' },
        ]}
        onChange={(arrow) => set({ arrow })}
      />
    </Section>
  )
}

function ShapeSection({ layer, onRun, palette }: { layer: Layer; onRun: (cmd: unknown) => void; palette: string[] }) {
  const style = (layer.style ?? {}) as Record<string, any>
  const hasStroke = !!style.stroke && Number(style.strokeWidth) > 0
  return (
    <Section title="外观">
      <ColorField
        label="填充"
        palette={palette}
        value={String(style.fill ?? '#000000')}
        testId="shape-fill"
        onCommit={(fill) => onRun({ type: 'setShapeProps', id: layer.id, fill })}
      />
      <ColorField
        label="描边"
        allowNone
        palette={palette}
        value={hasStroke ? String(style.stroke) : ''}
        onCommit={(stroke) =>
          onRun(
            stroke
              ? { type: 'setShapeProps', id: layer.id, stroke, strokeWidth: Number(style.strokeWidth) || 4 }
              : { type: 'setShapeProps', id: layer.id, stroke: '', strokeWidth: 0 },
          )
        }
      />
      <Grid2>
        {hasStroke && (
          <NumField label="描边粗细" value={Number(style.strokeWidth ?? 0)} min={0} onCommit={(strokeWidth) => onRun({ type: 'setShapeProps', id: layer.id, strokeWidth })} />
        )}
        {layer.shape?.kind === 'rect' && (
          <NumField
            label="圆角"
            value={Number(style.cornerRadius ?? 0)}
            min={0}
            max={Math.round(Math.min(layer.width, layer.height) / 2)}
            onCommit={(cornerRadius) => onRun({ type: 'setShapeProps', id: layer.id, cornerRadius })}
          />
        )}
      </Grid2>
    </Section>
  )
}
