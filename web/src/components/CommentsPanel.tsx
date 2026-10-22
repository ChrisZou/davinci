import { useEffect, useRef, useState } from 'react'
import type { EditComment } from '../api'
import type { EditComments } from './useEditComments'
import type { Board } from '../types'

const statusLabel: Record<EditComment['status'], string> = {
  running: '正在修改', done: '已修改', failed: '未完成', cancelled: '已取消', needs_info: '需要补充',
}

export function CommentsPanel({ ai, board, selected }: { ai: EditComments; board: Board; selected: string[] }) {
  const { comments, providers, provider, setProvider, error, loaded, sending, cancelling, running, available, detect, cancel } = ai
  const [text, setText] = useState('')
  const bottom = useRef<HTMLDivElement>(null)
  // One conversation per project: a comment may add boards and land on one of them.
  const current = comments
  const target = selected.length ? selected.map((id) => board.layers.find((l) => l.id === id)?.name || '所选图层').join('、') : '整个画板'
  useEffect(() => { bottom.current?.scrollIntoView({ block: 'nearest' }) }, [current.length, current.at(-1)?.status])
  const submit = async (retry?: EditComment) => {
    if (await ai.submit({ text: retry?.text ?? text, boardId: retry?.boardId ?? board.id, layerIds: retry?.layerIds ?? selected }) && !retry) setText('')
  }

  return <div className="flex min-h-0 flex-1 flex-col" data-testid="comments-panel">
    <div className="min-h-0 flex-1 overflow-y-auto px-4 py-4" aria-label="评论记录">
      {!loaded ? <p className="text-xs text-muted">正在读取评论…</p> : <ol className="m-0 flex list-none flex-col gap-5 p-0">
        {current.map((c) => <li key={c.id} className="text-xs" data-testid="edit-comment">
          <div className="mb-2 flex items-center justify-between text-[10px] text-faint"><span>你</span><time dateTime={c.createdAt}>{new Date(c.createdAt).toLocaleString('zh-CN', { month: 'numeric', day: 'numeric', hour: '2-digit', minute: '2-digit' })}</time></div>
          <div className="rounded-2xl rounded-tr-sm bg-paper px-3 py-3">
            <div className="mb-1.5 truncate text-[10px] text-muted" title={c.target}>↳ {c.target}</div>
            <p className="m-0 whitespace-pre-wrap break-words leading-6">{c.text}</p>
          </div>
          <div className="mt-3 border-l-2 border-line pl-3" aria-live="polite">
            <div className="flex items-center gap-1.5 text-[11px] font-semibold">
              <span className={`h-1.5 w-1.5 rounded-full ${c.status === 'running' ? 'animate-pulse bg-accent' : c.status === 'done' ? 'bg-ok' : 'bg-ghost'}`} />
              <span>{c.provider === 'codex' ? 'Codex' : 'Claude Code'}</span>
              <span className={`ml-auto text-[10px] font-normal ${c.status === 'done' ? 'text-ok' : 'text-muted'}`}>{statusLabel[c.status]}</span>
            </div>
            {c.status !== 'running' && c.reply && <p className="mb-0 mt-1.5 whitespace-pre-wrap break-words text-[11px] leading-5 text-muted">{c.reply}</p>}
            {(c.status === 'failed' || c.status === 'cancelled') && <button className="mt-2 text-[11px] font-semibold text-accent disabled:opacity-40" disabled={!!running || sending || !available} onClick={() => void submit(c)}>重试</button>}
          </div>
        </li>)}
      </ol>}
      <div ref={bottom} />
    </div>
    <form className="border-t border-line p-3" onSubmit={(e) => { e.preventDefault(); void submit() }}>
      <div className="mb-2 flex items-center justify-between gap-2">
        <label className="sr-only" htmlFor="comment-provider">改图 AI</label>
        <select id="comment-provider" className="min-w-0 rounded-lg border border-line bg-card px-2 py-1.5 text-[11px] text-ink-2" value={provider} disabled={!!running || sending} onChange={(e) => setProvider(e.target.value)}>
          {(providers.length ? providers : [{ id: 'codex', name: 'Codex', available: false }, { id: 'claude', name: 'Claude Code', available: false }]).map((p) => <option key={p.id} value={p.id}>{p.name}{p.available ? '' : ' · 未检测到'}</option>)}
        </select>
        <button type="button" aria-label="刷新检测" title="刷新检测" className="h-7 w-7 shrink-0 text-lg text-muted hover:text-accent" onClick={() => void detect()}><span aria-hidden>↻</span></button>
      </div>
      <div className="rounded-xl border border-line bg-paper/60 px-3 pt-2.5 focus-within:border-accent/60">
        <div className="truncate text-[10px] text-muted" title={`${board.name} · ${target}`}>↳ {target}</div>
        <label htmlFor="edit-comment" className="sr-only">修改意见</label>
        <textarea id="edit-comment" className="min-h-[88px] w-full resize-y border-0 bg-transparent py-2 text-xs leading-6 text-ink outline-none placeholder:text-faint" placeholder="修改意见" value={text} maxLength={4000} onChange={(e) => setText(e.target.value)} onKeyDown={(e) => { if ((e.metaKey || e.ctrlKey) && e.key === 'Enter' && !e.nativeEvent.isComposing) { e.preventDefault(); if (available && loaded) void submit() } }} />
      </div>
      {error && <p role="alert" className="mb-0 mt-2 text-[11px] leading-5 text-accent">{error}</p>}
      {!available && <p className="mb-0 mt-2 text-[11px] leading-5 text-muted">未检测到 AI，请安装 Codex 或 Claude Code。</p>}
      <div className="mt-3 flex items-center justify-end gap-2">
        {running ? <button type="button" className="btn-ghost h-8 px-3 text-xs" disabled={cancelling} onClick={() => void cancel()}>{cancelling ? '取消中…' : '取消'}</button> : <button type="submit" className="btn-primary h-8 px-3 text-xs" disabled={!loaded || sending || !available || !text.trim()}>{sending ? '提交中…' : '修改 ↑'}</button>}
      </div>
    </form>
  </div>
}
