/**
 * The document's compact style strings, as the panels read them.
 */

/** "colour:blur:offsetX:offsetY" → its parts. */
export function parseShadow(s?: string): { color: string; blur: number; offsetX: number; offsetY: number } | undefined {
  if (!s) return undefined
  const parts = String(s).split(':')
  const color = parts[0] || 'rgba(0,0,0,0.6)'
  const num = (i: number, d: number) => {
    const v = Number(parts[i])
    return Number.isFinite(v) ? v : d
  }
  return { color, blur: num(1, 12), offsetX: num(2, 0), offsetY: num(3, 0) }
}

/** "colour:width" → its parts (a bare colour is 1px wide). */
export function parseStroke(s?: string): { stroke?: string; strokeWidth?: number } {
  if (!s) return {}
  const i = String(s).lastIndexOf(':')
  if (i <= 0) return { stroke: String(s), strokeWidth: 1 }
  const w = Number(String(s).slice(i + 1))
  return { stroke: String(s).slice(0, i), strokeWidth: Number.isFinite(w) && w > 0 ? w : 1 }
}
