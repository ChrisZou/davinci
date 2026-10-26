import { useRef } from 'react'
import type { LinearGradient, ShapeStyle } from '../types'
import { gradientCSS, readGradient } from '../render/gradient'
import { ColorField, NumField, Segmented } from './fields'

export function ShapeFill({ style, palette, onChange }: {
  style: ShapeStyle
  palette: string[]
  onChange: (props: Record<string, unknown>) => void
}) {
  const gradient = readGradient(style.fillGradient)
  const baseColor = style.fill && !['none', 'transparent'].includes(style.fill) ? style.fill : '#000000'
  const previous = useRef<LinearGradient | null>(null)
  if (gradient) previous.current = gradient
  const set = (next: LinearGradient) => {
    previous.current = next
    onChange({ fillGradient: next })
  }
  const stop = (index: number, patch: Partial<LinearGradient['stops'][number]>) => {
    const current = previous.current ?? gradient
    if (!current) return
    set({ ...current, stops: current.stops.map((s, i) => i === index ? { ...s, ...patch } : s) as LinearGradient['stops'] })
  }

  return (
    <div className="flex flex-col gap-3" data-testid="shape-fill-control">
      <Segmented
        label="填充"
        value={gradient ? 'gradient' : 'solid'}
        options={[{ value: 'solid', label: '纯色' }, { value: 'gradient', label: '渐变' }]}
        onChange={(mode) => {
          if (mode === 'solid') onChange({ fillGradient: null })
          else set(previous.current ?? {
            type: 'linear', angle: 180,
            stops: [{ color: baseColor, opacity: 1 }, { color: baseColor, opacity: 0 }],
          })
        }}
      />
      {gradient ? (
        <>
          <div className="flex items-center gap-2">
            <div className="dv-checker h-7 min-w-0 flex-1 overflow-hidden rounded-lg border border-line" aria-hidden="true">
              <div className="h-full w-full" style={{ background: gradientCSS(gradient) }} />
            </div>
            <button type="button" className="icon-btn h-7 w-7 shrink-0" title="交换颜色" aria-label="交换渐变颜色" onClick={() => set({ ...gradient, stops: [gradient.stops[1], gradient.stops[0]] })}>
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round">
                <path d="M4 8h16m-4-4 4 4-4 4M20 16H4m4-4-4 4 4 4" />
              </svg>
            </button>
          </div>
          <div className="flex flex-col gap-3">
            {gradient.stops.map((s, i) => (
              <div key={i} className="grid grid-cols-[minmax(0,1fr)_80px] items-start gap-2">
                <ColorField label={i === 0 ? '起点' : '终点'} value={s.color} showPalettes={false} testId={`gradient-color-${i}`} onCommit={(color) => {
                  if (color.trim() && CSS.supports('color', color)) stop(i, { color })
                }} />
                <div className="flex min-w-0 flex-col gap-2">
                  <span className="text-xs text-muted">不透明度</span>
                  <NumField label="" ariaLabel={`${i === 0 ? '起点' : '终点'}不透明度`} value={s.opacity} min={0} max={1} scale={100} suffix="%" testId={`gradient-opacity-${i}`} onCommit={(opacity) => stop(i, { opacity })} />
                </div>
              </div>
            ))}
          </div>
          <div className="flex items-center gap-2">
            <NumField label="角度" value={gradient.angle} min={0} max={360} suffix="°" testId="gradient-angle" onCommit={(angle) => set({ ...(previous.current ?? gradient), angle })} />
            <div className="flex gap-0.5 rounded-[10px] bg-paper p-0.5">
              {[{ angle: 90, name: '从左到右' }, { angle: 180, name: '从上到下' }, { angle: 135, name: '从左上到右下' }].map((direction) => (
                <button key={direction.angle} type="button" title={direction.name} aria-label={direction.name} aria-pressed={gradient.angle === direction.angle}
                  className={`flex h-8 w-8 items-center justify-center rounded-lg transition-colors ${gradient.angle === direction.angle ? 'bg-card text-ink shadow-sm' : 'text-muted hover:text-ink'}`}
                  onClick={() => set({ ...(previous.current ?? gradient), angle: direction.angle })}>
                  <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" style={{ transform: `rotate(${direction.angle}deg)` }}>
                    <path d="M12 19V5m-5 5 5-5 5 5" />
                  </svg>
                </button>
              ))}
            </div>
          </div>
        </>
      ) : (
        <ColorField label="颜色" palette={palette} value={style.fill ?? '#000000'} testId="shape-fill" onCommit={(fill) => onChange({ fill })} />
      )}
    </div>
  )
}
