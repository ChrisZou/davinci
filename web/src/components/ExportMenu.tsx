import { useEffect, useRef, useState } from 'react'
import type { Document, ProjectDoc } from '../types'
import type { Editor } from '../editor/Editor'
import { encodeBoard } from '../editor/ck'
import { exportBoards, exportName } from '../editor/export'
import { Segmented, Slider } from './fields'

type Format = 'png' | 'jpeg' | 'webp'

/**
 * Downloads (or copies) the document straight from the canvas.
 *
 * The server has an export endpoint too; here the page renders with the very
 * same CanvasKit renderer, so `editor.toDataURL` skips the round trip and
 * gives the same pixels — the options below are the `export` command's.
 */
export function ExportMenu({
  doc,
  project,
  name,
  editor,
  selected,
  onLog,
}: {
  doc: Document
  project: ProjectDoc | null
  name: string
  editor: Editor | undefined
  selected: string[]
  onLog: (line: string) => void
}) {
  const [open, setOpen] = useState(false)
  const [format, setFormat] = useState<Format>('png')
  const [scale, setScale] = useState('1')
  const [quality, setQuality] = useState(0.92)
  const [transparent, setTransparent] = useState(false)
  const [onlySelected, setOnlySelected] = useState(false)
  const [scope, setScope] = useState<'current' | 'all'>('current')
  const [busy, setBusy] = useState('')
  const box = useRef<HTMLDivElement>(null)

  // Clicking anywhere else closes the panel, like any other menu.
  useEffect(() => {
    if (!open) return
    const onDown = (e: MouseEvent) => {
      if (box.current && !box.current.contains(e.target as Node)) setOpen(false)
    }
    window.addEventListener('mousedown', onDown)
    return () => window.removeEventListener('mousedown', onDown)
  }, [open])

  const boardCount = project?.boards.length ?? 1
  useEffect(() => {
    if (boardCount <= 1) setScope('current')
  }, [boardCount])

  const allBoards = scope === 'all' && boardCount > 1
  const canSelect = !allBoards && selected.length > 0
  const useSelection = onlySelected && canSelect
  const m = Number(scale)
  const canTransparent = format !== 'jpeg'

  const render = (fmt: Format) =>
    editor!.toDataURL(fmt, m, {
      quality,
      transparent: canTransparent && transparent,
      ids: useSelection ? selected : undefined,
    })

  const download = async () => {
    if (!editor || busy) return
    setBusy('download')
    try {
      const archive = allBoards && project
        ? await exportBoards(project.boards, { format, scale: m, quality, transparent: canTransparent && transparent }, encodeBoard)
        : null
      const url = archive ? URL.createObjectURL(archive) : await render(format)
      const a = document.createElement('a')
      a.href = url
      a.download = archive
        ? `${exportName(name)}.zip`
        : `${name || 'davinci'}${useSelection ? '-选中' : ''}.${format === 'jpeg' ? 'jpg' : format}`
      try {
        a.click()
      } finally {
        // Give the browser time to start the download before releasing its URL.
        if (archive) window.setTimeout(() => URL.revokeObjectURL(url), 60_000)
      }
      setOpen(false)
    } catch (e: any) {
      onLog(`✗ 导出失败：${e?.message ?? e}`)
    } finally {
      setBusy('')
    }
  }

  const copy = async () => {
    if (!editor || busy || allBoards) return
    setBusy('copy')
    try {
      // The clipboard only takes PNG, whatever format is picked for download.
      const blob = render('png').then((u) => fetch(u).then((r) => r.blob()))
      await navigator.clipboard.write([new ClipboardItem({ 'image/png': blob })])
      onLog('✓ 已复制 PNG 到剪贴板')
      setOpen(false)
    } catch (e: any) {
      onLog(`✗ 复制失败：${e?.message ?? e}`)
    } finally {
      setBusy('')
    }
  }

  const size = allBoards ? `${boardCount} 张（各画板 ${scale}×）` : useSelection ? '选中范围' : `${Math.round(doc.canvas.width * m)}×${Math.round(doc.canvas.height * m)}`

  return (
    <div className="relative" ref={box}>
      <button
        className="btn-primary ml-1.5"
        onClick={() => setOpen((v) => !v)}
        aria-expanded={open}
        data-testid="export-button"
      >
        导出图片
      </button>
      {open && (
        <div className="menu absolute right-0 top-full z-30 mt-3 flex w-72 flex-col gap-4 !p-5" data-testid="export-panel">
          {boardCount > 1 && (
            <Segmented
              label="导出范围"
              value={scope}
              options={[
                { value: 'current', label: '当前画板' },
                { value: 'all', label: '所有画板' },
              ]}
              onChange={setScope}
            />
          )}
          <Segmented
            label="格式"
            value={format}
            options={[
              { value: 'png', label: 'PNG' },
              { value: 'jpeg', label: 'JPG' },
              { value: 'webp', label: 'WebP' },
            ]}
            onChange={setFormat}
          />
          <Segmented
            label="倍率"
            value={scale}
            options={[
              { value: '1', label: '1×' },
              { value: '2', label: '2×' },
              { value: '3', label: '3×' },
            ]}
            onChange={setScale}
          />
          {format !== 'png' && (
            <Slider label="质量" value={quality} min={0.3} max={1} step={0.01} display={(v) => `${Math.round(v * 100)}%`} onChange={setQuality} />
          )}
          <div className="flex flex-col gap-2.5 text-[13px] text-ink">
            <label className={`flex items-center gap-2 ${canTransparent ? '' : 'opacity-40'}`}>
              <input type="checkbox" className="h-4 w-4 accent-accent" disabled={!canTransparent} checked={canTransparent && transparent} onChange={(e) => setTransparent(e.target.checked)} />
              透明背景
            </label>
            <label className={`flex items-center gap-2 ${canSelect ? '' : 'opacity-40'}`} title={allBoards ? '所有画板会导出每个画板的全部图层' : undefined}>
              <input type="checkbox" className="h-4 w-4 accent-accent" disabled={!canSelect} checked={useSelection} onChange={(e) => setOnlySelected(e.target.checked)} />
              只导出选中的 {selected.length || ''} 个图层
            </label>
          </div>
          <div className="text-xs text-muted">输出尺寸 <span className="tabular-nums text-ink">{size}</span></div>
          <div className="flex gap-2">
            <button
              className="btn-primary flex-1"
              disabled={!!busy}
              onClick={download}
            >
              {busy === 'download' ? '导出中…' : allBoards ? '下载 ZIP' : '下载'}
            </button>
            <button
              className="btn-ghost flex-1 px-2 disabled:opacity-50"
              disabled={!!busy || allBoards}
              onClick={copy}
              title={allBoards ? '复制到剪贴板仅支持当前画板' : '复制为 PNG，可直接粘贴到小红书、微信等'}
            >
              {busy === 'copy' ? '复制中…' : '复制到剪贴板'}
            </button>
          </div>
          {allBoards && <p className="text-xs text-muted">所有画板将打包为 ZIP；复制到剪贴板仅支持当前画板。</p>}
        </div>
      )}
    </div>
  )
}
