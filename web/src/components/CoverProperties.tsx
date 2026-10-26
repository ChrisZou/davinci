import type { ProjectSummary } from '../api'

export type CoverMetadata = Pick<ProjectSummary, 'source' | 'author' | 'likes' | 'link' | 'note' | 'tags'>

/** The reference cover's metadata, separate from its editable layer properties. */
export function CoverProperties({ metadata }: { metadata: CoverMetadata }) {
  let sourceURL: URL | null = null
  try {
    const url = new URL(metadata.link ?? '')
    if (url.protocol === 'https:' || url.protocol === 'http:') sourceURL = url
  } catch { /* A missing or invalid link is plain text, never a navigation target. */ }
  const source = metadata.source?.trim() || sourceURL?.hostname || '—'
  const likes = Number.isFinite(metadata.likes) && metadata.likes >= 0 ? metadata.likes : 0

  return (
    <aside
      className="card mt-4 flex max-h-[40%] w-full shrink-0 flex-col overflow-hidden md:ml-4 md:mt-0 md:max-h-none md:w-[300px]"
      aria-label="封面属性"
      data-testid="cover-properties"
    >
      <div className="shrink-0 border-b border-line px-5 py-4">
        <h2 className="section-title">属性</h2>
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto px-5 py-5">
        <dl className="flex flex-col gap-5 text-[13px]">
          <div className="grid grid-cols-[56px_minmax(0,1fr)] items-start gap-3">
            <dt className="pt-0.5 text-xs text-muted">来源</dt>
            <dd className="min-w-0 leading-relaxed text-ink">
              {sourceURL ? (
                <a
                  href={sourceURL.href}
                  target="_blank"
                  rel="noopener noreferrer"
                  title={sourceURL.href}
                  aria-label={`打开来源：${source}`}
                  className="inline-flex max-w-full items-start gap-1.5 text-accent no-underline hover:underline"
                >
                  <span className="min-w-0 break-words [overflow-wrap:anywhere]">{source}</span>
                  <svg className="mt-1 shrink-0" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                    <path d="M14 3h7v7M21 3 10 14M10 3H5a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-5" />
                  </svg>
                </a>
              ) : <span className="break-words [overflow-wrap:anywhere]">{source}</span>}
            </dd>
          </div>
          <div className="grid grid-cols-[56px_minmax(0,1fr)] items-start gap-3">
            <dt className="pt-0.5 text-xs text-muted">作者</dt>
            <dd className="break-words leading-relaxed text-ink [overflow-wrap:anywhere]">{metadata.author?.trim() || '—'}</dd>
          </div>
          <div className="grid grid-cols-[56px_minmax(0,1fr)] items-baseline gap-3">
            <dt className="text-xs text-muted">点赞数</dt>
            <dd className="tabular-nums text-ink">{likes.toLocaleString('zh-CN')}</dd>
          </div>
        </dl>
        {!!metadata.tags?.length && (
          <section className="mt-6 border-t border-line pt-5" aria-label="标签">
            <h3 className="mb-3 text-xs font-normal text-muted">标签</h3>
            <div className="flex flex-wrap gap-1.5">
              {metadata.tags.map((tag, i) => <span key={`${i}:${tag}`} className="max-w-full break-words rounded-lg bg-paper px-2.5 py-1 text-xs leading-relaxed text-ink-2 [overflow-wrap:anywhere]">{tag}</span>)}
            </div>
          </section>
        )}
        <section className="mt-6 border-t border-line pt-5" aria-label="备注">
          <h3 className="mb-3 text-xs font-normal text-muted">备注</h3>
          <p className="whitespace-pre-wrap break-words text-[13px] leading-7 text-ink-2 [overflow-wrap:anywhere]">{metadata.note?.trim() || '—'}</p>
        </section>
      </div>
    </aside>
  )
}
