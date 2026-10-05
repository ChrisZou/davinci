import { useCallback, useEffect, useRef, useState } from 'react'
import { api, type ProjectSummary, type TagCount } from '../api'
import { navigate } from '../App'
import { ConfirmDialog, Modal } from '../components/Dialog'
import { Lockup } from '../components/Brand'

/**
 * The template library: projects kept as references rather than works. Each
 * is a full, layered project — it opens in the editor like any other — so this
 * page is a different shelf, not a different kind of thing. Templates come in
 * all shapes, so they sit in a masonry of columns; tags along the top filter
 * them, and the chosen one opens on the right with why it was kept.
 *
 * A picture dropped or pasted (⌘V) anywhere on the page — or an image URL
 * pasted as text — becomes a new template.
 *
 * AI reaches the same library with `davinci tpl …`.
 */
export function Templates() {
  const [query, setQuery] = useState('')
  const [tag, setTag] = useState('')
  const [templates, setTemplates] = useState<ProjectSummary[] | null>(null)
  const [tags, setTags] = useState<TagCount[]>([])
  const [error, setError] = useState('')
  // /templates?t=<id> opens with that template chosen.
  const [selected, setSelected] = useState<string | null>(() => new URLSearchParams(location.search).get('t'))
  const [uploading, setUploading] = useState(0)
  const [dropping, setDropping] = useState(false)
  const [linkOpen, setLinkOpen] = useState(false)
  const fileRef = useRef<HTMLInputElement>(null)
  const seq = useRef(0)

  useEffect(() => {
    document.title = '模板库 · davinci'
  }, [])

  const loadTemplates = useCallback(async () => {
    const mine = ++seq.current
    try {
      const list = await api.projects({ kind: 'template', q: query.trim() || undefined, tag: tag || undefined })
      // A slower answer to an older search must not overwrite a newer one.
      if (mine === seq.current) setTemplates(list)
    } catch (e: any) {
      if (mine === seq.current) setError(e?.message ?? String(e))
    }
  }, [query, tag])

  const loadTags = useCallback(async () => {
    try {
      setTags(await api.projectTags('template'))
    } catch (e: any) {
      setError(e?.message ?? String(e))
    }
  }, [])

  useEffect(() => {
    void loadTags()
  }, [loadTags])

  // Typing in the search box waits for a pause before asking the server.
  useEffect(() => {
    const t = window.setTimeout(() => void loadTemplates(), query ? 200 : 0)
    return () => window.clearTimeout(t)
  }, [loadTemplates, query])

  // A tag that no template carries any more stops filtering.
  useEffect(() => {
    if (tag && !tags.some((t) => t.name === tag)) setTag('')
  }, [tags, tag])

  const reload = useCallback(async () => {
    await Promise.all([loadTemplates(), loadTags()])
  }, [loadTemplates, loadTags])

  /** Saves pictures (files or image URLs) as templates; the last one opens on the right. */
  const save = useCallback(
    async (sources: (File | string)[]) => {
      const list = sources.filter((s) => typeof s === 'string' || s.type.startsWith('image/'))
      if (!list.length) return
      setUploading(list.length)
      let last: ProjectSummary | null = null
      try {
        for (const src of list) {
          // Saved while a tag is picked, a template gets that tag too.
          last = await api.projectFromImage(src, { kind: 'template', tags: tag ? [tag] : [] })
          setUploading((n) => n - 1)
        }
      } catch (e: any) {
        setError(`收藏失败：${e?.message ?? e}`)
      } finally {
        setUploading(0)
        await reload()
      }
      if (last) setSelected(last.id)
    },
    [tag, reload],
  )

  // ⌘V: a copied picture, or the URL of one.
  useEffect(() => {
    const onPaste = (e: ClipboardEvent) => {
      const t = e.target as HTMLElement
      if (t.closest('input, textarea, [contenteditable]')) return
      const files = Array.from(e.clipboardData?.files ?? []).filter((f) => f.type.startsWith('image/'))
      if (files.length) {
        e.preventDefault()
        void save(files)
        return
      }
      const text = e.clipboardData?.getData('text/plain').trim() ?? ''
      if (/^https?:\/\/\S+$/.test(text)) {
        e.preventDefault()
        void save([text])
      }
    }
    window.addEventListener('paste', onPaste)
    return () => window.removeEventListener('paste', onPaste)
  }, [save])

  const current = templates?.find((t) => t.id === selected) ?? null

  return (
    <div
      className="flex h-full flex-col bg-paper"
      onDragOver={(e) => {
        if (!Array.from(e.dataTransfer.types).includes('Files')) return
        e.preventDefault()
        if (!dropping) setDropping(true)
      }}
      onDragLeave={(e) => {
        if (!e.currentTarget.contains(e.relatedTarget as Node)) setDropping(false)
      }}
      onDrop={(e) => {
        e.preventDefault()
        setDropping(false)
        void save(Array.from(e.dataTransfer.files))
      }}
    >
      <header className="flex h-[84px] shrink-0 items-center gap-4 px-8">
        <a
          href="/"
          onClick={(e) => {
            e.preventDefault()
            navigate('/')
          }}
          aria-label="回到首页"
        >
          <Lockup size={30} />
        </a>
        <span className="h-4 w-px bg-line-strong" />
        <h1 className="m-0 font-serif text-2xl font-black text-ink">模板库</h1>
        <span className="text-xs text-faint">收藏喜欢的封面，照着它做新的作品</span>
        <div className="flex-1" />
        <input
          aria-label="搜索模板"
          placeholder="搜名称、标签、备注…"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          className="h-9 w-72 rounded-full border border-line-strong bg-card px-4 text-[13px] text-ink outline-none placeholder:text-faint focus:ring-2 focus:ring-accent/30"
        />
        <button className="btn-ghost" onClick={() => setLinkOpen(true)} disabled={uploading > 0}>
          从链接添加
        </button>
        <button
          className="btn-primary"
          disabled={uploading > 0}
          title="也可以直接把图片拖进来，或者 ⌘V 粘贴"
          onClick={() => fileRef.current?.click()}
          data-testid="template-upload"
        >
          {uploading > 0 ? `收藏中… 还剩 ${uploading}` : '收藏封面'}
        </button>
        <input
          ref={fileRef}
          type="file"
          accept="image/*"
          multiple
          className="hidden"
          onChange={(e) => {
            const files = Array.from(e.target.files ?? [])
            e.target.value = ''
            void save(files)
          }}
        />
      </header>

      <div className="flex min-h-0 flex-1 gap-5 px-5 pb-5">
        <main className="card relative min-w-0 flex-1 overflow-y-auto p-6">
          {error && (
            <div role="alert" className="mb-4 flex items-center justify-between rounded-xl bg-accent-soft px-4 py-2.5 text-sm text-accent-dark">
              {error}
              <button className="text-xs underline" onClick={() => setError('')}>
                知道了
              </button>
            </div>
          )}
          {tags.length > 0 && (
            <div className="mb-5 flex flex-wrap gap-2" role="group" aria-label="按标签筛选">
              <TagChip label="全部" active={!tag} onClick={() => setTag('')} />
              {tags.map((t) => (
                <TagChip key={t.name} label={t.name} count={t.count} active={tag === t.name} onClick={() => setTag(tag === t.name ? '' : t.name)} />
              ))}
            </div>
          )}
          <div className="mb-4 flex items-baseline gap-3">
            <h2 className="m-0 font-serif text-xl font-black">{tag ? `#${tag}` : '全部模板'}</h2>
            <span className="text-xs text-faint">{templates ? `${templates.length} 个${query ? `匹配「${query}」` : ''}` : ''}</span>
          </div>
          {templates === null && <p className="text-sm text-faint">载入中……</p>}
          {templates?.length === 0 && (
            <p className="rounded-2xl border border-dashed border-line-strong px-6 py-14 text-center text-sm leading-relaxed text-muted">
              {query || tag ? '没有匹配的模板。' : '模板库还是空的。看到喜欢的封面，存下来拖进这里，或者复制图片后按 ⌘V；自己的作品也可以在首页移进来。'}
              <br />
              也可以在 Claude Code 里用 <code className="rounded bg-paper-deep px-1.5 py-0.5 text-xs">davinci tpl add</code> 收藏。
            </p>
          )}
          <div className="columns-[220px] gap-4">
            {templates?.map((t) => (
              <TemplateTile key={t.id} t={t} selected={selected === t.id} onClick={() => setSelected(t.id)} />
            ))}
          </div>
          {dropping && (
            <div className="pointer-events-none absolute inset-3 flex items-center justify-center rounded-3xl border-2 border-dashed border-accent/60 bg-accent-soft/70 font-serif text-lg font-black text-accent">
              松开鼠标，收藏到模板库{tag ? `（打上 #${tag}）` : ''}
            </div>
          )}
        </main>

        {current && (
          <TemplateDetail
            key={current.id}
            t={current}
            allTags={tags}
            onClose={() => setSelected(null)}
            onChanged={async (gone) => {
              if (gone) setSelected(null)
              await reload()
            }}
            onError={setError}
          />
        )}
      </div>

      {linkOpen && (
        <LinkDialog
          onCancel={() => setLinkOpen(false)}
          onSubmit={(url) => {
            setLinkOpen(false)
            void save([url])
          }}
        />
      )}
    </div>
  )
}

function TagChip({ label, count, active, onClick }: { label: string; count?: number; active: boolean; onClick: () => void }) {
  return (
    <button
      aria-pressed={active}
      onClick={onClick}
      className={`h-8 rounded-full px-3.5 text-[13px] transition-colors ${active ? 'bg-ink font-bold text-paper' : 'bg-paper text-ink-2 hover:bg-paper-deep'}`}
    >
      {label}
      {count != null && <span className={`ml-1.5 text-xs ${active ? 'text-paper/70' : 'text-faint'}`}>{count}</span>}
    </button>
  )
}

/** A template at its own shape, in a masonry column. Double-click opens it in the editor. */
function TemplateTile({ t, selected, onClick }: { t: ProjectSummary; selected: boolean; onClick: () => void }) {
  const ratio = t.width > 0 && t.height > 0 ? `${t.width} / ${t.height}` : '3 / 4'
  return (
    <button
      type="button"
      title={`${t.name}（双击打开编辑）`}
      onClick={onClick}
      onDoubleClick={() => navigate(`/editor/${t.id}`)}
      className="group mb-4 flex w-full break-inside-avoid flex-col gap-1.5 text-left"
      data-testid="template-item"
    >
      <span
        className={`block w-full overflow-hidden rounded-xl bg-paper-deep transition-shadow ${
          selected ? 'ring-2 ring-accent ring-offset-2 ring-offset-card' : 'group-hover:shadow-[var(--shadow-float)]'
        }`}
        style={{ aspectRatio: ratio }}
      >
        <img src={api.thumbnailURL(t)} alt="" loading="lazy" draggable={false} className="h-full w-full object-cover" />
      </span>
      <span className="truncate px-0.5 text-xs text-ink-2">{t.name}</span>
      {t.tags.length > 0 && <span className="truncate px-0.5 text-[11px] text-faint">{t.tags.map((x) => `#${x}`).join(' ')}</span>}
    </button>
  )
}

/** The chosen template: a preview, what to call it, why it was kept, where it came from. */
function TemplateDetail({
  t,
  allTags,
  onClose,
  onChanged,
  onError,
}: {
  t: ProjectSummary
  allTags: TagCount[]
  onClose: () => void
  /** `gone`: the template left the library (deleted, or moved back to the works). */
  onChanged: (gone: boolean) => Promise<void>
  onError: (msg: string) => void
}) {
  const [name, setName] = useState(t.name)
  const [tags, setTags] = useState(t.tags.join('，'))
  const [note, setNote] = useState(t.note ?? '')
  const [link, setLink] = useState(t.link ?? '')
  const [confirm, setConfirm] = useState(false)
  const [starting, setStarting] = useState(false)

  const save = async (patch: Parameters<typeof api.updateProject>[1], gone = false) => {
    try {
      await api.updateProject(t.id, patch)
      await onChanged(gone)
    } catch (e: any) {
      onError(e?.message ?? String(e))
    }
  }

  const parseTags = (s: string) => s.split(/[,，、]/).map((x) => x.trim()).filter(Boolean)
  const current = parseTags(tags)
  const suggestions = allTags.filter((x) => !current.includes(x.name)).slice(0, 12)

  return (
    <aside className="card flex w-[360px] shrink-0 flex-col overflow-y-auto" aria-label="模板详情">
      <div className="flex items-center justify-between px-5 pt-4">
        <span className="text-xs text-muted">
          {t.width}×{t.height}
        </span>
        <button className="icon-btn h-8 w-8 text-faint" aria-label="关闭" onClick={onClose}>
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round">
            <path d="M6 6l12 12M18 6 6 18" />
          </svg>
        </button>
      </div>
      <button
        className="mx-5 mt-3 flex h-80 shrink-0 items-center justify-center overflow-hidden rounded-2xl bg-paper-deep p-3"
        title="打开编辑"
        onClick={() => navigate(`/editor/${t.id}`)}
      >
        <img src={api.thumbnailURL(t)} alt={t.name} className="max-h-full max-w-full rounded-md object-contain" />
      </button>
      <div className="flex flex-col gap-4 px-5 py-5">
        <input
          aria-label="模板名称"
          value={name}
          onChange={(e) => setName(e.target.value)}
          onBlur={() => name.trim() && name !== t.name && void save({ name: name.trim() })}
          onKeyDown={(e) => e.key === 'Enter' && (e.target as HTMLInputElement).blur()}
          className="-mx-1.5 rounded-lg bg-transparent px-1.5 py-0.5 font-serif text-xl font-black text-ink outline-none hover:bg-paper focus:bg-paper"
        />
        <div className="flex gap-2">
          <button
            className="btn-primary flex-1"
            disabled={starting}
            title="把这个模板整份复制成一个新作品，在副本上改"
            onClick={async () => {
              setStarting(true)
              try {
                const p = await api.duplicateProject(t.id)
                navigate(`/editor/${p.id}`)
              } catch (e: any) {
                onError(e?.message ?? String(e))
                setStarting(false)
              }
            }}
          >
            {starting ? '新建中…' : '用这个模板新建'}
          </button>
          <button className="btn-ghost" title="直接编辑模板本身" onClick={() => navigate(`/editor/${t.id}`)}>
            编辑模板
          </button>
        </div>
        <label className="flex flex-col gap-2">
          <span className="text-xs text-muted">标签（逗号分隔）</span>
          <input
            value={tags}
            onChange={(e) => setTags(e.target.value)}
            onBlur={() => {
              const next = parseTags(tags)
              if (next.join('|') !== t.tags.join('|')) void save({ tags: next })
            }}
            placeholder="比如：大字，红底白字，人物压字"
            className="h-9 rounded-[10px] bg-paper px-3 text-[13px] text-ink outline-none placeholder:text-faint focus:ring-2 focus:ring-accent/30"
          />
          {suggestions.length > 0 && (
            <span className="flex flex-wrap gap-1.5">
              {suggestions.map((s) => (
                <button
                  key={s.name}
                  type="button"
                  className="rounded-full bg-paper px-2.5 py-0.5 text-[11px] text-muted hover:bg-paper-deep hover:text-ink"
                  onClick={() => {
                    const next = [...current, s.name]
                    setTags(next.join('，'))
                    void save({ tags: next })
                  }}
                >
                  + {s.name}
                </button>
              ))}
            </span>
          )}
        </label>
        <label className="flex flex-col gap-2">
          <span className="text-xs text-muted">备注：喜欢它哪里</span>
          <textarea
            value={note}
            onChange={(e) => setNote(e.target.value)}
            onBlur={() => note !== (t.note ?? '') && void save({ note })}
            rows={6}
            placeholder="比如：标题压满上半屏，人物叠在字上；黄黑撞色很抓眼"
            className="resize-y rounded-[10px] bg-paper px-3 py-2.5 text-[13px] leading-relaxed text-ink outline-none placeholder:text-faint focus:ring-2 focus:ring-accent/30"
          />
        </label>
        <label className="flex flex-col gap-2">
          <span className="flex items-center justify-between text-xs text-muted">
            来源链接
            {/^https?:\/\//.test(t.link ?? '') && (
              <a href={t.link} target="_blank" rel="noreferrer" className="text-accent hover:underline">
                打开原帖 ↗
              </a>
            )}
          </span>
          <input
            value={link}
            onChange={(e) => setLink(e.target.value)}
            onBlur={() => link.trim() !== (t.link ?? '') && void save({ link: link.trim() })}
            onKeyDown={(e) => e.key === 'Enter' && (e.target as HTMLInputElement).blur()}
            placeholder="原帖地址（可选）"
            className="h-9 rounded-[10px] bg-paper px-3 text-[13px] text-ink outline-none placeholder:text-faint focus:ring-2 focus:ring-accent/30"
          />
        </label>
        <div className="flex flex-col gap-1 text-[11px] text-faint">
          <span>
            ID <code className="select-all">{t.id}</code>
          </span>
          <span>
            在 Claude Code 里照着做：<code className="select-all">davinci tpl use {t.id}</code>
          </span>
        </div>
        <div className="flex gap-2">
          <button className="chip flex-1" onClick={() => void save({ kind: 'design' }, true)}>
            移回作品
          </button>
          <button className="chip flex-1 text-accent" onClick={() => setConfirm(true)}>
            删除模板
          </button>
        </div>
      </div>
      {confirm && (
        <ConfirmDialog
          title={`删除「${t.name}」？`}
          message="模板和它的全部图层都会被删除，此操作不能撤销。照着它做的作品不受影响。"
          confirmLabel="删除"
          danger
          onCancel={() => setConfirm(false)}
          onConfirm={async () => {
            setConfirm(false)
            try {
              await api.deleteProject(t.id)
              await onChanged(true)
            } catch (e: any) {
              onError(e?.message ?? String(e))
            }
          }}
        />
      )}
    </aside>
  )
}

function LinkDialog({ onCancel, onSubmit }: { onCancel: () => void; onSubmit: (url: string) => void }) {
  const [url, setUrl] = useState('')
  const ok = /^https?:\/\/\S+$/.test(url.trim())
  return (
    <Modal onClose={onCancel} label="从链接添加" width="w-[460px]">
      <form
        className="p-6"
        onSubmit={(e) => {
          e.preventDefault()
          if (ok) onSubmit(url.trim())
        }}
      >
        <h2 className="font-serif text-lg font-black text-ink">从链接添加</h2>
        <p className="mt-2 text-sm leading-relaxed text-muted">粘贴图片地址（右键图片 →「复制图片地址」），服务端会把图片存下来，做成一个模板。</p>
        <input
          autoFocus
          value={url}
          onChange={(e) => setUrl(e.target.value)}
          placeholder="https://…"
          className="mt-4 h-10 w-full rounded-[10px] bg-paper px-3 text-[13px] text-ink outline-none placeholder:text-faint focus:ring-2 focus:ring-accent/30"
        />
        <div className="mt-6 flex justify-end gap-2">
          <button type="button" className="btn-ghost" onClick={onCancel}>
            取消
          </button>
          <button type="submit" className="btn-primary" disabled={!ok}>
            收藏
          </button>
        </div>
      </form>
    </Modal>
  )
}
