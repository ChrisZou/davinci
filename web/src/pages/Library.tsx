import { useEffect, useRef, useState } from 'react'
import { api, type LibraryCategory, type LibraryItem } from '../api'
import { ConfirmDialog } from '../components/Dialog'
import { ItemTile, useLibrary } from '../components/LibraryParts'
import { Lockup } from '../components/Brand'

/**
 * The material library: shelves on the left, the pictures of the chosen shelf
 * (or all of them) in a grid, and the chosen picture's details on the right.
 * Files dropped anywhere on the page go onto the current shelf.
 *
 * AI reaches the same library with `davinci lib …`.
 */
export function Library() {
  const [category, setCategory] = useState('')
  const [query, setQuery] = useState('')
  const { categories, items, error, setError, reload } = useLibrary(category, query)
  const [selected, setSelected] = useState<LibraryItem | null>(null)
  const [uploading, setUploading] = useState(0)
  const [dropping, setDropping] = useState(false)
  const [picking, setPicking] = useState(false)
  const fileRef = useRef<HTMLInputElement>(null)
  // Where the files being picked go: set when the button (or the category
  // menu under it) opens the file dialog, read when the dialog returns.
  const target = useRef<LibraryCategory | null>(null)
  const pickBox = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!picking) return
    const onDown = (e: MouseEvent) => {
      if (pickBox.current && !pickBox.current.contains(e.target as Node)) setPicking(false)
    }
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && setPicking(false)
    window.addEventListener('mousedown', onDown)
    window.addEventListener('keydown', onKey)
    return () => {
      window.removeEventListener('mousedown', onDown)
      window.removeEventListener('keydown', onKey)
    }
  }, [picking])

  /** Opens the file dialog for a category (and shows that category). */
  const chooseFiles = (cat: LibraryCategory) => {
    target.current = cat
    setPicking(false)
    setCategory(cat.id)
    setSelected(null)
    fileRef.current?.click()
  }

  useEffect(() => {
    document.title = '素材库 · davinci'
  }, [])

  const current = categories?.find((c) => c.id === category)
  const total = categories?.reduce((n, c) => n + c.count, 0) ?? 0

  const upload = async (files: File[], into: LibraryCategory | null | undefined) => {
    const images = files.filter((f) => f.type.startsWith('image/'))
    if (!images.length) return
    if (!into) {
      setError('先在左边选一个分类，再把图片拖进来')
      return
    }
    setUploading(images.length)
    try {
      for (const f of images) {
        await api.addLibraryItem(f, into.id)
        setUploading((n) => n - 1)
      }
    } catch (e: any) {
      setError(`上传失败：${e?.message ?? e}`)
    } finally {
      setUploading(0)
      await reload()
    }
  }

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
        void upload(Array.from(e.dataTransfer.files), current)
      }}
    >
      <header className="flex h-[84px] shrink-0 items-center gap-4 px-8">
        <Lockup size={30} />
        <span className="h-4 w-px bg-line-strong" />
        <h1 className="m-0 font-serif text-2xl font-black text-ink">素材库</h1>
        <div className="flex-1" />
        <input
          aria-label="搜索素材"
          placeholder="搜名称、标签、说明…"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          className="h-9 w-72 rounded-full border border-line-strong bg-card px-4 text-[13px] text-ink outline-none placeholder:text-faint focus:ring-2 focus:ring-accent/30"
        />
        <div className="relative" ref={pickBox}>
          <button
            className="btn-primary"
            disabled={uploading > 0 || !categories?.length}
            aria-expanded={current ? undefined : picking}
            title={current ? `上传到「${current.name}」，也可以直接把图片拖进来` : '选择要上传到哪个分类'}
            onClick={() => (current ? chooseFiles(current) : setPicking((v) => !v))}
            data-testid="upload-button"
          >
            {uploading > 0 ? `上传中… 还剩 ${uploading}` : current ? `上传到「${current.name}」` : '上传素材 ▾'}
          </button>
          {/* In 全部 there is no shelf yet: ask which one, then open the file dialog. */}
          {picking && !current && (
            <div className="menu absolute right-0 top-full z-20 mt-2 w-52" role="menu" aria-label="上传到哪个分类">
              <div className="px-2.5 pb-1 pt-1.5 text-xs text-muted">上传到</div>
              {categories?.map((c) => (
                <button key={c.id} role="menuitem" className="menu-item" onClick={() => chooseFiles(c)}>
                  <span>{c.name}</span>
                  <span className="text-xs text-faint">{c.count}</span>
                </button>
              ))}
            </div>
          )}
        </div>
        <input
          ref={fileRef}
          type="file"
          accept="image/*"
          multiple
          className="hidden"
          onChange={(e) => {
            const files = Array.from(e.target.files ?? [])
            e.target.value = ''
            void upload(files, target.current)
          }}
        />
      </header>

      <div className="flex min-h-0 flex-1 gap-5 px-5 pb-5">
        <CategoryList
          categories={categories}
          total={total}
          current={category}
          onPick={(id) => {
            setCategory(id)
            setSelected(null)
          }}
          onChanged={reload}
          onError={setError}
        />

        <main className="card relative min-w-0 flex-1 overflow-y-auto p-6">
          {error && (
            <div role="alert" className="mb-4 flex items-center justify-between rounded-xl bg-accent-soft px-4 py-2.5 text-sm text-accent-dark">
              {error}
              <button className="text-xs underline" onClick={() => setError('')}>
                知道了
              </button>
            </div>
          )}
          <div className="mb-4 flex items-baseline gap-3">
            <h2 className="m-0 font-serif text-xl font-black">{current ? current.name : '全部素材'}</h2>
            <span className="text-xs text-faint">{items ? `${items.length} 个${query ? `匹配「${query}」` : ''}` : ''}</span>
          </div>
          {items === null && <p className="text-sm text-faint">载入中……</p>}
          {items?.length === 0 && (
            <p className="rounded-2xl border border-dashed border-line-strong px-6 py-14 text-center text-sm leading-relaxed text-muted">
              {query ? '没有匹配的素材。' : current ? `「${current.name}」里还没有素材。把图片拖进来，或者点右上角上传。` : '素材库还是空的。先选一个分类，再把图片拖进来。'}
              <br />
              也可以在 Claude Code 里用 <code className="rounded bg-paper-deep px-1.5 py-0.5 text-xs">davinci lib add</code> 批量导入。
            </p>
          )}
          <div className="grid grid-cols-[repeat(auto-fill,minmax(140px,1fr))] gap-4">
            {items?.map((it) => (
              <ItemTile key={it.id} item={it} selected={selected?.id === it.id} onClick={() => setSelected(it)} />
            ))}
          </div>
          {dropping && (
            <div className="pointer-events-none absolute inset-3 flex items-center justify-center rounded-3xl border-2 border-dashed border-accent/60 bg-accent-soft/70 font-serif text-lg font-black text-accent">
              {current ? `松开鼠标，放进「${current.name}」` : '先在左边选一个分类'}
            </div>
          )}
        </main>

        {selected && categories && (
          <ItemDetail
            key={selected.id}
            id={selected.id}
            categories={categories}
            onClose={() => setSelected(null)}
            onChanged={async (it) => {
              if (it) setSelected(it)
              else setSelected(null)
              await reload()
            }}
            onError={setError}
          />
        )}
      </div>
    </div>
  )
}

function CategoryList({
  categories,
  total,
  current,
  onPick,
  onChanged,
  onError,
}: {
  categories: LibraryCategory[] | null
  total: number
  current: string
  onPick: (id: string) => void
  onChanged: () => Promise<void>
  onError: (msg: string) => void
}) {
  const [adding, setAdding] = useState(false)
  const [renaming, setRenaming] = useState<string | null>(null)
  const [confirm, setConfirm] = useState<LibraryCategory | null>(null)
  const [menu, setMenu] = useState<{ cat: LibraryCategory; x: number; y: number } | null>(null)
  const menuBox = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (!menu) return
    const close = (e: Event) => {
      if (e instanceof MouseEvent && menuBox.current?.contains(e.target as Node)) return
      setMenu(null)
    }
    const onKey = (e: KeyboardEvent) => e.key === 'Escape' && setMenu(null)
    window.addEventListener('mousedown', close)
    window.addEventListener('blur', close)
    window.addEventListener('keydown', onKey)
    return () => {
      window.removeEventListener('mousedown', close)
      window.removeEventListener('blur', close)
      window.removeEventListener('keydown', onKey)
    }
  }, [menu])

  const row = (id: string, name: string, count: number) => (
    <button
      className={`flex h-10 w-full items-center justify-between rounded-xl px-3 text-left text-[13px] transition-colors ${
        current === id ? 'bg-accent-soft font-bold text-ink' : 'text-ink-2 hover:bg-paper'
      }`}
      onClick={() => onPick(id)}
    >
      <span className="truncate">{name}</span>
      <span className="text-xs text-faint">{count}</span>
    </button>
  )

  return (
    <aside className="card flex w-[220px] shrink-0 flex-col gap-1 overflow-y-auto p-3" aria-label="分类">
      <h2 className="section-title px-3 pb-2 pt-2">分类</h2>
      {row('', '全部', total)}
      {categories?.map((c) =>
        renaming === c.id ? (
          <NameInput
            key={c.id}
            initial={c.name}
            onDone={async (name) => {
              setRenaming(null)
              if (!name || name === c.name) return
              try {
                await api.renameCategory(c.id, name)
                await onChanged()
              } catch (e: any) {
                onError(e?.message ?? String(e))
              }
            }}
          />
        ) : (
          <div
            key={c.id}
            onDoubleClick={() => setRenaming(c.id)}
            // Rename and delete live in the right-click menu, so the list stays clean.
            onContextMenu={(e) => {
              e.preventDefault()
              setMenu({ cat: c, x: e.clientX, y: e.clientY })
            }}
          >
            {row(c.id, c.name, c.count)}
          </div>
        ),
      )}
      {adding ? (
        <NameInput
          initial=""
          placeholder="分类名"
          onDone={async (name) => {
            setAdding(false)
            if (!name) return
            try {
              const c = await api.createCategory(name)
              await onChanged()
              onPick(c.id)
            } catch (e: any) {
              onError(e?.message ?? String(e))
            }
          }}
        />
      ) : (
        <button className="mt-1 flex h-10 items-center gap-2 rounded-xl px-3 text-[13px] text-muted hover:bg-paper hover:text-ink" onClick={() => setAdding(true)}>
          <span className="text-base leading-none">+</span> 新建分类
        </button>
      )}
      <p className="mt-auto px-3 pt-4 text-[11px] leading-relaxed text-faint">右键分类可以改名或删除。</p>
      {menu && (
        <div ref={menuBox} role="menu" className="menu fixed z-40 w-44" style={{ left: menu.x, top: menu.y }} data-testid="category-menu">
          <button
            role="menuitem"
            className="menu-item"
            onClick={() => {
              setRenaming(menu.cat.id)
              setMenu(null)
            }}
          >
            重命名
          </button>
          <button
            role="menuitem"
            className="menu-item text-accent"
            disabled={menu.cat.count > 0}
            title={menu.cat.count > 0 ? '分类里还有素材，先移走或删掉才能删分类' : undefined}
            onClick={() => {
              setConfirm(menu.cat)
              setMenu(null)
            }}
          >
            <span>删除分类</span>
            {menu.cat.count > 0 && <span className="text-xs text-faint">还有 {menu.cat.count} 个</span>}
          </button>
        </div>
      )}
      {confirm && (
        <ConfirmDialog
          title={`删除分类「${confirm.name}」？`}
          confirmLabel="删除"
          danger
          onCancel={() => setConfirm(null)}
          onConfirm={async () => {
            const c = confirm
            setConfirm(null)
            try {
              await api.deleteCategory(c.id)
              if (current === c.id) onPick('')
              await onChanged()
            } catch (e: any) {
              onError(e?.message ?? String(e))
            }
          }}
        />
      )}
    </aside>
  )
}

function NameInput({ initial, placeholder, onDone }: { initial: string; placeholder?: string; onDone: (name: string) => void }) {
  const [v, setV] = useState(initial)
  const done = useRef(false)
  const finish = (name: string) => {
    if (done.current) return
    done.current = true
    onDone(name.trim())
  }
  return (
    <input
      autoFocus
      value={v}
      placeholder={placeholder}
      onFocus={(e) => e.target.select()}
      onChange={(e) => setV(e.target.value)}
      onBlur={() => finish(v)}
      onKeyDown={(e) => {
        if (e.key === 'Enter') finish(v)
        if (e.key === 'Escape') finish(initial)
      }}
      className="h-10 w-full rounded-xl bg-paper px-3 text-[13px] text-ink outline-none ring-2 ring-accent/30"
    />
  )
}

/** The chosen picture: a big preview, its editable name/shelf/tags, and what it is for. */
function ItemDetail({
  id,
  categories,
  onClose,
  onChanged,
  onError,
}: {
  id: string
  categories: LibraryCategory[]
  onClose: () => void
  onChanged: (item: LibraryItem | null) => Promise<void>
  onError: (msg: string) => void
}) {
  const [item, setItem] = useState<LibraryItem | null>(null)
  const [name, setName] = useState('')
  const [tags, setTags] = useState('')
  const [showMeta, setShowMeta] = useState(false)
  const [confirm, setConfirm] = useState(false)

  useEffect(() => {
    let alive = true
    void api
      .libraryItem(id)
      .then((it) => {
        if (!alive) return
        setItem(it)
        setName(it.name)
        setTags(it.tags.join('，'))
      })
      .catch((e) => onError(e?.message ?? String(e)))
    return () => {
      alive = false
    }
  }, [id, onError])

  const save = async (patch: Parameters<typeof api.updateLibraryItem>[1]) => {
    try {
      const it = await api.updateLibraryItem(id, patch)
      setItem({ ...it, meta: item?.meta })
      await onChanged(it)
    } catch (e: any) {
      onError(e?.message ?? String(e))
    }
  }

  if (!item) {
    return <aside className="card w-[340px] shrink-0 p-6 text-sm text-faint">载入中……</aside>
  }

  return (
    <aside className="card flex w-[340px] shrink-0 flex-col overflow-y-auto" aria-label="素材详情">
      <div className="flex items-center justify-between px-5 pt-4">
        <span className="text-xs text-muted">
          {item.category} · {item.width}×{item.height}
        </span>
        <button className="icon-btn h-8 w-8 text-faint" aria-label="关闭" onClick={onClose}>
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round">
            <path d="M6 6l12 12M18 6 6 18" />
          </svg>
        </button>
      </div>
      <div className="dv-checker mx-5 mt-3 flex h-72 shrink-0 items-center justify-center overflow-hidden rounded-2xl p-3">
        <img src={`${item.thumb}?w=640`} alt={item.name} className="max-h-full max-w-full object-contain" />
      </div>
      <div className="flex flex-col gap-4 px-5 py-5">
        <input
          aria-label="素材名称"
          value={name}
          onChange={(e) => setName(e.target.value)}
          onBlur={() => name.trim() && name !== item.name && void save({ name: name.trim() })}
          onKeyDown={(e) => e.key === 'Enter' && (e.target as HTMLInputElement).blur()}
          className="-mx-1.5 rounded-lg bg-transparent px-1.5 py-0.5 font-serif text-xl font-black text-ink outline-none hover:bg-paper focus:bg-paper"
        />
        <label className="flex flex-col gap-2">
          <span className="text-xs text-muted">分类</span>
          <select
            value={item.categoryId}
            onChange={(e) => void save({ category: e.target.value })}
            className="h-9 cursor-pointer rounded-[10px] bg-paper px-3 text-[13px] text-ink outline-none"
          >
            {categories.map((c) => (
              <option key={c.id} value={c.id}>
                {c.name}
              </option>
            ))}
          </select>
        </label>
        <label className="flex flex-col gap-2">
          <span className="text-xs text-muted">标签（逗号分隔）</span>
          <input
            value={tags}
            onChange={(e) => setTags(e.target.value)}
            onBlur={() => {
              const next = tags.split(/[,，、]/).map((t) => t.trim()).filter(Boolean)
              if (next.join('|') !== item.tags.join('|')) void save({ tags: next })
            }}
            placeholder="比如：指向，惊讶"
            className="h-9 rounded-[10px] bg-paper px-3 text-[13px] text-ink outline-none placeholder:text-faint focus:ring-2 focus:ring-accent/30"
          />
        </label>
        {item.description && (
          <div className="flex flex-col gap-2">
            <span className="text-xs text-muted">说明</span>
            <p className="m-0 whitespace-pre-wrap rounded-[10px] bg-paper px-3 py-2.5 text-xs leading-relaxed text-ink-2">{item.description}</p>
          </div>
        )}
        {item.meta != null && (
          <div className="flex flex-col gap-2">
            <button className="self-start text-xs text-muted hover:text-ink" onClick={() => setShowMeta((v) => !v)}>
              {showMeta ? '收起' : '查看'}标注数据（给 AI 用）
            </button>
            {showMeta && (
              <pre className="max-h-64 overflow-auto rounded-[10px] bg-paper p-3 font-mono text-[10px] leading-relaxed text-ink-2">
                {JSON.stringify(item.meta, null, 2)}
              </pre>
            )}
          </div>
        )}
        <div className="flex flex-col gap-1 text-[11px] text-faint">
          <span>
            ID <code className="select-all">{item.id}</code>
          </span>
          <span>
            在 Claude Code 里插入：<code className="select-all">davinci lib insert {item.id}</code>
          </span>
        </div>
        <button className="chip text-accent" onClick={() => setConfirm(true)}>
          从素材库删除
        </button>
      </div>
      {confirm && (
        <ConfirmDialog
          title={`删除「${item.name}」？`}
          message="只是从素材库里拿掉，已经用在设计里的图片不受影响。"
          confirmLabel="删除"
          danger
          onCancel={() => setConfirm(false)}
          onConfirm={async () => {
            setConfirm(false)
            try {
              await api.deleteLibraryItem(item.id)
              await onChanged(null)
            } catch (e: any) {
              onError(e?.message ?? String(e))
            }
          }}
        />
      )}
    </aside>
  )
}
