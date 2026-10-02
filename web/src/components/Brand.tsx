import { navigate } from '../App'

/**
 * The davinci mark (the hand-drawn vermilion d) and the lockup with its serif
 * wordmark. The artwork lives in web/public, generated from the source in
 * docs/brand.
 */
export function Mark({ size = 28, className = '' }: { size?: number; className?: string }) {
  // The artwork is taller than it is wide (803×1040); size is its height.
  return <img src="/logo-mark.png" alt="" draggable={false} style={{ height: size, width: 'auto' }} className={className} />
}

/** Mark + wordmark, as a link home. */
export function Lockup({ size = 32 }: { size?: number }) {
  return (
    <a
      href="/"
      aria-label="davinci 首页"
      className="flex items-center gap-2 no-underline"
      onClick={(e) => {
        e.preventDefault()
        navigate('/')
      }}
    >
      <Mark size={size} />
      <span className="font-serif font-black tracking-tight text-ink" style={{ fontSize: Math.round(size * 0.72) }}>
        davinci
      </span>
    </a>
  )
}
