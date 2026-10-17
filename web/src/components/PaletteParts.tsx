import { useEffect, useRef, useState } from 'react'
import { api, type Palette, type PaletteColors, type PaletteDraft } from '../api'
import { PaletteExamples } from './PaletteExamples'
import { paletteShortName } from './palettePresentation'
import { ConfirmDialog } from './Dialog'
import { reloadPalettes, usePalettes } from './paletteState'

export const PALETTE_ROLES = [
  ['bg', '背景色'], ['text', '主文字色'], ['highlight', '高亮色'], ['text2', '次文字色'], ['accent', '点缀色'],
] as const

/** A small cover mock, shared by cards and the editable detail preview. */
export function PalettePreview({ palette, name = '配色' }: { palette: PaletteColors; name?: string }) {
  const title = paletteShortName(name)
  const titleSize = [...title].length > 12 ? '7cqw' : [...title].length > 8 ? '8cqw' : '10cqw'
  return (
    <div className="palette-preview relative flex aspect-[4/3] w-full flex-col justify-center overflow-hidden rounded-xl p-[10%]" style={{ background: palette.bg }} aria-label="封面预览">
      <span className="absolute -right-[5%] top-[12%] h-[55%] w-[40%] rotate-12 rounded-[25%]" style={{ background: palette.highlight }} />
      <span className="absolute bottom-[12%] right-[14%] h-[17%] w-[17%] rounded-full" style={{ background: palette.accent }} />
      <strong className="palette-preview-title relative z-10 max-w-[72%] break-words line-clamp-2 shrink-0 font-serif font-black leading-[1.4]" style={{ color: palette.text, fontSize: titleSize }}>{title}</strong>
      <span className="relative z-10 mt-[5%] h-[1.5cqw] shrink-0 w-[44%] rounded-full" style={{ background: palette.text2 }} />
      <span className="relative z-10 mt-[3%] h-[1.5cqw] shrink-0 w-[30%] rounded-full" style={{ background: palette.text2 }} />
    </div>
  )
}
export function PaletteTile({ palette, selected, onClick }: { palette: Palette; selected?: boolean; onClick: () => void }) {
  return (
    <button type="button" onClick={onClick} aria-pressed={selected} className="group flex min-w-0 flex-col gap-2 text-left" data-testid="palette-item">
      <span className={`w-full overflow-hidden rounded-xl transition-shadow ${selected ? 'ring-2 ring-accent ring-offset-2 ring-offset-card' : 'group-hover:shadow-[var(--shadow-float)]'}`}>
        <div className="palette-tile-visual">
          {palette.imageUrl ? <img src={palette.imageUrl} alt="" loading="lazy" className="palette-tile-image" /> : <PalettePreview palette={palette} name={palette.name} />}
        </div>
        <span className="flex h-5">{PALETTE_ROLES.map(([key]) => <span key={key} className="flex-1" style={{ background: palette[key] }} />)}</span>
      </span>
      <span title={palette.name} className="w-full truncate px-0.5 text-xs text-ink-2">{palette.name}</span>
    </button>
  )
}

function draftOf(p: PaletteDraft): PaletteDraft {
  return { name: p.name, bg: p.bg, text: p.text, highlight: p.highlight, text2: p.text2, accent: p.accent, imageUrl: p.imageUrl ?? '' }
}

export function PaletteDetail({ initial, id, sourceImage, sourceFile, full, detailHref, onClose, onSaved, onApply }: {
  initial: PaletteDraft; id?: string; sourceImage?: string; sourceFile?: File; full?: boolean; detailHref?: string; onClose: () => void
  onSaved: (palette: Palette | null) => void
  onApply?: (id: string) => Promise<void>
}) {
  const [draft, setDraft] = useState(() => draftOf(initial))
  const [saved, setSaved] = useState(() => draftOf(initial))
  const [imageFile, setImageFile] = useState(sourceFile)
  const [imagePreview, setImagePreview] = useState(sourceImage)
  const imageInput = useRef<HTMLInputElement>(null)
  useEffect(() => {
    if (!imageFile) { setImagePreview(undefined); return }
    const url = URL.createObjectURL(imageFile); setImagePreview(url)
    return () => URL.revokeObjectURL(url)
  }, [imageFile])
  const imageURL = imagePreview ?? sourceImage ?? draft.imageUrl
  const [busy, setBusy] = useState(false)
  const [confirm, setConfirm] = useState(false)
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')
  const timer = useRef<ReturnType<typeof setTimeout>>()
  useEffect(() => () => clearTimeout(timer.current), [])
  const invalidRole = PALETTE_ROLES.find(([key]) => !/^#[0-9a-f]{6}$/i.test(draft[key]))
  const validation = !draft.name.trim() ? '请输入名称' : [...draft.name.trim()].length > 120 ? '名称不能超过 120 个字' : invalidRole ? `${invalidRole[1]}须为 #RRGGBB 色值` : ''
  const valid = !validation
  const dirty = !!imageFile || JSON.stringify(draft) !== JSON.stringify(saved)
  const feedback = (text: string) => { clearTimeout(timer.current); setNotice(text); timer.current = setTimeout(() => setNotice(''), 2500) }
  const act = async (fn: () => Promise<void>) => {
    setBusy(true); setError('')
    try { await fn() } catch (e) { setError(e instanceof Error ? e.message : String(e)) } finally { setBusy(false) }
  }
  const save = async () => {
    const palette = await api.savePalette(draft, id, imageFile)
    setDraft(draftOf(palette)); setSaved(draftOf(palette)); setImageFile(undefined); setImagePreview(undefined); onSaved(palette)
    await reloadPalettes(true); feedback('已保存')
  }
  return (
    <section className={`flex min-h-0 flex-col gap-4 overflow-y-auto p-5 ${full ? 'palette-detail-full' : ''}`} aria-label={id ? '配色详情' : '新增配色'}>
      {!full && <div className="flex items-center justify-between">
        <h2 className="m-0 font-serif text-lg font-black">{id ? '配色详情' : '新增配色'}</h2>
        <button type="button" className="icon-btn h-8 w-8" title="关闭" aria-label="关闭配色详情" onClick={onClose}>×</button>
      </div>}
      {full && <h1 className="palette-detail-title">{draft.name || '新增配色'}</h1>}
      {detailHref && <a href={detailHref} target="_blank" rel="noreferrer" className="text-xs text-muted hover:text-accent">查看详情 ↗</a>}
      <div className={full ? 'palette-detail-top' : 'contents'}>
        <div className={full ? 'palette-original' : 'contents'}>
          {imageURL ? <img src={imageURL} alt="配色原图" className="h-28 w-full shrink-0 rounded-lg bg-paper object-contain" /> : full && <div className="flex flex-col items-center gap-4"><span className="text-sm text-muted">暂无原图</span><button className="btn-ghost" disabled={busy} onClick={() => imageInput.current?.click()}>上传原图</button></div>}
        </div>
        <div className={full ? 'palette-controls card p-4' : 'contents'}>
          {!full && <PalettePreview palette={draft} name={draft.name} />}
          {imageURL && <div className="flex flex-wrap gap-2">
            {imageURL && <button className="btn-ghost" disabled={busy} onClick={() => imageInput.current?.click()}>更换原图</button>}
            {imageURL && <button className="btn-ghost" disabled={busy} onClick={() => void act(async () => {
              let file = imageFile
              if (!file) {
                const response = await fetch(imageURL)
                if (!response.ok) throw new Error('原图读取失败')
                file = new File([await response.blob()], '原图')
              }
              const colors = await api.analyzePalette(file)
              setDraft((current) => ({ ...current, ...Object.fromEntries(PALETTE_ROLES.map(([key]) => [key, colors[key]])) }))
              feedback('已重新提色，请预览后保存')
            })}>按原图重新提色</button>}
          </div>}
          <input ref={imageInput} type="file" accept="image/png,image/jpeg,image/gif,image/webp" aria-label="上传或更换原图" className="hidden" disabled={busy} onChange={(e) => {
            const file = e.target.files?.[0]; e.target.value = ''
            if (!file) return
            if (file.size > 20 * 1024 * 1024) { setError('图片不能超过 20 MB'); return }
            setError(''); setImageFile(file)
          }} />
          <label className="flex flex-col gap-2 text-xs text-muted">名称
            <input aria-label="配色名称" className="h-9 rounded-[10px] bg-paper px-3 text-sm text-ink outline-none focus:ring-2 focus:ring-accent/30" value={draft.name} maxLength={120} disabled={busy} onChange={(e) => setDraft({ ...draft, name: e.target.value })} />
          </label>
          <div className="palette-role-fields flex flex-col gap-3">
            {PALETTE_ROLES.map(([key, label]) => (
              <div key={key} className="flex items-center gap-2">
                <label className="relative h-8 w-8 shrink-0 cursor-pointer rounded-full border border-line-strong" style={{ background: draft[key] }}>
                  <input type="color" aria-label={label} disabled={busy} value={/^#[0-9a-f]{6}$/i.test(draft[key]) ? draft[key] : '#000000'} className="absolute inset-0 h-full w-full cursor-pointer opacity-0" onChange={(e) => setDraft({ ...draft, [key]: e.target.value })} />
                </label>
                <label className="flex min-w-0 flex-1 items-center gap-2">
                  <span className="w-16 shrink-0 text-xs text-muted">{label}</span>
                  <input aria-label={`${label}（色值）`} aria-invalid={!/^#[0-9a-f]{6}$/i.test(draft[key])} disabled={busy} className="h-8 min-w-0 w-full rounded-[10px] bg-paper px-2 font-mono text-xs outline-none focus:ring-2 focus:ring-accent/30" value={draft[key]} onChange={(e) => setDraft({ ...draft, [key]: e.target.value })} />
                </label>
                <button className="icon-btn h-8 w-8 shrink-0" title={`复制 ${draft[key]}`} aria-label={`复制${label} ${draft[key]}`} onClick={() => void act(async () => { await navigator.clipboard.writeText(draft[key]); feedback(`已复制 ${draft[key]}`) })}>
                  <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8"><rect x="8" y="8" width="12" height="12" rx="2" /><path d="M16 8V4H4v12h4" /></svg>
                </button>
              </div>
            ))}
          </div>
          {(error || validation) && <p role="alert" className="m-0 text-xs text-accent">{error || validation}</p>}
          <span role="status" className="text-xs text-muted empty:hidden">{notice}</span>
          <div className="flex flex-wrap gap-2">
            <button className="btn-primary" disabled={busy || !valid || (!!id && !dirty)} onClick={() => void act(save)}>保存</button>
            {id && onApply && <button className="btn-ghost" disabled={busy || dirty} title={dirty ? '请先保存修改' : undefined} onClick={() => void act(async () => { await onApply(id); feedback('已应用') })}>应用到当前画板</button>}
            {id && <button className="icon-btn ml-auto text-accent" disabled={busy} title="删除配色" aria-label="删除配色" onClick={() => setConfirm(true)}>
              <svg width="17" height="17" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8"><path d="M4 6h16M9 6V3h6v3M6 6l1 15h10l1-15M10 10v7M14 10v7" /></svg>
            </button>}
          </div>
        </div>
      </div>
      {full && <PaletteExamples palette={draft} />}
      {confirm && <ConfirmDialog title={`删除「${saved.name}」？`} confirmLabel="删除" danger onCancel={() => setConfirm(false)} onConfirm={() => { setConfirm(false); void act(async () => { await api.deletePalette(id!); await reloadPalettes(true); onSaved(null) }) }} />}
    </section>
  )
}

/** Shared browsing/add/detail flow for the standalone page and editor drawer. */
export function PaletteBrowser({ compact, onApply, selectedID, onSelect, detailQuery = '' }: {
  compact?: boolean; onApply?: (id: string) => Promise<void>
  selectedID?: string; onSelect?: (id?: string) => void; detailQuery?: string
}) {
  const { items, error: loadError } = usePalettes()
  const [selected, setSelected] = useState<Palette | null>(null)
  const [draft, setDraft] = useState<PaletteDraft | null>(null)
  const [draftVersion, setDraftVersion] = useState(0)
  const [sourceFile, setSourceFile] = useState<File>()
  const [query, setQuery] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const file = useRef<HTMLInputElement>(null)
  const seq = useRef(0)
  useEffect(() => {
    const mine = ++seq.current
    if (!selectedID) { setSelected(null); setError(''); return }
    setSelected((current) => current?.id === selectedID ? current : null); setError('')
    void api.palette(selectedID).then((p) => { if (mine === seq.current) { setSelected(p); setDraft(null); setSourceFile(undefined); setError('') } })
      .catch((e: Error) => { if (mine === seq.current) setError(e.message) })
    return () => { seq.current++ }
  }, [selectedID])
  const choose = (p: Palette | null) => {
    setSelected(p); setDraft(null); setSourceFile(undefined)
    if ((p?.id ?? '') !== (selectedID ?? '')) onSelect?.(p?.id)
  }
  const analyze = async (f?: File) => {
    if (!f || busy) return
    setBusy(true); setError('')
    try { const next = await api.analyzePalette(f); choose(null); setSourceFile(f); setDraft(next); setDraftVersion((v) => v + 1) }
    catch (e) { setError(e instanceof Error ? e.message : String(e)) }
    finally { setBusy(false) }
  }
  const detail = selected ?? draft
  return (
    <div className={`flex min-h-0 flex-1 ${compact ? 'flex-col' : 'flex-col overflow-y-auto'}`}>
      {!detail && !(selectedID && !compact) && <main className={`${compact ? '' : 'card'} flex min-w-0 flex-1 flex-col overflow-hidden`}>
        <div className="flex items-center gap-3 p-5">
          <input aria-label="搜索配色" placeholder="搜索配色" className="h-9 min-w-0 flex-1 rounded-full bg-paper px-4 text-sm outline-none focus:ring-2 focus:ring-accent/30" value={query} onChange={(e) => setQuery(e.target.value)} />
          <button className="btn-primary shrink-0" disabled={busy} onClick={() => file.current?.click()}>{busy ? '解析中…' : '从图片新增'}</button>
        </div>
        <div className="min-h-0 flex-1 overflow-y-auto px-5 pb-5">
          {items === null && !loadError && <p className="text-sm text-muted">载入中…</p>}
          {items?.length === 0 && <p className="py-10 text-center text-sm text-muted">暂无配色</p>}
          <div className={`grid ${compact ? 'grid-cols-2' : 'grid-cols-[repeat(auto-fill,minmax(min(180px,100%),1fr))]'} gap-4`}>
            {items?.filter((p) => p.name.toLowerCase().includes(query.trim().toLowerCase())).map((p) => <PaletteTile key={p.id} palette={p} selected={selected?.id === p.id} onClick={() => choose(p)} />)}
          </div>
        </div>
      </main>}
      {!compact && draft && <button className="btn-ghost self-start mb-4" onClick={() => choose(null)}>← 配色</button>}
      {!compact && selectedID && !detail && !error && <p className="text-sm text-muted">载入中…</p>}
      <input ref={file} type="file" accept="image/png,image/jpeg,image/gif,image/webp" className="hidden" onChange={(e) => { const f = e.target.files?.[0]; e.target.value = ''; void analyze(f) }} />
      {(error || loadError) && <p role="alert" className="m-3 text-sm text-accent">{error || loadError}</p>}
      {detail && <div className={`${compact ? 'min-h-0 flex-1 overflow-y-auto' : 'min-w-0 pb-6'}`}>
        <PaletteDetail key={selected?.id ?? `new-${draftVersion}`} initial={detail} id={selected?.id} sourceFile={sourceFile} full={!compact} detailHref={compact && selected ? `/palettes/${encodeURIComponent(selected.id)}${detailQuery}` : undefined} onClose={() => choose(null)} onSaved={choose} onApply={onApply} />
      </div>}
    </div>
  )
}
