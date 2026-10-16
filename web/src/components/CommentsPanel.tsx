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
  const textarea = useRef<HTMLTextAreaElement>(null)
  const current = comments.filter((c) => c.boardId === board.id)
  const target = selected.length ? selected.map((id) => board.layers.find((l) => l.id === id)?.name || '所选图层').join('、') : '整个画板'
  useEffect(() => { bottom.current?.scrollIntoView({ block: 'nearest' }) }, [current.length, current.at(-1)?.status])
  const submit = async (retry?: EditComment) => {
    if (await ai.submit({ text: retry?.text ?? text, boardId: retry?.boardId ?? board.id, layerIds: retry?.layerIds ?? selected }) && !retry) setText('')
  }

  return <div className="flex min-h-0 flex-1 flex-col" data-testid="comments-panel">
    <div className="border-b border-line px-4 py-3">
      <div className="flex items-center justify-between gap-2">
        <span className="section-title">把想法留在画稿上</span>
        <span className="text-[10px] tracking-widest text-faint">AI EDIT</span>
      </div>
      <p className="m-0 mt-1 text-[11px] leading-relaxed text-muted">选中图层，说说怎么改。修改后可用顶部撤销。</p>
    </div>
    <div className="min-h-0 flex-1 overflow-y-auto px-4 py-4" aria-label="评论记录">
      {!loaded ? <p className="text-xs text-muted">正在读取评论…</p> : current.length === 0 ? <div className="pt-7">
        <span className="font-serif text-[36px] leading-none text-accent/60" aria-hidden>“</span>
        <h3 className="m-0 mt-2 font-serif text-[18px] text-ink">就在这里，改到满意。</h3>
        <p className="mb-5 mt-2 text-xs leading-6 text-muted">标题、配色、位置、图片效果，<br />把修改意见直接交给 AI。</p>
        <div className="flex flex-col gap-2">
          {['标题更醒目一点', '整体配色更温暖', '调整排版，增加留白'].map((example) => <button key={example} className="rounded-xl border border-line px-3 py-2.5 text-left text-xs text-ink-2 transition-colors hover:border-accent/40 hover:bg-accent-soft/40" onClick={() => { setText(example); textarea.current?.focus() }}>{example}<span className="float-right text-faint">↗</span></button>)}
        </div>
      </div> : <ol className="m-0 flex list-none flex-col gap-5 p-0">
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
            <p className="mb-0 mt-1.5 whitespace-pre-wrap break-words text-[11px] leading-5 text-muted">{c.status === 'running' ? '正在查看画稿、理解修改意见…' : c.reply}</p>
            {(c.status === 'failed' || c.status === 'cancelled') && <button className="mt-2 text-[11px] font-semibold text-accent disabled:opacity-40" disabled={!!running || sending || !available} onClick={() => void submit(c)}>按最新画稿重试 ↗</button>}
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
        <button type="button" className="shrink-0 text-[10px] text-muted hover:text-accent" onClick={() => void detect()}>刷新检测</button>
      </div>
      <div className="rounded-xl border border-line bg-paper/60 px-3 pt-2.5 focus-within:border-accent/60">
        <div className="truncate text-[10px] text-muted" title={`${board.name} · ${target}`}>↳ {target}</div>
        <label htmlFor="edit-comment" className="sr-only">修改意见</label>
        <textarea ref={textarea} id="edit-comment" className="min-h-[88px] w-full resize-y border-0 bg-transparent py-2 text-xs leading-6 text-ink outline-none placeholder:text-faint" placeholder="例如：把标题放大，改成暖黄色" value={text} maxLength={4000} onChange={(e) => setText(e.target.value)} onKeyDown={(e) => { if ((e.metaKey || e.ctrlKey) && e.key === 'Enter' && !e.nativeEvent.isComposing) { e.preventDefault(); if (available && loaded) void submit() } }} />
      </div>
      {error && <p role="alert" className="mb-0 mt-2 text-[11px] leading-5 text-accent">{error}</p>}
      {!available && <p className="mb-0 mt-2 text-[11px] leading-5 text-muted">请先在本机安装并登录 Codex 或 Claude Code，再刷新检测。</p>}
      <div className="mt-3 flex items-center justify-between gap-2">
        <span className="text-[10px] text-faint">{running ? '完成后自动更新画稿' : '使用本机 AI 登录与额度'}</span>
        {running ? <button type="button" className="btn-ghost h-8 px-3 text-xs" disabled={cancelling} onClick={() => void cancel()}>{cancelling ? '取消中…' : '取消修改'}</button> : <button type="submit" className="btn-primary h-8 px-3 text-xs" disabled={!loaded || sending || !available || !text.trim()}>{sending ? '提交中…' : '评论并修改 ↑'}</button>}
      </div>
    </form>
  </div>
}
