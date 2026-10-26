import type { LinearGradient } from '../types'

/** Imported documents can bypass command validation. Ignore malformed gradients. */
export function readGradient(value: unknown): LinearGradient | null {
  const g = value as LinearGradient | undefined
  if (!g || g.type !== 'linear' || !Number.isFinite(g.angle) || !Array.isArray(g.stops) || g.stops.length !== 2) return null
  if (!g.stops.every((s) => s && typeof s.color === 'string' && s.color.trim() && Number.isFinite(s.opacity) && s.opacity >= 0 && s.opacity <= 1)) return null
  return g
}

/** CSS gradient endpoints in the shape's centred local coordinates, including non-square boxes. */
export function gradientLine(width: number, height: number, angle: number): [[number, number], [number, number]] {
  const radians = angle * Math.PI / 180
  const dx = Math.sin(radians)
  const dy = -Math.cos(radians)
  const half = (Math.abs(width * dx) + Math.abs(height * dy)) / 2
  return [[-dx * half, -dy * half], [dx * half, dy * half]]
}

/** CSS and Skia both interpolate premultiplied colours, avoiding dark transparent edges. */
export function gradientCSS(g: LinearGradient): string {
  const colors = g.stops.map((s) => `color-mix(in srgb, ${s.color} ${s.opacity * 100}%, transparent)`)
  return `linear-gradient(${g.angle}deg in srgb, ${colors.join(', ')})`
}
