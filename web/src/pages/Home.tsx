import { useCallback, useEffect, useMemo, useState } from 'react'
import { api, type CanvasPreset, type ProjectSummary } from '../api'
import { navigate } from '../App'
import { ConfirmDialog, Modal } from '../components/Dialog'
import { Lockup } from '../components/Brand'

/**
 * The home page: a row of canvas sizes to start from (one click creates the
 * project and opens it), then the recent designs with their thumbnails.
 */

/** Tile colours for the size previews, in order. */
const TILE_FILLS = ['#c23a22', '#1c1b18', '#2f4a43', '#f2c230', '#8a8478', '#e2dccf']

export function Home() {
  const [projects, setProjects] = useState<ProjectSummary[] | null>(null)
  const [presets, setPresets] = useState<CanvasPreset[]>([])
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [query, setQuery] = useState('')
  const [custom, setCustom] = useState(false)
  const [confirmDelete, setConfirmDelete] = useState<ProjectSummary | null>(null)

  const reload = useCallback(async () => {
    try {
      setError('')
      const [p, ps] = await Promise.all([api.projects(), api.presets()])
      setProjects(p)
      setPresets(ps)
    } catch (e: any) {
      setError(e?.message ?? String(e))
    }
  }, [])

  useEffect(() => {
    document.title = 'davinci'
    void reload()
  }, [reload])

  async function create(input: { preset?: string; width?: number; height?: number }) {
    setBusy(true)
    try {
      const proj = await api.createProject(input)
      navigate(`/editor/${proj.id}`)
    } catch (e: any) {
      setError(e?.message ?? String(e))
      setBusy(false)
    }
  }

  async function remove(id: string) {
    setConfirmDelete(null)
    try {
      await api.deleteProject(id)
      await reload()
    } catch (e: any) {
      setError(e?.message ?? String(e))
    }
  }

  // Newest first is the server's order already.
  const shown = useMemo(() => {
    const q = query.trim().toLowerCase()
    return (projects ?? []).filter((p) => !q || p.name.toLowerCase().includes(q))
  }, [projects, query])

  return (
    <div className="relative h-full overflow-y-auto bg-paper">
      {/* Places to go, top right — the same floating island the editor uses. */}
      <nav aria-label="导航" className="absolute right-5 top-4 z-10">
        <a
          href="/library"
          className="island flex h-[52px] items-center gap-2 px-5 text-[13px] font-bold text-ink hover:text-accent"
          onClick={(e) => {
            e.preventDefault()
            navigate('/library')
          }}
          data-testid="library-link"
        >
          <svg width="17" height="17" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round">
            <path d="M4 5h6v6H4zM14 5h6v6h-6zM4 15h6v4H4zM14 15h6v4h-6z" />
          </svg>
          素材库
        </a>
      </nav>
      <div className="mx-auto flex max-w-[1240px] flex-col items-center gap-12 px-10 pb-16 pt-12">
        <section className="flex flex-col items-center gap-6">
          <Lockup size={40} />
          <h1 className="m-0 font-serif text-[40px] font-black tracking-tight text-ink">新建一张图</h1>
          <div className="flex flex-wrap justify-center gap-4">
            {presets.map((p, i) => (
              <SizeTile
                key={p.key}
                name={shortName(p.name)}
                size={ratioOf(p.width, p.height)}
                w={p.width}
                h={p.height}
                fill={TILE_FILLS[i % TILE_FILLS.length]}
                disabled={busy}
                onClick={() => void create({ preset: p.key })}
              />
            ))}
            <SizeTile name="自定义" size="任意尺寸" w={1} h={1} fill="#e2dccf" dashed disabled={busy} onClick={() => setCustom(true)} />
          </div>
        </section>

        {error && (
          <div role="alert" className="w-full rounded-2xl bg-accent-soft px-5 py-3 text-sm text-accent-dark">
            {error}
          </div>
        )}

        <section className="flex w-full flex-col gap-5">
          <div className="flex items-center gap-3">
            <h2 className="m-0 font-serif text-xl font-black">最近</h2>
            <span className="text-xs text-faint">{projects ? `${projects.length} 个设计` : ''}</span>
            <div className="flex-1" />
            <input
              aria-label="搜索项目"
              placeholder="搜索"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              className="h-9 w-56 rounded-full border border-line-strong bg-card px-4 text-[13px] text-ink outline-none placeholder:text-faint focus:ring-2 focus:ring-accent/30"
            />
          </div>

          {projects === null && <p className="text-sm text-faint">载入中……</p>}
          {projects?.length === 0 && (
            <p className="rounded-2xl border border-dashed border-line-strong px-6 py-12 text-center text-sm leading-relaxed text-muted">
              还没有设计。选上面一个尺寸开始，
              <br />
              或者在 Claude Code 里用 <code className="rounded bg-paper-deep px-1.5 py-0.5 text-xs">davinci new "封面"</code> 创建。
            </p>
          )}
          {projects !== null && projects.length > 0 && shown.length === 0 && <p className="text-sm text-faint">没有名字里带「{query}」的设计。</p>}

          <div className="grid grid-cols-[repeat(auto-fill,minmax(180px,1fr))] gap-5">
            {shown.map((p) => (
              <div key={p.id} className="group relative flex flex-col gap-2.5">
                <a
                  href={`/editor/${p.id}`}
                  className="flex flex-col gap-2.5 no-underline"
                  onClick={(e) => {
                    e.preventDefault()
                    navigate(`/editor/${p.id}`)
                  }}
                >
                  <div className="flex aspect-[180/250] items-center justify-center rounded-2xl bg-card p-5 shadow-[0_1px_0_var(--color-line)] transition-shadow group-hover:shadow-[var(--shadow-float)]">
                    <Thumbnail project={p} />
                  </div>
                  <span className="flex flex-col gap-0.5 px-1">
                    <span className="truncate text-sm font-bold text-ink">{p.name}</span>
                    <span className="text-xs text-faint">{when(p.updatedAt)}</span>
                  </span>
                </a>
                <button
                  onClick={() => setConfirmDelete(p)}
                  title="删除"
                  aria-label={`删除「${p.name}」`}
                  className="icon-btn absolute right-2 top-2 h-8 w-8 bg-card/90 text-muted opacity-0 shadow-[var(--shadow-float)] transition-opacity hover:!bg-accent-soft hover:text-accent focus:opacity-100 group-hover:opacity-100"
                >
                  <svg width="15" height="15" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round">
                    <path d="M4 7h16M10 11v6M14 11v6M6 7l1 13h10l1-13M9 7V4h6v3" />
                  </svg>
                </button>
              </div>
            ))}
          </div>
        </section>
      </div>

      {confirmDelete && (
        <ConfirmDialog
          title={`删除「${confirmDelete.name}」？`}
          message="项目和它的全部图层都会被删除，此操作不能撤销。"
          confirmLabel="删除"
          danger
          onConfirm={() => void remove(confirmDelete.id)}
          onCancel={() => setConfirmDelete(null)}
        />
      )}
      {custom && <CustomSize busy={busy} onClose={() => setCustom(false)} onCreate={(width, height) => void create({ width, height })} />}
    </div>
  )
}

/** A size to start from, drawn at its own aspect ratio. */
function SizeTile({
  name,
  size,
  w,
  h,
  fill,
  dashed,
  disabled,
  onClick,
}: {
  name: string
  size: string
  w: number
  h: number
  fill: string
  dashed?: boolean
  disabled?: boolean
  onClick: () => void
}) {
  // Fit the aspect ratio inside an 88×74 box.
  const k = Math.min(88 / w, 74 / h)
  return (
    <button
      disabled={disabled}
      onClick={onClick}
      className="flex h-[156px] w-[152px] flex-col items-center justify-end gap-3.5 rounded-[20px] bg-card pb-[18px] shadow-[var(--shadow-card)] transition-all hover:-translate-y-0.5 hover:shadow-[var(--shadow-float)] disabled:opacity-50"
    >
      <span
        className={`rounded-[5px] ${dashed ? 'border-2 border-dashed border-ghost' : ''}`}
        style={{ width: Math.round(w * k), height: Math.round(h * k), background: dashed ? 'transparent' : fill }}
      />
      <span className="flex flex-col items-center gap-0.5">
        <span className="text-sm font-bold text-ink">{name}</span>
        <span className="text-[11px] text-faint">{size}</span>
      </span>
    </button>
  )
}

/** "小红书封面 3:4" → "小红书封面": the ratio is shown on its own line. */
function shortName(name: string): string {
  return name.replace(/\s*\d+\s*:\s*\d+\s*$/, '').trim() || name
}

function ratioOf(w: number, h: number): string {
  const gcd = (a: number, b: number): number => (b ? gcd(b, a % b) : a)
  const g = gcd(w, h)
  const a = w / g
  const b = h / g
  return a <= 32 && b <= 32 ? `${a}:${b}` : `${w}×${h}`
}

/** "刚刚", "5 分钟前", "昨天" … for the card's second line. */
function when(iso: string): string {
  const t = new Date(iso).getTime()
  const s = (Date.now() - t) / 1000
  if (s < 60) return '刚刚'
  if (s < 3600) return `${Math.floor(s / 60)} 分钟前`
  if (s < 86400) return `${Math.floor(s / 3600)} 小时前`
  if (s < 172800) return '昨天'
  const d = new Date(t)
  return d.getFullYear() === new Date().getFullYear() ? `${d.getMonth() + 1} 月 ${d.getDate()} 日` : d.toLocaleDateString('zh-CN')
}

/**
 * The saved thumbnail, letterboxed to the tile. A project no editor has saved
 * yet has none; it shows its size instead.
 */
function Thumbnail({ project }: { project: ProjectSummary }) {
  const [failed, setFailed] = useState(false)
  if (failed) {
    const k = Math.min(120 / project.width, 160 / project.height)
    return (
      <span
        className="flex items-center justify-center rounded-sm bg-white text-xs text-faint shadow-[var(--shadow-pop)]"
        style={{ width: Math.round(project.width * k), height: Math.round(project.height * k) }}
      >
        {project.width}×{project.height}
      </span>
    )
  }
  return (
    <img
      src={api.thumbnailURL(project)}
      alt=""
      loading="lazy"
      className="max-h-full max-w-full rounded-sm object-contain shadow-[var(--shadow-pop)]"
      onError={() => setFailed(true)}
    />
  )
}

function CustomSize({ busy, onClose, onCreate }: { busy: boolean; onClose: () => void; onCreate: (w: number, h: number) => void }) {
  const [w, setW] = useState('1242')
  const [h, setH] = useState('1656')
  const W = Math.round(Number(w))
  const H = Math.round(Number(h))
  const ok = W >= 1 && H >= 1 && W <= 20000 && H <= 20000
  return (
    <Modal onClose={onClose} label="自定义尺寸">
      <form
        className="flex flex-col gap-5 p-6"
        onSubmit={(e) => {
          e.preventDefault()
          if (ok) onCreate(W, H)
        }}
      >
        <h2 className="font-serif text-lg font-black text-ink">自定义尺寸</h2>
        <div className="flex items-center gap-3">
          <label className="flex h-10 flex-1 items-center gap-2 rounded-[10px] bg-paper px-3">
            <span className="text-xs text-muted">宽</span>
            <input autoFocus type="number" min={1} max={20000} value={w} onChange={(e) => setW(e.target.value)} className="w-full bg-transparent text-right text-sm tabular-nums outline-none" />
          </label>
          <span className="text-faint">×</span>
          <label className="flex h-10 flex-1 items-center gap-2 rounded-[10px] bg-paper px-3">
            <span className="text-xs text-muted">高</span>
            <input type="number" min={1} max={20000} value={h} onChange={(e) => setH(e.target.value)} className="w-full bg-transparent text-right text-sm tabular-nums outline-none" />
          </label>
        </div>
        <div className="flex justify-end gap-2">
          <button type="button" className="btn-ghost" onClick={onClose}>
            取消
          </button>
          <button type="submit" className="btn-primary" disabled={!ok || busy}>
            创建
          </button>
        </div>
      </form>
    </Modal>
  )
}
