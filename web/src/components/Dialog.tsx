import { useEffect, type ReactNode } from 'react'

/**
 * A modal over the whole page. Escape and a click on the backdrop close it;
 * the editor's shortcuts stand down while one is open (the page checks
 * `data-dv-modal` before handling a key).
 */
export function Modal({
  onClose,
  children,
  width = 'w-96',
  label,
}: {
  onClose: () => void
  children: ReactNode
  width?: string
  label?: string
}) {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.stopPropagation()
        onClose()
      }
    }
    window.addEventListener('keydown', onKey, true)
    return () => window.removeEventListener('keydown', onKey, true)
  }, [onClose])

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-ink/25 p-4 backdrop-blur-[2px]"
      data-dv-modal
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) onClose()
      }}
    >
      <div
        role="dialog"
        aria-modal="true"
        aria-label={label}
        className={`${width} max-w-full rounded-[20px] bg-card shadow-[var(--shadow-pop)]`}
      >
        {children}
      </div>
    </div>
  )
}

/** True while any modal is open; keyboard handlers use it to stand down. */
export function modalOpen(): boolean {
  return !!document.querySelector('[data-dv-modal]')
}

export function ConfirmDialog({
  title,
  message,
  confirmLabel = '确定',
  danger,
  onConfirm,
  onCancel,
}: {
  title: string
  message?: ReactNode
  confirmLabel?: string
  danger?: boolean
  onConfirm: () => void
  onCancel: () => void
}) {
  return (
    <Modal onClose={onCancel} label={title}>
      <div className="p-6">
        <h2 className="font-serif text-lg font-black text-ink">{title}</h2>
        {message && <div className="mt-2 text-sm leading-relaxed text-muted">{message}</div>}
        <div className="mt-6 flex justify-end gap-2">
          <button className="btn-ghost" onClick={onCancel}>
            取消
          </button>
          <button autoFocus className={`btn-primary ${danger ? '' : 'bg-ink hover:bg-ink-2'}`} onClick={onConfirm}>
            {confirmLabel}
          </button>
        </div>
      </div>
    </Modal>
  )
}
