let context: CanvasRenderingContext2D | null | undefined

/** Canonical sRGB hex for deduplication; no colour/fully transparent is null. */
export function normalizeColor(value: string): string | null {
  let color = value.trim().toLowerCase()
  if (/^#(?:[\da-f]{3}|[\da-f]{4}|[\da-f]{6}|[\da-f]{8})$/.test(color)) {
    if (color.length === 4 || color.length === 5) color = '#' + [...color.slice(1)].map((c) => c + c).join('')
    if (color.length === 9) {
      if (color.endsWith('00')) return null
      if (color.endsWith('ff')) color = color.slice(0, 7)
    }
    return color
  }
  // Read RGB directly: drawing a translucent pixel and reading it back can
  // round 240 down to 239 during alpha premultiplication, breaking deduplication.
  const rgb = /^rgba?\(\s*(\d*\.?\d+)\s*,\s*(\d*\.?\d+)\s*,\s*(\d*\.?\d+)(?:\s*,\s*(\d*\.?\d+))?\s*\)$/.exec(color)
  if (rgb) {
    const channels = rgb.slice(1, 4).map(Number)
    const alpha = rgb[4] === undefined ? 1 : Number(rgb[4])
    if (channels.every((c) => c <= 255) && alpha <= 1) {
      const a = Math.round(alpha * 255)
      if (!a) return null
      const bytes = channels.map(Math.round)
      if (a !== 255) bytes.push(a)
      return '#' + bytes.map((v) => v.toString(16).padStart(2, '0')).join('')
    }
  }
  if (!color || /^(none|transparent|currentcolor|inherit|initial|unset|revert|revert-layer)$/.test(color) || /\b(var|env)\(/.test(color)) return null
  if (typeof document === 'undefined') return null
  context ??= document.createElement('canvas').getContext('2d', { willReadFrequently: true })
  if (!context) return null
  // Invalid CSS leaves fillStyle unchanged; use two sentinels to detect it.
  context.fillStyle = '#010203'
  context.fillStyle = color
  const parsed = context.fillStyle
  context.fillStyle = '#040506'
  context.fillStyle = color
  if (context.fillStyle !== parsed) return null
  if (parsed !== color && (parsed.startsWith('#') || parsed.startsWith('rgba('))) return normalizeColor(parsed)
  context.clearRect(0, 0, 1, 1)
  context.fillRect(0, 0, 1, 1)
  const [r, g, b, a] = context.getImageData(0, 0, 1, 1).data
  if (!a) return null
  return '#' + (a === 255 ? [r, g, b] : [r, g, b, a]).map((v) => v.toString(16).padStart(2, '0')).join('')
}
