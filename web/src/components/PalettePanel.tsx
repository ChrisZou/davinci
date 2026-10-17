import { useEffect, useRef } from 'react'
import { modalOpen } from './Dialog'
import { PaletteBrowser } from './PaletteParts'

export function PalettePanel({ onApply, onClose, project, board }: {
  onApply: (id: string) => Promise<void>; onClose: () => void; project: string; board?: string
}) {
  const box = useRef<HTMLDivElement>(null)
  useEffect(() => {
    const key = (e: KeyboardEvent) => { if (e.key === 'Escape' && !modalOpen()) onClose() }
    const down = (e: MouseEvent) => {
      const target = e.target as HTMLElement
      if (box.current?.contains(target) || target.closest('[data-palette-toggle]') || modalOpen()) return
      onClose()
    }
    window.addEventListener('keydown', key); window.addEventListener('mousedown', down)
    return () => { window.removeEventListener('keydown', key); window.removeEventListener('mousedown', down) }
  }, [onClose])
  return (
    <div ref={box} role="dialog" aria-label="配色" className="menu fixed bottom-5 left-5 top-[84px] z-30 flex w-[440px] max-w-[calc(100vw-40px)] flex-col !p-0" data-testid="palette-panel">
      <div className="flex items-center justify-between border-b border-line px-5 py-4">
        <h2 className="m-0 font-serif text-lg font-black">配色</h2>
        <a href={`/palettes?${new URLSearchParams({ project, ...(board ? { board } : {}) })}`} target="_blank" rel="noreferrer" className="text-xs text-muted hover:text-accent">管理配色 ↗</a>
        <button className="icon-btn h-8 w-8" title="关闭" aria-label="关闭配色" onClick={onClose}>×</button>
      </div>
      <PaletteBrowser compact onApply={onApply} />
    </div>
  )
}
