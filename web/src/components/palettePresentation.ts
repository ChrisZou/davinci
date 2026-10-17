import type { PaletteColors } from '../api'

/** Keep source/collection names out of the miniature cover. Count Unicode characters. */
export function paletteShortName(name: string) {
  let title = name.split('｜')[0].trim() || '配色'
  if ([...title].length > 14) title = title.split('×').slice(0, 2).map((part) => part.trim()).join(' × ')
  const chars = [...title]
  return chars.length > 14 ? `${chars.slice(0, 13).join('').trimEnd()}…` : title
}

function luminance(hex: string) {
  const values = (hex.match(/[a-f\d]{2}/gi) ?? ['00', '00', '00']).map((n) => {
    const v = parseInt(n, 16) / 255
    return v <= .04045 ? v / 12.92 : ((v + .055) / 1.055) ** 2.4
  })
  return .2126 * values[0] + .7152 * values[1] + .0722 * values[2]
}

/** Compare the highlight against the page, without changing any palette role. */
export function highlightStyle(palette: PaletteColors) {
  const a = luminance(palette.bg), b = luminance(palette.highlight)
  const contrast = (Math.max(a, b) + .05) / (Math.min(a, b) + .05)
  return contrast >= 3
    ? { color: palette.highlight, backgroundColor: 'transparent' }
    : { color: palette.text, backgroundColor: palette.highlight }
}
