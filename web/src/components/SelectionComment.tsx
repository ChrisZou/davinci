import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import type { Board } from '../types'
import type { SelectionFrame } from '../editor/Editor'
import type { EditComments } from './useEditComments'

/** A single composer for the complete selection, anchored to its union frame. */
export function SelectionComment({ ai, board, selected, frame }: {
  ai: EditComments; board: Board; selected: string[]; frame: SelectionFrame | null
}) {
  const [open, setOpen] = useState(false)
  const [text, setText] = useState('')
  const [submitted, setSubmitted] = useState<string | null>(null)
  const [height, setHeight] = useState(200)
  const root = useRef<HTMLDivElement>(null)
  const input = useRef<HTMLTextAreaElement>(null)
  const result = ai.comments.find((c) => c.id === submitted)
  const busy = ai.sending || !!ai.running
  const target = selected.length > 1 ? `已选 ${selected.length} 个图层` : board.layers.find((l) => l.id === selected[0])?.name || '所选图层'
  const visible = !!frame && frame.width > 40 && frame.height > 40 && frame.right >= 0 && frame.left <= frame.width && frame.bottom >= 0 && frame.top <= frame.height

  useLayoutEffect(() => {
    if (!open || !visible || !root.current) return
    const observer = new ResizeObserver(([entry]) => setHeight(entry.borderBoxSize[0]?.blockSize ?? entry.contentRect.height))
    observer.observe(root.current)
    input.current?.focus()
    return () => observer.disconnect()
  }, [open, visible])
  useEffect(() => {
    if (!open) return
    const outside = (e: PointerEvent) => { if (!root.current?.contains(e.target as Node)) setOpen(false) }
    document.addEventListener('pointerdown', outside)
    return () => document.removeEventListener('pointerdown', outside)
  }, [open])
  useEffect(() => { if (result?.status === 'done') setText('') }, [result?.status])

  if (!visible || !frame) return null
  const width = Math.min(open ? 304 : selected.length > 1 ? 146 : 112, frame.width - 24)
  const h = open ? height : 34
  // Prefer the right side; near an edge, tuck above or below the selection.
  const side = frame.right + 14 + width <= frame.width - 12
  const left = Math.max(12, Math.min(side ? frame.right + 14 : frame.right - width, frame.width - width - 12))
  const desiredTop = side ? frame.top : frame.top - h - 14 >= 12 ? frame.top - h - 14 : frame.bottom + 14
  const top = Math.max(12, Math.min(desiredTop, frame.height - h - 12))
  const submit = async () => {
    const comment = await ai.submit({ boardId: board.id, layerIds: selected, text })
    if (comment) setSubmitted(comment.id)
  }

  return <div ref={root} data-testid="selection-comment" className="absolute z-10" style={{ left, top, width }}
    onPointerDown={(e) => e.stopPropagation()} onContextMenu={(e) => { e.preventDefault(); e.stopPropagation() }}
    onKeyDown={(e) => { e.stopPropagation(); if (e.key === 'Escape') { e.preventDefault(); setOpen(false) } }}>
    {!open ? <button type="button" aria-expanded={false} aria-label={selected.length > 1 ? `AI 评论，修改 ${selected.length} 个图层` : 'AI 评论'}
      className="island flex h-[34px] w-full items-center justify-center gap-1.5 rounded-full border border-accent/25 text-xs font-semibold text-accent transition-colors hover:bg-accent-soft"
      onClick={() => setOpen(true)}><span aria-hidden>✦</span>{busy ? '正在修改…' : 'AI 评论'}{selected.length > 1 && !busy && <span className="text-[10px] font-normal text-muted">· {selected.length}</span>}</button>
      : <form aria-label="AI 评论改图" className="island overflow-y-auto rounded-2xl border border-accent/25 p-3" style={{ maxHeight: Math.max(40, frame.height - 24) }}
        onSubmit={(e) => { e.preventDefault(); void submit() }}>
        <div className="mb-2 flex items-center gap-2">
          <span className="text-xs font-semibold text-accent">✦ AI 评论</span>
          <span className="min-w-0 flex-1 truncate text-[10px] text-muted" title={target}>{target}</span>
          <button type="button" aria-label="收起 AI 评论" className="text-lg leading-none text-muted hover:text-ink" onClick={() => setOpen(false)}>×</button>
        </div>
        <textarea ref={input} aria-label="AI 修改意见" className="h-20 w-full resize-none rounded-lg border-0 bg-paper/60 px-2.5 py-2 text-xs leading-5 text-ink outline-none placeholder:text-faint focus:ring-1 focus:ring-accent/30"
          placeholder={selected.length > 1 ? '这些图层怎么改？例如：统一颜色、对齐排版' : '想怎么改？例如：标题放大，改成暖黄色'}
          maxLength={4000} value={text} disabled={busy} onChange={(e) => setText(e.target.value)}
          onKeyDown={(e) => { if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) { e.preventDefault(); void submit() } }} />
        {ai.error && <p role="alert" className="my-2 text-[11px] leading-5 text-accent">{ai.error}</p>}
        {result && result.status !== 'running' && <p role="status" className={`my-2 text-[11px] leading-5 ${result.status === 'done' ? 'text-ok' : 'text-accent'}`}>{result.status === 'done' ? '已修改，可用 ⌘Z 撤销。' : result.reply || '修改已取消，可以重新提交。'}</p>}
        {!ai.available && ai.loaded && <p className="my-2 text-[11px] leading-5 text-muted">请先安装并登录本机 Codex 或 Claude Code。<button type="button" className="text-accent" onClick={() => void ai.detect()}>重新检测</button></p>}
        <div className="mt-2 flex items-center justify-between gap-2">
          <span className="text-[10px] text-muted" aria-live="polite">{busy ? '正在修改，完成后自动更新' : 'Enter 提交 · Shift Enter 换行'}</span>
          {ai.running ? <button type="button" className="btn-ghost h-7 px-2 text-[11px]" disabled={ai.cancelling} onClick={() => void ai.cancel()}>取消</button>
            : <button type="submit" className="btn-primary h-7 px-3 text-[11px]" disabled={!text.trim() || !ai.loaded || !ai.available || ai.sending}>直接修改 ↑</button>}
        </div>
      </form>}
  </div>
}
